package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/AvengeMedia/dankinstall/internal/config"
	"github.com/AvengeMedia/dankinstall/internal/deps"
)

type configDeploymentResult struct {
	results []config.DeploymentResult
	error   error
}

type ExistingConfigInfo struct {
	ConfigType string
	Path       string
	Exists     bool
}

type configCheckResult struct {
	configs []ExistingConfigInfo
	error   error
}

func (m Model) viewDeployingConfigs() string {
	var b strings.Builder
	
	b.WriteString(m.renderBanner())
	b.WriteString("\n")
	
	title := m.styles.Title.Render("Deploying Configurations")
	b.WriteString(title)
	b.WriteString("\n\n")
	
	spinner := m.spinner.View()
	status := m.styles.Normal.Render("Setting up configuration files...")
	b.WriteString(fmt.Sprintf("%s %s", spinner, status))
	b.WriteString("\n\n")
	
	// Show progress information
	info := m.styles.Subtle.Render("• Creating backups of existing configurations\n• Deploying optimized configurations\n• Detecting system paths")
	b.WriteString(info)
	
	// Show live log output if available
	if len(m.installationLogs) > 0 {
		b.WriteString("\n\n")
		logHeader := m.styles.Subtle.Render("Configuration Log:")
		b.WriteString(logHeader)
		b.WriteString("\n")
		
		// Show last few lines of logs
		maxLines := 5
		startIdx := 0
		if len(m.installationLogs) > maxLines {
			startIdx = len(m.installationLogs) - maxLines
		}
		
		for i := startIdx; i < len(m.installationLogs); i++ {
			if m.installationLogs[i] != "" {
				logLine := m.styles.Subtle.Render("  " + m.installationLogs[i])
				b.WriteString(logLine)
				b.WriteString("\n")
			}
		}
	}
	
	return b.String()
}

func (m Model) updateDeployingConfigsState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if result, ok := msg.(configDeploymentResult); ok {
		if result.error != nil {
			m.err = result.error
			m.state = StateError
			m.isLoading = false
			return m, nil
		}
		
		// Log the deployment results
		for _, deployResult := range result.results {
			if deployResult.Deployed {
				logMsg := fmt.Sprintf("✓ %s configuration deployed", deployResult.ConfigType)
				if deployResult.BackupPath != "" {
					logMsg += fmt.Sprintf(" (backup: %s)", deployResult.BackupPath)
				}
				m.installationLogs = append(m.installationLogs, logMsg)
			}
		}
		
		m.state = StateInstallComplete
		m.isLoading = false
		return m, nil
	}
	
	return m, m.listenForLogs()
}

func (m Model) deployConfigurations() tea.Cmd {
	return func() tea.Msg {
		// Determine the selected window manager
		var wm deps.WindowManager
		switch m.selectedWM {
		case 0:
			wm = deps.WindowManagerNiri
		case 1:
			wm = deps.WindowManagerHyprland
		default:
			wm = deps.WindowManagerNiri
		}
		
		// Create config deployer
		deployer := config.NewConfigDeployer(m.logChan)
		
		// Deploy configurations
		results, err := deployer.DeployConfigurations(context.Background(), wm)
		
		return configDeploymentResult{
			results: results,
			error:   err,
		}
	}
}

func (m Model) viewConfigConfirmation() string {
	var b strings.Builder
	
	b.WriteString(m.renderBanner())
	b.WriteString("\n")
	
	title := m.styles.Title.Render("Configuration Deployment Confirmation")
	b.WriteString(title)
	b.WriteString("\n\n")
	
	if len(m.existingConfigs) == 0 {
		// No existing configs, proceed directly
		info := m.styles.Normal.Render("No existing configurations found. Proceeding with deployment...")
		b.WriteString(info)
		return b.String()
	}
	
	// Show existing configurations that will be overwritten
	warning := m.styles.Warning.Render("⚠ Existing configurations detected!")
	b.WriteString(warning)
	b.WriteString("\n\n")
	
	info := m.styles.Normal.Render("The following configuration files already exist and will be overwritten:")
	b.WriteString(info)
	b.WriteString("\n\n")
	
	for _, configInfo := range m.existingConfigs {
		if configInfo.Exists {
			configLine := m.styles.Subtle.Render(fmt.Sprintf("  • %s: %s", configInfo.ConfigType, configInfo.Path))
			b.WriteString(configLine)
			b.WriteString("\n")
		}
	}
	
	b.WriteString("\n")
	backup := m.styles.Success.Render("✓ Existing configurations will be backed up with timestamp")
	b.WriteString(backup)
	b.WriteString("\n\n")
	
	prompt := m.styles.Normal.Render("Press Enter to continue with deployment, or Ctrl+C to cancel")
	b.WriteString(prompt)
	
	return b.String()
}

func (m Model) updateConfigConfirmationState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if result, ok := msg.(configCheckResult); ok {
		if result.error != nil {
			m.err = result.error
			m.state = StateError
			return m, nil
		}
		
		m.existingConfigs = result.configs
		
		// Check if any configs exist
		hasExisting := false
		for _, config := range result.configs {
			if config.Exists {
				hasExisting = true
				break
			}
		}
		
		if !hasExisting {
			// No existing configs, proceed directly to deployment
			m.state = StateDeployingConfigs
			return m, m.deployConfigurations()
		}
		
		// Show confirmation view
		return m, nil
	}
	
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			m.state = StateDeployingConfigs
			return m, m.deployConfigurations()
		}
	}
	
	return m, nil
}

func (m Model) checkExistingConfigurations() tea.Cmd {
	return func() tea.Msg {
		var configs []ExistingConfigInfo
		
		// Check Niri config
		niriPath := filepath.Join(os.Getenv("HOME"), ".config", "niri", "config.kdl")
		niriExists := false
		if _, err := os.Stat(niriPath); err == nil {
			niriExists = true
		}
		configs = append(configs, ExistingConfigInfo{
			ConfigType: "Niri",
			Path:       niriPath,
			Exists:     niriExists,
		})
		
		// Check Ghostty config
		ghosttyPath := filepath.Join(os.Getenv("HOME"), ".config", "ghostty", "config")
		ghosttyExists := false
		if _, err := os.Stat(ghosttyPath); err == nil {
			ghosttyExists = true
		}
		configs = append(configs, ExistingConfigInfo{
			ConfigType: "Ghostty",
			Path:       ghosttyPath,
			Exists:     ghosttyExists,
		})
		
		return configCheckResult{
			configs: configs,
			error:   nil,
		}
	}
}