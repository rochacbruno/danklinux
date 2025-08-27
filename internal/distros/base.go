package distros

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AvengeMedia/dankinstall/internal/deps"
	"github.com/AvengeMedia/dankinstall/internal/installer"
)

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
	if _, err := os.Stat(dmsPath); err == nil {
		status = deps.StatusInstalled
	}

	return deps.Dependency{
		Name:        "dms (DankMaterialShell)",
		Status:      status,
		Description: "Desktop Management System configuration",
		Required:    true,
	}
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
		}
	}

	version := matches[1]
	if b.versionCompare(version, "0.2.0") >= 0 {
		return deps.Dependency{
			Name:        "quickshell",
			Status:      deps.StatusInstalled,
			Version:     version,
			Description: "QtQuick based desktop shell toolkit",
			Required:    true,
		}
	}

	return deps.Dependency{
		Name:        "quickshell",
		Status:      deps.StatusNeedsUpdate,
		Version:     version,
		Description: "QtQuick based desktop shell toolkit (needs 0.2.0+)",
		Required:    true,
	}
}

func (b *BaseDistribution) detectWindowManager(wm deps.WindowManager) deps.Dependency {
	switch wm {
	case deps.WindowManagerHyprland:
		status := deps.StatusMissing
		if b.commandExists("hyprland") || b.commandExists("Hyprland") {
			status = deps.StatusInstalled
		}
		return deps.Dependency{
			Name:        "hyprland",
			Status:      status,
			Description: "Dynamic tiling Wayland compositor",
			Required:    true,
		}
	case deps.WindowManagerNiri:
		status := deps.StatusMissing
		if b.commandExists("niri") {
			status = deps.StatusInstalled
		}
		return deps.Dependency{
			Name:        "niri",
			Status:      status,
			Description: "Scrollable-tiling Wayland compositor",
			Required:    true,
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
func (b *BaseDistribution) runWithProgress(cmd *exec.Cmd, progressChan chan<- installer.InstallProgressMsg, phase installer.InstallPhase, startProgress, endProgress float64) error {
	return b.runWithProgressStep(cmd, progressChan, phase, startProgress, endProgress, "Installing...")
}

func (b *BaseDistribution) runWithProgressStep(cmd *exec.Cmd, progressChan chan<- installer.InstallProgressMsg, phase installer.InstallPhase, startProgress, endProgress float64, stepMessage string) error {
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
			b.log(line)
			outputChan <- line
		}
	}()

	// Read stderr
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
				progressChan <- installer.InstallProgressMsg{
					Phase:      phase,
					Progress:   startProgress,
					Step:       "Command failed",
					IsComplete: false,
					LogOutput:  lastOutput,
					Error:      err,
				}
				return err
			}
			progressChan <- installer.InstallProgressMsg{
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
				progressChan <- installer.InstallProgressMsg{
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
			progressChan <- installer.InstallProgressMsg{
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
				progressChan <- installer.InstallProgressMsg{
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

// Post-installation configuration
func (b *BaseDistribution) postInstallConfig(ctx context.Context, _ deps.WindowManager, _ string, progressChan chan<- installer.InstallProgressMsg) error {
	// Clone DMS config if needed
	dmsPath := filepath.Join(os.Getenv("HOME"), ".config/quickshell/dms")
	if _, err := os.Stat(dmsPath); os.IsNotExist(err) {
		progressChan <- installer.InstallProgressMsg{
			Phase:       installer.PhaseConfiguration,
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