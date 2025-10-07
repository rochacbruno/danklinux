package distros

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/AvengeMedia/danklinux/internal/deps"
)

func init() {
	Register("opensuse-tumbleweed", "#73BA25", FamilySUSE, func(config DistroConfig, logChan chan<- string) Distribution {
		return NewOpenSUSEDistribution(config, logChan)
	})
}

type OpenSUSEDistribution struct {
	*BaseDistribution
	*ManualPackageInstaller
	config DistroConfig
}

func NewOpenSUSEDistribution(config DistroConfig, logChan chan<- string) *OpenSUSEDistribution {
	base := NewBaseDistribution(logChan)
	return &OpenSUSEDistribution{
		BaseDistribution:       base,
		ManualPackageInstaller: &ManualPackageInstaller{BaseDistribution: base},
		config:                 config,
	}
}

func (o *OpenSUSEDistribution) GetID() string {
	return o.config.ID
}

func (o *OpenSUSEDistribution) GetColorHex() string {
	return o.config.ColorHex
}

func (o *OpenSUSEDistribution) GetFamily() DistroFamily {
	return o.config.Family
}

func (o *OpenSUSEDistribution) GetPackageManager() PackageManagerType {
	return PackageManagerZypper
}

func (o *OpenSUSEDistribution) DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error) {
	return o.DetectDependenciesWithTerminal(ctx, wm, deps.TerminalGhostty)
}

func (o *OpenSUSEDistribution) DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error) {
	var dependencies []deps.Dependency

	// DMS at the top (shell is prominent)
	dependencies = append(dependencies, o.detectDMS())

	// Terminal with choice support
	dependencies = append(dependencies, o.detectSpecificTerminal(terminal))

	// Common detections using base methods
	dependencies = append(dependencies, o.detectGit())
	dependencies = append(dependencies, o.detectWindowManager(wm))
	dependencies = append(dependencies, o.detectQuickshell())
	dependencies = append(dependencies, o.detectXDGPortal())
	dependencies = append(dependencies, o.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == deps.WindowManagerHyprland {
		dependencies = append(dependencies, o.detectHyprlandTools()...)
	}

	// Niri-specific tools
	if wm == deps.WindowManagerNiri {
		dependencies = append(dependencies, o.detectXwaylandSatellite())
	}

	// Base detections (common across distros)
	dependencies = append(dependencies, o.detectMatugen())
	dependencies = append(dependencies, o.detectDgop())
	dependencies = append(dependencies, o.detectFonts()...)
	dependencies = append(dependencies, o.detectClipboardTools()...)

	return dependencies, nil
}

func (o *OpenSUSEDistribution) detectXDGPortal() deps.Dependency {
	status := deps.StatusMissing
	if o.packageInstalled("xdg-desktop-portal-gtk") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (o *OpenSUSEDistribution) detectPolkitAgent() deps.Dependency {
	status := deps.StatusMissing
	if o.packageInstalled("mate-polkit") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "mate-polkit",
		Status:      status,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (o *OpenSUSEDistribution) packageInstalled(pkg string) bool {
	cmd := exec.Command("rpm", "-q", pkg)
	err := cmd.Run()
	return err == nil
}

func (o *OpenSUSEDistribution) GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping {
	return o.GetPackageMappingWithVariants(wm, make(map[string]deps.PackageVariant))
}

func (o *OpenSUSEDistribution) GetPackageMappingWithVariants(wm deps.WindowManager, variants map[string]deps.PackageVariant) map[string]PackageMapping {
	packages := map[string]PackageMapping{
		// Standard zypper packages
		"git":                    {Name: "git", Repository: RepoTypeSystem},
		"ghostty":                {Name: "ghostty", Repository: RepoTypeSystem},
		"kitty":                  {Name: "kitty", Repository: RepoTypeSystem},
		"alacritty":              {Name: "alacritty", Repository: RepoTypeSystem},
		"wl-clipboard":           {Name: "wl-clipboard-rs", Repository: RepoTypeSystem},
		"xdg-desktop-portal-gtk": {Name: "xdg-desktop-portal-gtk", Repository: RepoTypeSystem},
		"mate-polkit":            {Name: "mate-polkit", Repository: RepoTypeSystem},
		"font-firacode":          {Name: "fira-code-fonts", Repository: RepoTypeSystem},
		"cliphist":               {Name: "cliphist", Repository: RepoTypeSystem},

		// Manual builds
		"dms (DankMaterialShell)": {Name: "dms", Repository: RepoTypeManual, BuildFunc: "installDankMaterialShell"},
		"dgop":                    {Name: "dgop", Repository: RepoTypeManual, BuildFunc: "installDgop"},
		"font-material-symbols":   {Name: "font-material-symbols", Repository: RepoTypeManual, BuildFunc: "installMaterialSymbolsFont"},
		"font-inter":              {Name: "font-inter", Repository: RepoTypeManual, BuildFunc: "installInterFont"},
		"quickshell":              {Name: "quickshell", Repository: RepoTypeManual, BuildFunc: "installQuickshell"},
		"matugen":                 {Name: "matugen", Repository: RepoTypeManual, BuildFunc: "installMatugen"},
	}

	switch wm {
	case deps.WindowManagerHyprland:
		packages["hyprland"] = PackageMapping{Name: "hyprland", Repository: RepoTypeSystem}
		packages["grim"] = PackageMapping{Name: "grim", Repository: RepoTypeSystem}
		packages["slurp"] = PackageMapping{Name: "slurp", Repository: RepoTypeSystem}
		packages["hyprctl"] = PackageMapping{Name: "hyprland", Repository: RepoTypeSystem}
		packages["hyprpicker"] = PackageMapping{Name: "hyprpicker", Repository: RepoTypeSystem}
		packages["grimblast"] = PackageMapping{Name: "grimblast", Repository: RepoTypeManual, BuildFunc: "installGrimblast"}
		packages["jq"] = PackageMapping{Name: "jq", Repository: RepoTypeSystem}
	case deps.WindowManagerNiri:
		packages["niri"] = PackageMapping{Name: "niri", Repository: RepoTypeSystem}
		packages["xwayland-satellite"] = PackageMapping{Name: "xwayland-satellite", Repository: RepoTypeSystem}
	}

	return packages
}

func (o *OpenSUSEDistribution) detectXwaylandSatellite() deps.Dependency {
	status := deps.StatusMissing
	if o.commandExists("xwayland-satellite") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xwayland-satellite",
		Status:      status,
		Description: "Xwayland support",
		Required:    true,
	}
}

func (o *OpenSUSEDistribution) getPrerequisites() []string {
	return []string{
		"make",
		"unzip",
		"gcc",
		"gcc-c++",
	}
}

func (o *OpenSUSEDistribution) InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	prerequisites := o.getPrerequisites()
	var missingPkgs []string

	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.06,
		Step:       "Checking prerequisites...",
		IsComplete: false,
		LogOutput:  "Checking prerequisite packages",
	}

	for _, pkg := range prerequisites {
		checkCmd := exec.CommandContext(ctx, "rpm", "-q", pkg)
		if err := checkCmd.Run(); err != nil {
			missingPkgs = append(missingPkgs, pkg)
		}
	}

	_, err := exec.LookPath("go")
	if err != nil {
		o.log("go not found in PATH, will install go")
		missingPkgs = append(missingPkgs, "go")
	} else {
		o.log("go already available in PATH")
	}

	if len(missingPkgs) == 0 {
		o.log("All prerequisites already installed")
		return nil
	}

	o.log(fmt.Sprintf("Installing prerequisites: %s", strings.Join(missingPkgs, ", ")))
	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
		Progress:    0.08,
		Step:        fmt.Sprintf("Installing %d prerequisites...", len(missingPkgs)),
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo zypper install -y %s", strings.Join(missingPkgs, " ")),
		LogOutput:   fmt.Sprintf("Installing prerequisites: %s", strings.Join(missingPkgs, ", ")),
	}

	args := []string{"zypper", "install", "-y"}
	args = append(args, missingPkgs...)
	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		o.logError("failed to install prerequisites", err)
		o.log(fmt.Sprintf("Prerequisites command output: %s", string(output)))
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}
	o.log(fmt.Sprintf("Prerequisites install output: %s", string(output)))

	return nil
}

