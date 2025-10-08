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
	"syscall"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/server/freedesktop"
	"github.com/AvengeMedia/danklinux/internal/server/loginctl"
	"github.com/AvengeMedia/danklinux/internal/server/models"
	"github.com/AvengeMedia/danklinux/internal/server/network"
)

type Capabilities struct {
	Capabilities []string `json:"capabilities"`
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

func Start() error {
	cleanupStaleSockets()

	if err := InitializeNetworkManager(); err != nil {
		log.Warnf("Network manager unavailable: %v", err)
	}

	if err := InitializeLoginctlManager(); err != nil {
		log.Warnf("Loginctl manager unavailable: %v", err)
	}

	if err := InitializeFreedeskManager(); err != nil {
		log.Warnf("Freedesktop manager unavailable: %v", err)
	}

	socketPath := GetSocketPath()
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Infof("DMS API Server listening on: %s", socketPath)
	log.Info("Protocol: JSON over Unix socket")
	log.Info("Request format: {\"id\": <any>, \"method\": \"...\", \"params\": {...}}")
	log.Info("Response format: {\"id\": <any>, \"result\": {...}} or {\"id\": <any>, \"error\": \"...\"}")
	log.Info("Available methods:")
	log.Info("  ping - Test connection")
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
	log.Info(" freedesktop.settings.setColorScheme   - Set color scheme (params: preferDark)")
	log.Info(" freedesktop.settings.getColorScheme   - Get color scheme")

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleConnection(conn)
	}
}
