package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) viewPasswordPrompt() string {
	var b strings.Builder

	b.WriteString(m.renderBanner())
	b.WriteString("\n")

	title := m.styles.Title.Render("Sudo Authentication")
	b.WriteString(title)
	b.WriteString("\n\n")

	message := "Installation requires sudo privileges.\nPlease enter your password to continue:"
	b.WriteString(m.styles.Normal.Render(message))
	b.WriteString("\n\n")

	// Password input
	b.WriteString(m.passwordInput.View())
	b.WriteString("\n")

	// Show validation status
	if m.packageProgress.step == "Validating sudo password..." {
		spinner := m.spinner.View()
		status := m.styles.Normal.Render(m.packageProgress.step)
		b.WriteString(spinner + " " + status)
		b.WriteString("\n")
	} else if m.packageProgress.error != nil {
		errorMsg := m.styles.Error.Render("✗ " + m.packageProgress.error.Error() + ". Please try again.")
		b.WriteString(errorMsg)
		b.WriteString("\n")
	} else if m.packageProgress.step == "Password validation failed" {
		errorMsg := m.styles.Error.Render("✗ Incorrect password. Please try again.")
		b.WriteString(errorMsg)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := m.styles.Subtle.Render("Enter: Continue, Esc: Back, Ctrl+C: Cancel")
	b.WriteString(help)

	return b.String()
}

func (m Model) updatePasswordPromptState(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if validMsg, ok := msg.(passwordValidMsg); ok {
		if validMsg.valid {
			// Password is valid, proceed with installation
			m.sudoPassword = validMsg.password
			m.passwordInput.SetValue("") // Clear password input
			// Clear any error state
			m.packageProgress = packageInstallProgressMsg{}
			m.state = StateInstallingPackages
			m.isLoading = true
			return m, tea.Batch(m.spinner.Tick, m.installPackages())
		} else {
			// Password is invalid, show error and stay on password prompt
			m.packageProgress = packageInstallProgressMsg{
				progress:  0.0,
				step:      "Password validation failed",
				error:     fmt.Errorf("incorrect password"),
				logOutput: "Authentication failed",
			}
			m.passwordInput.SetValue("")
			m.passwordInput.Focus()
			return m, nil
		}
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			// Don't allow multiple validation attempts while one is in progress
			if m.packageProgress.step == "Validating sudo password..." {
				return m, nil
			}

			// Validate password first
			password := m.passwordInput.Value()
			if password == "" {
				return m, nil // Don't proceed with empty password
			}

			// Clear any previous error and show validation in progress
			m.packageProgress = packageInstallProgressMsg{
				progress:   0.01,
				step:       "Validating sudo password...",
				isComplete: false,
				logOutput:  "Testing password with sudo -v",
			}
			return m, m.validatePassword(password)
		case "esc":
			// Go back to dependency review
			m.passwordInput.SetValue("")
			m.packageProgress = packageInstallProgressMsg{} // Clear any validation state
			m.state = StateDependencyReview
			return m, nil
		}
	}

	m.passwordInput, cmd = m.passwordInput.Update(msg)
	return m, cmd
}

func (m Model) validatePassword(password string) tea.Cmd {
	return func() tea.Msg {
		// Test password with sudo -v (validate)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Use a more reliable command that will definitely fail with wrong password
		cmdStr := fmt.Sprintf("echo '%s' | sudo -S -v", password)
		cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)

		// Capture both stdout and stderr to see what's happening
		output, err := cmd.CombinedOutput()

		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				// Timeout - probably stuck waiting for password
				return passwordValidMsg{password: "", valid: false}
			}

			outputStr := string(output)
			if strings.Contains(outputStr, "Sorry, try again") ||
				strings.Contains(outputStr, "incorrect password") ||
				strings.Contains(outputStr, "authentication failure") {
				return passwordValidMsg{password: "", valid: false}
			}

			// Other error - probably authentication failure
			return passwordValidMsg{password: "", valid: false}
		}

		// Command succeeded - password is valid
		return passwordValidMsg{password: password, valid: true}
	}
}
