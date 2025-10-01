package dms

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

type AppState int

const (
	StateMainMenu AppState = iota
	StateUpdate
	StateUpdatePassword
	StateUpdateProgress
	StateShell
	StateGreeterMenu
	StateGreeterCompositorSelect
	StateGreeterPassword
	StateGreeterInstalling
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

	updateProgressChan chan updateProgressMsg
	updateProgress     updateProgressMsg
	updateLogs         []string
	sudoPassword       string
	passwordInput      string
	passwordError      string

	// Window manager states
	hyprlandInstalled bool
	niriInstalled     bool

	// Greeter states
	selectedGreeterItem   int
	greeterInstallChan    chan greeterProgressMsg
	greeterProgress       greeterProgressMsg
	greeterLogs           []string
	greeterNeedsPassword    bool
	greeterPasswordInput    string
	greeterPasswordError    string
	greeterSudoPassword     string
	greeterCompositors      []string
	greeterSelectedComp     int
	greeterChosenCompositor string
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
		version:            version,
		detector:           detector,
		dependencies:       dependencies,
		state:              StateMainMenu,
		selectedItem:       0,
		updateToggles:      updateToggles,
		updateDeps:         dependencies,
		updateProgressChan: make(chan updateProgressMsg, 100),
		hyprlandInstalled:  hyprlandInstalled,
		niriInstalled:      niriInstalled,
		greeterInstallChan: make(chan greeterProgressMsg, 100),
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

	// Greeter management
	items = append(items, MenuItem{Label: "Greeter", Action: StateGreeterMenu})

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
	case updateProgressMsg:
		m.updateProgress = msg
		if msg.logOutput != "" {
			m.updateLogs = append(m.updateLogs, msg.logOutput)
		}
		return m, m.waitForProgress()
	case updateCompleteMsg:
		m.updateProgress.complete = true
		m.updateProgress.err = msg.err
		m.dependencies = m.detector.GetInstalledComponents()
		m.updateDeps = m.dependencies
		m.menuItems = m.buildMenuItems()

		// Restart shell if update was successful and shell is running
		if msg.err == nil && m.isShellRunning() {
			restartShell()
		}
		return m, nil
	case greeterProgressMsg:
		m.greeterProgress = msg
		if msg.logOutput != "" {
			m.greeterLogs = append(m.greeterLogs, msg.logOutput)
		}
		return m, m.waitForGreeterProgress()
	case greeterPasswordValidMsg:
		if msg.valid {
			m.greeterSudoPassword = msg.password
			m.greeterPasswordInput = ""
			m.greeterPasswordError = ""
			m.state = StateGreeterInstalling
			m.greeterProgress = greeterProgressMsg{step: "Starting greeter installation..."}
			m.greeterLogs = []string{}
			return m, tea.Batch(m.performGreeterInstall(), m.waitForGreeterProgress())
		} else {
			m.greeterPasswordError = "Incorrect password. Please try again."
			m.greeterPasswordInput = ""
		}
		return m, nil
	case passwordValidMsg:
		if msg.valid {
			m.sudoPassword = msg.password
			m.passwordInput = ""
			m.passwordError = ""
			m.state = StateUpdateProgress
			m.updateProgress = updateProgressMsg{progress: 0.0, step: "Starting update..."}
			m.updateLogs = []string{}
			return m, tea.Batch(m.performUpdate(), m.waitForProgress())
		} else {
			m.passwordError = "Incorrect password. Please try again."
			m.passwordInput = ""
		}
		return m, nil
	case tea.KeyMsg:
		switch m.state {
		case StateMainMenu:
			return m.updateMainMenu(msg)
		case StateUpdate:
			return m.updateUpdateView(msg)
		case StateUpdatePassword:
			return m.updatePasswordView(msg)
		case StateUpdateProgress:
			return m.updateProgressView(msg)
		case StateShell:
			return m.updateShellView(msg)
		case StateGreeterMenu:
			return m.updateGreeterMenu(msg)
		case StateGreeterCompositorSelect:
			return m.updateGreeterCompositorSelect(msg)
		case StateGreeterPassword:
			return m.updateGreeterPasswordView(msg)
		case StateGreeterInstalling:
			return m.updateGreeterInstalling(msg)
		case StateAbout:
			return m.updateAboutView(msg)
		}
	}

	return m, nil
}

type updateProgressMsg struct {
	progress  float64
	step      string
	complete  bool
	err       error
	logOutput string
}

type updateCompleteMsg struct {
	err error
}

type passwordValidMsg struct {
	password string
	valid    bool
}

type greeterProgressMsg struct {
	step      string
	complete  bool
	err       error
	logOutput string
}

type greeterPasswordValidMsg struct {
	password string
	valid    bool
}

func (m Model) waitForProgress() tea.Cmd {
	return func() tea.Msg {
		return <-m.updateProgressChan
	}
}

func (m Model) waitForGreeterProgress() tea.Cmd {
	return func() tea.Msg {
		return <-m.greeterInstallChan
	}
}

func (m Model) View() string {
	switch m.state {
	case StateMainMenu:
		return m.renderMainMenu()
	case StateUpdate:
		return m.renderUpdateView()
	case StateUpdatePassword:
		return m.renderPasswordView()
	case StateUpdateProgress:
		return m.renderProgressView()
	case StateShell:
		return m.renderShellView()
	case StateGreeterMenu:
		return m.renderGreeterMenu()
	case StateGreeterCompositorSelect:
		return m.renderGreeterCompositorSelect()
	case StateGreeterPassword:
		return m.renderGreeterPasswordView()
	case StateGreeterInstalling:
		return m.renderGreeterInstalling()
	case StateAbout:
		return m.renderAboutView()
	default:
		return m.renderMainMenu()
	}
}
