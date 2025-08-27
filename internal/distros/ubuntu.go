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
	Register("ubuntu", "#E95420", func(config DistroConfig, logChan chan<- string) Distribution {
		return NewUbuntuDistribution(config, logChan)
	})
}

type UbuntuDistribution struct {
	*BaseDistribution
	*ManualPackageInstaller
	config DistroConfig
}

func NewUbuntuDistribution(config DistroConfig, logChan chan<- string) *UbuntuDistribution {
	base := NewBaseDistribution(logChan)
	return &UbuntuDistribution{
		BaseDistribution:       base,
		ManualPackageInstaller: &ManualPackageInstaller{BaseDistribution: base},
		config:                 config,
	}
}

func (u *UbuntuDistribution) GetID() string {
	return u.config.ID
}

func (u *UbuntuDistribution) GetColorHex() string {
	return u.config.ColorHex
}

func (u *UbuntuDistribution) GetPackageManager() PackageManagerType {
	return PackageManagerAPT
}

func (u *UbuntuDistribution) DetectDependencies(ctx context.Context, wm deps.WindowManager) ([]deps.Dependency, error) {
	return u.DetectDependenciesWithTerminal(ctx, wm, deps.TerminalGhostty)
}

func (u *UbuntuDistribution) DetectDependenciesWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]deps.Dependency, error) {
	var dependencies []deps.Dependency

	// DMS at the top (shell is prominent)
	dependencies = append(dependencies, u.detectDMS())

	// Terminal with choice support
	dependencies = append(dependencies, u.detectSpecificTerminal(terminal))

	// Common detections using base methods
	dependencies = append(dependencies, u.detectGit())
	dependencies = append(dependencies, u.detectWindowManager(wm))
	dependencies = append(dependencies, u.detectQuickshell())
	dependencies = append(dependencies, u.detectXDGPortal())
	dependencies = append(dependencies, u.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == deps.WindowManagerHyprland {
		dependencies = append(dependencies, u.detectHyprlandTools()...)
	}

	// Base detections (common across distros)
	dependencies = append(dependencies, u.detectMatugen())
	dependencies = append(dependencies, u.detectDgop())
	dependencies = append(dependencies, u.detectFonts()...)
	dependencies = append(dependencies, u.detectClipboardTools()...)

	return dependencies, nil
}

func (u *UbuntuDistribution) detectXDGPortal() deps.Dependency {
	status := deps.StatusMissing
	if u.packageInstalled("xdg-desktop-portal-gtk") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (u *UbuntuDistribution) detectPolkitAgent() deps.Dependency {
	status := deps.StatusMissing
	if u.packageInstalled("mate-polkit") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "mate-polkit",
		Status:      status,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (u *UbuntuDistribution) packageInstalled(pkg string) bool {
	cmd := exec.Command("dpkg", "-l", pkg)
	err := cmd.Run()
	return err == nil
}

func (u *UbuntuDistribution) GetPackageMapping(wm deps.WindowManager) map[string]PackageMapping {
	packages := map[string]PackageMapping{
		// Standard APT packages
		"git":                    {Name: "git", Repository: RepoTypeSystem},
		"kitty":                  {Name: "kitty", Repository: RepoTypeSystem},
		"wl-clipboard":           {Name: "wl-clipboard", Repository: RepoTypeSystem},
		"xdg-desktop-portal-gtk": {Name: "xdg-desktop-portal-gtk", Repository: RepoTypeSystem},
		"mate-polkit":            {Name: "mate-polkit", Repository: RepoTypeSystem},
		"font-firacode":          {Name: "fonts-firacode", Repository: RepoTypeSystem},
		"font-inter":             {Name: "fonts-inter-variable", Repository: RepoTypeSystem},

		// Manual builds (niri and quickshell likely not available in Ubuntu repos or PPAs)
		"niri":                  {Name: "niri", Repository: RepoTypeManual, BuildFunc: "installNiri"},
		"quickshell":            {Name: "quickshell", Repository: RepoTypeManual, BuildFunc: "installQuickshell"},
		"ghostty":               {Name: "ghostty", Repository: RepoTypeManual, BuildFunc: "installGhostty"},
		"matugen":               {Name: "matugen", Repository: RepoTypeManual, BuildFunc: "installMatugen"},
		"dgop":                  {Name: "dgop", Repository: RepoTypeManual, BuildFunc: "installDgop"},
		"cliphist":              {Name: "cliphist", Repository: RepoTypeManual, BuildFunc: "installCliphist"},
		"font-material-symbols": {Name: "font-material-symbols", Repository: RepoTypeManual, BuildFunc: "installMaterialSymbolsFont"},
	}

	// Add window manager specific packages
	switch wm {
	case deps.WindowManagerHyprland:
		// Hyprland likely needs to be built from source on Ubuntu
		packages["hyprland"] = PackageMapping{Name: "hyprland", Repository: RepoTypeManual, BuildFunc: "installHyprland"}
		packages["grim"] = PackageMapping{Name: "grim", Repository: RepoTypeSystem}
		packages["slurp"] = PackageMapping{Name: "slurp", Repository: RepoTypeSystem}
		packages["hyprctl"] = PackageMapping{Name: "hyprland", Repository: RepoTypeManual, BuildFunc: "installHyprland"}
		packages["hyprpicker"] = PackageMapping{Name: "hyprpicker", Repository: RepoTypeManual, BuildFunc: "installHyprpicker"}
		packages["grimblast"] = PackageMapping{Name: "grimblast", Repository: RepoTypeManual, BuildFunc: "installGrimblast"}
		packages["jq"] = PackageMapping{Name: "jq", Repository: RepoTypeSystem}
	case deps.WindowManagerNiri:
		packages["niri"] = PackageMapping{Name: "niri", Repository: RepoTypeManual, BuildFunc: "installNiri"}
	}

	return packages
}

func (u *UbuntuDistribution) InstallPrerequisites(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.06,
		Step:       "Updating package lists...",
		IsComplete: false,
		LogOutput:  "Updating APT package lists",
	}

	// Update package lists
	updateCmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S apt update", sudoPassword))
	if err := u.runWithProgress(updateCmd, progressChan, installer.PhasePrerequisites, 0.06, 0.07); err != nil {
		return fmt.Errorf("failed to update package lists: %w", err)
	}

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhasePrerequisites,
		Progress:    0.08,
		Step:        "Installing build-essential...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo apt install -y build-essential",
		LogOutput:   "Installing build tools",
	}

	// Install build-essential (equivalent to base-devel on Arch)
	checkCmd := exec.CommandContext(ctx, "dpkg", "-l", "build-essential")
	if err := checkCmd.Run(); err != nil {
		// Not installed, install it
		cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S apt install -y build-essential", sudoPassword))
		if err := u.runWithProgress(cmd, progressChan, installer.PhasePrerequisites, 0.08, 0.09); err != nil {
			return fmt.Errorf("failed to install build-essential: %w", err)
		}
	}

	// Install additional development tools needed for building from source
	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhasePrerequisites,
		Progress:    0.10,
		Step:        "Installing development dependencies...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo apt install -y curl wget git cmake ninja-build pkg-config",
		LogOutput:   "Installing additional development tools",
	}

	devToolsCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S apt install -y curl wget git cmake ninja-build pkg-config", sudoPassword))
	if err := u.runWithProgress(devToolsCmd, progressChan, installer.PhasePrerequisites, 0.10, 0.12); err != nil {
		return fmt.Errorf("failed to install development tools: %w", err)
	}

	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.12,
		Step:       "Prerequisites installation complete",
		IsComplete: false,
		LogOutput:  "Prerequisites successfully installed",
	}

	return nil
}

