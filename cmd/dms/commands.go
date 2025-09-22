package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvengeMedia/dankinstall/internal/distros"
	"github.com/AvengeMedia/dankinstall/internal/dms"
	"github.com/AvengeMedia/dankinstall/internal/errdefs"
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
	if err != nil && !errors.Is(err, &distros.UnsupportedDistributionError{}) {
		fmt.Printf("Error initializing DMS detector: %v\n", err)
		os.Exit(1)
	} else if (errors.Is(err, &distros.UnsupportedDistributionError{})) {
		fmt.Println("Interactive mode is not supported on this distribution.")
		fmt.Println("Please run 'dms --help' for available commands.")
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
		if errors.Is(updateErr, errdefs.ErrUpdateCancelled) {
			fmt.Println("Update cancelled.")
			return
		}
		fmt.Printf("Error updating DMS: %v\n", updateErr)
		os.Exit(1)
	}

	fmt.Println("Update complete! Restarting DMS...")
	restartShell()
}

func updateArchLinux() error {
	// Check if dms-shell-git is installed via pacman
	if !isPackageInstalled("dms-shell-git") {
		fmt.Println("Info: dms-shell-git package not found in installed packages.")
		fmt.Println("Info: Falling back to git-based update method...")
		return updateOtherDistros()
	}

	var helper string
	var updateCmd *exec.Cmd

	if commandExists("yay") {
		helper = "yay"
		updateCmd = exec.Command("yay", "-S", "dms-shell-git")
	} else if commandExists("paru") {
		helper = "paru"
		updateCmd = exec.Command("paru", "-S", "dms-shell-git")
	} else {
		fmt.Println("Error: Neither yay nor paru found - please install an AUR helper")
		fmt.Println("Info: Falling back to git-based update method...")
		return updateOtherDistros()
	}

	fmt.Printf("This will update DankMaterialShell using %s.\n", helper)
	if !confirmUpdate() {
		return errdefs.ErrUpdateCancelled
	}

	fmt.Printf("\nRunning: %s -S dms-shell-git\n", helper)
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	err := updateCmd.Run()
	if err != nil {
		fmt.Printf("Error: Failed to update using %s: %v\n", helper, err)
	}

	fmt.Println("dms successfully updated")
	return nil
}

func updateNixOS() error {
	fmt.Println("This will update DankMaterialShell using nix profile.")
	if !confirmUpdate() {
		return errdefs.ErrUpdateCancelled
	}

	fmt.Println("\nRunning: nix profile upgrade github:AvengeMedia/DankMaterialShell")
	updateCmd := exec.Command("nix", "profile", "upgrade", "github:AvengeMedia/DankMaterialShell")
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	err := updateCmd.Run()
	if err != nil {
		fmt.Printf("Error: Failed to update using nix profile: %v\n", err)
		fmt.Println("Falling back to git-based update method...")
		return updateOtherDistros()
	}

	fmt.Println("dms successfully updated")
	return nil
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

	fmt.Printf("Found DMS configuration at %s\n", dmsPath)
	fmt.Println("\nThis will update DankMaterialShell using git.")
	fmt.Println("Your current configuration will be backed up to a timestamped branch.")
	if !confirmUpdate() {
		return errdefs.ErrUpdateCancelled
	}

	timestamp := time.Now().Unix()
	backupBranch := fmt.Sprintf("master-%d", timestamp)

	if err := os.Chdir(dmsPath); err != nil {
		return fmt.Errorf("failed to change to DMS directory: %w", err)
	}

	fmt.Printf("\nCreating backup branch: %s\n", backupBranch)
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

	fmt.Printf("dms successfully updated (previous version backed up as branch '%s')\n", backupBranch)
	return nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func isPackageInstalled(packageName string) bool {
	cmd := exec.Command("pacman", "-Q", packageName)
	err := cmd.Run()
	return err == nil
}

func confirmUpdate() bool {
	fmt.Print("Do you want to proceed with the update? (y/N): ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading input: %v\n", err)
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
