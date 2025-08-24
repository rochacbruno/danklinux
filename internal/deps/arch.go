package deps

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type ArchDetector struct {
	*BaseDetector
}

func NewArchDetector(logChan chan<- string) *ArchDetector {
	return &ArchDetector{
		BaseDetector: NewBaseDetector(logChan),
	}
}

func (a *ArchDetector) DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error) {
	var deps []Dependency

	// Arch-specific detections
	deps = append(deps, a.detectGit())
	deps = append(deps, a.detectWindowManager(wm)) // Only detect chosen WM
	deps = append(deps, a.detectQuickshell())
	deps = append(deps, a.detectXDGPortal())
	deps = append(deps, a.detectPolkitAgent())

	// Base detections (common across distros)
	deps = append(deps, a.detectMatugen())
	deps = append(deps, a.detectDgop())
	deps = append(deps, a.detectDMS())
	deps = append(deps, a.detectTerminal())
	deps = append(deps, a.detectCursorTheme())
	deps = append(deps, a.detectFonts()...)
	deps = append(deps, a.detectClipboardTools()...)

	return deps, nil
}

func (a *ArchDetector) detectGit() Dependency {
	status := StatusMissing
	if a.commandExists("git") {
		status = StatusInstalled
	}

	return Dependency{
		Name:        "git",
		Status:      status,
		Description: "Version control system",
		Required:    true,
	}
}

func (a *ArchDetector) detectWindowManager(wm WindowManager) Dependency {
	switch wm {
	case WindowManagerHyprland:
		status := StatusMissing
		if a.commandExists("hyprland") || a.commandExists("Hyprland") {
			status = StatusInstalled
		}
		return Dependency{
			Name:        "hyprland",
			Status:      status,
			Description: "Dynamic tiling Wayland compositor",
			Required:    true,
		}
	case WindowManagerNiri:
		status := StatusMissing
		if a.commandExists("niri") {
			status = StatusInstalled
		}
		return Dependency{
			Name:        "niri",
			Status:      status,
			Description: "Scrollable-tiling Wayland compositor",
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

func (a *ArchDetector) detectQuickshell() Dependency {
	if !a.commandExists("qs") {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusMissing,
			Description: "QtQuick based desktop shell toolkit",
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
	if a.versionCompare(version, "0.2.0") >= 0 {
		return Dependency{
			Name:        "quickshell",
			Status:      StatusInstalled,
			Version:     version,
			Description: "QtQuick based desktop shell toolkit",
			Required:    true,
		}
	}

	return Dependency{
		Name:        "quickshell",
		Status:      StatusNeedsUpdate,
		Version:     version,
		Description: "QtQuick based desktop shell toolkit (needs 0.2.0+)",
		Required:    true,
	}
}

func (a *ArchDetector) detectXDGPortal() Dependency {
	status := StatusMissing
	if a.packageInstalled("xdg-desktop-portal-gtk") {
		status = StatusInstalled
	}

	return Dependency{
		Name:        "xdg-desktop-portal-gtk",
		Status:      status,
		Description: "Desktop integration portal for GTK",
		Required:    true,
	}
}

func (a *ArchDetector) detectPolkitAgent() Dependency {
	polkitPaths := []string{
		"/usr/lib/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/lib/polkit-gnome/polkit-gnome-authentication-agent-1",
		"/usr/lib/polkit-kde-authentication-agent-1",
	}

	for _, path := range polkitPaths {
		if _, err := os.Stat(path); err == nil {
			return Dependency{
				Name:        "polkit-agent",
				Status:      StatusInstalled,
				Description: "PolicyKit authentication agent",
				Required:    true,
			}
		}
	}

	return Dependency{
		Name:        "mate-polkit",
		Status:      StatusMissing,
		Description: "PolicyKit authentication agent",
		Required:    true,
	}
}

func (a *ArchDetector) packageInstalled(pkg string) bool {
	cmd := exec.Command("pacman", "-Q", pkg)
	err := cmd.Run()
	return err == nil
}

func (a *ArchDetector) versionCompare(v1, v2 string) int {
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