func (u *UbuntuDistribution) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- installer.InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- installer.InstallProgressMsg{
		Phase:      installer.PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	if err := u.InstallPrerequisites(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	systemPkgs, ppaPkgs, manualPkgs := u.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 2: Enable PPA repositories
	if len(ppaPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.15,
			Step:       "Enabling PPA repositories...",
			IsComplete: false,
			LogOutput:  "Setting up PPA repositories for additional packages",
		}
		if err := u.enablePPARepos(ctx, ppaPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to enable PPA repositories: %w", err)
		}
	}

	// Phase 3: System Packages (APT)
	if len(systemPkgs) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.35,
			Step:       fmt.Sprintf("Installing %d system packages...", len(systemPkgs)),
			IsComplete: false,
			NeedsSudo:  true,
			LogOutput:  fmt.Sprintf("Installing system packages: %s", strings.Join(systemPkgs, ", ")),
		}
		if err := u.installAPTPackages(ctx, systemPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install APT packages: %w", err)
		}
	}

	// Phase 4: PPA Packages
	ppaPkgNames := u.extractPackageNames(ppaPkgs)
	if len(ppaPkgNames) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseAURPackages, // Reusing AUR phase for PPA
			Progress:   0.65,
			Step:       fmt.Sprintf("Installing %d PPA packages...", len(ppaPkgNames)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing PPA packages: %s", strings.Join(ppaPkgNames, ", ")),
		}
		if err := u.installPPAPackages(ctx, ppaPkgNames, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install PPA packages: %w", err)
		}
	}

	// Phase 5: Manual Builds
	if len(manualPkgs) > 0 {
		// Install build dependencies first
		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.80,
			Step:       "Installing build dependencies...",
			IsComplete: false,
			LogOutput:  "Installing build tools for manual compilation",
		}
		if err := u.installBuildDependencies(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install build dependencies: %w", err)
		}

		progressChan <- installer.InstallProgressMsg{
			Phase:      installer.PhaseSystemPackages,
			Progress:   0.85,
			Step:       fmt.Sprintf("Building %d packages from source...", len(manualPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Building from source: %s", strings.Join(manualPkgs, ", ")),
		}
		if err := u.InstallManualPackages(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
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
	if err := u.postInstallConfig(ctx, wm, sudoPassword, progressChan); err != nil {
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

func (u *UbuntuDistribution) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []PackageMapping, []string) {
	systemPkgs := []string{}
	ppaPkgs := []PackageMapping{}
	manualPkgs := []string{}

	packageMap := u.GetPackageMapping(wm)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			u.log(fmt.Sprintf("Warning: No package mapping for %s", dep.Name))
			continue
		}

		switch pkgInfo.Repository {
		case RepoTypeSystem:
			systemPkgs = append(systemPkgs, pkgInfo.Name)
		case RepoTypePPA:
			ppaPkgs = append(ppaPkgs, pkgInfo)
		case RepoTypeManual:
			manualPkgs = append(manualPkgs, dep.Name)
		}
	}

	return systemPkgs, ppaPkgs, manualPkgs
}

func (u *UbuntuDistribution) extractPackageNames(packages []PackageMapping) []string {
	names := make([]string, len(packages))
	for i, pkg := range packages {
		names[i] = pkg.Name
	}
	return names
}

func (u *UbuntuDistribution) enablePPARepos(ctx context.Context, ppaPkgs []PackageMapping, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	enabledRepos := make(map[string]bool)

	// Install software-properties-common first if needed
	installPPACmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S apt install -y software-properties-common", sudoPassword))
	if err := u.runWithProgress(installPPACmd, progressChan, installer.PhaseSystemPackages, 0.15, 0.17); err != nil {
		return fmt.Errorf("failed to install software-properties-common: %w", err)
	}

	for _, pkg := range ppaPkgs {
		if pkg.RepoURL != "" && !enabledRepos[pkg.RepoURL] {
			u.log(fmt.Sprintf("Enabling PPA repository: %s", pkg.RepoURL))
			progressChan <- installer.InstallProgressMsg{
				Phase:       installer.PhaseSystemPackages,
				Progress:    0.20,
				Step:        fmt.Sprintf("Enabling PPA repo %s...", pkg.RepoURL),
				IsComplete:  false,
				NeedsSudo:   true,
				CommandInfo: fmt.Sprintf("sudo add-apt-repository -y %s", pkg.RepoURL),
			}

			cmd := exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S add-apt-repository -y %s", sudoPassword, pkg.RepoURL))
			if err := u.runWithProgress(cmd, progressChan, installer.PhaseSystemPackages, 0.20, 0.22); err != nil {
				u.logError(fmt.Sprintf("failed to enable PPA repo %s", pkg.RepoURL), err)
				return fmt.Errorf("failed to enable PPA repo %s: %w", pkg.RepoURL, err)
			}
			u.log(fmt.Sprintf("PPA repo %s enabled successfully", pkg.RepoURL))
			enabledRepos[pkg.RepoURL] = true
		}
	}

	// Update package lists after adding PPAs
	if len(enabledRepos) > 0 {
		progressChan <- installer.InstallProgressMsg{
			Phase:       installer.PhaseSystemPackages,
			Progress:    0.25,
			Step:        "Updating package lists...",
			IsComplete:  false,
			NeedsSudo:   true,
			CommandInfo: "sudo apt update",
		}

		updateCmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S apt update", sudoPassword))
		if err := u.runWithProgress(updateCmd, progressChan, installer.PhaseSystemPackages, 0.25, 0.27); err != nil {
			return fmt.Errorf("failed to update package lists after adding PPAs: %w", err)
		}
	}

	return nil
}

func (u *UbuntuDistribution) installAPTPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	u.log(fmt.Sprintf("Installing APT packages: %s", strings.Join(packages, ", ")))

	args := []string{"apt", "install", "-y"}
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
	return u.runWithProgress(cmd, progressChan, installer.PhaseSystemPackages, 0.40, 0.60)
}

