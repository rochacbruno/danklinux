package dms

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateMainMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.selectedItem > 0 {
			m.selectedItem--
		}
	case "down", "j":
		if m.selectedItem < len(m.menuItems)-1 {
			m.selectedItem++
		}
	case "enter", " ":
		if m.selectedItem < len(m.menuItems) {
			selectedAction := m.menuItems[m.selectedItem].Action
			selectedLabel := m.menuItems[m.selectedItem].Label

			switch selectedAction {
			case StateUpdate:
				m.state = StateUpdate
				m.selectedUpdateDep = 0
			case StateShell:
				// Handle shell management based on label
				if selectedLabel == "Terminate Shell" {
					terminateShell()
					// Rebuild menu to update shell status
					m.menuItems = m.buildMenuItems()
					// Reset selection if it's now out of bounds
					if m.selectedItem >= len(m.menuItems) {
						m.selectedItem = len(m.menuItems) - 1
					}
				} else {
					startShellDaemon()
					// Rebuild menu to update shell status
					m.menuItems = m.buildMenuItems()
					// Reset selection if it's now out of bounds
					if m.selectedItem >= len(m.menuItems) {
						m.selectedItem = len(m.menuItems) - 1
					}
				}
			case StateAbout:
				m.state = StateAbout
			}
		}
	}
	return m, nil
}

func (m Model) updateUpdateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.state = StateMainMenu
	case "up", "k":
		if m.selectedUpdateDep > 0 {
			m.selectedUpdateDep--
		}
	case "down", "j":
		if m.selectedUpdateDep < len(m.updateDeps)-1 {
			m.selectedUpdateDep++
		}
	case " ":
		if len(m.updateDeps) > 0 {
			depName := m.updateDeps[m.selectedUpdateDep].Name
			m.updateToggles[depName] = !m.updateToggles[depName]
		}
	case "enter":
		// TODO: Implement update logic
		return m, nil
	}
	return m, nil
}


func (m Model) updateShellView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		m.state = StateMainMenu
	default:
		// TODO: Launch shell and exit TUI
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateAboutView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		if msg.String() == "esc" {
			m.state = StateMainMenu
		} else {
			return m, tea.Quit
		}
	}
	return m, nil
}

func terminateShell() {
	cmd := exec.Command("pkill", "-f", "qs -c dms")
	cmd.Run()
}

func startShellDaemon() {
	cmd := exec.Command("qs", "-c", "dms")
	cmd.Start()
}
