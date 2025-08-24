package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type PacmanInstaller struct {
	logChan chan<- string
}

func NewPacmanInstaller(logChan chan<- string) *PacmanInstaller {
	return &PacmanInstaller{
		logChan: logChan,
	}
}

func (p *PacmanInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	p.logChan <- fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", "))
	
	args := append([]string{"-S", "--noconfirm"}, packages...)
	cmd := exec.CommandContext(ctx, "pacman", args...)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		p.logChan <- fmt.Sprintf("Package installation failed: %s", string(output))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	p.logChan <- "Package installation completed successfully"
	progressFunc("all", 1.0, "Installation complete", true)
	
	return nil
}