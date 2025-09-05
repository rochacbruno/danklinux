package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func init() {
	// Add flags
	shellCmd.Flags().BoolP("daemon", "d", false, "Run in daemon mode")

	// Add subcommands to shell
	shellCmd.AddCommand(shellDaemonCmd)
	shellCmd.AddCommand(shellRestartCmd)
	shellCmd.AddCommand(shellKillCmd)
	shellCmd.AddCommand(shellIPCCmd)

	rootCmd.AddCommand(versionCmd, shellCmd)
	rootCmd.SetHelpTemplate(getHelpTemplate())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
