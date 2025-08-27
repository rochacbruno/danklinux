package distros

import (
	"github.com/AvengeMedia/dankinstall/internal/deps"
	"github.com/AvengeMedia/dankinstall/internal/installer"
)

// NewDependencyDetector creates a DependencyDetector for the specified distribution
func NewDependencyDetector(distribution string, logChan chan<- string) (deps.DependencyDetector, error) {
	distro, err := NewDistribution(distribution, logChan)
	if err != nil {
		return nil, err
	}
	return distro, nil
}

// NewPackageInstaller creates a PackageInstaller for the specified distribution
func NewPackageInstaller(distribution string, logChan chan<- string) (installer.PackageInstaller, error) {
	distro, err := NewDistribution(distribution, logChan)
	if err != nil {
		return nil, err
	}
	return distro, nil
}
