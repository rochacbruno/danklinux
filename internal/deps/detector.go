package deps

import (
	"context"
	"fmt"

	"github.com/AvengeMedia/dankinstall/internal/osinfo"
)

type DependencyStatus int

const (
	StatusMissing DependencyStatus = iota
	StatusInstalled
	StatusNeedsUpdate
	StatusNeedsReinstall
)

type Dependency struct {
	Name        string
	Status      DependencyStatus
	Version     string
	Description string
	Required    bool
}

type WindowManager int

const (
	WindowManagerHyprland WindowManager = iota
	WindowManagerNiri
)

type Terminal int

const (
	TerminalGhostty Terminal = iota
	TerminalKitty
)

type DependencyDetector interface {
	DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error)
	DetectDependenciesWithTerminal(ctx context.Context, wm WindowManager, terminal Terminal) ([]Dependency, error)
}

func NewDependencyDetector(distribution string, logChan chan<- string) (DependencyDetector, error) {
	distroInfo, err := osinfo.GetDistroInfo(distribution)
	if err != nil {
		return nil, err
	}

	switch distroInfo.DetectorType {
	case "arch":
		return NewArchDetector(logChan), nil
	case "fedora":
		return NewFedoraDetector(logChan), nil
	default:
		return nil, fmt.Errorf("unsupported detector type: %s", distroInfo.DetectorType)
	}
}