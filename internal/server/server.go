package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/server/freedesktop"
	"github.com/AvengeMedia/danklinux/internal/server/loginctl"
	"github.com/AvengeMedia/danklinux/internal/server/models"
	"github.com/AvengeMedia/danklinux/internal/server/network"
)

const APIVersion = 4

type Capabilities struct {
	Capabilities []string `json:"capabilities"`
}

type ServerInfo struct {
	APIVersion   int      `json:"apiVersion"`
	Capabilities []string `json:"capabilities"`
}

type ServiceEvent struct {
	Service string      `json:"service"`
	Data    interface{} `json:"data"`
}

var networkManager *network.Manager
var loginctlManager *loginctl.Manager
var freedesktopManager *freedesktop.Manager

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

		pidStr := strings.TrimPrefix(entry.Name(), "danklinux-")
		pidStr = strings.TrimSuffix(pidStr, ".sock")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			socketPath := filepath.Join(dir, entry.Name())
			os.Remove(socketPath)
			log.Debugf("Removed stale socket: %s", socketPath)
			continue
		}

		err = process.Signal(syscall.Signal(0))
		if err != nil {
			socketPath := filepath.Join(dir, entry.Name())
			os.Remove(socketPath)
			log.Debugf("Removed stale socket: %s", socketPath)
		}
	}
}

func InitializeNetworkManager() error {
	manager, err := network.NewManager()
	if err != nil {
		log.Warnf("Failed to initialize network manager: %v", err)
		return err
	}

	networkManager = manager

	log.Info("Network manager initialized")
	return nil
}

func InitializeLoginctlManager() error {
	manager, err := loginctl.NewManager()
	if err != nil {
		log.Warnf("Failed to initialize loginctl manager: %v", err)
		return err
	}

	loginctlManager = manager

	log.Info("Loginctl manager initialized")
	return nil
}

func InitializeFreedeskManager() error {
	manager, err := freedesktop.NewManager()
	if err != nil {
		log.Warnf("Failed to initialize freedesktop manager: %v", err)
		return err
	}

	freedesktopManager = manager

	log.Info("Freedesktop manager initialized")
	return nil
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	caps := getCapabilities()
	capsData, _ := json.Marshal(caps)
	conn.Write(capsData)
	conn.Write([]byte("\n"))

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()

		var req models.Request
		if err := json.Unmarshal(line, &req); err != nil {
			models.RespondError(conn, 0, "invalid json")
			continue
		}

		RouteRequest(conn, req)
	}
}

func getCapabilities() Capabilities {
	caps := []string{"plugins"}

	if networkManager != nil {
		caps = append(caps, "network")
	}

	if loginctlManager != nil {
		caps = append(caps, "loginctl")
	}

	if freedesktopManager != nil {
		caps = append(caps, "freedesktop")
	}

	return Capabilities{Capabilities: caps}
}

func getServerInfo() ServerInfo {
	caps := []string{"plugins"}

	if networkManager != nil {
		caps = append(caps, "network")
	}

	if loginctlManager != nil {
		caps = append(caps, "loginctl")
	}

	if freedesktopManager != nil {
		caps = append(caps, "freedesktop")
	}

	return ServerInfo{
		APIVersion:   APIVersion,
		Capabilities: caps,
	}
}