func (o *OpenSUSEDistribution) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	if err := o.InstallPrerequisites(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	systemPkgs, manualPkgs := o.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 2: System Packages (Zypper)
	if len(systemPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
			Progress:   0.35,
			Step:       fmt.Sprintf("Installing %d system packages...", len(systemPkgs)),
			IsComplete: false,
			NeedsSudo:  true,
			LogOutput:  fmt.Sprintf("Installing system packages: %s", strings.Join(systemPkgs, ", ")),
		}
		if err := o.installZypperPackages(ctx, systemPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install zypper packages: %w", err)
		}
	}

	// Phase 3: Manual Builds
	if len(manualPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
			Progress:   0.85,
			Step:       fmt.Sprintf("Building %d packages from source...", len(manualPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Building from source: %s", strings.Join(manualPkgs, ", ")),
		}
		if err := o.InstallManualPackages(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install manual packages: %w", err)
		}
	}

	// Phase 4: Configuration
	progressChan <- InstallProgressMsg{
		Phase:      PhaseConfiguration,
		Progress:   0.90,
		Step:       "Configuring system...",
		IsComplete: false,
		LogOutput:  "Starting post-installation configuration...",
	}
	if err := o.postInstallConfig(ctx, wm, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to configure system: %w", err)
	}

	// Phase 5: Complete
	progressChan <- InstallProgressMsg{
		Phase:      PhaseComplete,
		Progress:   1.0,
		Step:       "Installation complete!",
		IsComplete: true,
		LogOutput:  "All packages installed and configured successfully",
	}

	return nil
}

func (o *OpenSUSEDistribution) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []string) {
	systemPkgs := []string{}
	manualPkgs := []string{}

	variantMap := make(map[string]deps.PackageVariant)
	for _, dep := range dependencies {
		variantMap[dep.Name] = dep.Variant
	}

	packageMap := o.GetPackageMappingWithVariants(wm, variantMap)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			o.log(fmt.Sprintf("Warning: No package mapping for %s", dep.Name))
			continue
		}

		switch pkgInfo.Repository {
		case RepoTypeSystem:
			systemPkgs = append(systemPkgs, pkgInfo.Name)
		case RepoTypeManual:
			manualPkgs = append(manualPkgs, dep.Name)
		}
	}

	return systemPkgs, manualPkgs
}

func (o *OpenSUSEDistribution) installZypperPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	o.log(fmt.Sprintf("Installing zypper packages: %s", strings.Join(packages, ", ")))

	args := []string{"zypper", "install", "-y"}
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
	return o.runWithProgress(cmd, progressChan, PhaseSystemPackages, 0.40, 0.60)
}
