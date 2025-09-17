package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AvengeMedia/dankinstall/internal/deps"
)

type ConfigDeployer struct {
	logChan chan<- string
}

type DeploymentResult struct {
	ConfigType string
	Path       string
	BackupPath string
	Deployed   bool
	Error      error
}

func NewConfigDeployer(logChan chan<- string) *ConfigDeployer {
	return &ConfigDeployer{
		logChan: logChan,
	}
}

func (cd *ConfigDeployer) log(message string) {
	if cd.logChan != nil {
		cd.logChan <- message
	}
}

// DeployConfigurations deploys all necessary configurations based on the chosen window manager
func (cd *ConfigDeployer) DeployConfigurations(ctx context.Context, wm deps.WindowManager) ([]DeploymentResult, error) {
	return cd.DeployConfigurationsWithTerminal(ctx, wm, deps.TerminalGhostty)
}

// DeployConfigurationsWithTerminal deploys all necessary configurations based on chosen window manager and terminal
func (cd *ConfigDeployer) DeployConfigurationsWithTerminal(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal) ([]DeploymentResult, error) {
	return cd.DeployConfigurationsSelective(ctx, wm, terminal, nil, nil)
}

func (cd *ConfigDeployer) DeployConfigurationsSelective(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal, installedDeps []deps.Dependency, replaceConfigs map[string]bool) ([]DeploymentResult, error) {
	return cd.DeployConfigurationsSelectiveWithReinstalls(ctx, wm, terminal, installedDeps, replaceConfigs, nil)
}

func (cd *ConfigDeployer) DeployConfigurationsSelectiveWithReinstalls(ctx context.Context, wm deps.WindowManager, terminal deps.Terminal, installedDeps []deps.Dependency, replaceConfigs map[string]bool, reinstallItems map[string]bool) ([]DeploymentResult, error) {
	var results []DeploymentResult

	shouldReplaceConfig := func(configType string) bool {
		if replaceConfigs == nil {
			return true
		}
		replace, exists := replaceConfigs[configType]
		return !exists || replace
	}

	switch wm {
	case deps.WindowManagerNiri:
		if shouldReplaceConfig("Niri") {
			result, err := cd.deployNiriConfig(ctx, terminal)
			results = append(results, result)
			if err != nil {
				return results, fmt.Errorf("failed to deploy Niri config: %w", err)
			}
		}
	case deps.WindowManagerHyprland:
		if shouldReplaceConfig("Hyprland") {
			result, err := cd.deployHyprlandConfig(ctx, terminal)
			results = append(results, result)
			if err != nil {
				return results, fmt.Errorf("failed to deploy Hyprland config: %w", err)
			}
		}
	}

	switch terminal {
	case deps.TerminalGhostty:
		if shouldReplaceConfig("Ghostty") {
			ghosttyResult, err := cd.deployGhosttyConfig(ctx)
			results = append(results, ghosttyResult)
			if err != nil {
				return results, fmt.Errorf("failed to deploy Ghostty config: %w", err)
			}
		}
	case deps.TerminalKitty:
		if shouldReplaceConfig("Kitty") {
			kittyResult, err := cd.deployKittyConfig(ctx)
			results = append(results, kittyResult)
			if err != nil {
				return results, fmt.Errorf("failed to deploy Kitty config: %w", err)
			}
		}
	}

	return results, nil
}

// deployNiriConfig handles Niri configuration deployment with backup and merging
func (cd *ConfigDeployer) deployNiriConfig(ctx context.Context, terminal deps.Terminal) (DeploymentResult, error) {
	result := DeploymentResult{
		ConfigType: "Niri",
		Path:       filepath.Join(os.Getenv("HOME"), ".config", "niri", "config.kdl"),
	}

	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	var existingConfig string
	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Niri configuration")

		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}
		existingConfig = string(existingData)

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		result.BackupPath = result.Path + ".backup." + timestamp
		if err := os.WriteFile(result.BackupPath, existingData, 0644); err != nil {
			result.Error = fmt.Errorf("failed to create backup: %w", err)
			return result, result.Error
		}
		cd.log(fmt.Sprintf("Backed up existing config to %s", result.BackupPath))
	}

	// Detect polkit agent path
	polkitPath, err := cd.detectPolkitAgent()
	if err != nil {
		cd.log(fmt.Sprintf("Warning: Could not detect polkit agent: %v", err))
		polkitPath = "/usr/lib/mate-polkit/polkit-mate-authentication-agent-1" // fallback
	}

	// Determine terminal command based on choice
	var terminalCommand string
	switch terminal {
	case deps.TerminalGhostty:
		terminalCommand = "ghostty"
	case deps.TerminalKitty:
		terminalCommand = "kitty"
	case deps.TerminalAlacritty:
		terminalCommand = "alacritty"
	default:
		terminalCommand = "ghostty" // fallback to ghostty
	}

	newConfig := strings.ReplaceAll(NiriConfig, "{{POLKIT_AGENT_PATH}}", polkitPath)
	newConfig = strings.ReplaceAll(newConfig, "{{TERMINAL_COMMAND}}", terminalCommand)

	// If there was an existing config, merge the output sections
	if existingConfig != "" {
		mergedConfig, err := cd.mergeNiriOutputSections(newConfig, existingConfig)
		if err != nil {
			cd.log(fmt.Sprintf("Warning: Failed to merge output sections: %v", err))
		} else {
			newConfig = mergedConfig
			cd.log("Successfully merged existing output sections")
		}
	}

	if err := os.WriteFile(result.Path, []byte(newConfig), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result, result.Error
	}

	result.Deployed = true
	cd.log("Successfully deployed Niri configuration")
	return result, nil
}

