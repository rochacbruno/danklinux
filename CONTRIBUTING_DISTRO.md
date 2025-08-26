# Adding New Linux Distributions

This guide explains how to add support for new Linux distributions to the dankdots installer.

## Architecture Overview

The codebase uses a centralized registry system with detector/installer implementations:

- **Registry** (`internal/osinfo/detector.go`) - Central distro configuration
- **Detectors** (`internal/deps/`) - Check what packages are installed/missing  
- **Installers** (`internal/installer/`) - Handle package installation

## Adding Support

### Method 1: Use Existing Implementation (Derivatives)

For distros that use the same package manager as an existing distro, just add one line to the registry.

**Example: Adding CachyOS (Arch-based)**

```go
// internal/osinfo/detector.go
var AllSupportedDistros = []DistroInfo{
    {ID: "arch", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"},
    {ID: "cachyos", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"},
}
```

That's it! CachyOS now uses Arch's detection and installation logic.

### Method 2: Create New Implementation

For distros with different package management, create new implementations:

1. Create detector/installer files
2. Add to the registry
3. Update factory functions

#### Required Files

For a new distro called `mydistro`, create:

1. `internal/deps/mydistro.go` - Package detection logic
2. `internal/installer/mydistro.go` - Package installation logic

## New Detector Implementation

### Structure

```go
package deps

type MydistroDetector struct {
    *BaseDetector
}

func NewMydistroDetector(logChan chan<- string) *MydistroDetector {
    return &MydistroDetector{
        BaseDetector: NewBaseDetector(logChan),
    }
}
```

### Required Methods

```go
// Main detection entry point
func (d *MydistroDetector) DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error)

// Detection with terminal choice
func (d *MydistroDetector) DetectDependenciesWithTerminal(ctx context.Context, wm WindowManager, terminal Terminal) ([]Dependency, error)

// Package-specific detection methods
func (d *MydistroDetector) detectGit() Dependency
func (d *MydistroDetector) detectWindowManager(wm WindowManager) Dependency
func (d *MydistroDetector) detectQuickshell() Dependency
func (d *MydistroDetector) detectXDGPortal() Dependency
func (d *MydistroDetector) detectPolkitAgent() Dependency

// Distro-specific package check
func (d *MydistroDetector) packageInstalled(pkg string) bool
```

### Detection Pattern

Follow this order in `DetectDependenciesWithTerminal`:

1. DMS (shell) - `f.detectDMS()`
2. Terminal - `f.detectSpecificTerminal(terminal)`
3. Distro-specific packages (git, WM, quickshell, etc.)
4. Common packages - `f.detectMatugen()`, `f.detectDgop()`, fonts, clipboard tools

### Package Detection

Use distro-appropriate commands in `packageInstalled()`:

- **Arch**: `pacman -Q package`
- **Fedora**: `rpm -q package` 
- **Debian/Ubuntu**: `dpkg -s package`
- **openSUSE**: `rpm -q package`

## Installer Implementation

### Structure

```go
package installer

type MydistroInstaller struct {
    logChan chan<- string
}

func NewMydistroInstaller(logChan chan<- string) *MydistroInstaller {
    return &MydistroInstaller{logChan: logChan}
}
```

### Required Methods

```go
// Main installation method
func (i *MydistroInstaller) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error

// Package categorization
func (i *MydistroInstaller) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []string)

// Package mapping
func (i *MydistroInstaller) getPackageMap(wm deps.WindowManager) map[string]PackageInfo

// Post-install configuration
func (i *MydistroInstaller) postInstallConfig(ctx context.Context, wm deps.WindowManager, sudoPassword string, progressChan chan<- InstallProgressMsg) error
```

### Installation Phases

Follow this phase pattern:

1. **Prerequisites** (0.05-0.12) - Install build tools, enable repos
2. **System Packages** (0.35-0.60) - Standard repo packages
3. **Special Packages** (0.65-0.85) - AUR/COPR/PPA packages
4. **Configuration** (0.90-0.95) - Clone DMS config
5. **Complete** (1.0) - Finished

### Package Mapping

Create a map of dependency names to distro packages:

```go
func (i *MydistroInstaller) getPackageMap(wm deps.WindowManager) map[string]PackageInfo {
    packageMap := map[string]PackageInfo{
        "git":                     {"git", false},
        "quickshell":              {"quickshell-git", true}, // true = special repo
        "ghostty":                 {"ghostty", false},
        // ... more packages
    }
    
    // Add WM-specific packages
    switch wm {
    case deps.WindowManagerHyprland:
        packageMap["hyprland"] = PackageInfo{"hyprland", false}
    case deps.WindowManagerNiri:
        packageMap["niri"] = PackageInfo{"niri-git", true}
    }
    
    return packageMap
}
```

### Registry Integration

After creating implementations, add them to the system:

#### 1. Add to Registry

```go
// internal/osinfo/detector.go
var AllSupportedDistros = []DistroInfo{
    {ID: "arch", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"},
    {ID: "fedora", HexColorCode: "#0B57A4", DetectorType: "fedora", InstallerType: "fedora"},
    {ID: "mydistro", HexColorCode: "#FF0000", DetectorType: "mydistro", InstallerType: "mydistro"},
}
```

#### 2. Update Factory Functions

```go
// internal/deps/detector.go - Add to switch statement
switch distroInfo.DetectorType {
case "arch":
    return NewArchDetector(logChan), nil
case "fedora": 
    return NewFedoraDetector(logChan), nil
case "mydistro":
    return NewMydistroDetector(logChan), nil
```

```go
// internal/installer/interface.go - Add to switch statement  
switch distroInfo.InstallerType {
case "arch":
    return NewArchInstaller(logChan), nil
case "fedora":
    return NewFedoraInstaller(logChan), nil  
case "mydistro":
    return NewMydistroInstaller(logChan), nil
```

## Common Derivative Examples

### Arch-based Distros
```go
{ID: "cachyos", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"},
{ID: "endeavouros", HexColorCode: "#7F3FBF", DetectorType: "arch", InstallerType: "arch"},
{ID: "manjaro", HexColorCode: "#35BF5C", DetectorType: "arch", InstallerType: "arch"},
```

### Fedora-based Distros
```go
{ID: "nobara", HexColorCode: "#0B57A4", DetectorType: "fedora", InstallerType: "fedora"},
```

### Debian-based Distros  
```go
{ID: "ubuntu", HexColorCode: "#E95420", DetectorType: "debian", InstallerType: "debian"},
{ID: "mint", HexColorCode: "#87CF3E", DetectorType: "debian", InstallerType: "debian"},
```

## Testing

Test your implementation:

1. Package detection works correctly
2. Installation phases progress properly
3. Error handling for missing packages
4. Derivative distro inheritance works

## Examples

- **Arch**: Standard packages via pacman, AUR via manual build
- **Fedora**: Standard packages via dnf, COPR repos for extras
- **Debian**: Standard packages via apt, PPAs for extras

Look at existing implementations in `internal/deps/arch.go` and `internal/installer/fedora.go` for reference patterns.