package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/deps"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) viewSelectWindowManager() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	title := m.styles.Title.Render("Choose Window Manager")
	b.WriteString(title)
	b.WriteString("\n\n")

	options := []struct {
		name        string
		description string
	}{
		{"niri", "Scrollable-tiling Wayland compositor."},
		{"hyprland", "Dynamic tiling Wayland compositor."},
	}

	for i, option := range options {
		if i == m.selectedWM {
			selected := m.styles.SelectedOption.Render("▶ " + option.name)
			b.WriteString(selected)
			b.WriteString("\n")
			desc := m.styles.Subtle.Render("  " + option.description)
			b.WriteString(desc)
		} else {
			normal := m.styles.Normal.Render("  " + option.name)
			b.WriteString(normal)
			b.WriteString("\n")
			desc := m.styles.Subtle.Render("  " + option.description)
			b.WriteString(desc)
		}
		b.WriteString("\n")
		if i < len(options)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	help := m.styles.Subtle.Render("Use ↑/↓ to navigate, Enter to select")
	b.WriteString(help)

	return b.String()
}

func (m Model) updateSelectWindowManagerState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up":
			if m.selectedWM > 0 {
				m.selectedWM--
			}
		case "down":
			if m.selectedWM < 1 {
				m.selectedWM++
			}
		case "enter":
			m.state = StateDetectingDeps
			m.isLoading = true
			return m, tea.Batch(m.spinner.Tick, m.detectDependencies())
		}
	}
	return m, m.listenForLogs()
}

func (m Model) detectDependencies() tea.Cmd {
	return func() tea.Msg {
		if m.osInfo == nil {
			return depsDetectedMsg{deps: nil, err: fmt.Errorf("OS info not available")}
		}

		detector, err := deps.NewDependencyDetector(m.osInfo.Distribution, m.logChan)
		if err != nil {
			return depsDetectedMsg{deps: nil, err: err}
		}

		// Convert TUI selection to deps enum
		var wm deps.WindowManager
		if m.selectedWM == 0 {
			wm = deps.WindowManagerNiri // First option is Niri
		} else {
			wm = deps.WindowManagerHyprland // Second option is Hyprland  
		}
		
		dependencies, err := detector.DetectDependencies(context.Background(), wm)
		return depsDetectedMsg{deps: dependencies, err: err}
	}
}
