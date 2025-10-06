package main

import (
	"os"

	"github.com/AvengeMedia/danklinux/internal/log"
)

var Version = "dev"

func init() {
	// Add flags
	runCmd.Flags().BoolP("daemon", "d", false, "Run in daemon mode")

	// Add subcommands to greeter
	greeterCmd.AddCommand(greeterInstallCmd)

	// Add subcommands to plugins
	pluginsCmd.AddCommand(pluginsBrowseCmd, pluginsListCmd, pluginsInstallCmd, pluginsUninstallCmd)

	// Add commands to root
	rootCmd.AddCommand(versionCmd, runCmd, restartCmd, killCmd, ipcCmd, updateCmd, greeterCmd, debugSrvCmd, pluginsCmd)
	rootCmd.SetHelpTemplate(getHelpTemplate())
}

func main() {
	// Block root
	if os.Geteuid() == 0 {
		log.Fatal("This program should not be run as root. Exiting.")
	}

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
