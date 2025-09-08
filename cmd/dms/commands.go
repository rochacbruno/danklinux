package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvengeMedia/dankinstall/internal/distros"
	"github.com/AvengeMedia/dankinstall/internal/dms"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dms",
	Short: "DankLinux Manager",
	Long:  "DankLinux Management CLI\n\nThe DMS management interface provides an overview of your installed\ncomponents and allows you to manage your setup.",
	Run:   runInteractiveMode,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run:   runVersion,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Launch quickshell with DMS configuration",
	Long:  "Launch quickshell with DMS configuration (qs -c dms)",
	Run: func(cmd *cobra.Command, args []string) {
		daemon, _ := cmd.Flags().GetBool("daemon")
		if daemon {
			runShellDaemon()
		} else {
			runShellInteractive()
		}
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart quickshell with DMS configuration",
	Long:  "Kill existing DMS shell processes and restart quickshell with DMS configuration",
	Run: func(cmd *cobra.Command, args []string) {
		restartShell()
	},
}

var killCmd = &cobra.Command{
	Use:   "kill",
	Short: "Kill running DMS shell processes",
	Long:  "Kill all running quickshell processes with DMS configuration",
	Run: func(cmd *cobra.Command, args []string) {
		killShell()
	},
}

var ipcCmd = &cobra.Command{
	Use:   "ipc",
	Short: "Send IPC commands to running DMS shell",
	Long:  "Send IPC commands to running DMS shell (qs -c dms ipc <args>)",
	Run: func(cmd *cobra.Command, args []string) {
		runShellIPCCommand(args)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update DankMaterialShell to the latest version",
	Long:  "Update DankMaterialShell to the latest version using the appropriate package manager for your distribution",
	Run: func(cmd *cobra.Command, args []string) {
		runUpdate()
	},
}

func runInteractiveMode(cmd *cobra.Command, args []string) {
	detector, err := dms.NewDetector()
	if err != nil {
		fmt.Printf("Error initializing DMS detector: %v\n", err)
		os.Exit(1)
	}

	if !detector.IsDMSInstalled() {
		fmt.Println("Error: DankMaterialShell (DMS) is not detected as installed on this system.")
		fmt.Println("Please install DMS using dankinstall before using this management interface.")
		os.Exit(1)
	}

	model := dms.NewModel(Version)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}

func runVersion(cmd *cobra.Command, args []string) {
	printASCII()
	fmt.Printf("DankLinux Manager v%s\n", Version)
}

func runUpdate() {
	fmt.Println("Updating DankMaterialShell...")

	// Detect the operating system
	osInfo, err := distros.GetOSInfo()
	if err != nil {
		fmt.Printf("Error detecting OS: %v\n", err)
		os.Exit(1)
	}

	var updateErr error
	switch strings.ToLower(osInfo.Distribution.ID) {
	case "arch", "cachyos", "endeavouros", "manjaro":
		updateErr = updateArchLinux()
	case "nixos":
		updateErr = updateNixOS()
	default:
		updateErr = updateOtherDistros()
	}

	if updateErr != nil {
		fmt.Printf("Error updating DMS: %v\n", updateErr)
		os.Exit(1)
	}

	fmt.Println("Update complete! Restarting DMS...")
	restartShell()
}

func updateArchLinux() error {
	var updateCmd *exec.Cmd

	if commandExists("yay") {
		fmt.Println("Using yay to update dms-shell-git...")
		updateCmd = exec.Command("yay", "-S", "--noconfirm", "dms-shell-git")
	} else if commandExists("paru") {
		fmt.Println("Using paru to update dms-shell-git...")
		updateCmd = exec.Command("paru", "-S", "--noconfirm", "dms-shell-git")
	} else {
		// Install itself doesn't depend on an AUR helper, but it's the easiest way to do updates later
		return fmt.Errorf("neither yay nor paru found - please install an AUR helper")
	}

	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	return updateCmd.Run()
}

func updateNixOS() error {
	fmt.Println("Using nix profile upgrade to update DankMaterialShell...")
	updateCmd := exec.Command("nix", "profile", "upgrade", "github:AvengeMedia/DankMaterialShell")
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	return updateCmd.Run()
}

// Manual update strategy is basically:
// cd ~/.config/quickshell/dms
// git fetch
// git branch -M master master-<timestamp>
// git checkout -b master origin/master
// dms restart
func updateOtherDistros() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	dmsPath := filepath.Join(homeDir, ".config", "quickshell", "dms")

	if _, err := os.Stat(dmsPath); os.IsNotExist(err) {
		return fmt.Errorf("DMS configuration directory not found at %s", dmsPath)
	}

	fmt.Printf("Updating DMS configuration in %s...\n", dmsPath)

	timestamp := time.Now().Format("20060102-150405")
	backupBranch := fmt.Sprintf("master-%s", timestamp)

	if err := os.Chdir(dmsPath); err != nil {
		return fmt.Errorf("failed to change to DMS directory: %w", err)
	}

	fmt.Printf("Creating backup branch: %s\n", backupBranch)
	backupCmd := exec.Command("git", "branch", "-M", "master", backupBranch)
	backupCmd.Stdout = os.Stdout
	backupCmd.Stderr = os.Stderr
	if err := backupCmd.Run(); err != nil {
		return fmt.Errorf("failed to create backup branch: %w", err)
	}

	fmt.Println("Fetching latest changes...")
	fetchCmd := exec.Command("git", "fetch")
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch changes: %w", err)
	}

	fmt.Println("Checking out latest master branch...")
	checkoutCmd := exec.Command("git", "checkout", "-b", "master", "origin/master")
	checkoutCmd.Stdout = os.Stdout
	checkoutCmd.Stderr = os.Stderr
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout new master: %w", err)
	}

	fmt.Printf("Successfully updated DMS! Previous version backed up as branch '%s'\n", backupBranch)
	return nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