// deployGhosttyConfig handles Ghostty configuration deployment with backup
func (cd *ConfigDeployer) deployGhosttyConfig(ctx context.Context) (DeploymentResult, error) {
	result := DeploymentResult{
		ConfigType: "Ghostty",
		Path:       filepath.Join(os.Getenv("HOME"), ".config", "ghostty", "config"),
	}

	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Ghostty configuration")

		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		result.BackupPath = result.Path + ".backup." + timestamp
		if err := os.WriteFile(result.BackupPath, existingData, 0644); err != nil {
			result.Error = fmt.Errorf("failed to create backup: %w", err)
			return result, result.Error
		}
		cd.log(fmt.Sprintf("Backed up existing config to %s", result.BackupPath))
	}

	if err := os.WriteFile(result.Path, []byte(GhosttyConfig), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result, result.Error
	}

	result.Deployed = true
	cd.log("Successfully deployed Ghostty configuration")
	return result, nil
}

// deployKittyConfig handles Kitty configuration deployment with backup
func (cd *ConfigDeployer) deployKittyConfig(ctx context.Context) (DeploymentResult, error) {
	result := DeploymentResult{
		ConfigType: "Kitty",
		Path:       filepath.Join(os.Getenv("HOME"), ".config", "kitty", "kitty.conf"),
	}

	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Kitty configuration")

		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		result.BackupPath = result.Path + ".backup." + timestamp
		if err := os.WriteFile(result.BackupPath, existingData, 0644); err != nil {
			result.Error = fmt.Errorf("failed to create backup: %w", err)
			return result, result.Error
		}
		cd.log(fmt.Sprintf("Backed up existing config to %s", result.BackupPath))
	}

	if err := os.WriteFile(result.Path, []byte(KittyConfig), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result, result.Error
	}

	result.Deployed = true
	cd.log("Successfully deployed Kitty configuration")
	return result, nil
}

// detectPolkitAgent tries to find the polkit authentication agent on the system
// Prioritizes mate-polkit paths since that's what we install
func (cd *ConfigDeployer) detectPolkitAgent() (string, error) {
	// Prioritize mate-polkit paths first
	matePaths := []string{
		"/usr/lib/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/libexec/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/lib/polkit-mate/polkit-mate-authentication-agent-1",
		"/usr/lib/x86_64-linux-gnu/mate-polkit/polkit-mate-authentication-agent-1",
	}

	for _, path := range matePaths {
		if _, err := os.Stat(path); err == nil {
			cd.log(fmt.Sprintf("Found mate-polkit agent at: %s", path))
			return path, nil
		}
	}

	// Fallback to other polkit agents if mate-polkit is not found
	fallbackPaths := []string{
		"/usr/lib/polkit-gnome/polkit-gnome-authentication-agent-1",
		"/usr/libexec/polkit-gnome-authentication-agent-1",
	}

	for _, path := range fallbackPaths {
		if _, err := os.Stat(path); err == nil {
			cd.log(fmt.Sprintf("Found fallback polkit agent at: %s", path))
			return path, nil
		}
	}

	return "", fmt.Errorf("no polkit agent found in common locations")
}

// mergeNiriOutputSections extracts output sections from existing config and merges them into the new config
func (cd *ConfigDeployer) mergeNiriOutputSections(newConfig, existingConfig string) (string, error) {
	// Regular expression to match output sections (including commented ones)
	outputRegex := regexp.MustCompile(`(?m)^(/-)?\s*output\s+"[^"]+"\s*\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}`)

	// Find all output sections in the existing config
	existingOutputs := outputRegex.FindAllString(existingConfig, -1)

	if len(existingOutputs) == 0 {
		// No output sections to merge
		return newConfig, nil
	}

	// Remove the example output section from the new config
	exampleOutputRegex := regexp.MustCompile(`(?m)^/-output "eDP-2" \{[^{}]*(?:\{[^{}]*\}[^{}]*)*\}`)
	mergedConfig := exampleOutputRegex.ReplaceAllString(newConfig, "")

	// Find where to insert the output sections (after the input section)
	inputEndRegex := regexp.MustCompile(`(?m)^}$`)
	inputMatches := inputEndRegex.FindAllStringIndex(newConfig, -1)

	if len(inputMatches) < 1 {
		return "", fmt.Errorf("could not find insertion point for output sections")
	}

	// Insert after the first closing brace (end of input section)
	insertPos := inputMatches[0][1]

	var builder strings.Builder
	builder.WriteString(mergedConfig[:insertPos])
	builder.WriteString("\n// Outputs from existing configuration\n")

	for _, output := range existingOutputs {
		builder.WriteString(output)
		builder.WriteString("\n")
	}

	builder.WriteString(mergedConfig[insertPos:])

	return builder.String(), nil
}

