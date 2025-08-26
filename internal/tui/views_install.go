package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) viewInstallingPackages() string {
	var b strings.Builder
	
	b.WriteString(m.renderBanner())
	b.WriteString("\n")
	
	title := m.styles.Title.Render("Installing Packages")
	b.WriteString(title)
	b.WriteString("\n\n")
	
	if !m.packageProgress.isComplete {
		spinner := m.spinner.View()
		status := m.styles.Normal.Render(m.packageProgress.step)
		b.WriteString(fmt.Sprintf("%s %s", spinner, status))
		b.WriteString("\n\n")
		
		// Show progress bar
		progressBar := fmt.Sprintf("[%s%s] %.0f%%", 
			strings.Repeat("█", int(m.packageProgress.progress*30)),
			strings.Repeat("░", 30-int(m.packageProgress.progress*30)),
			m.packageProgress.progress*100)
		b.WriteString(m.styles.Normal.Render(progressBar))
		b.WriteString("\n")
		
		// Show command info if available
		if m.packageProgress.commandInfo != "" {
			cmdInfo := m.styles.Subtle.Render("$ " + m.packageProgress.commandInfo)
			b.WriteString(cmdInfo)
			b.WriteString("\n")
		}
		
		// Show live log output
		if len(m.installationLogs) > 0 {
			b.WriteString("\n")
			logHeader := m.styles.Subtle.Render("Live Output:")
			b.WriteString(logHeader)
			b.WriteString("\n")
			
			// Show last few lines of accumulated logs
			maxLines := 8
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
		
		// Show error if any
		if m.packageProgress.error != nil {
			b.WriteString("\n")
			errorMsg := m.styles.Error.Render("Error: " + m.packageProgress.error.Error())
			b.WriteString(errorMsg)
		}
		
		// Show sudo prompt if needed
		if m.packageProgress.needsSudo {
			sudoWarning := m.styles.Warning.Render("⚠ Using provided sudo password")
			b.WriteString(sudoWarning)
		}
	} else {
		if m.packageProgress.error != nil {
			errorMsg := m.styles.Error.Render("✗ Installation failed: " + m.packageProgress.error.Error())
			b.WriteString(errorMsg)
		} else {
			success := m.styles.Success.Render("✓ Installation complete!")
			b.WriteString(success)
		}
	}
	
	return b.String()
}

func (m Model) viewInstallComplete() string {
	var b strings.Builder
	
	b.WriteString(m.renderBanner())
	b.WriteString("\n")
	
	title := m.styles.Success.Render("Setup Complete!")
	b.WriteString(title)
	b.WriteString("\n\n")
	
	success := m.styles.Success.Render("✓ All packages installed and configurations deployed.")
	b.WriteString(success)
	b.WriteString("\n\n")
	
	// Show what was accomplished
	accomplishments := []string{
		"• Window manager and dependencies installed",
		"• Terminal and development tools configured",
		"• Configuration files deployed with backups",
		"• System optimized for DankMaterialShell",
	}
	
	for _, item := range accomplishments {
		b.WriteString(m.styles.Subtle.Render(item))
		b.WriteString("\n")
	}
	
	b.WriteString("\n")
	info := m.styles.Normal.Render("Your system is ready! Log out and log back in to start using\nyour new desktop environment.\n\nPress Enter to exit.")
	b.WriteString(info)
	
	return b.String()
}

func (m Model) viewError() string {
	var b strings.Builder
	
	b.WriteString(m.renderBanner())
	b.WriteString("\n")
	
	title := m.styles.Error.Render("Installation Failed")
	b.WriteString(title)
	b.WriteString("\n\n")
	
	if m.err != nil {
		error := m.styles.Error.Render("✗ " + m.err.Error())
		b.WriteString(error)
		b.WriteString("\n\n")
	}
	
	// Show package progress error if available
	if m.packageProgress.error != nil {
		packageError := m.styles.Error.Render("Package Installation Error: " + m.packageProgress.error.Error())
		b.WriteString(packageError)
		b.WriteString("\n\n")
	}
	
	// Show persistent installation logs
	if len(m.installationLogs) > 0 {
		logHeader := m.styles.Warning.Render("Installation Logs (last 15 lines):")
		b.WriteString(logHeader)
		b.WriteString("\n")
		
		// Show more lines in error state for better debugging
		maxLines := 15
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
		b.WriteString("\n")
	}
	
	help := m.styles.Subtle.Render("Press Enter to exit")
	b.WriteString(help)
	
	return b.String()
}

func (m Model) updateInstallingPackagesState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if progressMsg, ok := msg.(packageInstallProgressMsg); ok {
		m.packageProgress = progressMsg
		
		// Accumulate log output
		if progressMsg.logOutput != "" {
			m.installationLogs = append(m.installationLogs, progressMsg.logOutput)
			// Keep only last 50 lines to preserve more context for debugging
			if len(m.installationLogs) > 50 {
				m.installationLogs = m.installationLogs[len(m.installationLogs)-50:]
			}
		}
		
		if progressMsg.isComplete {
			if progressMsg.error != nil {
				m.state = StateError
				m.isLoading = false
			} else {
				m.state = StateConfigConfirmation
				m.isLoading = true
				return m, tea.Batch(m.spinner.Tick, m.checkExistingConfigurations())
			}
		}
		return m, m.listenForPackageProgress()
	}
	return m, m.listenForLogs()
}

func (m Model) updateInstallCompleteState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			return m, tea.Quit
		}
	}
	return m, m.listenForLogs()
}

func (m Model) updateErrorState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			return m, tea.Quit
		}
	}
	return m, m.listenForLogs()
}


func (m Model) listenForPackageProgress() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.packageProgressChan
		if !ok {
			return packageProgressCompletedMsg{}
		}
		// Always return the message, completion will be handled in updateInstallingPackagesState
		return msg
	}
}