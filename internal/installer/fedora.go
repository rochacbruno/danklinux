package installer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

type FedoraInstaller struct {
	*BaseInstaller
}

type FedoraPackageInfo struct {
	PackageName string
	Repository  string // "dnf", "copr", "manual"
	COPRRepo    string // COPR repository name if needed
}

func NewFedoraInstaller(logChan chan<- string) *FedoraInstaller {
	return &FedoraInstaller{
		BaseInstaller: NewBaseInstaller(logChan),
	}
}

func (f *FedoraInstaller) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	// Ensure we have dnf-plugins-core for COPR
	f.log("Ensuring dnf-plugins-core is available...")
	if err := f.ensureDnfPlugins(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install dnf-plugins-core: %w", err)
	}

	dnfPkgs, coprPkgs, manualPkgs := f.categorizePackages(dependencies, wm, reinstallFlags)

	// Phase 2: Enable COPR repositories
	if len(coprPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
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
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
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
	if len(coprPkgs) > 0 {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseAURPackages, // Reusing AUR phase for COPR
			Progress:   0.65,
			Step:       fmt.Sprintf("Installing %d COPR packages...", len(coprPkgs)),
			IsComplete: false,
			LogOutput:  fmt.Sprintf("Installing COPR packages: %s", f.getPackageNames(coprPkgs)),
		}
		if err := f.installCOPRPackages(ctx, coprPkgs, sudoPassword, progressChan); err != nil {
			return fmt.Errorf("failed to install COPR packages: %w", err)
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
		if err := f.InstallManualPackages(ctx, manualPkgs, sudoPassword, progressChan); err != nil {
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
	if err := f.postInstallConfig(ctx, wm, sudoPassword, progressChan); err != nil {
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

func (f *FedoraInstaller) ensureDnfPlugins(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
		Progress:    0.06,
		Step:        "Checking dnf-plugins-core...",
		IsComplete:  false,
		LogOutput:   "Checking if dnf-plugins-core is installed",
	}
	
	checkCmd := exec.CommandContext(ctx, "rpm", "-q", "dnf-plugins-core")
	if err := checkCmd.Run(); err == nil {
		f.log("dnf-plugins-core already installed")
		return nil
	}

	f.log("Installing dnf-plugins-core...")
	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
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

func (f *FedoraInstaller) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []FedoraPackageInfo, []string) {
	dnfPkgs := []string{}
	coprPkgs := []FedoraPackageInfo{}
	manualPkgs := []string{}

	packageMap := f.getPackageMap(wm)

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
		case "dnf":
			dnfPkgs = append(dnfPkgs, pkgInfo.PackageName)
		case "copr":
			coprPkgs = append(coprPkgs, pkgInfo)
		case "manual":
			manualPkgs = append(manualPkgs, dep.Name)
		}
	}

	return dnfPkgs, coprPkgs, manualPkgs
}

func (f *FedoraInstaller) enableCOPRRepos(ctx context.Context, coprPkgs []FedoraPackageInfo, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	enabledRepos := make(map[string]bool)
	
	for _, pkg := range coprPkgs {
		if pkg.COPRRepo != "" && !enabledRepos[pkg.COPRRepo] {
			f.log(fmt.Sprintf("Enabling COPR repository: %s", pkg.COPRRepo))
			progressChan <- InstallProgressMsg{
				Phase:       PhaseSystemPackages,
				Progress:    0.20,
				Step:        fmt.Sprintf("Enabling COPR repo %s...", pkg.COPRRepo),
				IsComplete:  false,
				NeedsSudo:   true,
				CommandInfo: fmt.Sprintf("sudo dnf copr enable -y %s", pkg.COPRRepo),
			}

			cmd := exec.CommandContext(ctx, "bash", "-c", 
				fmt.Sprintf("echo '%s' | sudo -S dnf copr enable -y %s 2>&1", sudoPassword, pkg.COPRRepo))
			output, err := cmd.CombinedOutput()
			if err != nil {
				f.logError(fmt.Sprintf("failed to enable COPR repo %s", pkg.COPRRepo), err)
				f.log(fmt.Sprintf("COPR enable command output: %s", string(output)))
				return fmt.Errorf("failed to enable COPR repo %s: %w", pkg.COPRRepo, err)
			}
			f.log(fmt.Sprintf("COPR repo %s enabled successfully: %s", pkg.COPRRepo, string(output)))
			enabledRepos[pkg.COPRRepo] = true
		}
	}

	return nil
}

func (f *FedoraInstaller) installDNFPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	f.log(fmt.Sprintf("Installing DNF packages: %s", strings.Join(packages, ", ")))

	args := []string{"dnf", "install", "-y"}
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
	return f.runWithProgress(cmd, progressChan, PhaseSystemPackages, 0.40, 0.60)
}

func (f *FedoraInstaller) installCOPRPackages(ctx context.Context, coprPkgs []FedoraPackageInfo, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(coprPkgs) == 0 {
		return nil
	}

	packageNames := f.getPackageNames(coprPkgs)
	f.log(fmt.Sprintf("Installing COPR packages: %s", strings.Join(packageNames, ", ")))

	args := []string{"dnf", "install", "-y"}
	args = append(args, packageNames...)

	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    0.70,
		Step:        "Installing COPR packages...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: fmt.Sprintf("sudo %s", strings.Join(args, " ")),
	}

	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return f.runWithProgress(cmd, progressChan, PhaseAURPackages, 0.70, 0.85)
}