func (u *UbuntuDistribution) installPPAPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	u.log(fmt.Sprintf("Installing PPA packages: %s", strings.Join(packages, ", ")))

	args := []string{"apt", "install", "-y"}
	args = append(args, packages...)

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhaseAURPackages,
		Progress:    0.70,
		Step:        "Installing PPA packages...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo %s", strings.Join(args, " ")),
	}

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return u.runWithProgress(cmd, progressChan, installer.PhaseAURPackages, 0.70, 0.85)
}

func (u *UbuntuDistribution) installBuildDependencies(ctx context.Context, manualPkgs []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	buildDeps := make(map[string]bool)

	for _, pkg := range manualPkgs {
		switch pkg {
		case "niri":
			buildDeps["curl"] = true
			buildDeps["libxkbcommon-dev"] = true
			buildDeps["libwayland-dev"] = true
			buildDeps["libudev-dev"] = true
			buildDeps["libinput-dev"] = true
			buildDeps["libdisplay-info-dev"] = true
			buildDeps["libpango1.0-dev"] = true
			buildDeps["libcairo-dev"] = true
		case "quickshell":
			buildDeps["qt6-base-dev"] = true
			buildDeps["qt6-declarative-dev"] = true
			buildDeps["qt6-wayland-dev"] = true
			buildDeps["libqt6svg6-dev"] = true
		case "hyprland":
			buildDeps["meson"] = true
			buildDeps["libwayland-dev"] = true
			buildDeps["libxkbcommon-dev"] = true
			buildDeps["libegl1-mesa-dev"] = true
			buildDeps["libgles2-mesa-dev"] = true
			buildDeps["libdrm-dev"] = true
			buildDeps["libxcb-dri3-dev"] = true
			buildDeps["libxcb-present-dev"] = true
			buildDeps["libxcb-composite0-dev"] = true
			buildDeps["libxcb-ewmh-dev"] = true
			buildDeps["libxcb-icccm4-dev"] = true
			buildDeps["libxcb-res0-dev"] = true
			buildDeps["libxcb-util0-dev"] = true
		case "ghostty":
			buildDeps["curl"] = true
			buildDeps["libgtk-4-dev"] = true
			buildDeps["libadwaita-1-dev"] = true
		case "matugen":
			buildDeps["curl"] = true
		case "cliphist":
			buildDeps["golang-go"] = true
		}
	}

	// Install language toolchains that need special handling
	for _, pkg := range manualPkgs {
		switch pkg {
		case "niri", "matugen":
			if err := u.installRust(ctx, sudoPassword, progressChan); err != nil {
				return fmt.Errorf("failed to install Rust: %w", err)
			}
		case "ghostty":
			if err := u.installZig(ctx, sudoPassword, progressChan); err != nil {
				return fmt.Errorf("failed to install Zig: %w", err)
			}
		}
	}

	if len(buildDeps) == 0 {
		return nil
	}

	depList := make([]string, 0, len(buildDeps))
	for dep := range buildDeps {
		depList = append(depList, dep)
	}

	args := []string{"apt", "install", "-y"}
	args = append(args, depList...)

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return u.runWithProgress(cmd, progressChan, installer.PhaseSystemPackages, 0.80, 0.82)
}

