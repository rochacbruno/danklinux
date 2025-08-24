package pkgmanager

import (
	"context"
	"fmt"
)

type ProgressFunc func(packageName string, progress float64, step string, isComplete bool)

type PackageManager interface {
	InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error
}

func NewPackageManager(distribution string, logChan chan<- string) (PackageManager, error) {
	switch distribution {
	case "arch":
		return NewPacmanInstaller(logChan), nil
	case "fedora":
		return NewDNFInstaller(logChan), nil
	default:
		return nil, fmt.Errorf("unsupported distribution: %s", distribution)
	}
}