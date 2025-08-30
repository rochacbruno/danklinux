package dms

import (
	"fmt"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/tui"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderMainMenu() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		MarginBottom(1)

	b.WriteString(headerStyle.Render("DankLinux Manager"))
	b.WriteString("\n")

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00D4AA")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))

	for i, item := range m.menuItems {
		if i == m.selectedItem {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("▶ %s", item.Label)))
		} else {
			b.WriteString(normalStyle.Render(fmt.Sprintf("  %s", item.Label)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1)

	instructions := "↑/↓: Navigate, Enter: Select, q/Esc: Exit"
	b.WriteString(instructionStyle.Render(instructions))

	return b.String()
}

func (m Model) renderUpdateView() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		MarginBottom(1)

	b.WriteString(headerStyle.Render("Update Dependencies"))

	if len(m.updateDeps) == 0 {
		b.WriteString("Loading dependencies...\n")
		return b.String()
	}

	// Categorize dependencies
	categories := m.categorizeDependencies()
	currentIndex := 0

	for _, category := range []string{"Shell", "Shared Components", "Hyprland Components", "Niri Components"} {
		deps, exists := categories[category]
		if !exists || len(deps) == 0 {
			continue
		}

		// Category header
		categoryStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7060ac")).
			Bold(true).
			MarginTop(1)

		b.WriteString(categoryStyle.Render(category + ":"))
		b.WriteString("\n")

		// Dependencies in this category
		for _, dep := range deps {
			var statusText, icon, reinstallMarker string
			var style lipgloss.Style

			isDMS := dep.Name == "dms (DankMaterialShell)"

			if m.updateToggles[dep.Name] {
				reinstallMarker = "🔄 "
				if dep.Status == 0 { // StatusMissing
					statusText = "Will be installed"
				} else {
					statusText = "Will be upgraded"
				}
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // Warning color
			} else {
				switch dep.Status {
				case 1: // StatusInstalled
					icon = "✓"
					if isDMS {
						statusText = "Will be upgraded"
						style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // Warning for DMS default
					} else {
						statusText = "Installed"
						style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")) // Neutral white
					}
				case 0: // StatusMissing
					icon = "○"
					statusText = "Not installed"
					style = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")) // Gray
				case 2: // StatusNeedsUpdate
					icon = "△"
					statusText = "Will be upgraded"
					style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // Warning
				case 3: // StatusNeedsReinstall
					icon = "!"
					statusText = "Will be upgraded"
					style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")) // Warning
				}
			}

			line := fmt.Sprintf("%s%s%-25s %s", reinstallMarker, icon, dep.Name, statusText)

			if currentIndex == m.selectedUpdateDep {
				line = "▶ " + line
				selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7060ac")).Bold(true)
				b.WriteString(selectedStyle.Render(line))
			} else {
				line = "  " + line
				b.WriteString(style.Render(line))
			}
			b.WriteString("\n")
			currentIndex++
		}
	}

	b.WriteString("\n")
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1)

	instructions := "↑/↓: Navigate, Space: Toggle, Enter: Update Selected, Esc: Back"
	b.WriteString(instructionStyle.Render(instructions))

	return b.String()
}

func (m Model) renderInstallWMView() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		MarginBottom(1)

	b.WriteString(headerStyle.Render("Installing Window Manager..."))
	b.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))

	b.WriteString(normalStyle.Render("Installation in progress..."))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("This will install the selected window manager and its dependencies."))
	b.WriteString("\n\n")

	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1)

	instructions := "Please wait... Press Esc to cancel"
	b.WriteString(instructionStyle.Render(instructions))

	return b.String()
}

func (m Model) renderShellView() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		MarginBottom(1)

	b.WriteString(headerStyle.Render("Shell"))
	b.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))

	b.WriteString(normalStyle.Render("Opening interactive shell..."))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("This will launch a shell with DMS environment loaded."))
	b.WriteString("\n\n")

	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1)

	instructions := "Press any key to launch shell, Esc: Back"
	b.WriteString(instructionStyle.Render(instructions))

	return b.String()
}

func (m Model) renderAboutView() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		MarginBottom(1)

	b.WriteString(headerStyle.Render("About DankMaterialShell"))
	b.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))

	b.WriteString(normalStyle.Render(fmt.Sprintf("DMS Management Interface v%s", m.version)))
	b.WriteString("\n\n")
	b.WriteString(normalStyle.Render("DankMaterialShell is a comprehensive desktop environment"))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("built around Quickshell, providing a modern Material Design"))
	b.WriteString("\n")
	b.WriteString(normalStyle.Render("experience for Wayland compositors."))
	b.WriteString("\n\n")

	b.WriteString(normalStyle.Render("Components:"))
	b.WriteString("\n")
	for _, dep := range m.dependencies {
		status := "✗"
		if dep.Status == 1 {
			status = "✓"
		}
		b.WriteString(normalStyle.Render(fmt.Sprintf("  %s %s", status, dep.Name)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1)

	instructions := "Esc: Back to main menu"
	b.WriteString(instructionStyle.Render(instructions))

	return b.String()
}

func (m Model) renderBanner() string {
	theme := tui.TerminalTheme()

	logo := `
██████╗  █████╗ ███╗   ██╗██╗  ██╗
██╔══██╗██╔══██╗████╗  ██║██║ ██╔╝
██║  ██║███████║██╔██╗ ██║█████╔╝ 
██║  ██║██╔══██║██║╚██╗██║██╔═██╗ 
██████╔╝██║  ██║██║ ╚████║██║  ██╗
╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝`

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Primary)).
		Bold(true).
		MarginBottom(1)

	return titleStyle.Render(logo)
}

func (m Model) categorizeDependencies() map[string][]DependencyInfo {
	categories := map[string][]DependencyInfo{
		"Shell":               {},
		"Shared Components":   {},
		"Hyprland Components": {},
		"Niri Components":     {},
	}

	// System dependencies to exclude from update list
	excludeList := map[string]bool{
		"git":                         true,
		"polkit-agent":                true,
		"jq":                          true,
		"xdg-desktop-portal":          true,
		"xdg-desktop-portal-wlr":      true,
		"xdg-desktop-portal-hyprland": true,
		"xdg-desktop-portal-gtk":      true,
	}

	for _, dep := range m.updateDeps {
		// Skip system dependencies
		if excludeList[dep.Name] {
			continue
		}

		switch dep.Name {
		case "dms (DankMaterialShell)", "quickshell":
			categories["Shell"] = append(categories["Shell"], dep)
		case "hyprland", "grim", "slurp", "hyprctl", "hyprpicker", "grimblast":
			categories["Hyprland Components"] = append(categories["Hyprland Components"], dep)
		case "niri":
			categories["Niri Components"] = append(categories["Niri Components"], dep)
		default:
			categories["Shared Components"] = append(categories["Shared Components"], dep)
		}
	}

	return categories
}
