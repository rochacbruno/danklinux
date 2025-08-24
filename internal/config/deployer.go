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
	var results []DeploymentResult

	switch wm {
	case deps.WindowManagerNiri:
		result, err := cd.deployNiriConfig(ctx)
		results = append(results, result)
		if err != nil {
			return results, fmt.Errorf("failed to deploy Niri config: %w", err)
		}
	case deps.WindowManagerHyprland:
		// Future: Add Hyprland config deployment
		cd.log("Hyprland configuration deployment not yet implemented")
	}

	// Deploy Ghostty config regardless of window manager
	ghosttyResult, err := cd.deployGhosttyConfig(ctx)
	results = append(results, ghosttyResult)
	if err != nil {
		return results, fmt.Errorf("failed to deploy Ghostty config: %w", err)
	}

	return results, nil
}

// deployNiriConfig handles Niri configuration deployment with backup and merging
func (cd *ConfigDeployer) deployNiriConfig(ctx context.Context) (DeploymentResult, error) {
	result := DeploymentResult{
		ConfigType: "Niri",
		Path:       filepath.Join(os.Getenv("HOME"), ".config", "niri", "config.kdl"),
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	// Check if existing config exists
	var existingConfig string
	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Niri configuration")
		
		// Read existing config
		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}
		existingConfig = string(existingData)

		// Create backup
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

	// Generate new config with polkit path injection
	newConfig := strings.Replace(NiriConfig, "{{POLKIT_AGENT_PATH}}", polkitPath, 1)

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

	// Write new config
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

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(result.Path)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		result.Error = fmt.Errorf("failed to create config directory: %w", err)
		return result, result.Error
	}

	// Check if existing config exists
	if _, err := os.Stat(result.Path); err == nil {
		cd.log("Found existing Ghostty configuration")
		
		// Read existing config for backup
		existingData, err := os.ReadFile(result.Path)
		if err != nil {
			result.Error = fmt.Errorf("failed to read existing config: %w", err)
			return result, result.Error
		}

		// Create backup
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		result.BackupPath = result.Path + ".backup." + timestamp
		if err := os.WriteFile(result.BackupPath, existingData, 0644); err != nil {
			result.Error = fmt.Errorf("failed to create backup: %w", err)
			return result, result.Error
		}
		cd.log(fmt.Sprintf("Backed up existing config to %s", result.BackupPath))
	}

	// Write new config
	if err := os.WriteFile(result.Path, []byte(GhosttyConfig), 0644); err != nil {
		result.Error = fmt.Errorf("failed to write config: %w", err)
		return result, result.Error
	}

	result.Deployed = true
	cd.log("Successfully deployed Ghostty configuration")
	return result, nil
}

// detectPolkitAgent tries to find the polkit authentication agent on the system
func (cd *ConfigDeployer) detectPolkitAgent() (string, error) {
	possiblePaths := []string{
		"/usr/lib/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/libexec/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/lib/polkit-mate/polkit-mate-authentication-agent-1",
		"/usr/lib/x86_64-linux-gnu/mate-polkit/polkit-mate-authentication-agent-1",
		"/usr/lib/polkit-gnome/polkit-gnome-authentication-agent-1",
		"/usr/libexec/polkit-gnome-authentication-agent-1",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			cd.log(fmt.Sprintf("Found polkit agent at: %s", path))
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
	
	// Build the merged config
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