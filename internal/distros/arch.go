package distros

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

func init() {
	Register("arch", "#1793D1", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewArchDistribution(config, logChan)
	})
	Register("cachyos", "#08A283", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewArchDistribution(config, logChan)
	})
	Register("endeavouros", "#7F3FBF", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewArchDistribution(config, logChan)
	})
	Register("manjaro", "#35BF5C", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewArchDistribution(config, logChan)
	})
}

type ArchDistribution struct {
	*BaseDistribution
	*ManualPackageInstaller
	config DistroConfig
}

func NewArchDistribution(config DistroConfig, logChan chan<- string) *ArchDistribution {
	base := NewBaseDistribution(logChan)
	return &ArchDistribution{
		BaseDistribution:       base,
		ManualPackageInstaller: &ManualPackageInstaller{BaseDistribution: base},
		config:                 config,
	}
}

func (a *ArchDistribution) GetID() string {
	return a.config.ID
}

func (a *ArchDistribution) GetColorHex() string {
	return a.config.ColorHex
}

func (a *ArchDistribution) GetPackageManager() PackageManagerType {
	return PackageManagerPacman
}

func (a *ArchDistribution) DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error) {
	return a.DetectDependenciesWithTerminal(ctx, wm, deps.TerminalGhostty)
}

