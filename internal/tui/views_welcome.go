package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) viewWelcome() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	title := m.styles.Title.Render("DANK installer")
	b.WriteString(title)
	b.WriteString("\n")

	if m.osInfo != nil {
		info := fmt.Sprintf("System: %s %s (%s)\n", m.osInfo.PrettyName, m.osInfo.VersionID, m.osInfo.Architecture)
		b.WriteString(m.styles.Normal.Render(info))
		b.WriteString("\n")

		overview := "This will install and configure a complete niri or Hyprland environment.\n"
		overview += "Preconfigured with dms, dynamic theming, and all the out of the box things you need..\n"
		b.WriteString(m.styles.Normal.Render(overview))
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
