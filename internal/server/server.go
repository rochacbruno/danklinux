package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/plugins"
)

type Request struct {
	ID     interface{}            `json:"id,omitempty"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type Response[T any] struct {
	ID     interface{} `json:"id,omitempty"`
	Result *T          `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type PluginInfo struct {
	Name         string   `json:"name"`
	Category     string   `json:"category,omitempty"`
	Author       string   `json:"author,omitempty"`
	Description  string   `json:"description,omitempty"`
	Repo         string   `json:"repo,omitempty"`
	Path         string   `json:"path,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Compositors  []string `json:"compositors,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Installed    bool     `json:"installed,omitempty"`
	FirstParty   bool     `json:"firstParty,omitempty"`
	Note         string   `json:"note,omitempty"`
}

type SuccessResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func sortPluginInfoByFirstParty(pluginInfos []PluginInfo) {
	sort.SliceStable(pluginInfos, func(i, j int) bool {
		isFirstPartyI := strings.HasPrefix(pluginInfos[i].Repo, "https://github.com/AvengeMedia")
		isFirstPartyJ := strings.HasPrefix(pluginInfos[j].Repo, "https://github.com/AvengeMedia")
		if isFirstPartyI != isFirstPartyJ {
			return isFirstPartyI
		}
		return false
	})
}

func getSocketDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return runtime
	}

	if os.Getuid() == 0 {
		if _, err := os.Stat("/run"); err == nil {
			return "/run/dankdots"
		}
		return "/var/run/dankdots"
	}

	return os.TempDir()
}

func GetSocketPath() string {
	return filepath.Join(getSocketDir(), fmt.Sprintf("danklinux-%d.sock", os.Getpid()))
}

func cleanupStaleSockets() {
	dir := getSocketDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "danklinux-") || !strings.HasSuffix(entry.Name(), ".sock") {
			continue
		}

		// Extract PID from filename: danklinux-<pid>.sock
		pidStr := strings.TrimPrefix(entry.Name(), "danklinux-")
		pidStr = strings.TrimSuffix(pidStr, ".sock")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Check if process exists by sending signal 0
		process, err := os.FindProcess(pid)
		if err != nil {
			// Process doesn't exist, remove socket
			socketPath := filepath.Join(dir, entry.Name())
			os.Remove(socketPath)
			log.Debugf("Removed stale socket: %s", socketPath)
			continue
		}

		// On Unix, FindProcess always succeeds, so we need to send a signal to check
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			// Process doesn't exist, remove socket
			socketPath := filepath.Join(dir, entry.Name())
			os.Remove(socketPath)
			log.Debugf("Removed stale socket: %s", socketPath)
		}
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			respondError(conn, nil, "invalid json")
			continue
		}

		handleRequest(conn, req)
	}
}

func respondError(conn net.Conn, id interface{}, errMsg string) {
	log.Errorf("DMS API Error: id=%v error=%s", id, errMsg)
	resp := Response[any]{ID: id, Error: errMsg}
	json.NewEncoder(conn).Encode(resp)
}

func respond[T any](conn net.Conn, id interface{}, result T) {
	resp := Response[T]{ID: id, Result: &result}
	json.NewEncoder(conn).Encode(resp)
}

func handleRequest(conn net.Conn, req Request) {
	log.Debugf("DMS API Request: method=%s id=%v", req.Method, req.ID)

	switch req.Method {
	case "plugins.list":
		handlePluginsList(conn, req)
	case "plugins.listInstalled":
		handlePluginsListInstalled(conn, req)
	case "plugins.install":
		handlePluginsInstall(conn, req)
	case "plugins.uninstall":
		handlePluginsUninstall(conn, req)
	case "plugins.update":
		handlePluginsUpdate(conn, req)
	case "plugins.search":
		handlePluginsSearch(conn, req)
	case "ping":
		respond(conn, req.ID, "pong")
	default:
		respondError(conn, req.ID, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func handlePluginsList(conn net.Conn, req Request) {
	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	pluginList, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	result := make([]PluginInfo, len(pluginList))
	for i, p := range pluginList {
		installed, _ := manager.IsInstalled(p)
		result[i] = PluginInfo{
			Name:         p.Name,
			Category:     p.Category,
			Author:       p.Author,
			Description:  p.Description,
			Repo:         p.Repo,
			Path:         p.Path,
			Capabilities: p.Capabilities,
			Compositors:  p.Compositors,
			Dependencies: p.Dependencies,
			Installed:    installed,
			FirstParty:   strings.HasPrefix(p.Repo, "https://github.com/AvengeMedia"),
		}
	}

	respond(conn, req.ID, result)
}

func handlePluginsListInstalled(conn net.Conn, req Request) {
	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	installedNames, err := manager.ListInstalled()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list installed plugins: %v", err))
		return
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	allPlugins, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	pluginMap := make(map[string]plugins.Plugin)
	for _, p := range allPlugins {
		pluginMap[p.Name] = p
	}

	result := make([]PluginInfo, 0, len(installedNames))
	for _, name := range installedNames {
		if plugin, ok := pluginMap[name]; ok {
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
			})
		} else {
			result = append(result, PluginInfo{
				Name: name,
				Note: "not in registry",
			})
		}
	}

	sortPluginInfoByFirstParty(result)

	respond(conn, req.ID, result)
}

func handlePluginsInstall(conn net.Conn, req Request) {
	name, ok := req.Params["name"].(string)
	if !ok {
		respondError(conn, req.ID, "missing or invalid 'name' parameter")
		return
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	pluginList, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	var plugin *plugins.Plugin
	for _, p := range pluginList {
		if p.Name == name {
			plugin = &p
			break
		}
	}

	if plugin == nil {
		respondError(conn, req.ID, fmt.Sprintf("plugin not found: %s", name))
		return
	}

	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	if err := manager.Install(*plugin); err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to install plugin: %v", err))
		return
	}

	respond(conn, req.ID, SuccessResult{
		Success: true,
		Message: fmt.Sprintf("plugin installed: %s", name),
	})
}

func handlePluginsUninstall(conn net.Conn, req Request) {
	name, ok := req.Params["name"].(string)
	if !ok {
		respondError(conn, req.ID, "missing or invalid 'name' parameter")
		return
	}

	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	// Check if plugin is installed
	installed, err := manager.IsInstalled(plugins.Plugin{Name: name})
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to check if plugin is installed: %v", err))
		return
	}

	if !installed {
		respondError(conn, req.ID, fmt.Sprintf("plugin not installed: %s", name))
		return
	}

	// Try to get plugin info from registry for proper uninstall (handles monorepo metadata)
	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	pluginList, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	var plugin *plugins.Plugin
	for _, p := range pluginList {
		if p.Name == name {
			plugin = &p
			break
		}
	}

	// If not in registry, create a minimal plugin object for uninstall
	if plugin == nil {
		plugin = &plugins.Plugin{Name: name}
	}

	if err := manager.Uninstall(*plugin); err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to uninstall plugin: %v", err))
		return
	}

	respond(conn, req.ID, SuccessResult{
		Success: true,
		Message: fmt.Sprintf("plugin uninstalled: %s", name),
	})
}

