package distros

import (
	"context"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

// PackageManagerType defines the package manager a distro uses
type PackageManagerType string

const (
	PackageManagerPacman PackageManagerType = "pacman"
	PackageManagerDNF    PackageManagerType = "dnf"
	PackageManagerAPT    PackageManagerType = "apt"
	PackageManagerZypper PackageManagerType = "zypper"
	PackageManagerNix    PackageManagerType = "nix"
)

// RepositoryType defines the type of repository for a package
type RepositoryType string

const (
	RepoTypeSystem RepositoryType = "system" // Standard system repo (pacman, dnf, apt)
	RepoTypeAUR    RepositoryType = "aur"    // Arch User Repository
	RepoTypeCOPR   RepositoryType = "copr"   // Fedora COPR
	RepoTypePPA    RepositoryType = "ppa"    // Ubuntu PPA
	RepoTypeFlake  RepositoryType = "flake"  // Nix flake
	RepoTypeManual RepositoryType = "manual" // Manual build from source
)

// InstallPhase represents the current phase of installation
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

// InstallProgressMsg represents progress during package installation
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

// PackageMapping defines how to install a package on a specific distro
type PackageMapping struct {
	Name       string         // Package name to install
	Repository RepositoryType // Repository type
	RepoURL    string         // Repository URL if needed (e.g., COPR repo, PPA)
	BuildFunc  string         // Name of manual build function if RepoTypeManual
}

// Distribution defines a Linux distribution with all its specific configurations
type Distribution interface {
	// Metadata
	GetID() string
	GetColorHex() string
	GetPackageManager() PackageManagerType

	// Dependency Detection
	DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error)
	DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error)

	// Package Installation
	InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error

	// Package Mapping
	GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping

	// Prerequisites
	InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error
}

// DistroConfig holds configuration for a distribution
type DistroConfig struct {
	ID          string
	ColorHex    string
	Constructor func(config DistroConfig, logChan chan<- string) Distribution
}

// Registry holds all supported distributions
var Registry = make(map[string]DistroConfig)

// Register adds a distribution to the registry
func Register(id, colorHex string, constructor func(config DistroConfig, logChan chan<- string) Distribution) {
	Registry[id] = DistroConfig{
		ID:          id,
		ColorHex:    colorHex,
		Constructor: constructor,
	}
}

// NewDistribution creates a distribution instance by ID
func NewDistribution(id string, logChan chan<- string) (Distribution, error) {
	config, exists := Registry[id]
	if !exists {
		return nil, &UnsupportedDistributionError{ID: id}
	}
	return config.Constructor(config, logChan), nil
}

// UnsupportedDistributionError is returned when a distribution is not supported
type UnsupportedDistributionError struct {
	ID string
}

func (e *UnsupportedDistributionError) Error() string {
	return "unsupported distribution: " + e.ID
}
