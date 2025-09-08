package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func init() {
	// Add flags
	runCmd.Flags().BoolP("daemon", "d", false, "Run in daemon mode")

	// Add commands to root
	rootCmd.AddCommand(versionCmd, runCmd, restartCmd, killCmd, ipcCmd, updateCmd)
	rootCmd.SetHelpTemplate(getHelpTemplate())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
