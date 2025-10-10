//go:build distro_binary

package dms

import (
	"os/exec"
	"time"

	"github.com/AvengeMedia/danklinux/internal/log"
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
			case StateShell:
				if selectedLabel == "Terminate Shell" {
					terminateShell()
					m.menuItems = m.buildMenuItems()
					if m.selectedItem >= len(m.menuItems) {
						m.selectedItem = len(m.menuItems) - 1
					}
				} else {
					startShellDaemon()
					m.menuItems = m.buildMenuItems()
					if m.selectedItem >= len(m.menuItems) {
						m.selectedItem = len(m.menuItems) - 1
					}
				}
			case StatePluginsMenu:
				m.state = StatePluginsMenu
				m.selectedPluginsMenuItem = 0
				m.pluginsMenuItems = m.buildPluginsMenuItems()
			case StateAbout:
				m.state = StateAbout
			}
		}
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
	patterns := []string{"dms run", "qs -c dms"}
	for _, pattern := range patterns {
		cmd := exec.Command("pkill", "-f", pattern)
		cmd.Run()
	}
}

func startShellDaemon() {
	cmd := exec.Command("dms", "run", "-d")
	if err := cmd.Start(); err != nil {
		log.Errorf("Error starting daemon: %v", err)
	}
}

func restartShell() {
	terminateShell()
	time.Sleep(500 * time.Millisecond)
	startShellDaemon()
}
