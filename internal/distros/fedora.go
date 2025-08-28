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
	Register("fedora", "#0B57A4", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewFedoraDistribution(config, logChan)
	})
	Register("nobara", "#0B57A4", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewFedoraDistribution(config, logChan)
	})
}

type FedoraDistribution struct {
	*BaseDistribution
	*ManualPackageInstaller
	config DistroConfig
}

func NewFedoraDistribution(config DistroConfig, logChan chan<- string) *FedoraDistribution {
	base := NewBaseDistribution(logChan)
	return &FedoraDistribution{
		BaseDistribution:       base,
		ManualPackageInstaller: &ManualPackageInstaller{BaseDistribution: base},
		config:                 config,
	}
}

func (f *FedoraDistribution) GetID() string {
	return f.config.ID
}

func (f *FedoraDistribution) GetColorHex() string {
	return f.config.ColorHex
}

func (f *FedoraDistribution) GetPackageManager() PackageManagerType {
	return PackageManagerDNF
}

func (f *FedoraDistribution) DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error) {
	return f.DetectDependenciesWithTerminal(ctx, wm, deps.TerminalGhostty)
}

func (f *FedoraDistribution) DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error) {
	var dependencies []deps.Dependency

	// DMS at the top (shell is prominent)
	dependencies = append(dependencies, f.detectDMS())

	// Terminal with choice support
	dependencies = append(dependencies, f.detectSpecificTerminal(terminal))

	// Common detections using base methods
	dependencies = append(dependencies, f.detectGit())
	dependencies = append(dependencies, f.detectWindowManager(wm))
	dependencies = append(dependencies, f.detectQuickshell())
	dependencies = append(dependencies, f.detectXDGPortal())
	dependencies = append(dependencies, f.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == deps.WindowManagerHyprland {
		dependencies = append(dependencies, f.detectHyprlandTools()...)
	}

	// Base detections (common across distros)
	dependencies = append(dependencies, f.detectMatugen())
	dependencies = append(dependencies, f.detectDgop())
	dependencies = append(dependencies, f.detectFonts()...)
	dependencies = append(dependencies, f.detectClipboardTools()...)

	return dependencies, nil
}

func (f *FedoraDistribution) detectXDGPortal() deps.Dependency {
	status := deps.StatusMissing
	if f.packageInstalled("xdg-desktop-portal-gtk") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (f *FedoraDistribution) detectPolkitAgent() deps.Dependency {
	status := deps.StatusMissing
	if f.packageInstalled("mate-polkit") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "mate-polkit",
		Status:      status,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (f *FedoraDistribution) packageInstalled(pkg string) bool {
	cmd := exec.Command("rpm", "-q", pkg)
	err := cmd.Run()
	return err == nil
}

func (f *FedoraDistribution) GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping {
	packages := map[string]PackageMapping{
		// Standard DNF packages
		"git":                    {Name: "git", Repository: RepoTypeSystem},
		"ghostty":                {Name: "ghostty", Repository: RepoTypeCOPR, RepoURL: "alternateved/ghostty"},
		"kitty":                  {Name: "kitty", Repository: RepoTypeSystem},
		"wl-clipboard":           {Name: "wl-clipboard", Repository: RepoTypeSystem},
		"xdg-desktop-portal-gtk": {Name: "xdg-desktop-portal-gtk", Repository: RepoTypeSystem},
		"mate-polkit":            {Name: "mate-polkit", Repository: RepoTypeSystem},
		"font-firacode":          {Name: "fira-code-fonts", Repository: RepoTypeSystem},

		// COPR packages
		"quickshell": {Name: "quickshell", Repository: RepoTypeCOPR, RepoURL: "errornointernet/quickshell"},
		"matugen":    {Name: "matugen", Repository: RepoTypeCOPR, RepoURL: "heus-sueh/packages"},
		"cliphist":   {Name: "cliphist", Repository: RepoTypeCOPR, RepoURL: "alternateved/cliphist"},

		// Manual builds
		"dgop":                  {Name: "dgop", Repository: RepoTypeManual, BuildFunc: "installDgop"},
		"font-material-symbols": {Name: "font-material-symbols", Repository: RepoTypeManual, BuildFunc: "installMaterialSymbolsFont"},
		"font-inter":            {Name: "font-inter", Repository: RepoTypeManual, BuildFunc: "installInterFont"},
	}

	// Add window manager specific packages
	switch wm {
	case deps.WindowManagerHyprland:
		packages["hyprland"] = PackageMapping{Name: "hyprland", Repository: RepoTypeCOPR, RepoURL: "solopasha/hyprland"}
		packages["grim"] = PackageMapping{Name: "grim", Repository: RepoTypeSystem}
		packages["slurp"] = PackageMapping{Name: "slurp", Repository: RepoTypeSystem}
		packages["hyprctl"] = PackageMapping{Name: "hyprland", Repository: RepoTypeCOPR, RepoURL: "solopasha/hyprland"}
		packages["hyprpicker"] = PackageMapping{Name: "hyprpicker", Repository: RepoTypeCOPR, RepoURL: "solopasha/hyprland"}
		packages["grimblast"] = PackageMapping{Name: "grimblast", Repository: RepoTypeManual, BuildFunc: "installGrimblast"}
		packages["jq"] = PackageMapping{Name: "jq", Repository: RepoTypeSystem}
	case deps.WindowManagerNiri:
		packages["niri"] = PackageMapping{Name: "niri", Repository: RepoTypeCOPR, RepoURL: "yalter/niri-git"}
	}

	return packages
}

func (f *FedoraDistribution) InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.06,
		Step:       "Checking dnf-plugins-core...",
		IsComplete: false,
		LogOutput:  "Checking if dnf-plugins-core is installed",
	}

	checkCmd := exec.CommandContext(ctx, "rpm", "-q", "dnf-plugins-core")
	if err := checkCmd.Run(); err == nil {
		f.log("dnf-plugins-core already installed")
		return nil
	}

	f.log("Installing dnf-plugins-core...")
	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhasePrerequisites,
		Progress:    0.08,
		Step:        "Installing dnf-plugins-core...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo dnf install -y dnf-plugins-core",
		LogOutput:   "Installing dnf-plugins-core for COPR support",
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S dnf install -y dnf-plugins-core 2>&1", sudoPassword))
	output, err := cmd.CombinedOutput()
	if err != nil {
		f.logError("failed to install dnf-plugins-core", err)
		f.log(fmt.Sprintf("dnf-plugins-core command output: %s", string(output)))
		return fmt.Errorf("failed to install dnf-plugins-core: %w", err)
	}
	f.log(fmt.Sprintf("dnf-plugins-core install output: %s", string(output)))

	return nil
}

func (f *FedoraDistribution) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- installer.InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	if err := f.InstallPrerequisites(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	dnfPkgs, coprPkgs, manualPkgs := f.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 2: Enable COPR repositories
	if len(coprPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.15,
			Step:       "Enabling COPR repositories...",
			IsComplete: false,
			LogOutput:  "Setting up COPR repositories for additional packages",
		}
		if err := f.enableCOPRRepos(ctx, coprPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to enable COPR repositories: %w", err)
		}
	}

	// Phase 3: System Packages (DNF)
	if len(dnfPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.35,
			Step:       fmt.Sprintf("Installing %d system packages...", len(dnfPkgs)),
			IsComplete: false,
			NeedsSudo:  true,
			LogOutput:  fmt.Sprintf("Installing system packages: %s", strings.Join(dnfPkgs, ", ")),
		}
		if err := f.installDNFPackages(ctx, dnfPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install DNF packages: %w", err)
		}
	}

	// Phase 4: COPR Packages
	coprPkgNames := f.extractPackageNames(coprPkgs)
	if len(coprPkgNames) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseAURPackages, // Reusing AUR phase for COPR
			Progress:   0.65,
			Step:       fmt.Sprintf("Installing %d COPR packages...", len(coprPkgNames)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing COPR packages: %s", strings.Join(coprPkgNames, ", ")),
		}
		if err := f.installCOPRPackages(ctx, coprPkgNames, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install COPR packages: %w", err)
		}
	}

	// Phase 5: Manual Builds
	if len(manualPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.85,
			Step:       fmt.Sprintf("Building %d packages from source...", len(manualPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Building from source: %s", strings.Join(manualPkgs, ", ")),
		}
		if err := f.InstallManualPackages(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install manual packages: %w", err)
		}
	}

	// Phase 6: Configuration
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhaseConfiguration,
		Progress:   0.90,
		Step:       "Configuring system...",
		IsComplete: false,
		LogOutput:  "Starting post-installation configuration...",
	}
	if err := f.postInstallConfig(ctx, wm, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to configure system: %w", err)
	}

	// Phase 7: Complete
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhaseComplete,
		Progress:   1.0,
		Step:       "Installation complete!",
		IsComplete: true,
		LogOutput:  "All packages installed and configured successfully",
	}

	return nil
}

