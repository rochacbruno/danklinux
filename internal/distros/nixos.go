package distros

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/deps"
	"github.com/AvengeMedia/dankinstall/internal/installer"
)

func init() {
	Register("nixos", "#7EBAE4", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewNixOSDistribution(config, logChan)
	})
}

type NixOSDistribution struct {
	*BaseDistribution
	config DistroConfig
}

func NewNixOSDistribution(config DistroConfig, logChan chan<- string) *NixOSDistribution {
	base := NewBaseDistribution(logChan)
	return &NixOSDistribution{
		BaseDistribution: base,
		config:           config,
	}
}

func (n *NixOSDistribution) GetID() string {
	return n.config.ID
}

func (n *NixOSDistribution) GetColorHex() string {
	return n.config.ColorHex
}

func (n *NixOSDistribution) GetPackageManager() PackageManagerType {
	return PackageManagerNix
}

func (n *NixOSDistribution) DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error) {
	return n.DetectDependenciesWithTerminal(ctx, wm, deps.TerminalGhostty)
}

func (n *NixOSDistribution) DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error) {
	var dependencies []deps.Dependency

	// DMS at the top (shell is prominent)
	dependencies = append(dependencies, n.detectDMS())

	// Terminal with choice support
	dependencies = append(dependencies, n.detectSpecificTerminal(terminal))

	// Common detections using base methods
	dependencies = append(dependencies, n.detectGit())
	dependencies = append(dependencies, n.detectWindowManager(wm))
	dependencies = append(dependencies, n.detectQuickshell())
	dependencies = append(dependencies, n.detectXDGPortal())
	dependencies = append(dependencies, n.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == deps.WindowManagerHyprland {
		dependencies = append(dependencies, n.detectHyprlandTools()...)
	}

	// Base detections (common across distros)
	dependencies = append(dependencies, n.detectMatugen())
	dependencies = append(dependencies, n.detectDgop())
	dependencies = append(dependencies, n.detectFonts()...)
	dependencies = append(dependencies, n.detectClipboardTools()...)

	return dependencies, nil
}

func (n *NixOSDistribution) detectXDGPortal() deps.Dependency {
	status := deps.StatusMissing
	if n.packageInstalled("xdg-desktop-portal-gtk") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (n *NixOSDistribution) detectPolkitAgent() deps.Dependency {
	if n.packageInstalled("mate-polkit") {
		return deps.Dependency{
			Name:        "polkit-agent",
			Status:      deps.StatusInstalled,
			Description: "PolicyKit authentication agent",
			Required:    true,
		}
	}

	return deps.Dependency{
		Name:        "mate-polkit",
		Status:      deps.StatusMissing,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (n *NixOSDistribution) packageInstalled(pkg string) bool {
	cmd := exec.Command("nix", "profile", "list")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), pkg)
}

func (n *NixOSDistribution) GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping {
	packages := map[string]PackageMapping{
		"git":                    {Name: "nixpkgs#git", Repository: RepoTypeSystem},
		"quickshell":             {Name: "github:quickshell-mirror/quickshell", Repository: RepoTypeFlake},
		"matugen":                {Name: "github:InioX/matugen", Repository: RepoTypeFlake},
		"dgop":                   {Name: "github:AvengeMedia/dgop", Repository: RepoTypeFlake},
		"ghostty":                {Name: "nixpkgs#ghostty", Repository: RepoTypeSystem},
		"cliphist":               {Name: "nixpkgs#cliphist", Repository: RepoTypeSystem},
		"wl-clipboard":           {Name: "nixpkgs#wl-clipboard", Repository: RepoTypeSystem},
		"xdg-desktop-portal-gtk": {Name: "nixpkgs#xdg-desktop-portal-gtk", Repository: RepoTypeSystem},
		"mate-polkit":            {Name: "nixpkgs#mate.mate-polkit", Repository: RepoTypeSystem},
		"font-material-symbols":  {Name: "nixpkgs#material-symbols", Repository: RepoTypeSystem},
		"font-firacode":          {Name: "nixpkgs#fira-code", Repository: RepoTypeSystem},
		"font-inter":             {Name: "nixpkgs#inter", Repository: RepoTypeSystem},
	}

	// Add window manager specific packages
	switch wm {
	case deps.WindowManagerHyprland:
		packages["hyprland"] = PackageMapping{Name: "nixpkgs#hyprland", Repository: RepoTypeSystem}
		packages["grim"] = PackageMapping{Name: "nixpkgs#grim", Repository: RepoTypeSystem}
		packages["slurp"] = PackageMapping{Name: "nixpkgs#slurp", Repository: RepoTypeSystem}
		packages["hyprctl"] = PackageMapping{Name: "nixpkgs#hyprland", Repository: RepoTypeSystem}
		packages["hyprpicker"] = PackageMapping{Name: "nixpkgs#hyprpicker", Repository: RepoTypeSystem}
		packages["grimblast"] = PackageMapping{Name: "github:hyprwm/contrib#grimblast", Repository: RepoTypeFlake}
		packages["jq"] = PackageMapping{Name: "nixpkgs#jq", Repository: RepoTypeSystem}
	case deps.WindowManagerNiri:
		packages["niri"] = PackageMapping{Name: "nixpkgs#niri", Repository: RepoTypeSystem}
	}

	return packages
}

func (n *NixOSDistribution) InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.10,
		Step:       "NixOS prerequisites ready",
		IsComplete: false,
		LogOutput:  "NixOS package manager is ready to use",
	}
	return nil
}

