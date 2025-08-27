// Package deps provides dependency detection functionality for different Linux distributions.
// This file contains the Fedora-specific dependency detector implementation.
package deps

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// FedoraDetector implements dependency detection for Fedora-based distributions.
// It extends BaseDetector with Fedora-specific package detection methods.
type FedoraDetector struct {
	*BaseDetector
}

// NewFedoraDetector creates a new instance of FedoraDetector.
// logChan is used for sending log messages during detection operations.
func NewFedoraDetector(logChan chan<- string) *FedoraDetector {
	return &FedoraDetector{
		BaseDetector: NewBaseDetector(logChan),
	}
}

// DetectDependencies detects all required dependencies for the specified window manager.
// Uses Ghostty as the default terminal emulator.
func (f *FedoraDetector) DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error) {
	return f.DetectDependenciesWithTerminal(ctx, wm, TerminalGhostty)
}

// DetectDependenciesWithTerminal detects all required dependencies with a specific terminal choice.
// This is the main detection method that coordinates all dependency checks in priority order:
// 1. DankMaterialShell (most prominent component)
// 2. Terminal emulator (user's choice)
// 3. Fedora-specific system packages
// 4. Common cross-distro packages
func (f *FedoraDetector) DetectDependenciesWithTerminal(ctx context.Context, wm WindowManager, terminal Terminal) ([]Dependency, error) {
	var deps []Dependency

	// DMS at the top (shell is prominent)
	deps = append(deps, f.detectDMS())
	
	// Terminal with choice support
	deps = append(deps, f.detectSpecificTerminal(terminal))
	
	// Fedora-specific detections
	deps = append(deps, f.detectGit())
	deps = append(deps, f.detectWindowManager(wm))
	deps = append(deps, f.detectQuickshell())
	deps = append(deps, f.detectXDGPortal())
	deps = append(deps, f.detectPolkitAgent())

	// Hyprland-specific tools
	if wm == WindowManagerHyprland {
		deps = append(deps, f.detectHyprlandTools()...)
	}

	// Base detections (common across distros)
	deps = append(deps, f.detectMatugen())
	deps = append(deps, f.detectDgop())
	deps = append(deps, f.detectFonts()...)
	deps = append(deps, f.detectClipboardTools()...)

	return deps, nil
}

// detectGit checks if Git version control system is installed.
// Uses command existence check since Git is available in standard Fedora repos.
func (f *FedoraDetector) detectGit() Dependency {
	status := StatusMissing
	if f.commandExists("git") {
		status = StatusInstalled
	}

	return Dependency{
		Name:        "git",
		Status:      status,
		Description: "Version control system",
		Required:    true,
	}
}

func (f *FedoraDetector) detectWindowManager(wm WindowManager) Dependency {
	switch wm {
	case WindowManagerHyprland:
		status := StatusMissing
		if f.commandExists("hyprland") || f.commandExists("Hyprland") {
			status = StatusInstalled
		}
		return Dependency{
			Name:        "hyprland",
			Status:      status,
			Description: "Dynamic tiling Wayland compositor (via COPR)",
			Required:    true,
		}
	case WindowManagerNiri:
		status := StatusMissing
		if f.commandExists("niri") {
			status = StatusInstalled
		}
		return Dependency{
			Name:        "niri",
			Status:      status,
			Description: "Scrollable-tiling Wayland compositor (via COPR)",
			Required:    true,
		}
	default:
		// Fallback - shouldn't happen
		return Dependency{
			Name:        "unknown-wm",
			Status:      StatusMissing,
			Description: "Unknown window manager",
			Required:    true,
		}
	}
}

func (f *FedoraDetector) detectQuickshell() Dependency {
	if !f.commandExists("qs") {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusMissing,
			Description: "QtQuick based desktop shell toolkit (via COPR)",
			Required:    true,
		}
	}

	cmd := exec.Command("qs", "--version")
	output, err := cmd.Output()
	if err != nil {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusNeedsReinstall,
			Description: "QtQuick based desktop shell toolkit (version check failed)",
			Required:    true,
		}
	}

	versionStr := string(output)
	versionRegex := regexp.MustCompile(`quickshell (\d+\.\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(versionStr)

	if len(matches) < 2 {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusNeedsReinstall,
			Description: "QtQuick based desktop shell toolkit (unknown version)",
			Required:    true,
		}
	}

	version := matches[1]
	if f.versionCompare(version, "0.2.0") >= 0 {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusInstalled,
			Version:     version,
			Description: "QtQuick based desktop shell toolkit (via COPR)",
			Required:    true,
		}
	}

	return Dependency{
		Name:        "quickshell",
		Status:      StatusNeedsUpdate,
		Version:     version,
		Description: "QtQuick based desktop shell toolkit (needs 0.2.0+, via COPR)",
		Required:    true,
	}
}

func (f *FedoraDetector) detectXDGPortal() Dependency {
	status := StatusMissing
	if f.packageInstalled("xdg-desktop-portal-gtk") {
		status = StatusInstalled
	}

	return Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (f *FedoraDetector) detectPolkitAgent() Dependency {
	status := StatusMissing
	if f.packageInstalled("mate-polkit") {
		status = StatusInstalled
	}

	return Dependency{
		Name:        "mate-polkit",
		Status:      status,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (f *FedoraDetector) packageInstalled(pkg string) bool {
	cmd := exec.Command("rpm", "-q", pkg)
	err := cmd.Run()
	return err == nil
}

func (f *FedoraDetector) detectHyprlandTools() []Dependency {
	var deps []Dependency

	tools := []struct {
		name        string
		description string
	}{
		{"grim", "Screenshot utility for Wayland"},
		{"slurp", "Region selection utility for Wayland"},
		{"hyprctl", "Hyprland control utility"},
		{"hyprpicker", "Color picker for Hyprland"},
		{"jq", "JSON processor"},
	}

	for _, tool := range tools {
		status := StatusMissing
		if f.commandExists(tool.name) {
			status = StatusInstalled
		}

		deps = append(deps, Dependency{
			Name:        tool.name,
			Status:      status,
			Description: tool.description,
			Required:    true,
		})
	}

	return deps
}

func (f *FedoraDetector) versionCompare(v1, v2 string) int {
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