func handleSubscribe(conn net.Conn, req models.Request) {
	clientID := fmt.Sprintf("meta-client-%p", conn)

	var services []string
	if servicesParam, ok := req.Params["services"].([]interface{}); ok {
		for _, s := range servicesParam {
			if str, ok := s.(string); ok {
				services = append(services, str)
			}
		}
	}

	if len(services) == 0 {
		services = []string{"all"}
	}

	subscribeAll := false
	for _, s := range services {
		if s == "all" {
			subscribeAll = true
			break
		}
	}

	var wg sync.WaitGroup
	eventChan := make(chan ServiceEvent, 256)
	stopChan := make(chan struct{})

	shouldSubscribe := func(service string) bool {
		if subscribeAll {
			return true
		}
		for _, s := range services {
			if s == service {
				return true
			}
		}
		return false
	}

	if shouldSubscribe("network") && networkManager != nil {
		wg.Add(1)
		netChan := networkManager.Subscribe(clientID + "-network")
		go func() {
			defer wg.Done()
			defer networkManager.Unsubscribe(clientID + "-network")

			initialState := networkManager.GetState()
			select {
			case eventChan <- ServiceEvent{Service: "network", Data: initialState}:
			case <-stopChan:
				return
			}

			for {
				select {
				case state, ok := <-netChan:
					if !ok {
						return
					}
					select {
					case eventChan <- ServiceEvent{Service: "network", Data: state}:
					case <-stopChan:
						return
					}
				case <-stopChan:
					return
				}
			}
		}()
	}

	if shouldSubscribe("loginctl") && loginctlManager != nil {
		wg.Add(1)
		loginChan := loginctlManager.Subscribe(clientID + "-loginctl")
		go func() {
			defer wg.Done()
			defer loginctlManager.Unsubscribe(clientID + "-loginctl")

			initialState := loginctlManager.GetState()
			select {
			case eventChan <- ServiceEvent{Service: "loginctl", Data: initialState}:
			case <-stopChan:
				return
			}

			for {
				select {
				case state, ok := <-loginChan:
					if !ok {
						return
					}
					select {
					case eventChan <- ServiceEvent{Service: "loginctl", Data: state}:
					case <-stopChan:
						return
					}
				case <-stopChan:
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(eventChan)
	}()

	info := getServerInfo()
	if err := json.NewEncoder(conn).Encode(models.Response[ServiceEvent]{
		ID:     req.ID,
		Result: &ServiceEvent{Service: "server", Data: info},
	}); err != nil {
		close(stopChan)
		return
	}

	for event := range eventChan {
		if err := json.NewEncoder(conn).Encode(models.Response[ServiceEvent]{
			ID:     req.ID,
			Result: &event,
		}); err != nil {
			close(stopChan)
			return
		}
	}
}

func cleanupManagers() {
	if networkManager != nil {
		networkManager.Close()
	}
	if loginctlManager != nil {
		loginctlManager.Close()
	}
	if freedesktopManager != nil {
		freedesktopManager.Close()
	}
}

func Start(printDocs bool) error {
	cleanupStaleSockets()

	socketPath := GetSocketPath()
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer cleanupManagers()

	go func() {
		if err := InitializeNetworkManager(); err != nil {
			log.Warnf("Network manager unavailable: %v", err)
		}
	}()

	go func() {
		if err := InitializeLoginctlManager(); err != nil {
			log.Warnf("Loginctl manager unavailable: %v", err)
		}
	}()

	go func() {
		if err := InitializeFreedeskManager(); err != nil {
			log.Warnf("Freedesktop manager unavailable: %v", err)
		}
	}()

	log.Infof("DMS API Server listening on: %s", socketPath)
	log.Info("Protocol: JSON over Unix socket")
	log.Info("Request format: {\"id\": <any>, \"method\": \"...\", \"params\": {...}}")
	log.Info("Response format: {\"id\": <any>, \"result\": {...}} or {\"id\": <any>, \"error\": \"...\"}")
	if printDocs {
		log.Info("Available methods:")
		log.Info("  ping          - Test connection")
		log.Info("  getServerInfo - Get server info (API version and capabilities)")
		log.Info("  subscribe     - Subscribe to multiple services (params: services [default: all])")
		log.Info("Plugins:")
		log.Info(" plugins.list                - List all plugins")
		log.Info(" plugins.listInstalled       - List installed plugins")
		log.Info(" plugins.install             - Install plugin (params: name)")
		log.Info(" plugins.uninstall           - Uninstall plugin (params: name)")
		log.Info(" plugins.update              - Update plugin (params: name)")
		log.Info(" plugins.search              - Search plugins (params: query, category?, compositor?, capability?)")
		log.Info("Network:")
		log.Info(" network.getState            - Get current network state")
		log.Info(" network.wifi.scan           - Scan for WiFi networks")
		log.Info(" network.wifi.networks       - Get WiFi network list")
		log.Info(" network.wifi.connect        - Connect to WiFi (params: ssid, password?, username?)")
		log.Info(" network.wifi.disconnect     - Disconnect WiFi")
		log.Info(" network.wifi.forget         - Forget network (params: ssid)")
		log.Info(" network.wifi.toggle         - Toggle WiFi radio")
		log.Info(" network.wifi.enable         - Enable WiFi")
		log.Info(" network.wifi.disable        - Disable WiFi")
		log.Info(" network.ethernet.connect    - Connect Ethernet")
		log.Info(" network.ethernet.disconnect - Disconnect Ethernet")
		log.Info(" network.preference.set      - Set preference (params: preference [auto|wifi|ethernet])")
		log.Info(" network.info                - Get network info (params: ssid)")
		log.Info(" network.subscribe           - Subscribe to network state changes (streaming)")
		log.Info("Loginctl:")
		log.Info(" loginctl.getState           - Get current session state")
		log.Info(" loginctl.lock               - Lock session")
		log.Info(" loginctl.unlock             - Unlock session")
		log.Info(" loginctl.activate           - Activate session")
		log.Info(" loginctl.setIdleHint        - Set idle hint (params: idle)")
		log.Info(" loginctl.setLockBeforeSuspend - Set lock before suspend (params: enabled)")
		log.Info(" loginctl.setSleepInhibitorEnabled - Enable/disable sleep inhibitor (params: enabled)")
		log.Info(" loginctl.lockerReady        - Signal locker UI is ready (releases sleep inhibitor)")
		log.Info(" loginctl.terminate          - Terminate session")
		log.Info(" loginctl.subscribe          - Subscribe to session state changes (streaming)")
		log.Info("Freedesktop:")
		log.Info(" freedesktop.getState                  - Get accounts & settings state")
		log.Info(" freedesktop.accounts.setIconFile      - Set profile icon (params: path)")
		log.Info(" freedesktop.accounts.setRealName      - Set real name (params: name)")
		log.Info(" freedesktop.accounts.setEmail         - Set email (params: email)")
		log.Info(" freedesktop.accounts.setLanguage      - Set language (params: language)")
		log.Info(" freedesktop.accounts.setLocation      - Set location (params: location)")
		log.Info(" freedesktop.accounts.getUserIconFile  - Get user icon (params: username)")
		log.Info(" freedesktop.settings.getColorScheme   - Get color scheme")
		log.Info(" freedesktop.settings.setIconTheme     - Set icon theme (params: iconTheme)")
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConnection(conn)
	}
}