func (u *UbuntuDistribution) installRust(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if u.commandExists("cargo") {
		return nil
	}

	rustCmd := exec.CommandContext(ctx, "bash", "-c", "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y")
	return u.runWithProgress(rustCmd, progressChan, installer.PhaseSystemPackages, 0.82, 0.84)
}

func (u *UbuntuDistribution) installZig(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if u.commandExists("zig") {
		return nil
	}

	zigUrl := "https://ziglang.org/download/0.11.0/zig-linux-x86_64-0.11.0.tar.xz"
	zigTmp := "/tmp/zig.tar.xz"

	downloadCmd := exec.CommandContext(ctx, "curl", "-L", zigUrl, "-o", zigTmp)
	if err := u.runWithProgress(downloadCmd, progressChan, installer.PhaseSystemPackages, 0.84, 0.85); err != nil {
		return fmt.Errorf("failed to download Zig: %w", err)
	}

	extractCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S tar -xf %s -C /opt/", sudoPassword, zigTmp))
	if err := u.runWithProgress(extractCmd, progressChan, installer.PhaseSystemPackages, 0.85, 0.86); err != nil {
		return fmt.Errorf("failed to extract Zig: %w", err)
	}

	linkCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S ln -sf /opt/zig-linux-x86_64-0.11.0/zig /usr/local/bin/zig", sudoPassword))
	return u.runWithProgress(linkCmd, progressChan, installer.PhaseSystemPackages, 0.86, 0.87)
}

