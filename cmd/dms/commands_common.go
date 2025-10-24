package main

import (
	"fmt"
	"strings"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/plugins"
	"github.com/AvengeMedia/danklinux/internal/server"
	"github.com/spf13/cobra"
)

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

var debugSrvCmd = &cobra.Command{
	Use:   "debug-srv",
	Short: "Start the debug server",
	Long:  "Start the Unix socket debug server for DMS",
	Run: func(cmd *cobra.Command, args []string) {
		if err := startDebugServer(); err != nil {
			log.Fatalf("Error starting debug server: %v", err)
		}
	},
}

var pluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "Manage DMS plugins",
	Long:  "Browse and manage DMS plugins from the registry",
}

var pluginsBrowseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Browse available plugins",
	Long:  "Browse available plugins from the DMS plugin registry",
	Run: func(cmd *cobra.Command, args []string) {
		if err := browsePlugins(); err != nil {
			log.Fatalf("Error browsing plugins: %v", err)
		}
	},
}

var pluginsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins",
	Long:  "List all installed DMS plugins",
	Run: func(cmd *cobra.Command, args []string) {
		if err := listInstalledPlugins(); err != nil {
			log.Fatalf("Error listing plugins: %v", err)
		}
	},
}

var pluginsInstallCmd = &cobra.Command{
	Use:   "install <plugin-id>",
	Short: "Install a plugin by ID",
	Long:  "Install a DMS plugin from the registry using its ID (e.g., 'myPlugin'). Plugin names with spaces are also supported for backward compatibility.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := installPluginCLI(args[0]); err != nil {
			log.Fatalf("Error installing plugin: %v", err)
		}
	},
}

var pluginsUninstallCmd = &cobra.Command{
	Use:   "uninstall <plugin-id>",
	Short: "Uninstall a plugin by ID",
	Long:  "Uninstall a DMS plugin using its ID (e.g., 'myPlugin'). Plugin names with spaces are also supported for backward compatibility.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := uninstallPluginCLI(args[0]); err != nil {
			log.Fatalf("Error uninstalling plugin: %v", err)
		}
	},
}

func runVersion(cmd *cobra.Command, args []string) {
	printASCII()
	fmt.Printf("%s\n", Version)
}

func startDebugServer() error {
	return server.Start(true)
}

func browsePlugins() error {
	registry, err := plugins.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}

	manager, err := plugins.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	fmt.Println("Fetching plugin registry...")
	pluginList, err := registry.List()
	if err != nil {
		return fmt.Errorf("failed to list plugins: %w", err)
	}

	if len(pluginList) == 0 {
		fmt.Println("No plugins found in registry.")
		return nil
	}

	fmt.Printf("\nAvailable Plugins (%d):\n\n", len(pluginList))
	for _, plugin := range pluginList {
		installed, _ := manager.IsInstalled(plugin)
		installedMarker := ""
		if installed {
			installedMarker = " [Installed]"
		}

		fmt.Printf("  %s%s\n", plugin.Name, installedMarker)
		fmt.Printf("    ID: %s\n", plugin.ID)
		fmt.Printf("    Category: %s\n", plugin.Category)
		fmt.Printf("    Author: %s\n", plugin.Author)
		fmt.Printf("    Description: %s\n", plugin.Description)
		fmt.Printf("    Repository: %s\n", plugin.Repo)
		if len(plugin.Capabilities) > 0 {
			fmt.Printf("    Capabilities: %s\n", strings.Join(plugin.Capabilities, ", "))
		}
		if len(plugin.Compositors) > 0 {
			fmt.Printf("    Compositors: %s\n", strings.Join(plugin.Compositors, ", "))
		}
		if len(plugin.Dependencies) > 0 {
			fmt.Printf("    Dependencies: %s\n", strings.Join(plugin.Dependencies, ", "))
		}
		fmt.Println()
	}

	return nil
}

func listInstalledPlugins() error {
	manager, err := plugins.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}

	installedNames, err := manager.ListInstalled()
	if err != nil {
		return fmt.Errorf("failed to list installed plugins: %w", err)
	}

	if len(installedNames) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	allPlugins, err := registry.List()
	if err != nil {
		return fmt.Errorf("failed to list plugins: %w", err)
	}

	pluginMap := make(map[string]plugins.Plugin)
	for _, p := range allPlugins {
		pluginMap[p.ID] = p
	}

	fmt.Printf("\nInstalled Plugins (%d):\n\n", len(installedNames))
	for _, id := range installedNames {
		if plugin, ok := pluginMap[id]; ok {
			fmt.Printf("  %s\n", plugin.Name)
			fmt.Printf("    ID: %s\n", plugin.ID)
			fmt.Printf("    Category: %s\n", plugin.Category)
			fmt.Printf("    Author: %s\n", plugin.Author)
			fmt.Println()
		} else {
			fmt.Printf("  %s (not in registry)\n\n", id)
		}
	}

	return nil
}

func installPluginCLI(idOrName string) error {
	registry, err := plugins.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}

	manager, err := plugins.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	pluginList, err := registry.List()
	if err != nil {
		return fmt.Errorf("failed to list plugins: %w", err)
	}

	// First, try to find by ID (preferred method)
	var plugin *plugins.Plugin
	for _, p := range pluginList {
		if p.ID == idOrName {
			plugin = &p
			break
		}
	}

	// Fallback to name for backward compatibility
	if plugin == nil {
		for _, p := range pluginList {
			if p.Name == idOrName {
				plugin = &p
				break
			}
		}
	}

	if plugin == nil {
		return fmt.Errorf("plugin not found: %s", idOrName)
	}

	installed, err := manager.IsInstalled(*plugin)
	if err != nil {
		return fmt.Errorf("failed to check install status: %w", err)
	}

	if installed {
		return fmt.Errorf("plugin already installed: %s", plugin.Name)
	}

	fmt.Printf("Installing plugin: %s (ID: %s)\n", plugin.Name, plugin.ID)
	if err := manager.Install(*plugin); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	fmt.Printf("Plugin installed successfully: %s\n", plugin.Name)
	return nil
}

func uninstallPluginCLI(idOrName string) error {
	manager, err := plugins.NewManager()
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create registry: %w", err)
	}

	pluginList, err := registry.List()
	if err != nil {
		return fmt.Errorf("failed to list plugins: %w", err)
	}

	// First, try to find by ID (preferred method)
	var plugin *plugins.Plugin
	for _, p := range pluginList {
		if p.ID == idOrName {
			plugin = &p
			break
		}
	}

	// Fallback to name for backward compatibility
	if plugin == nil {
		for _, p := range pluginList {
			if p.Name == idOrName {
				plugin = &p
				break
			}
		}
	}

	if plugin == nil {
		return fmt.Errorf("plugin not found: %s", idOrName)
	}

	installed, err := manager.IsInstalled(*plugin)
	if err != nil {
		return fmt.Errorf("failed to check install status: %w", err)
	}

	if !installed {
		return fmt.Errorf("plugin not installed: %s", plugin.Name)
	}

	fmt.Printf("Uninstalling plugin: %s (ID: %s)\n", plugin.Name, plugin.ID)
	if err := manager.Uninstall(*plugin); err != nil {
		return fmt.Errorf("failed to uninstall plugin: %w", err)
	}

	fmt.Printf("Plugin uninstalled successfully: %s\n", plugin.Name)
	return nil
}
