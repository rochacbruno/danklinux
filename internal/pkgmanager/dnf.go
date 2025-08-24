package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type DNFInstaller struct {
	logChan chan<- string
}

func NewDNFInstaller(logChan chan<- string) *DNFInstaller {
	return &DNFInstaller{
		logChan: logChan,
	}
}

func (d *DNFInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	if progressFunc != nil {
		progressFunc("", 0.05, "Initializing DNF package installation...", false)
	}

	d.log("Updating DNF package lists...")
	if progressFunc != nil {
		progressFunc("", 0.10, "Updating DNF package lists...", false)
	}

	updateCmd := exec.CommandContext(ctx, "dnf", "makecache", "--refresh", "-y")
	if output, err := updateCmd.CombinedOutput(); err != nil {
		d.log(fmt.Sprintf("Error updating dnf: %s", string(output)))
		return fmt.Errorf("failed to update dnf: %w", err)
	}

	if progressFunc != nil {
		progressFunc("", 0.25, "Installing packages...", false)
	}
	d.log(fmt.Sprintf("Installing %d packages...", len(packages)))

	progressDone := make(chan struct{})
	go func() {
		currentProgress := 0.30
		for currentProgress < 0.80 {
			select {
			case <-progressDone:
				return
			case <-ctx.Done():
				return
			default:
				currentProgress += 0.03
				if progressFunc != nil {
					progressFunc("", currentProgress, fmt.Sprintf("Installing %d packages...", len(packages)), false)
				}
				select {
				case <-progressDone:
					return
				case <-ctx.Done():
					return
				default:
					time.Sleep(300 * time.Millisecond)
				}
			}
		}
	}()

	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.CommandContext(ctx, "dnf", args...)
	output, err := installCmd.CombinedOutput()
	close(progressDone)

	if err != nil {
		d.log(fmt.Sprintf("Error installing packages: %s", string(output)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	if progressFunc != nil {
		progressFunc("", 1.0, "Installation complete", true)
	}
	d.log("Packages installed successfully")

	return nil
}

func (d *DNFInstaller) log(message string) {
	if d.logChan != nil {
		d.logChan <- message
	}
}