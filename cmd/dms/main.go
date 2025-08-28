package main

import (
	"fmt"
	"os"
)

var Version = "dev"

func init() {
	shellCmd.Flags().BoolP("daemon", "d", false, "Run as daemon (background process)")
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
