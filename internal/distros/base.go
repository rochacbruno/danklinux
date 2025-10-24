package distros

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/AvengeMedia/danklinux/internal/deps"
	"github.com/AvengeMedia/danklinux/internal/version"
)

const forceQuickshellGit = false
const forceDMSGit = false

// BaseDistribution provides common functionality for all distributions
type BaseDistribution struct {
	logChan      chan<- string
	fontDetector *deps.FontDetector
}

// NewBaseDistribution creates a new base distribution
func NewBaseDistribution(logChan chan<- string) *BaseDistribution {
	return &BaseDistribution{
		logChan:      logChan,
		fontDetector: deps.NewFontDetector(logChan),
	}
}

// Common helper methods
func (b *BaseDistribution) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (b *BaseDistribution) CommandExists(cmd string) bool {
	return b.commandExists(cmd)
}

func (b *BaseDistribution) log(message string) {
	if b.logChan != nil {
		b.logChan <- message
	}
}

func (b *BaseDistribution) logError(message string, err error) {
	errorMsg := fmt.Sprintf("ERROR: %s: %v", message, err)
	b.log(errorMsg)
}

// Common dependency detection methods
func (b *BaseDistribution) detectGit() deps.Dependency {
	status := deps.StatusMissing
	if b.commandExists("git") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "git",
		Status:      status,
		Description: "Version control system",
		Required:    true,
	}
}

func (b *BaseDistribution) detectMatugen() deps.Dependency {
	status := deps.StatusMissing
	if b.commandExists("matugen") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "matugen",
		Status:      status,
		Description: "Material Design color generation tool",
		Required:    true,
	}
}

func (b *BaseDistribution) detectDgop() deps.Dependency {
	status := deps.StatusMissing
	if b.commandExists("dgop") {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "dgop",
		Status:      status,
		Description: "Desktop portal management tool",
		Required:    true,
	}
}

func (b *BaseDistribution) detectDMS() deps.Dependency {
	dmsPath := filepath.Join(os.Getenv("HOME"), ".config/quickshell/dms")

	status := deps.StatusMissing
	currentVersion := ""

	if _, err := os.Stat(dmsPath); err == nil {
		status = deps.StatusInstalled

		// Only get current version, don't check for updates (lazy loading)
		current, err := version.GetCurrentDMSVersion()
		if err == nil {
			currentVersion = current
		}
	}

	dep := deps.Dependency{
		Name:        "dms (DankMaterialShell)",
		Status:      status,
		Description: "Desktop Management System configuration",
		Required:    true,
	}

	if currentVersion != "" {
		dep.Version = currentVersion
	}

	return dep
}

func (b *BaseDistribution) detectSpecificTerminal(terminal deps.Terminal) deps.Dependency {
	switch terminal {
	case deps.TerminalGhostty:
		status := deps.StatusMissing
		if b.commandExists("ghostty") {
			status = deps.StatusInstalled
		}
		return deps.Dependency{
			Name:        "ghostty",
			Status:      status,
			Description: "A fast, native terminal emulator built in Zig.",
			Required:    true,
		}
	case deps.TerminalKitty:
		status := deps.StatusMissing
		if b.commandExists("kitty") {
			status = deps.StatusInstalled
		}
		return deps.Dependency{
			Name:        "kitty",
			Status:      status,
			Description: "A feature-rich, customizable terminal emulator.",
			Required:    true,
		}
	case deps.TerminalAlacritty:
		status := deps.StatusMissing
		if b.commandExists("alacritty") {
			status = deps.StatusInstalled
		}
		return deps.Dependency{
			Name:        "alacritty",
			Status:      status,
			Description: "A simple terminal emulator. (No dynamic theming)",
			Required:    true,
		}
	default:
		return b.detectSpecificTerminal(deps.TerminalGhostty)
	}
}

func (b *BaseDistribution) detectClipboardTools() []deps.Dependency {
	var dependencies []deps.Dependency

	cliphist := deps.StatusMissing
	if b.commandExists("cliphist") {
		cliphist = deps.StatusInstalled
	}

	wlClipboard := deps.StatusMissing
	if b.commandExists("wl-copy") && b.commandExists("wl-paste") {
		wlClipboard = deps.StatusInstalled
	}

	dependencies = append(dependencies,
		deps.Dependency{
			Name:        "cliphist",
			Status:      cliphist,
			Description: "Wayland clipboard manager",
			Required:    true,
		},
		deps.Dependency{
			Name:        "wl-clipboard",
			Status:      wlClipboard,
			Description: "Wayland clipboard utilities",
			Required:    true,
		},
	)

	return dependencies
}