// deployHyprlandConfig handles Hyprland configuration deployment with backup and merging
func (cd *ConfigDeployer) deployHyprlandConfig(ctx context.Context, terminal deps.Terminal) (DeploymentResult, error) {
	result := DeploymentResult{
		ConfigType: "Hyprland",
		Path:       filepath.Join(os.Getenv("HOME"), ".config", "hypr", "hyprland.conf"),
	}

	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	var existingConfig string
	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Hyprland configuration")

		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}
		existingConfig = string(existingData)

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		result.BackupPath = result.Path + ".backup." + timestamp
		if err := os.WriteFile(result.BackupPath, existingData, 0644); err != nil {
			result.Error = fmt.Errorf("failed to create backup: %w", err)
			return result, result.Error
		}
		cd.log(fmt.Sprintf("Backed up existing config to %s", result.BackupPath))
	}

	// Detect polkit agent path
	polkitPath, err := cd.detectPolkitAgent()
	if err != nil {
		cd.log(fmt.Sprintf("Warning: Could not detect polkit agent: %v", err))
		polkitPath = "/usr/lib/mate-polkit/polkit-mate-authentication-agent-1" // fallback
	}

	// Determine terminal command based on choice
	var terminalCommand string
	switch terminal {
	case deps.TerminalGhostty:
		terminalCommand = "ghostty"
	case deps.TerminalKitty:
		terminalCommand = "kitty"
	case deps.TerminalAlacritty:
		terminalCommand = "alacritty"
	default:
		terminalCommand = "ghostty" // fallback to ghostty
	}

	newConfig := strings.ReplaceAll(HyprlandConfig, "{{POLKIT_AGENT_PATH}}", polkitPath)
	newConfig = strings.ReplaceAll(newConfig, "{{TERMINAL_COMMAND}}", terminalCommand)

	// If there was an existing config, merge the monitor sections
	if existingConfig != "" {
		mergedConfig, err := cd.mergeHyprlandMonitorSections(newConfig, existingConfig)
		if err != nil {
			cd.log(fmt.Sprintf("Warning: Failed to merge monitor sections: %v", err))
		} else {
			newConfig = mergedConfig
			cd.log("Successfully merged existing monitor sections")
		}
	}

	if err := os.WriteFile(result.Path, []byte(newConfig), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result, result.Error
	}

	result.Deployed = true
	cd.log("Successfully deployed Hyprland configuration")
	return result, nil
}

// mergeHyprlandMonitorSections extracts monitor sections from existing config and merges them into the new config
func (cd *ConfigDeployer) mergeHyprlandMonitorSections(newConfig, existingConfig string) (string, error) {
	// Regular expression to match monitor lines (including commented ones)
	// Matches: monitor = NAME, RESOLUTION, POSITION, SCALE, etc.
	// Also matches commented versions: # monitor = ...
	monitorRegex := regexp.MustCompile(`(?m)^#?\s*monitor\s*=.*$`)

	// Find all monitor lines in the existing config
	existingMonitors := monitorRegex.FindAllString(existingConfig, -1)

	if len(existingMonitors) == 0 {
		// No monitor sections to merge
		return newConfig, nil
	}

	// Remove the example monitor line from the new config
	exampleMonitorRegex := regexp.MustCompile(`(?m)^# monitor = eDP-2.*$`)
	mergedConfig := exampleMonitorRegex.ReplaceAllString(newConfig, "")

	// Find where to insert the monitor sections (after the MONITOR CONFIG header)
	monitorHeaderRegex := regexp.MustCompile(`(?m)^# MONITOR CONFIG\n# ==================$`)
	headerMatch := monitorHeaderRegex.FindStringIndex(mergedConfig)

	if headerMatch == nil {
		return "", fmt.Errorf("could not find MONITOR CONFIG section")
	}

	// Insert after the header
	insertPos := headerMatch[1] + 1 // +1 for the newline

	var builder strings.Builder
	builder.WriteString(mergedConfig[:insertPos])
	builder.WriteString("# Monitors from existing configuration\n")

	for _, monitor := range existingMonitors {
		builder.WriteString(monitor)
		builder.WriteString("\n")
	}

	builder.WriteString(mergedConfig[insertPos:])

	return builder.String(), nil
}
