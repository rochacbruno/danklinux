package tui

import "github.com/charmbracelet/lipgloss"

func (m Model) renderBanner() string {
	logo := `
██████╗  █████╗ ███╗   ██╗██╗  ██╗
██╔══██╗██╔══██╗████╗  ██║██║ ██╔╝
██║  ██║███████║██╔██╗ ██║█████╔╝ 
██║  ██║██╔══██║██║╚██╗██║██╔═██╗ 
██████╔╝██║  ██║██║ ╚████║██║  ██╗
╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝ `

	theme := PurpleTheme()
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Primary)).
		Bold(true).
		Align(lipgloss.Center).
		MarginBottom(2)

	return style.Render(logo)
}
