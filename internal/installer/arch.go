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

type ArchInstaller struct {
	logChan chan<- string
}

func NewArchInstaller(logChan chan<- string) *ArchInstaller {
	return &ArchInstaller{
		logChan: logChan,
	}
}

func (a *ArchInstaller) InstallPackages(ctx context.Context, dependencies []deps.Dependency, wm deps.WindowManager, sudoPassword string, reinstallFlags map[string]bool, progressChan chan<- InstallProgressMsg) error {
	// Phase 1: Check Prerequisites
	progressChan <- InstallProgressMsg{
		Phase:      PhasePrerequisites,
		Progress:   0.05,
		Step:       "Checking system prerequisites...",
		IsComplete: false,
		LogOutput:  "Starting prerequisite check...",
	}

	// Ensure we have base-devel for building packages
	a.log("Checking base-devel installation...")
	if err := a.ensureBaseDevel(ctx, sudoPassword, progressChan); err != nil {
		return fmt.Errorf("failed to install base-devel: %w", err)
	}


	systemPkgs, aurPkgs := a.categorizePackages(dependencies, wm, reinstallFlags)

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
	} else {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseSystemPackages,
			Progress:   0.60,
			Step:       "No system packages to install",
			IsComplete: false,
			LogOutput:  "All system packages already installed or not needed",
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
	} else {
		progressChan <- InstallProgressMsg{
			Phase:      PhaseAURPackages,
			Progress:   0.80,
			Step:       "No AUR packages to install",
			IsComplete: false,
			LogOutput:  "All AUR packages already installed or not needed",
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


func (a *ArchInstaller) ensureBaseDevel(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// Check if base-devel is already installed
	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
		Progress:    0.06,
		Step:        "Checking base-devel...",
		IsComplete:  false,
		LogOutput:   "Checking if base-devel is installed",
	}
	
	checkCmd := exec.CommandContext(ctx, "pacman", "-Qq", "base-devel")
	if err := checkCmd.Run(); err == nil {
		a.log("base-devel already installed")
		progressChan <- InstallProgressMsg{
			Phase:       PhasePrerequisites,
			Progress:    0.10,
			Step:        "base-devel already installed",
			IsComplete:  false,
			LogOutput:   "base-devel is already installed on the system",
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

	// Use echo to pipe password to sudo
	cmd := exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo '%s' | sudo -S pacman -S --needed --noconfirm base-devel", sudoPassword))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install base-devel: %w", err)
	}

	progressChan <- InstallProgressMsg{
		Phase:       PhasePrerequisites,
		Progress:    0.12,
		Step:        "base-devel installation complete",
		IsComplete:  false,
		LogOutput:   "base-devel successfully installed",
	}

	return nil
}

func (a *ArchInstaller) categorizePackages(dependencies []deps.Dependency, wm deps.WindowManager, reinstallFlags map[string]bool) ([]string, []string) {
	systemPkgs := []string{}
	aurPkgs := []string{}

	packageMap := a.getPackageMap(wm)

	for _, dep := range dependencies {
		// Skip installed packages unless marked for reinstall
		if dep.Status == deps.StatusInstalled && !reinstallFlags[dep.Name] {
			continue // Skip already installed
		}

		pkgInfo, exists := packageMap[dep.Name]
		if !exists {
			a.log(fmt.Sprintf("Warning: No package mapping for %s", dep.Name))
			continue
		}

		if pkgInfo.IsAUR {
			aurPkgs = append(aurPkgs, pkgInfo.PackageName)
		} else {
			systemPkgs = append(systemPkgs, pkgInfo.PackageName)
		}
	}

	return systemPkgs, aurPkgs
}

func (a *ArchInstaller) installSystemPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
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

	// Use echo to pipe password to sudo
	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	return a.runWithProgress(cmd, progressChan, PhaseSystemPackages, 0.40, 0.60)
}