func (b *BaseDistribution) detectFonts() []deps.Dependency {
	requiredFonts := []string{
		"material-symbols",
		"inter",
		"firacode",
	}

	var dependencies []deps.Dependency

	for _, font := range requiredFonts {
		found, _ := b.fontDetector.DetectFont(font)
		status := deps.StatusMissing
		if found {
			status = deps.StatusInstalled
		}

		dependencies = append(dependencies, deps.Dependency{
			Name:        "font-" + font,
			Status:      status,
			Description: strings.Title(font) + " font family",
			Required:    true,
		})
	}

	return dependencies
}

func (b *BaseDistribution) detectHyprlandTools() []deps.Dependency {
	var dependencies []deps.Dependency

	tools := []struct {
		name        string
		description string
	}{
		{"grim", "Screenshot utility for Wayland"},
		{"slurp", "Region selection utility for Wayland"},
		{"hyprctl", "Hyprland control utility"},
		{"hyprpicker", "Color picker for Hyprland"},
		{"grimblast", "Screenshot script for Hyprland"},
		{"jq", "JSON processor"},
	}

	for _, tool := range tools {
		status := deps.StatusMissing
		if b.commandExists(tool.name) {
			status = deps.StatusInstalled
		}

		dependencies = append(dependencies, deps.Dependency{
			Name:        tool.name,
			Status:      status,
			Description: tool.description,
			Required:    true,
		})
	}

	return dependencies
}

