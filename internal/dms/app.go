package dms

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

type AppState int

const (
	StateMainMenu AppState = iota
	StateUpdate
	StateInstallWM
	StateShell
	StateAbout
)

type Model struct {
	version      string
	detector     *Detector
	dependencies []DependencyInfo
	state        AppState
	selectedItem int
	width        int
	height       int

	// Menu items
	menuItems []MenuItem

	updateDeps        []DependencyInfo
	selectedUpdateDep int
	updateToggles     map[string]bool

	// Window manager states
	hyprlandInstalled bool
	niriInstalled     bool
}

type MenuItem struct {
	Label  string
	Action AppState
}

func NewModel(version string) Model {
	detector, _ := NewDetector()
	dependencies := detector.GetInstalledComponents()

	// Use the proper detection method for both window managers
	hyprlandInstalled, niriInstalled, err := detector.GetWindowManagerStatus()
	if err != nil {
		// Fallback to false if detection fails
		hyprlandInstalled = false
		niriInstalled = false
	}

	updateToggles := make(map[string]bool)
	for _, dep := range dependencies {
		if dep.Name == "dms (DankMaterialShell)" {
			updateToggles[dep.Name] = true
			break
		}
	}

	m := Model{
		version:           version,
		detector:          detector,
		dependencies:      dependencies,
		state:             StateMainMenu,
		selectedItem:      0,
		updateToggles:     updateToggles,
		updateDeps:        dependencies,
		hyprlandInstalled: hyprlandInstalled,
		niriInstalled:     niriInstalled,
	}

	m.menuItems = m.buildMenuItems()
	return m
}

func (m *Model) buildMenuItems() []MenuItem {
	items := []MenuItem{
		{Label: "Update", Action: StateUpdate},
	}

	// Shell management
	if m.isShellRunning() {
		items = append(items, MenuItem{Label: "Terminate Shell", Action: StateShell})
	} else {
		items = append(items, MenuItem{Label: "Start Shell (Daemon)", Action: StateShell})
	}

	// Window manager installation
	if !m.niriInstalled {
		items = append(items, MenuItem{Label: "Install Niri", Action: StateInstallWM})
	}

	if !m.hyprlandInstalled {
		items = append(items, MenuItem{Label: "Install Hyprland", Action: StateInstallWM})
	}

	items = append(items, MenuItem{Label: "About", Action: StateAbout})

	return items
}

func (m *Model) isShellRunning() bool {
	cmd := exec.Command("pgrep", "-f", "qs -c dms")
	err := cmd.Run()
	return err == nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch m.state {
		case StateMainMenu:
			return m.updateMainMenu(msg)
		case StateUpdate:
			return m.updateUpdateView(msg)
		case StateInstallWM:
			return m.updateInstallWMView(msg)
		case StateShell:
			return m.updateShellView(msg)
		case StateAbout:
			return m.updateAboutView(msg)
		}
	}

	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case StateMainMenu:
		return m.renderMainMenu()
	case StateUpdate:
		return m.renderUpdateView()
	case StateInstallWM:
		return m.renderInstallWMView()
	case StateShell:
		return m.renderShellView()
	case StateAbout:
		return m.renderAboutView()
	default:
		return m.renderMainMenu()
	}
}
