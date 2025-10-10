//go:build !distro_binary

package dms

import (
	"errors"

	"github.com/AvengeMedia/danklinux/internal/distros"
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
			case StateGreeterMenu:
				m.state = StateGreeterMenu
				m.selectedGreeterItem = 0
			case StateAbout:
				m.state = StateAbout
			}
		}
	}
	return m, nil
}

func runInteractiveMode(detector *Detector, version string) error {
	if !detector.IsDMSInstalled() {
		return errors.New("DMS not installed")
	}

	model := NewModel(version)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func checkDistroSupport(detector *Detector) error {
	if detector == nil {
		return &distros.UnsupportedDistributionError{}
	}
	return nil
}
