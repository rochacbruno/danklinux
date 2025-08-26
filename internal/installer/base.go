package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type BaseInstaller struct {
	logChan chan<- string
}

func NewBaseInstaller(logChan chan<- string) *BaseInstaller {
	return &BaseInstaller{
		logChan: logChan,
	}
}

// InstallManualPackages handles packages that need manual building
func (b *BaseInstaller) InstallManualPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	b.log(fmt.Sprintf("Installing manual packages: %s", strings.Join(packages, ", ")))

	for _, pkg := range packages {
		switch pkg {
		case "dgop":
			if err := b.installDgop(ctx, sudoPassword, progressChan); err != nil {
				return fmt.Errorf("failed to install dgop: %w", err)
			}
		default:
			b.log(fmt.Sprintf("Warning: No manual build method for %s", pkg))
		}
	}

	return nil
}

// Manual build installations - can be overridden by distros that have packages
func (b *BaseInstaller) installDgop(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	b.log("Installing dgop from source...")

	// Create temporary directory
	tmpDir := "/tmp/dgop-build"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone repository
	progressChan <- InstallProgressMsg{
		Phase:       PhaseSystemPackages,
		Progress:    0.1,
		Step:        "Cloning dgop repository...",
		IsComplete:  false,
		CommandInfo: "git clone https://github.com/AvengeMedia/dgop.git",
	}

	cloneCmd := exec.CommandContext(ctx, "git", "clone", "https://github.com/AvengeMedia/dgop.git", tmpDir)
	if err := cloneCmd.Run(); err != nil {
		b.logError("failed to clone dgop repository", err)
		return fmt.Errorf("failed to clone dgop repository: %w", err)
	}

	// Build
	progressChan <- InstallProgressMsg{
		Phase:       PhaseSystemPackages,
		Progress:    0.4,
		Step:        "Building dgop...",
		IsComplete:  false,
		CommandInfo: "make",
	}

	buildCmd := exec.CommandContext(ctx, "make")
	buildCmd.Dir = tmpDir
	if err := buildCmd.Run(); err != nil {
		b.logError("failed to build dgop", err)
		return fmt.Errorf("failed to build dgop: %w", err)
	}

	// Install
	progressChan <- InstallProgressMsg{
		Phase:       PhaseSystemPackages,
		Progress:    0.7,
		Step:        "Installing dgop...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo make install",
	}

	installCmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S make install", sudoPassword))
	installCmd.Dir = tmpDir
	if err := installCmd.Run(); err != nil {
		b.logError("failed to install dgop", err)
		return fmt.Errorf("failed to install dgop: %w", err)
	}

	b.log("dgop installed successfully from source")
	return nil
}

// Base package map - can be extended by specific distros
func (b *BaseInstaller) getBasePackageMap() map[string]string {
	return map[string]string{
		"dgop": "manual", // Indicates manual build required
	}
}

func (b *BaseInstaller) log(message string) {
	if b.logChan != nil {
		b.logChan <- message
	}
}

func (b *BaseInstaller) logError(message string, err error) {
	errorMsg := fmt.Sprintf("ERROR: %s: %v", message, err)
	b.log(errorMsg)
}
