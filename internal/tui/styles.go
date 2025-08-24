package tui

import (
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

type AppTheme struct {
	Primary    string
	Secondary  string
	Accent     string
	Text       string
	Subtle     string
	Error      string
	Warning    string
	Success    string
	Background string
	Surface    string
}

func PurpleTheme() AppTheme {
	return AppTheme{
		Primary:    "#ccbeff",
		Secondary:  "#4a3e76",
		Accent:     "#e7deff",
		Text:       "#e6e1e9",
		Subtle:     "#cac4cf",
		Error:      "#ffb4ab",
		Warning:    "#eeb8ca",
		Success:    "#ccbeff",
		Background: "#141318",
		Surface:    "#201f24",
	}
}

func NewStyles(theme AppTheme) Styles {
	return Styles{
		Title: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Primary)).
			Bold(true).
			MarginLeft(1).
			MarginBottom(1),

		Normal: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Text)),

		Bold: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Text)).
			Bold(true),

		Subtle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Subtle)),

		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Error)),

		Warning: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Warning)),

		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#33275e")).
			Background(lipgloss.Color(theme.Primary)).
			Padding(0, 1),

		Key: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Accent)).
			Bold(true),

		SpinnerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Primary)),

		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Success)).
			Bold(true),

		HighlightButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#33275e")).
			Background(lipgloss.Color(theme.Primary)).
			Padding(0, 2).
			Bold(true),

		SelectedOption: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Accent)).
			Bold(true),
	}
}

type Styles struct {
	Title           lipgloss.Style
	Normal          lipgloss.Style
	Bold            lipgloss.Style
	Subtle          lipgloss.Style
	Warning         lipgloss.Style
	Error           lipgloss.Style
	StatusBar       lipgloss.Style
	Key             lipgloss.Style
	SpinnerStyle    lipgloss.Style
	Success         lipgloss.Style
	HighlightButton lipgloss.Style
	SelectedOption  lipgloss.Style
}

func (s Styles) NewThemedProgress(width int) progress.Model {
	theme := PurpleTheme()
	prog := progress.New(
		progress.WithGradient(theme.Secondary, theme.Primary),
	)

	prog.Width = width
	prog.ShowPercentage = true
	prog.PercentFormat = "%.0f%%"
	prog.PercentageStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Text)).
		Bold(true)

	return prog
}