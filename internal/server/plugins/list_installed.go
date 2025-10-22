package plugins

import (
	"fmt"
	"net"
	"strings"

	"github.com/AvengeMedia/danklinux/internal/plugins"
	"github.com/AvengeMedia/danklinux/internal/server/models"
)

func HandleListInstalled(conn net.Conn, req models.Request) {
	manager, err := plugins.NewManager()
	if err != nil {
		models.RespondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	installedNames, err := manager.ListInstalled()
	if err != nil {
		models.RespondError(conn, req.ID, fmt.Sprintf("failed to list installed plugins: %v", err))
		return
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		models.RespondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	allPlugins, err := registry.List()
	if err != nil {
		models.RespondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	pluginMap := make(map[string]plugins.Plugin)
	for _, p := range allPlugins {
		pluginMap[p.Name] = p
	}

	result := make([]PluginInfo, 0, len(installedNames))
	for _, name := range installedNames {
		if plugin, ok := pluginMap[name]; ok {
			hasUpdate := false
			// Check if plugin has updates available
			if hasUpdates, err := manager.HasUpdates(name, plugin); err == nil {
				hasUpdate = hasUpdates
			}

			result = append(result, PluginInfo{
				Name:         plugin.Name,
				Category:     plugin.Category,
				Author:       plugin.Author,
				Description:  plugin.Description,
				Repo:         plugin.Repo,
				Path:         plugin.Path,
				Capabilities: plugin.Capabilities,
				Compositors:  plugin.Compositors,
				Dependencies: plugin.Dependencies,
				FirstParty:   strings.HasPrefix(plugin.Repo, "https://github.com/AvengeMedia"),
				HasUpdate:    hasUpdate,
			})
		} else {
			result = append(result, PluginInfo{
				Name: name,
				Note: "not in registry",
			})
		}
	}

	SortPluginInfoByFirstParty(result)

	models.Respond(conn, req.ID, result)
}