func (u *UbuntuDistribution) installGhosttyUbuntu(ctx context.Context, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	u.log("Installing Ghostty using Ubuntu installer script...")

	progressChan <- installer.InstallProgressMsg{
		Phase:       installer.PhaseSystemPackages,
		Progress:    0.1,
		Step:        "Running Ghostty Ubuntu installer...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "curl -fsSL https://raw.githubusercontent.com/mkasberg/ghostty-ubuntu/HEAD/install.sh | sudo bash",
		LogOutput:   "Installing Ghostty using pre-built Ubuntu package",
	}

	installCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/mkasberg/ghostty-ubuntu/HEAD/install.sh)\"", sudoPassword))

	if err := u.runWithProgress(installCmd, progressChan, installer.PhaseSystemPackages, 0.1, 0.9); err != nil {
		return fmt.Errorf("failed to install Ghostty: %w", err)
	}

	u.log("Ghostty installed successfully using Ubuntu installer")
	return nil
}

// Override InstallManualPackages for Ubuntu to handle Ubuntu-specific installations
func (u *UbuntuDistribution) InstallManualPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- installer.InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	u.log(fmt.Sprintf("Installing manual packages: %s", strings.Join(packages, ", ")))

	for _, pkg := range packages {
		switch pkg {
		case "ghostty":
			if err := u.installGhosttyUbuntu(ctx, sudoPassword, progressChan); err != nil {
				return fmt.Errorf("failed to install ghostty: %w", err)
			}
		default:
			// Use the base ManualPackageInstaller for other packages
			if err := u.ManualPackageInstaller.InstallManualPackages(ctx, []string{pkg}, sudoPassword, progressChan); err != nil {
				return fmt.Errorf("failed to install %s: %w", pkg, err)
			}
		}
	}

	return nil
}