func handlePluginsUpdate(conn net.Conn, req Request) {
	name, ok := req.Params["name"].(string)
	if !ok {
		respondError(conn, req.ID, "missing or invalid 'name' parameter")
		return
	}

	log.Debugf("DMS API Update request for plugin: %s", name)

	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	// Check if plugin is installed
	installed, err := manager.IsInstalled(plugins.Plugin{Name: name})
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to check if plugin is installed: %v", err))
		return
	}

	if !installed {
		respondError(conn, req.ID, fmt.Sprintf("plugin not installed: %s", name))
		return
	}

	// Try to get plugin info from registry for proper update (repo URL needed)
	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	pluginList, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	var plugin *plugins.Plugin
	for _, p := range pluginList {
		if p.Name == name {
			plugin = &p
			break
		}
	}

	// If not in registry, create minimal plugin object (update will still work via git pull)
	if plugin == nil {
		plugin = &plugins.Plugin{Name: name}
	}

	if err := manager.Update(*plugin); err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to update plugin: %v", err))
		return
	}

	respond(conn, req.ID, SuccessResult{
		Success: true,
		Message: fmt.Sprintf("plugin updated: %s", name),
	})
}

func handlePluginsSearch(conn net.Conn, req Request) {
	query, ok := req.Params["query"].(string)
	if !ok {
		respondError(conn, req.ID, "missing or invalid 'query' parameter")
		return
	}

	registry, err := plugins.NewRegistry()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create registry: %v", err))
		return
	}

	pluginList, err := registry.List()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to list plugins: %v", err))
		return
	}

	searchResults := plugins.FuzzySearch(query, pluginList)

	if category, ok := req.Params["category"].(string); ok && category != "" {
		searchResults = plugins.FilterByCategory(category, searchResults)
	}

	if compositor, ok := req.Params["compositor"].(string); ok && compositor != "" {
		searchResults = plugins.FilterByCompositor(compositor, searchResults)
	}

	if capability, ok := req.Params["capability"].(string); ok && capability != "" {
		searchResults = plugins.FilterByCapability(capability, searchResults)
	}

	searchResults = plugins.SortByFirstParty(searchResults)

	manager, err := plugins.NewManager()
	if err != nil {
		respondError(conn, req.ID, fmt.Sprintf("failed to create manager: %v", err))
		return
	}

	result := make([]PluginInfo, len(searchResults))
	for i, p := range searchResults {
		installed, _ := manager.IsInstalled(p)
		result[i] = PluginInfo{
			Name:         p.Name,
			Category:     p.Category,
			Author:       p.Author,
			Description:  p.Description,
			Repo:         p.Repo,
			Path:         p.Path,
			Capabilities: p.Capabilities,
			Compositors:  p.Compositors,
			Dependencies: p.Dependencies,
			Installed:    installed,
			FirstParty:   strings.HasPrefix(p.Repo, "https://github.com/AvengeMedia"),
		}
	}

	respond(conn, req.ID, result)
}

func Start() error {
	cleanupStaleSockets()

	socketPath := GetSocketPath()
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Infof("DMS API Server listening on: %s", socketPath)
	log.Info("\nProtocol: JSON over Unix socket")
	log.Info("Request format: {\"id\": <any>, \"method\": \"...\", \"params\": {...}}")
	log.Info("Response format: {\"id\": <any>, \"result\": {...}} or {\"id\": <any>, \"error\": \"...\"}")
	log.Info("\nAvailable methods:")
	log.Info("  ping - Test connection")
	log.Info("  plugins.list - List all plugins")
	log.Info("  plugins.listInstalled - List installed plugins")
	log.Info("  plugins.install - Install plugin (params: name)")
	log.Info("  plugins.uninstall - Uninstall plugin (params: name)")
	log.Info("  plugins.update - Update plugin (params: name)")
	log.Info("  plugins.search - Search plugins (params: query, category?, compositor?, capability?)")

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConnection(conn)
	}
}