func (a *ArchInstaller) installAURPackages(ctx context.Context, packages []string, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	if len(packages) == 0 {
		return nil
	}

	a.log(fmt.Sprintf("Installing AUR packages manually: %s", strings.Join(packages, ", ")))

	baseProgress := 0.65
	progressStep := 0.15 / float64(len(packages)) // Distribute progress across packages

	for i, pkg := range packages {
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

func (a *ArchInstaller) installSingleAURPackage(ctx context.Context, pkg, sudoPassword string, progressChan chan<- InstallProgressMsg, startProgress, endProgress float64) error {
	// Create temporary directory for this package
	tmpDir := fmt.Sprintf("/tmp/aur-build-%s", pkg)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone the AUR package
	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.1*(endProgress-startProgress),
		Step:        fmt.Sprintf("Cloning %s from AUR...", pkg),
		IsComplete:  false,
		CommandInfo: fmt.Sprintf("git clone https://aur.archlinux.org/%s.git", pkg),
	}

	cloneCmd := exec.CommandContext(ctx, "git", "clone", fmt.Sprintf("https://aur.archlinux.org/%s.git", pkg), filepath.Join(tmpDir, pkg))
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("failed to clone %s: %w", pkg, err)
	}

	packageDir := filepath.Join(tmpDir, pkg)

	// Build the package
	progressChan <- InstallProgressMsg{
		Phase:       PhaseAURPackages,
		Progress:    startProgress + 0.4*(endProgress-startProgress),
		Step:        fmt.Sprintf("Building %s...", pkg),
		IsComplete:  false,
		CommandInfo: "makepkg -s --noconfirm",
	}

	buildCmd := exec.CommandContext(ctx, "makepkg", "-s", "--noconfirm")
	buildCmd.Dir = packageDir
	buildCmd.Env = append(os.Environ(), "PKGEXT=.pkg.tar") // Disable compression for speed

	if err := buildCmd.Run(); err != nil {
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

	// Install the built package
	installArgs := []string{"pacman", "-U", "--noconfirm"}
	installArgs = append(installArgs, files...)
	
	cmdStr := fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, strings.Join(installArgs, " "))
	installCmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)

	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install built package %s: %w", pkg, err)
	}

	a.log(fmt.Sprintf("Successfully installed AUR package: %s", pkg))
	return nil
}

func (a *ArchInstaller) postInstallConfig(ctx context.Context, wm deps.WindowManager, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// Clone DMS config if needed
	dmsPath := filepath.Join(os.Getenv("HOME"), ".config/quickshell/dms")
	if _, err := os.Stat(dmsPath); os.IsNotExist(err) {
		progressChan <- InstallProgressMsg{
			Phase:       PhaseConfiguration,
			Progress:    0.90,
			Step:        "Installing DankMaterialShell config...",
			IsComplete:  false,
			CommandInfo: "git clone https://github.com/AvengeMedia/DankMaterialShell.git ~/.config/quickshell/dms",
		}

		// Ensure quickshell config dir exists
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

func (a *ArchInstaller) runWithProgress(cmd *exec.Cmd, progressChan chan<- InstallProgressMsg, phase InstallPhase, startProgress, endProgress float64) error {
	// Create pipes to capture output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Channel to collect all output
	outputChan := make(chan string, 100)
	done := make(chan error, 1)

	// Read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			a.log(line)
			outputChan <- line
		}
	}()

	// Read stderr  
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			a.log(line)
			outputChan <- line
		}
	}()

	// Wait for command to complete
	go func() {
		done <- cmd.Wait()
		close(outputChan)
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	progress := startProgress
	progressStep := (endProgress - startProgress) / 50 // More granular progress
	lastOutput := ""

	// Add timeout for stuck installations
	timeout := time.NewTimer(10 * time.Minute) // 10 minute timeout
	defer timeout.Stop()

	for {
		select {
		case err := <-done:
			if err != nil {
				a.logError("Command execution failed", err)
				a.log(fmt.Sprintf("Last output before failure: %s", lastOutput))
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
				// Reset timeout on new output
				timeout.Reset(10 * time.Minute)
			}
		case <-timeout.C:
			// Kill the process if it's taking too long
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


type PackageInfo struct {
	PackageName string
	IsAUR       bool
}

func (a *ArchInstaller) getPackageMap(wm deps.WindowManager) map[string]PackageInfo {
	packageMap := map[string]PackageInfo{
		"git":                     {"git", false},
		"quickshell":              {"quickshell-git", true},
		"matugen":                 {"matugen-bin", true},
		"dgop":                    {"dgop", true},
		"ghostty":                 {"ghostty", false},
		"cliphist":                {"cliphist", false},
		"wl-clipboard":            {"wl-clipboard", false},
		"xdg-desktop-portal-gtk":  {"xdg-desktop-portal-gtk", false},
		"mate-polkit":             {"mate-polkit", false},
		"font-material-symbols":   {"ttf-material-symbols-variable-git", true},
		"font-inter":              {"inter-font", false},
		"font-firacode":           {"ttf-fira-code", false},
	}

	// Add window manager specific packages
	switch wm {
	case deps.WindowManagerHyprland:
		packageMap["hyprland"] = PackageInfo{"hyprland", false}
	case deps.WindowManagerNiri:
		packageMap["niri"] = PackageInfo{"niri-git", true}
	}

	return packageMap
}

func (a *ArchInstaller) log(message string) {
	if a.logChan != nil {
		a.logChan <- message
	}
}

func (a *ArchInstaller) logError(message string, err error) {
	errorMsg := fmt.Sprintf("ERROR: %s: %v", message, err)
	a.log(errorMsg)
}