func (b *BaseDistribution) detectQuickshell() deps.Dependency {
	if !b.commandExists("qs") {
		return deps.Dependency{
			Name:        "quickshell",
			Status:      deps.StatusMissing,
			Description: "QtQuick based desktop shell toolkit",
			Required:    true,
			Variant:     deps.VariantStable,
			CanToggle:   true,
		}
	}

	cmd := exec.Command("qs", "--version")
	output, err := cmd.Output()
	if err != nil {
		return deps.Dependency{
			Name:        "quickshell",
			Status:      deps.StatusNeedsReinstall,
			Description: "QtQuick based desktop shell toolkit (version check failed)",
			Required:    true,
			Variant:     deps.VariantStable,
			CanToggle:   true,
		}
	}

	versionStr := string(output)
	versionRegex := regexp.MustCompile(`quickshell (\d+\.\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(versionStr)

	if len(matches) < 2 {
		return deps.Dependency{
			Name:        "quickshell",
			Status:      deps.StatusNeedsReinstall,
			Description: "QtQuick based desktop shell toolkit (unknown version)",
			Required:    true,
			Variant:     deps.VariantStable,
			CanToggle:   true,
		}
	}

	version := matches[1]
	variant := deps.VariantStable
	if strings.Contains(versionStr, "git") || strings.Contains(versionStr, "+") {
		variant = deps.VariantGit
	}

	if b.versionCompare(version, "0.2.0") >= 0 {
		return deps.Dependency{
			Name:        "quickshell",
			Status:      deps.StatusInstalled,
			Version:     version,
			Description: "QtQuick based desktop shell toolkit",
			Required:    true,
			Variant:     variant,
			CanToggle:   true,
		}
	}

	return deps.Dependency{
		Name:        "quickshell",
		Status:      deps.StatusNeedsUpdate,
		Variant:     variant,
		CanToggle:   true,
		Version:     version,
		Description: "QtQuick based desktop shell toolkit (needs 0.2.0+)",
		Required:    true,
	}
}

func (b *BaseDistribution) detectWindowManager(wm deps.WindowManager) deps.Dependency {
	switch wm {
	case deps.WindowManagerHyprland:
		status := deps.StatusMissing
		variant := deps.VariantStable
		version := ""

		if b.commandExists("hyprland") || b.commandExists("Hyprland") {
			status = deps.StatusInstalled
			cmd := exec.Command("hyprctl", "version")
			if output, err := cmd.Output(); err == nil {
				outStr := string(output)
				if strings.Contains(outStr, "git") || strings.Contains(outStr, "dirty") {
					variant = deps.VariantGit
				}
				if versionRegex := regexp.MustCompile(`v(\d+\.\d+\.\d+)`); versionRegex.MatchString(outStr) {
					matches := versionRegex.FindStringSubmatch(outStr)
					if len(matches) > 1 {
						version = matches[1]
					}
				}
			}
		}
		return deps.Dependency{
			Name:        "hyprland",
			Status:      status,
			Version:     version,
			Description: "Dynamic tiling Wayland compositor",
			Required:    true,
			Variant:     variant,
			CanToggle:   true,
		}
	case deps.WindowManagerNiri:
		status := deps.StatusMissing
		variant := deps.VariantStable
		version := ""

		if b.commandExists("niri") {
			status = deps.StatusInstalled
			cmd := exec.Command("niri", "--version")
			if output, err := cmd.Output(); err == nil {
				outStr := string(output)
				if strings.Contains(outStr, "git") || strings.Contains(outStr, "+") {
					variant = deps.VariantGit
				}
				if versionRegex := regexp.MustCompile(`niri (\d+\.\d+)`); versionRegex.MatchString(outStr) {
					matches := versionRegex.FindStringSubmatch(outStr)
					if len(matches) > 1 {
						version = matches[1]
					}
				}
			}
		}
		return deps.Dependency{
			Name:        "niri",
			Status:      status,
			Version:     version,
			Description: "Scrollable-tiling Wayland compositor",
			Required:    true,
			Variant:     variant,
			CanToggle:   true,
		}
	default:
		return deps.Dependency{
			Name:        "unknown-wm",
			Status:      deps.StatusMissing,
			Description: "Unknown window manager",
			Required:    true,
		}
	}
}

// Version comparison helper
func (b *BaseDistribution) versionCompare(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		if parts1[i] < parts2[i] {
			return -1
		}
		if parts1[i] > parts2[i] {
			return 1
		}
	}

	if len(parts1) < len(parts2) {
		return -1
	}
	if len(parts1) > len(parts2) {
		return 1
	}

	return 0
}

// Common installation helper
func (b *BaseDistribution) runWithProgress(cmd *exec.Cmd, progressChan chan<- InstallProgressMsg, phase InstallPhase, startProgress, endProgress float64) error {
	return b.runWithProgressStep(cmd, progressChan, phase, startProgress, endProgress, "Installing...")
}

func (b *BaseDistribution) runWithProgressStep(cmd *exec.Cmd, progressChan chan<- InstallProgressMsg, phase InstallPhase, startProgress, endProgress float64, stepMessage string) error {
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

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			b.log(line)
			outputChan <- line
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			b.log(line)
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
				b.logError("Command execution failed", err)
				b.log(fmt.Sprintf("Last output before failure: %s", lastOutput))
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
					Step:       stepMessage,
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

// installDMSBinary installs the DMS binary from GitHub releases
func (b *BaseDistribution) installDMSBinary(ctx context.Context, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	b.log("Installing/updating DMS binary...")

	// Detect architecture
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
	case "arm64":
	default:
		return fmt.Errorf("unsupported architecture for DMS: %s", arch)
	}

	progressChan <- InstallProgressMsg{
		Phase:       PhaseConfiguration,
		Progress:    0.80,
		Step:        "Downloading DMS binary...",
		IsComplete:  false,
		CommandInfo: fmt.Sprintf("Downloading dms-%s.gz", arch),
	}

	// Get latest release version
	latestVersionCmd := exec.CommandContext(ctx, "bash", "-c",
		`curl -s https://api.github.com/repos/AvengeMedia/danklinux/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'`)
	versionOutput, err := latestVersionCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get latest DMS version: %w", err)
	}
	version := strings.TrimSpace(string(versionOutput))
	if version == "" {
		return fmt.Errorf("could not determine latest DMS version")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	tmpDir := filepath.Join(homeDir, ".cache", "dankinstall", "manual-builds")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download the gzipped binary
	downloadURL := fmt.Sprintf("https://github.com/AvengeMedia/danklinux/releases/download/%s/dms-%s.gz", version, arch)
	gzPath := filepath.Join(tmpDir, "dms.gz")

	downloadCmd := exec.CommandContext(ctx, "curl", "-L", downloadURL, "-o", gzPath)
	if err := downloadCmd.Run(); err != nil {
		return fmt.Errorf("failed to download DMS binary: %w", err)
	}

	progressChan <- InstallProgressMsg{
		Phase:       PhaseConfiguration,
		Progress:    0.85,
		Step:        "Extracting DMS binary...",
		IsComplete:  false,
		CommandInfo: "gunzip dms.gz",
	}

	// Extract the binary
	extractCmd := exec.CommandContext(ctx, "gunzip", gzPath)
	if err := extractCmd.Run(); err != nil {
		return fmt.Errorf("failed to extract DMS binary: %w", err)
	}

	binaryPath := filepath.Join(tmpDir, "dms")

	// Make it executable
	chmodCmd := exec.CommandContext(ctx, "chmod", "+x", binaryPath)
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make DMS binary executable: %w", err)
	}

	progressChan <- InstallProgressMsg{
		Phase:       PhaseConfiguration,
		Progress:    0.88,
		Step:        "Installing DMS to /usr/local/bin...",
		IsComplete:  false,
		NeedsSudo:   true,
		CommandInfo: "sudo cp dms /usr/local/bin/",
	}

	// Install to /usr/local/bin
	installCmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo '%s' | sudo -S cp %s /usr/local/bin/dms", sudoPassword, binaryPath))
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install DMS binary: %w", err)
	}

	b.log("DMS binary installed successfully")
	return nil
}

// Post-installation configuration
func (b *BaseDistribution) postInstallConfig(ctx context.Context, _ deps.WindowManager, sudoPassword string, progressChan chan<- InstallProgressMsg) error {
	// DMS installation is now handled in the package installer itself
	return nil
}
