package installer

import (
	"context"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

type InstallPhase int

const (
	PhasePrerequisites InstallPhase = iota
	PhaseAURHelper
	PhaseSystemPackages
	PhaseAURPackages
	PhaseCursorTheme
	PhaseConfiguration
	PhaseComplete
)

type InstallProgressMsg struct {
	Phase       InstallPhase
	Progress    float64
	Step        string
	IsComplete  bool
	NeedsSudo   bool
	CommandInfo string
	LogOutput   string
	Error       error
}

type PackageInstaller interface {
	InstallPackages(ctx context.Context, deps []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error
}