func (f *FedoraDistribution) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []PackageMapping, []string) {
	dnfPkgs := []string{}
	coprPkgs := []PackageMapping{}
	manualPkgs := []string{}

	packageMap := f.GetPackageMapping(wm)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			f.log(fmt.Sprintf("Warning: No package mapping for %s", dep.Name))
			continue
		}

		switch pkgInfo.Repository {
		case RepoTypeSystem:
			dnfPkgs = append(dnfPkgs, pkgInfo.Name)
		case RepoTypeCOPR:
			coprPkgs = append(coprPkgs, pkgInfo)
		case RepoTypeManual:
			manualPkgs = append(manualPkgs, dep.Name)
		}
	}

	return dnfPkgs, coprPkgs, manualPkgs
}

func (f *FedoraDistribution) extractPackageNames(packages []PackageMapping) []string {
	names := make([]string, len(packages))
	for i, pkg := range packages {
		names[i] = pkg.Name
	}
	return names
}

func (f *FedoraDistribution) enableCOPRRepos(ctx context.Context, coprPkgs []PackageMapping, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	enabledRepos := make(map[string]bool)

	for _, pkg := range coprPkgs {
		if pkg.RepoURL != "" && !enabledRepos[pkg.RepoURL] {
			f.log(fmt.Sprintf("Enabling COPR repository: %s", pkg.RepoURL))
			progressChan <- installer.InstallProgressMsg{
				Phase:       installer.PhaseSystemPackages,
				Progress:    0.20,
				Step:        fmt.Sprintf("Enabling COPR repo %s...", pkg.RepoURL),
				IsComplete:  false,
				NeedsSudo:   true,
				CommandInfo: fmt.Sprintf("sudo dnf copr enable -y %s", pkg.RepoURL),
			}

			cmd := exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S dnf copr enable -y %s 2>&1", sudoPassword, pkg.RepoURL))
			output, err := cmd.CombinedOutput()
			if err != nil {
				f.logError(fmt.Sprintf("failed to enable COPR repo %s", pkg.RepoURL), err)
				f.log(fmt.Sprintf("COPR enable command output: %s", string(output)))
				return fmt.Errorf("failed to enable COPR repo %s: %w", pkg.RepoURL, err)
			}
			f.log(fmt.Sprintf("COPR repo %s enabled successfully: %s", pkg.RepoURL, string(output)))
			enabledRepos[pkg.RepoURL] = true

			// Special handling for niri COPR repo - set priority=1
			if pkg.RepoURL == "yalter/niri-git" {
				f.log("Setting priority=1 for niri COPR repo...")
				progressChan <- installer.InstallProgressMsg{
					Phase:       installer.PhaseSystemPackages,
					Progress:    0.22,
					Step:        "Setting niri COPR repo priority...",
					IsComplete:  false,
					NeedsSudo:   true,
					CommandInfo: "echo \"priority=1\" | sudo tee -a /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:yalter:niri-git.repo",
				}

				priorityCmd := exec.CommandContext(ctx, "bash", "-c",
					fmt.Sprintf("echo '%s' | sudo -S bash -c 'echo \"priority=1\" | tee -a /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:yalter:niri-git.repo' 2>&1", sudoPassword))
				priorityOutput, err := priorityCmd.CombinedOutput()
				if err != nil {
					f.logError("failed to set niri COPR repo priority", err)
					f.log(fmt.Sprintf("Priority command output: %s", string(priorityOutput)))
					return fmt.Errorf("failed to set niri COPR repo priority: %w", err)
				}
				f.log(fmt.Sprintf("niri COPR repo priority set successfully: %s", string(priorityOutput)))
			}
		}
	}

	return nil
}

func (f *FedoraDistribution) installDNFPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	f.log(fmt.Sprintf("Installing DNF packages: %s", strings.Join(packages, ", ")))

	args := []string{"dnf", "install", "-y"}
	args = append(args, packages...)

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhaseSystemPackages,
		Progress:    0.40,
		Step:        "Installing system packages...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo %s", strings.Join(args, " ")),
	}

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return f.runWithProgress(cmd, progressChan, installer.PhaseSystemPackages, 0.40, 0.60)
}

func (f *FedoraDistribution) installCOPRPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	f.log(fmt.Sprintf("Installing COPR packages: %s", strings.Join(packages, ", ")))

	args := []string{"dnf", "install", "-y"}
	args = append(args, packages...)

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhaseAURPackages,
		Progress:    0.70,
		Step:        "Installing COPR packages...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo %s", strings.Join(args, " ")),
	}

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return f.runWithProgress(cmd, progressChan, installer.PhaseAURPackages, 0.70, 0.85)
}
