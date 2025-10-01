package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func init() {
	// Add flags
	runCmd.Flags().BoolP("daemon", "d", false, "Run in daemon mode")

	// Add subcommands to greeter
	greeterCmd.AddCommand(greeterInstallCmd)

	// Add commands to root
	rootCmd.AddCommand(versionCmd, runCmd, restartCmd, killCmd, ipcCmd, updateCmd, greeterCmd)
	rootCmd.SetHelpTemplate(getHelpTemplate())
}

func main() {
	// Block root
	if os.Geteuid() == 0 {
		fmt.Println("This program should not be run as root. Exiting.")
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