func (f *FedoraInstaller) postInstallConfig(ctx context.Context, wm deps.WindowManager, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// Clone DMS config if needed (same as Arch)
	dmsPath := filepath.Join(os.Getenv("HOME"), ".config/quickshell/dms")
	if _, err := os.Stat(dmsPath); os.IsNotExist(err) {
		progressChan <- InstallProgressMsg{
			Phase:       PhaseConfiguration,
			Progress:    0.90,
			Step:        "Installing DankMaterialShell config...",
			IsComplete:  false,
			CommandInfo: "git clone https://github.com/AvengeMedia/DankMaterialShell.git ~/.config/quickshell/dms",
		}

		configDir := filepath.Dir(dmsPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create quickshell config directory: %w", err)
		}

		cloneCmd := exec.CommandContext(ctx, "git", "clone", 
			"https://github.com/AvengeMedia/DankMaterialShell.git", dmsPath)
		if err := cloneCmd.Run(); err != nil {
			return fmt.Errorf("failed to clone DankMaterialShell: %w", err)
		}
	}

	return nil
}

func (f *FedoraInstaller) runWithProgress(cmd *exec.Cmd, progressChan chan<- InstallProgressMsg, phase InstallPhase, startProgress, endProgress float64) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	outputChan := make(chan string, 100)
	done := make(chan error, 1)

	// Read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			f.log(line)
			outputChan <- line
		}
	}()

	// Read stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			f.log(line)
			outputChan <- line
		}
	}()

	go func() {
		done <- cmd.Wait()
		close(outputChan)
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	progress := startProgress
	progressStep := (endProgress - startProgress) / 50
	lastOutput := ""
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case err := <-done:
			if err != nil {
				f.logError("Command execution failed", err)
				f.log(fmt.Sprintf("Last output before failure: %s", lastOutput))
				progressChan <- InstallProgressMsg{
					Phase:      phase,
					Progress:   startProgress,
					Step:       "Command failed",
					IsComplete: false,
					LogOutput:  lastOutput,
					Error:      err,
				}
				return err
			}
			progressChan <- InstallProgressMsg{
				Phase:      phase,
				Progress:   endProgress,
				Step:       "Installation step complete",
				IsComplete: false,
				LogOutput:  lastOutput,
			}
			return nil
		case output, ok := <-outputChan:
			if ok {
				lastOutput = output
				progressChan <- InstallProgressMsg{
					Phase:      phase,
					Progress:   progress,
					Step:       "Installing...",
					IsComplete: false,
					LogOutput:  output,
				}
				timeout.Reset(10 * time.Minute)
			}
		case <-timeout.C:
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			err := fmt.Errorf("installation timed out after 10 minutes")
			progressChan <- InstallProgressMsg{
				Phase:      phase,
				Progress:   startProgress,
				Step:       "Installation timed out",
				IsComplete: false,
				LogOutput:  lastOutput,
				Error:      err,
			}
			return err
		case <-ticker.C:
			if progress < endProgress-0.01 {
				progress += progressStep
				progressChan <- InstallProgressMsg{
					Phase:      phase,
					Progress:   progress,
					Step:       "Installing...",
					IsComplete: false,
					LogOutput:  lastOutput,
				}
			}
		}
	}
}

func (f *FedoraInstaller) getPackageMap(wm deps.WindowManager) map[string]FedoraPackageInfo {
	packageMap := map[string]FedoraPackageInfo{
		// Standard DNF packages
		"git":                     {"git", "dnf", ""},
		"ghostty":                 {"ghostty", "dnf", ""}, // Available in Fedora 41+
		"kitty":                   {"kitty", "dnf", ""},
		"wl-clipboard":            {"wl-clipboard", "dnf", ""},
		"xdg-desktop-portal-gtk":  {"xdg-desktop-portal-gtk", "dnf", ""},
		"mate-polkit":             {"mate-polkit", "dnf", ""},
		"font-inter":              {"google-inter-fonts", "dnf", ""},
		"font-firacode":           {"fira-code-fonts", "dnf", ""},
		
		// COPR packages  
		"quickshell":              {"quickshell", "copr", "errornointernet/quickshell"},
		"matugen":                 {"matugen", "copr", "heus-sueh/packages"},
		
		"cliphist":                {"cliphist", "copr", "atim/cliphist"},
		
		// Manual builds
		"dgop":                    {"dgop", "manual", ""},
		"font-material-symbols":   {"font-material-symbols", "manual", ""},
	}

	// Add window manager specific packages
	switch wm {
	case deps.WindowManagerHyprland:
		packageMap["hyprland"] = FedoraPackageInfo{"hyprland", "copr", "solopasha/hyprland"}
	case deps.WindowManagerNiri:
		packageMap["niri"] = FedoraPackageInfo{"niri", "copr", "yalter/niri-git"}
	}

	return packageMap
}

func (f *FedoraInstaller) getPackageNames(coprPkgs []FedoraPackageInfo) []string {
	names := make([]string, len(coprPkgs))
	for i, pkg := range coprPkgs {
		names[i] = pkg.PackageName
	}
	return names
}

func (f *FedoraInstaller) HasAURHelper() (string, bool) {
	return "dnf", true
}

func (f *FedoraInstaller) InstallAURHelper(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// No AUR helper needed for Fedora - we use COPR instead
	return nil
}