func (n *NixOSDistribution) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- installer.InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	if err := n.InstallPrerequisites(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	nixpkgsPkgs, flakePkgs := n.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 2: Nixpkgs Packages
	if len(nixpkgsPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.35,
			Step:       fmt.Sprintf("Installing %d packages from nixpkgs...", len(nixpkgsPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing nixpkgs packages: %s", strings.Join(nixpkgsPkgs, ", ")),
		}
		if err := n.installNixpkgsPackages(ctx, nixpkgsPkgs, progressChan); err != nil {
			return fmt.Errorf("failed to install nixpkgs packages: %w", err)
		}
	}

	// Phase 3: Flake Packages
	if len(flakePkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseAURPackages,
			Progress:   0.65,
			Step:       fmt.Sprintf("Installing %d packages from flakes...", len(flakePkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing flake packages: %s", strings.Join(flakePkgs, ", ")),
		}
		if err := n.installFlakePackages(ctx, flakePkgs, progressChan); err != nil {
			return fmt.Errorf("failed to install flake packages: %w", err)
		}
	}

	// Phase 4: Complete
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhaseComplete,
		Progress:   1.0,
		Step:       "Installation complete!",
		IsComplete: true,
		LogOutput:  "All packages installed successfully",
	}

	return nil
}

func (n *NixOSDistribution) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []string) {
	nixpkgsPkgs := []string{}
	flakePkgs := []string{}

	packageMap := n.GetPackageMapping(wm)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			n.log(fmt.Sprintf("Warning: No package mapping found for %s", dep.Name))
			continue
		}

		switch pkgInfo.Repository {
		case RepoTypeSystem:
			nixpkgsPkgs = append(nixpkgsPkgs, pkgInfo.Name)
		case RepoTypeFlake:
			flakePkgs = append(flakePkgs, pkgInfo.Name)
		}
	}

	return nixpkgsPkgs, flakePkgs
}

func (n *NixOSDistribution) installNixpkgsPackages(ctx context.Context, packages []string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	n.log(fmt.Sprintf("Installing nixpkgs packages: %s", strings.Join(packages, ", ")))

	args := []string{"profile", "install"}
	args = append(args, packages...)

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhaseSystemPackages,
		Progress:    0.40,
		Step:        "Installing nixpkgs packages...",
		IsComplete:  false,
		CommandInfo: fmt.Sprintf("nix %s", strings.Join(args, " ")),
	}

	cmd := exec.CommandContext(ctx, "nix", args...)
	return n.runWithProgress(cmd, progressChan, installer.PhaseSystemPackages, 0.40, 0.60)
}

func (n *NixOSDistribution) installFlakePackages(ctx context.Context, packages []string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	n.log(fmt.Sprintf("Installing flake packages: %s", strings.Join(packages, ", ")))

	baseProgress := 0.65
	progressStep := 0.20 / float64(len(packages))

	for i, pkg := range packages {
		currentProgress := baseProgress + (float64(i) * progressStep)

		progressChan <- installer.InstallProgressMsg{
			Phase:       installer.PhaseAURPackages,
			Progress:    currentProgress,
			Step:        fmt.Sprintf("Installing flake package %s (%d/%d)...", pkg, i+1, len(packages)),
			IsComplete:  false,
			CommandInfo: fmt.Sprintf("nix profile install %s", pkg),
		}

		cmd := exec.CommandContext(ctx, "nix", "profile", "install", pkg)
		if err := n.runWithProgress(cmd, progressChan, installer.PhaseAURPackages, currentProgress, currentProgress+progressStep); err != nil {
			return fmt.Errorf("failed to install flake package %s: %w", pkg, err)
		}
	}

	return nil
}