func (a *ArchDistribution) DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error) {
	var dependencies []deps.Dependency

	// DMS at the top (shell is prominent)
	dependencies = append(dependencies, a.detectDMS())

	// Terminal with choice support
	dependencies = append(dependencies, a.detectSpecificTerminal(terminal))

	// Common detections using base methods
	dependencies = append(dependencies, a.detectGit())
	dependencies = append(dependencies, a.detectWindowManager(wm))
	dependencies = append(dependencies, a.detectQuickshell())
	dependencies = append(dependencies, a.detectXDGPortal())
	dependencies = append(dependencies, a.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == deps.WindowManagerHyprland {
		dependencies = append(dependencies, a.detectHyprlandTools()...)
	}

	// Niri-specific tools
	if wm == deps.WindowManagerNiri {
		dependencies = append(dependencies, a.detectXwaylandSatellite())
	}

	// Base detections (common across distros)
	dependencies = append(dependencies, a.detectMatugen())
	dependencies = append(dependencies, a.detectDgop())
	dependencies = append(dependencies, a.detectFonts()...)
	dependencies = append(dependencies, a.detectClipboardTools()...)

	return dependencies, nil
}

func (a *ArchDistribution) detectXDGPortal() deps.Dependency {
	status := deps.StatusMissing
	if a.packageInstalled("xdg-desktop-portal-gtk") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (a *ArchDistribution) detectPolkitAgent() deps.Dependency {
	status := deps.StatusMissing
	if a.packageInstalled("mate-polkit") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "mate-polkit",
		Status:      status,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (a *ArchDistribution) packageInstalled(pkg string) bool {
	cmd := exec.Command("pacman", "-Q", pkg)
	err := cmd.Run()
	return err == nil
}

func (a *ArchDistribution) GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping {
	return a.GetPackageMappingWithVariants(wm, make(map[string]deps.PackageVariant))
}

func (a *ArchDistribution) GetPackageMappingWithVariants(wm deps.WindowManager, variants map[string]deps.PackageVariant) map[string]PackageMapping {
	packages := map[string]PackageMapping{
		"dms (DankMaterialShell)": {Name: "dms-shell-git", Repository: RepoTypeAUR},
		"git":                     {Name: "git", Repository: RepoTypeSystem},
		"quickshell":              a.getQuickshellMapping(variants["quickshell"]),
		"matugen":                 {Name: "matugen-bin", Repository: RepoTypeAUR},
		"dgop":                    {Name: "dgop", Repository: RepoTypeAUR},
		"ghostty":                 {Name: "ghostty", Repository: RepoTypeSystem},
		"alacritty":               {Name: "alacritty", Repository: RepoTypeSystem},
		"cliphist":                {Name: "cliphist", Repository: RepoTypeSystem},
		"wl-clipboard":            {Name: "wl-clipboard", Repository: RepoTypeSystem},
		"xdg-desktop-portal-gtk":  {Name: "xdg-desktop-portal-gtk", Repository: RepoTypeSystem},
		"mate-polkit":             {Name: "mate-polkit", Repository: RepoTypeSystem},
		"font-material-symbols":   {Name: "material-symbols-git", Repository: RepoTypeAUR},
		"font-firacode":           {Name: "ttf-fira-code", Repository: RepoTypeSystem},
		"font-inter":              {Name: "inter-font", Repository: RepoTypeSystem},
	}

	switch wm {
	case deps.WindowManagerHyprland:
		packages["hyprland"] = a.getHyprlandMapping(variants["hyprland"])
		packages["grim"] = PackageMapping{Name: "grim", Repository: RepoTypeSystem}
		packages["slurp"] = PackageMapping{Name: "slurp", Repository: RepoTypeSystem}
		packages["hyprctl"] = a.getHyprlandMapping(variants["hyprland"])
		packages["hyprpicker"] = PackageMapping{Name: "hyprpicker", Repository: RepoTypeSystem}
		packages["grimblast"] = PackageMapping{Name: "grimblast", Repository: RepoTypeManual, BuildFunc: "installGrimblast"}
		packages["jq"] = PackageMapping{Name: "jq", Repository: RepoTypeSystem}
	case deps.WindowManagerNiri:
		packages["niri"] = a.getNiriMapping(variants["niri"])
		packages["xwayland-satellite"] = PackageMapping{Name: "xwayland-satellite", Repository: RepoTypeSystem}
	}

	return packages
}

func (a *ArchDistribution) getQuickshellMapping(variant deps.PackageVariant) PackageMapping {
	if variant == deps.VariantGit {
		return PackageMapping{Name: "quickshell-git", Repository: RepoTypeAUR}
	}
	return PackageMapping{Name: "quickshell", Repository: RepoTypeAUR}
}

func (a *ArchDistribution) getHyprlandMapping(variant deps.PackageVariant) PackageMapping {
	if variant == deps.VariantGit {
		return PackageMapping{Name: "hyprland-git", Repository: RepoTypeAUR}
	}
	return PackageMapping{Name: "hyprland", Repository: RepoTypeSystem}
}

func (a *ArchDistribution) getNiriMapping(variant deps.PackageVariant) PackageMapping {
	if variant == deps.VariantGit {
		return PackageMapping{Name: "niri-git", Repository: RepoTypeAUR}
	}
	return PackageMapping{Name: "niri", Repository: RepoTypeSystem}
}

func (a *ArchDistribution) detectXwaylandSatellite() deps.Dependency {
	status := deps.StatusMissing
	if a.commandExists("xwayland-satellite") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xwayland-satellite",
		Status:      status,
		Description: "Xwayland support",
		Required:    true,
	}
}

func (a *ArchDistribution) InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.06,
		Step:       "Checking base-devel...",
		IsComplete: false,
		LogOutput:  "Checking if base-devel is installed",
	}

	checkCmd := exec.CommandContext(ctx, "pacman", "-Qq", "base-devel")
	if err := checkCmd.Run(); err == nil {
		a.log("base-devel already installed")
		progressChan <- InstallProgressMsg{
			Phase:      PhasePrerequisites,
			Progress:   0.10,
			Step:       "base-devel already installed",
			IsComplete: false,
			LogOutput:  "base-devel is already installed on the system",
		}
		return nil
	}

	a.log("Installing base-devel...")
	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
		Progress:    0.08,
		Step:        "Installing base-devel...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo pacman -S --needed --noconfirm base-devel",
		LogOutput:   "Installing base-devel development tools",
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S pacman -S --needed --noconfirm base-devel", sudoPassword))
	if err := a.runWithProgress(cmd, progressChan, PhasePrerequisites, 0.08, 0.10); err != nil {
		return fmt.Errorf("failed to install base-devel: %w", err)
	}

	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.12,
		Step:       "base-devel installation complete",
		IsComplete: false,
		LogOutput:  "base-devel successfully installed",
	}

	return nil
}

