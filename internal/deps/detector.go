package deps

import (
	"context"
	"fmt"
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

type DependencyDetector interface {
	DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error)
}

func NewDependencyDetector(distribution string, logChan chan<- string) (DependencyDetector, error) {
	switch distribution {
	case "arch":
		return NewArchDetector(logChan), nil
	case "fedora":
		return NewFedoraDetector(logChan), nil
	default:
		return nil, fmt.Errorf("unsupported distribution: %s", distribution)
	}
}