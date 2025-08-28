package main

import (
	"fmt"
	"os"

	"github.com/AvengeMedia/dankinstall/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var Version = "dev"

func main() {
	model := tui.NewModel(Version)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
