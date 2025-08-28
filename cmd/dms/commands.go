package main

import (
	"fmt"
	"os"

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

var shellCmd = &cobra.Command{
	Use:   "shell",
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

var shellIPCCmd = &cobra.Command{
	Use:   "ipc",
	Short: "Send IPC commands to running DMS shell",
	Long:  "Send IPC commands to running DMS shell (qs -c dms ipc <args>)",
	Run: func(cmd *cobra.Command, args []string) {
		runShellIPCCommand(args)
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