func (a *ArchDistribution) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	if err := a.InstallPrerequisites(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	systemPkgs, aurPkgs, manualPkgs := a.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 3: System Packages
	if len(systemPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
			Progress:   0.35,
			Step:       fmt.Sprintf("Installing %d system packages...", len(systemPkgs)),
			IsComplete: false,
			NeedsSudo:  true,
			LogOutput:  fmt.Sprintf("Installing system packages: %s", strings.Join(systemPkgs, ", ")),
		}
		if err := a.installSystemPackages(ctx, systemPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install system packages: %w", err)
		}
	}

	// Phase 4: AUR Packages
	if len(aurPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseAURPackages,
			Progress:   0.65,
			Step:       fmt.Sprintf("Installing %d AUR packages...", len(aurPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing AUR packages: %s", strings.Join(aurPkgs, ", ")),
		}
		if err := a.installAURPackages(ctx, aurPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install AUR packages: %w", err)
		}
	}

	// Phase 5: Manual Builds
	if len(manualPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
			Progress:   0.85,
			Step:       fmt.Sprintf("Building %d packages from source...", len(manualPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Building from source: %s", strings.Join(manualPkgs, ", ")),
		}
		if err := a.InstallManualPackages(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install manual packages: %w", err)
		}
	}

	// Phase 6: Configuration
	progressChan <- InstallProgressMsg{
		Phase:      PhaseConfiguration,
		Progress:   0.90,
		Step:       "Configuring system...",
		IsComplete: false,
		LogOutput:  "Starting post-installation configuration...",
	}
	if err := a.postInstallConfig(ctx, wm, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to configure system: %w", err)
	}

	// Phase 7: Complete
	progressChan <- InstallProgressMsg{
		Phase:      PhaseComplete,
		Progress:   1.0,
		Step:       "Installation complete!",
		IsComplete: true,
		LogOutput:  "All packages installed and configured successfully",
	}

	return nil
}

func (a *ArchDistribution) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []string, []string) {
	systemPkgs := []string{}
	aurPkgs := []string{}
	manualPkgs := []string{}

	variantMap := make(map[string]deps.PackageVariant)
	for _, dep := range dependencies {
		variantMap[dep.Name] = dep.Variant
	}

	packageMap := a.GetPackageMappingWithVariants(wm, variantMap)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			// If no mapping exists, treat as manual build
			manualPkgs = append(manualPkgs, dep.Name)
			continue
		}

		switch pkgInfo.Repository {
		case RepoTypeAUR:
			aurPkgs = append(aurPkgs, pkgInfo.Name)
		case RepoTypeSystem:
			systemPkgs = append(systemPkgs, pkgInfo.Name)
		case RepoTypeManual:
			manualPkgs = append(manualPkgs, dep.Name)
		}
	}

	return systemPkgs, aurPkgs, manualPkgs
}

func (a *ArchDistribution) installSystemPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	a.log(fmt.Sprintf("Installing system packages: %s", strings.Join(packages, ", ")))

	args := []string{"pacman", "-S", "--needed", "--noconfirm"}
	args = append(args, packages...)

	progressChan <- InstallProgressMsg{
		Phase:       PhaseSystemPackages,
		Progress:    0.40,
		Step:        "Installing system packages...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo %s", strings.Join(args, " ")),
	}

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return a.runWithProgress(cmd, progressChan, PhaseSystemPackages, 0.40, 0.60)
}

func (a *ArchDistribution) installAURPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	a.log(fmt.Sprintf("Installing AUR packages manually: %s", strings.Join(packages, ", ")))

	hasNiri := false
	hasQuickshell := false
	for _, pkg := range packages {
		if pkg == "niri-git" {
			hasNiri = true
		}
		if pkg == "quickshell" || pkg == "quickshell-git" {
			hasQuickshell = true
		}
	}

	// If quickshell is in the list, install google-breakpad first if not already installed
	if hasQuickshell {
		if !a.packageInstalled("google-breakpad") {
			progressChan <- InstallProgressMsg{
				Phase:       PhaseAURPackages,
				Progress:    0.63,
				Step:        "Installing google-breakpad for quickshell...",
				IsComplete:  false,
				CommandInfo: "Installing prerequisite AUR package for quickshell",
			}

			if err := a.installSingleAURPackage(ctx, "google-breakpad", sudoPassword, progressChan, 0.63, 0.65); err != nil {
				return fmt.Errorf("failed to install google-breakpad prerequisite for quickshell: %w", err)
			}
		}
	}

	// If niri is in the list, install makepkg-git-lfs-proto first if not already installed
	if hasNiri {
		if !a.packageInstalled("makepkg-git-lfs-proto") {
			progressChan <- InstallProgressMsg{
				Phase:       PhaseAURPackages,
				Progress:    0.65,
				Step:        "Installing makepkg-git-lfs-proto for niri...",
				IsComplete:  false,
				CommandInfo: "Installing prerequisite for niri-git",
			}

			if err := a.installSingleAURPackage(ctx, "makepkg-git-lfs-proto", sudoPassword, progressChan, 0.65, 0.67); err != nil {
				return fmt.Errorf("failed to install makepkg-git-lfs-proto prerequisite for niri: %w", err)
			}
		}
	}

	// Reorder packages to ensure dms-shell-git dependencies are installed first
	orderedPackages := a.reorderAURPackages(packages)

	baseProgress := 0.67
	progressStep := 0.13 / float64(len(orderedPackages))

	for i, pkg := range orderedPackages {
		currentProgress := baseProgress + (float64(i) * progressStep)

		progressChan <- InstallProgressMsg{
			Phase:       PhaseAURPackages,
			Progress:    currentProgress,
			Step:        fmt.Sprintf("Installing AUR package %s (%d/%d)...", pkg, i+1, len(packages)),
			IsComplete:  false,
			CommandInfo: fmt.Sprintf("Building and installing %s", pkg),
		}

		if err := a.installSingleAURPackage(ctx, pkg, sudoPassword, progressChan, currentProgress, currentProgress+progressStep); err != nil {
			return fmt.Errorf("failed to install AUR package %s: %w", pkg, err)
		}
	}

	progressChan <- InstallProgressMsg{
		Phase:      PhaseAURPackages,
		Progress:   0.80,
		Step:       "All AUR packages installed successfully",
		IsComplete: false,
		LogOutput:  fmt.Sprintf("Successfully installed AUR packages: %s", strings.Join(packages, ", ")),
	}

	return nil
}

