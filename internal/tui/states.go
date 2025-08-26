package tui

type ApplicationState int

const (
	StateWelcome ApplicationState = iota
	StateSelectWindowManager
	StateSelectTerminal
	StateDetectingDeps
	StateDependencyReview
	StatePasswordPrompt
	StateInstallingPackages
	StateConfigConfirmation
	StateDeployingConfigs
	StateInstallComplete
	StateError
)