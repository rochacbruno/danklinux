package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) viewWelcome() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	// Create title - it IS left-aligned, just appears centered due to banner width
	theme := TerminalTheme()
	titleText := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Primary)).
		Bold(true).
		Render("Dank Linux Suite Installer")
	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Text)).
		Render("Installs the complete dank desktop suite.")
	b.WriteString(titleText)
	b.WriteString("\n")
	b.WriteString(subtitle)
	b.WriteString("\n\n")

	if m.osInfo != nil {
		// Style the distro name with its color
		distroStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.osInfo.Distribution.HexColorCode)).
			Bold(true)
		distroName := distroStyle.Render(m.osInfo.PrettyName)

		info := fmt.Sprintf("System: %s (%s)\n", distroName, m.osInfo.Architecture)
		b.WriteString(info)
		b.WriteString("\n")

		overview := m.styles.Bold.Render("What you get:") + "\n"
		bullet := m.styles.Key.Render("•")
		overview += fmt.Sprintf("  %s %s\n", bullet, m.styles.Normal.Render("The dms (DankMaterialShell)"))
		overview += fmt.Sprintf("  %s %s\n", bullet, m.styles.Normal.Render("niri or Hyprland"))
		overview += fmt.Sprintf("  %s %s\n", bullet, m.styles.Normal.Render("Ghostty or kitty - terminal"))
		overview += fmt.Sprintf("  %s %s\n", bullet, m.styles.Normal.Render("Automatic theming"))
		overview += fmt.Sprintf("  %s %s\n", bullet, m.styles.Normal.Render("Sane default configuration"))
		overview += fmt.Sprintf("  %s %s\n\n", bullet, m.styles.Normal.Render("A lot more for a pretty, highly functional desktop"))
		overview += m.styles.Normal.Render("Already have niri/Hyprland? Your existing config will be backed up.")
		b.WriteString(overview)
		b.WriteString("\n\n")

	} else if m.isLoading {
		spinner := m.spinner.View()
		loading := m.styles.Normal.Render("Detecting system...")
		b.WriteString(fmt.Sprintf("%s %s\n\n", spinner, loading))

	} else {
		// ! TODO - error state?
	}

	if m.osInfo != nil {
		help := m.styles.Subtle.Render("Press Enter to choose window manager, Ctrl+C to quit")
		b.WriteString(help)
	} else {
		help := m.styles.Subtle.Render("Press Enter to continue, Ctrl+C to quit")
		b.WriteString(help)
	}

	return b.String()
}

func (m Model) updateWelcomeState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if completeMsg, ok := msg.(osInfoCompleteMsg); ok {
		m.isLoading = false
		if completeMsg.err != nil {
			m.err = completeMsg.err
			m.state = StateError
		} else {
			m.osInfo = completeMsg.info
		}
		return m, m.listenForLogs()
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if m.osInfo != nil {
				m.state = StateSelectWindowManager
				return m, m.listenForLogs()
			}
		}
	}
	return m, m.listenForLogs()
}