func (a *ArchDistribution) reorderAURPackages(packages []string) []string {
	// Dependencies for dms-shell-git that need to be installed first
	dmsDepencies := []string{"quickshell", "quickshell-git", "dgop", "ttf-material-symbols-variable-git"}

	var deps []string
	var others []string
	var dmsShell []string

	for _, pkg := range packages {
		if pkg == "dms-shell-git" {
			dmsShell = append(dmsShell, pkg)
		} else {
			isDep := false
			for _, dep := range dmsDepencies {
				if pkg == dep {
					deps = append(deps, pkg)
					isDep = true
					break
				}
			}
			if !isDep {
				others = append(others, pkg)
			}
		}
	}

	// Order: dependencies first, then other packages, then dms-shell-git last
	result := append(deps, others...)
	result = append(result, dmsShell...)
	return result
}

func (a *ArchDistribution) installSingleAURPackage(ctx context.Context, pkg, sudoPassword string, progressChan chan<- InstallProgressMsg, startProgress, endProgress float64) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	buildDir := filepath.Join(homeDir, ".cache", "dankinstall", "aur-builds", pkg)
	
	// Clean up any existing cache first
	if err := os.RemoveAll(buildDir); err != nil {
		a.log(fmt.Sprintf("Warning: failed to clean existing cache for %s: %v", pkg, err))
	}
	
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(buildDir); removeErr != nil {
			a.log(fmt.Sprintf("Warning: failed to cleanup build directory %s: %v", buildDir, removeErr))
		}
	}()

	// Clone the AUR package
	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.1*(endProgress-startProgress),
		Step:        fmt.Sprintf("Cloning %s from AUR...", pkg),
		IsComplete:  false,
		CommandInfo: fmt.Sprintf("git clone https://aur.archlinux.org/%s.git", pkg),
	}

	cloneCmd := exec.CommandContext(ctx, "git", "clone", fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg), filepath.Join(buildDir, pkg))
	if err := a.runWithProgress(cloneCmd, progressChan, PhaseAURPackages, startProgress+0.1*(endProgress-startProgress), startProgress+0.2*(endProgress-startProgress)); err != nil {
		return fmt.Errorf("failed to clone %s: %w", pkg, err)
	}

	packageDir := filepath.Join(buildDir, pkg)

	if pkg == "niri-git" {
		pkgbuildPath := filepath.Join(packageDir, "PKGBUILD")
		sedCmd := exec.CommandContext(ctx, "sed", "-i", "s/makepkg-git-lfs-proto//g", pkgbuildPath)
		if err := sedCmd.Run(); err != nil {
			return fmt.Errorf("failed to patch PKGBUILD for niri-git: %w", err)
		}

		srcinfoPath := filepath.Join(packageDir, ".SRCINFO")
		sedCmd2 := exec.CommandContext(ctx, "sed", "-i", "/makedepends = makepkg-git-lfs-proto/d", srcinfoPath)
		if err := sedCmd2.Run(); err != nil {
			return fmt.Errorf("failed to patch .SRCINFO for niri-git: %w", err)
		}
	}

	// Remove problematic dependencies for dms-shell-git
	if pkg == "dms-shell-git" {
		srcinfoPath := filepath.Join(packageDir, ".SRCINFO")
		depsToRemove := []string{
			"depends = quickshell",
			"depends = dgop",
			"depends = ttf-material-symbols-variable-git",
		}

		for _, dep := range depsToRemove {
			sedCmd := exec.CommandContext(ctx, "sed", "-i", fmt.Sprintf("/%s/d", dep), srcinfoPath)
			if err := sedCmd.Run(); err != nil {
				return fmt.Errorf("failed to remove dependency %s from .SRCINFO for dms-shell-git: %w", dep, err)
			}
		}
	}

	// Remove all optdepends from .SRCINFO for all packages
	srcinfoPath := filepath.Join(packageDir, ".SRCINFO")
	optdepsCmd := exec.CommandContext(ctx, "sed", "-i", "/^[[:space:]]*optdepends = /d", srcinfoPath)
	if err := optdepsCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove optdepends from .SRCINFO for %s: %w", pkg, err)
	}

	// Pre-install dependencies from .SRCINFO
	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.3*(endProgress-startProgress),
		Step:        fmt.Sprintf("Installing dependencies for %s...", pkg),
		IsComplete:  false,
		CommandInfo: "Installing package dependencies and makedepends",
	}

	// Install dependencies and makedepends explicitly
	srcinfoPath = filepath.Join(packageDir, ".SRCINFO")

	depsCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf(`
			deps=$(grep "depends = " "%s" | grep -v "makedepends" | sed 's/.*depends = //' | tr '\n' ' ' | sed 's/[[:space:]]*$//')
			if [[ "%s" == *"quickshell"* ]]; then
				deps=$(echo "$deps" | sed 's/google-breakpad//g' | sed 's/  / /g' | sed 's/^ *//g' | sed 's/ *$//g')
			fi
			if [ ! -z "$deps" ] && [ "$deps" != " " ]; then
				echo '%s' | sudo -S pacman -S --needed --noconfirm $deps
			fi
		`, srcinfoPath, pkg, sudoPassword))

	if err := a.runWithProgress(depsCmd, progressChan, PhaseAURPackages, startProgress+0.3*(endProgress-startProgress), startProgress+0.35*(endProgress-startProgress)); err != nil {
		return fmt.Errorf("FAILED to install runtime dependencies for %s: %w", pkg, err)
	}

	makedepsCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf(`
			makedeps=$(grep -E "^[[:space:]]*makedepends = " "%s" | sed 's/^[[:space:]]*makedepends = //' | tr '\n' ' ')
			if [ ! -z "$makedeps" ]; then
				echo '%s' | sudo -S pacman -S --needed --noconfirm $makedeps
			fi
		`, srcinfoPath, sudoPassword))

	if err := a.runWithProgress(makedepsCmd, progressChan, PhaseAURPackages, startProgress+0.35*(endProgress-startProgress), startProgress+0.4*(endProgress-startProgress)); err != nil {
		return fmt.Errorf("FAILED to install make dependencies for %s: %w", pkg, err)
	}

	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.4*(endProgress-startProgress),
		Step:        fmt.Sprintf("Building %s...", pkg),
		IsComplete:  false,
		CommandInfo: "makepkg --noconfirm",
	}

	buildCmd := exec.CommandContext(ctx, "makepkg", "--noconfirm")
	buildCmd.Dir = packageDir
	buildCmd.Env = append(os.Environ(), "PKGEXT=.pkg.tar") // Disable compression for speed

	if err := a.runWithProgress(buildCmd, progressChan, PhaseAURPackages, startProgress+0.4*(endProgress-startProgress), startProgress+0.7*(endProgress-startProgress)); err != nil {
		return fmt.Errorf("failed to build %s: %w", pkg, err)
	}

	// Find built package file
	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.7*(endProgress-startProgress),
		Step:        fmt.Sprintf("Installing %s...", pkg),
		IsComplete:  false,
		CommandInfo: "sudo pacman -U built-package",
	}

	// Find .pkg.tar* files
	files, err := filepath.Glob(filepath.Join(packageDir, "*.pkg.tar*"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("no package files found after building %s", pkg)
	}

	installArgs := []string{"pacman", "-U", "--noconfirm"}
	installArgs = append(installArgs, files...)

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(installArgs, " "))
	installCmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)

	if err := a.runWithProgress(installCmd, progressChan, PhaseAURPackages, startProgress+0.7*(endProgress-startProgress), endProgress); err != nil {
		return fmt.Errorf("failed to install built package %s: %w", pkg, err)
	}

	a.log(fmt.Sprintf("Successfully installed AUR package: %s", pkg))
	return nil
}
