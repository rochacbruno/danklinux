package installer

import (
	"context"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

type FedoraInstaller struct {
	logChan chan<- string
}

func NewFedoraInstaller(logChan chan<- string) *FedoraInstaller {
	return &FedoraInstaller{
		logChan: logChan,
	}
}

func (f *FedoraInstaller) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error {
	// TODO: Implement Fedora installation logic
	progressChan <- InstallProgressMsg{
		Phase:      PhaseComplete,
		Progress:   1.0,
		Step:       "Fedora installation not yet implemented",
		IsComplete: true,
	}
	return nil
}

func (f *FedoraInstaller) HasAURHelper() (string, bool) {
	// Fedora doesn't use AUR
	return "dnf", true
}

func (f *FedoraInstaller) InstallAURHelper(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// No AUR helper needed for Fedora
	return nil
}

func (f *FedoraInstaller) log(message string) {
	if f.logChan != nil {
		f.logChan <- message
	}
}