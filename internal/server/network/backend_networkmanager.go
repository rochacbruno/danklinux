package network

import (
	"bytes"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/godbus/dbus/v5"
)

const (
	dbusNMPath                 = "/org/freedesktop/NetworkManager"
	dbusNMInterface            = "org.freedesktop.NetworkManager"
	dbusNMDeviceInterface      = "org.freedesktop.NetworkManager.Device"
	dbusNMWirelessInterface    = "org.freedesktop.NetworkManager.Device.Wireless"
	dbusNMAccessPointInterface = "org.freedesktop.NetworkManager.AccessPoint"
	dbusPropsInterface         = "org.freedesktop.DBus.Properties"

	NmDeviceStateReasonWrongPassword        = 8
	NmDeviceStateReasonSupplicantTimeout    = 24
	NmDeviceStateReasonSupplicantFailed     = 25
	NmDeviceStateReasonSecretsRequired      = 7
	NmDeviceStateReasonNoSecrets            = 6
	NmDeviceStateReasonNoSsid               = 10
	NmDeviceStateReasonDhcpClientFailed     = 14
	NmDeviceStateReasonIpConfigUnavailable  = 18
	NmDeviceStateReasonSupplicantDisconnect = 23
	NmDeviceStateReasonCarrier              = 40
	NmDeviceStateReasonNewActivation        = 60
)

type NetworkManagerBackend struct {
	nmConn         interface{}
	ethernetDevice interface{}
	wifiDevice     interface{}
	settings       interface{}
	wifiDev        interface{}

	dbusConn *dbus.Conn
	signals  chan *dbus.Signal
	sigWG    sync.WaitGroup
	stopChan chan struct{}

	secretAgent  *SecretAgent
	promptBroker PromptBroker

	state      *BackendState
	stateMutex sync.RWMutex

	lastFailedSSID string
	lastFailedTime int64
	failedMutex    sync.RWMutex

	onStateChange func()
}

func NewNetworkManagerBackend() (*NetworkManagerBackend, error) {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	backend := &NetworkManagerBackend{
		nmConn:   nm,
		stopChan: make(chan struct{}),
		state: &BackendState{
			Backend: "networkmanager",
		},
	}

	return backend, nil
}

func (b *NetworkManagerBackend) Initialize() error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	if s, err := gonetworkmanager.NewSettings(); err == nil {
		b.settings = s
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	for _, dev := range devices {
		devType, err := dev.GetPropertyDeviceType()
		if err != nil {
			continue
		}

		switch devType {
		case gonetworkmanager.NmDeviceTypeEthernet:
			if managed, _ := dev.GetPropertyManaged(); !managed {
				continue
			}
			b.ethernetDevice = dev
			if err := b.updateEthernetState(); err != nil {
				continue
			}
			_, err := b.listEthernetConnections()
			if err != nil {
				return fmt.Errorf("failed to get wired configurations: %w", err)
			}

		case gonetworkmanager.NmDeviceTypeWifi:
			b.wifiDevice = dev
			if w, err := gonetworkmanager.NewDeviceWireless(dev.GetPath()); err == nil {
				b.wifiDev = w
			}
			wifiEnabled, err := nm.GetPropertyWirelessEnabled()
			if err == nil {
				b.stateMutex.Lock()
				b.state.WiFiEnabled = wifiEnabled
				b.stateMutex.Unlock()
			}
			if err := b.updateWiFiState(); err != nil {
				continue
			}
			if wifiEnabled {
				if _, err := b.updateWiFiNetworks(); err != nil {
					log.Warnf("Failed to get initial networks: %v", err)
				}
			}
		}
	}

	if err := b.updatePrimaryConnection(); err != nil {
		return err
	}

	if _, err := b.ListVPNProfiles(); err != nil {
		log.Warnf("Failed to get initial VPN profiles: %v", err)
	}

	if _, err := b.ListActiveVPN(); err != nil {
		log.Warnf("Failed to get initial active VPNs: %v", err)
	}

	return nil
}

func (b *NetworkManagerBackend) Close() {
	close(b.stopChan)
	b.StopMonitoring()

	if b.secretAgent != nil {
		b.secretAgent.Close()
	}
}

func (b *NetworkManagerBackend) GetWiFiEnabled() (bool, error) {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	return nm.GetPropertyWirelessEnabled()
}

func (b *NetworkManagerBackend) SetWiFiEnabled(enabled bool) error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	err := nm.SetPropertyWirelessEnabled(enabled)
	if err != nil {
		return fmt.Errorf("failed to set WiFi enabled: %w", err)
	}

	b.stateMutex.Lock()
	b.state.WiFiEnabled = enabled
	b.stateMutex.Unlock()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) ScanWiFi() error {
	if b.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	b.stateMutex.RLock()
	enabled := b.state.WiFiEnabled
	b.stateMutex.RUnlock()

	if !enabled {
		return fmt.Errorf("WiFi is disabled")
	}

	if err := b.ensureWiFiDevice(); err != nil {
		return err
	}

	w := b.wifiDev.(gonetworkmanager.DeviceWireless)
	err := w.RequestScan()
	if err != nil {
		return fmt.Errorf("scan request failed: %w", err)
	}

	_, err = b.updateWiFiNetworks()
	return err
}

func (b *NetworkManagerBackend) GetWiFiNetworkDetails(ssid string) (*NetworkInfoResponse, error) {
	if b.wifiDevice == nil {
		return nil, fmt.Errorf("no WiFi device available")
	}

	if err := b.ensureWiFiDevice(); err != nil {
		return nil, err
	}
	wifiDev := b.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	s := b.settings
	if s == nil {
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}

	savedSSIDs := make(map[string]bool)
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		if connMeta, ok := connSettings["connection"]; ok {
			if connType, ok := connMeta["type"].(string); ok && connType == "802-11-wireless" {
				if wifiSettings, ok := connSettings["802-11-wireless"]; ok {
					if ssidBytes, ok := wifiSettings["ssid"].([]byte); ok {
						savedSSID := string(ssidBytes)
						savedSSIDs[savedSSID] = true
					}
				}
			}
		}
	}

	b.stateMutex.RLock()
	currentSSID := b.state.WiFiSSID
	currentBSSID := b.state.WiFiBSSID
	b.stateMutex.RUnlock()

	var bands []WiFiNetwork

	for _, ap := range apPaths {
		apSSID, err := ap.GetPropertySSID()
		if err != nil || apSSID != ssid {
			continue
		}

		strength, _ := ap.GetPropertyStrength()
		flags, _ := ap.GetPropertyFlags()
		wpaFlags, _ := ap.GetPropertyWPAFlags()
		rsnFlags, _ := ap.GetPropertyRSNFlags()
		freq, _ := ap.GetPropertyFrequency()
		maxBitrate, _ := ap.GetPropertyMaxBitrate()
		bssid, _ := ap.GetPropertyHWAddress()
		mode, _ := ap.GetPropertyMode()

		secured := flags != uint32(gonetworkmanager.Nm80211APFlagsNone) ||
			wpaFlags != uint32(gonetworkmanager.Nm80211APSecNone) ||
			rsnFlags != uint32(gonetworkmanager.Nm80211APSecNone)

		enterprise := (rsnFlags&uint32(gonetworkmanager.Nm80211APSecKeyMgmt8021X) != 0) ||
			(wpaFlags&uint32(gonetworkmanager.Nm80211APSecKeyMgmt8021X) != 0)

		var modeStr string
		switch mode {
		case gonetworkmanager.Nm80211ModeAdhoc:
			modeStr = "adhoc"
		case gonetworkmanager.Nm80211ModeInfra:
			modeStr = "infrastructure"
		case gonetworkmanager.Nm80211ModeAp:
			modeStr = "ap"
		default:
			modeStr = "unknown"
		}

		channel := frequencyToChannel(freq)

		network := WiFiNetwork{
			SSID:       ssid,
			BSSID:      bssid,
			Signal:     strength,
			Secured:    secured,
			Enterprise: enterprise,
			Connected:  ssid == currentSSID && bssid == currentBSSID,
			Saved:      savedSSIDs[ssid],
			Frequency:  freq,
			Mode:       modeStr,
			Rate:       maxBitrate / 1000,
			Channel:    channel,
		}

		bands = append(bands, network)
	}

	if len(bands) == 0 {
		return nil, fmt.Errorf("network not found: %s", ssid)
	}

	sort.Slice(bands, func(i, j int) bool {
		if bands[i].Connected && !bands[j].Connected {
			return true
		}
		if !bands[i].Connected && bands[j].Connected {
			return false
		}
		return bands[i].Signal > bands[j].Signal
	})

	return &NetworkInfoResponse{
		SSID:  ssid,
		Bands: bands,
	}, nil
}

func (b *NetworkManagerBackend) ConnectWiFi(req ConnectionRequest) error {
	if b.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	b.stateMutex.RLock()
	alreadyConnected := b.state.WiFiConnected && b.state.WiFiSSID == req.SSID
	b.stateMutex.RUnlock()

	if alreadyConnected && !req.Interactive {
		return nil
	}

	b.stateMutex.Lock()
	b.state.IsConnecting = true
	b.state.ConnectingSSID = req.SSID
	b.state.LastError = ""
	b.stateMutex.Unlock()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	existingConn, err := b.findConnection(req.SSID)
	if err == nil && existingConn != nil {
		dev := b.wifiDevice.(gonetworkmanager.Device)

		_, err := nm.ActivateConnection(existingConn, dev, nil)
		if err != nil {
			log.Warnf("[ConnectWiFi] Failed to activate existing connection: %v", err)
			b.stateMutex.Lock()
			b.state.IsConnecting = false
			b.state.ConnectingSSID = ""
			b.state.LastError = fmt.Sprintf("failed to activate connection: %v", err)
			b.stateMutex.Unlock()
			if b.onStateChange != nil {
				b.onStateChange()
			}
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		return nil
	}

	if err := b.createAndConnectWiFi(req); err != nil {
		log.Warnf("[ConnectWiFi] Failed to create and connect: %v", err)
		b.stateMutex.Lock()
		b.state.IsConnecting = false
		b.state.ConnectingSSID = ""
		b.state.LastError = err.Error()
		b.stateMutex.Unlock()
		if b.onStateChange != nil {
			b.onStateChange()
		}
		return err
	}

	return nil
}

func (b *NetworkManagerBackend) DisconnectWiFi() error {
	if b.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	dev := b.wifiDevice.(gonetworkmanager.Device)

	err := dev.Disconnect()
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	b.updateWiFiState()
	b.updatePrimaryConnection()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) ForgetWiFiNetwork(ssid string) error {
	conn, err := b.findConnection(ssid)
	if err != nil {
		return fmt.Errorf("connection not found: %w", err)
	}

	b.stateMutex.RLock()
	currentSSID := b.state.WiFiSSID
	isConnected := b.state.WiFiConnected
	b.stateMutex.RUnlock()

	err = conn.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete connection: %w", err)
	}

	if isConnected && currentSSID == ssid {
		b.stateMutex.Lock()
		b.state.WiFiConnected = false
		b.state.WiFiSSID = ""
		b.state.WiFiBSSID = ""
		b.state.WiFiSignal = 0
		b.state.WiFiIP = ""
		b.state.NetworkStatus = StatusDisconnected
		b.stateMutex.Unlock()
	}

	b.updateWiFiNetworks()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) GetWiredConnections() ([]WiredConnection, error) {
	return b.listEthernetConnections()
}

func (b *NetworkManagerBackend) GetWiredNetworkDetails(uuid string) (*WiredNetworkInfoResponse, error) {
	if b.ethernetDevice == nil {
		return nil, fmt.Errorf("no ethernet device available")
	}

	dev := b.ethernetDevice.(gonetworkmanager.Device)

	iface, _ := dev.GetPropertyInterface()
	driver, _ := dev.GetPropertyDriver()

	hwAddr := "Not available"
	var speed uint32 = 0
	wiredDevice, err := gonetworkmanager.NewDeviceWired(dev.GetPath())
	if err == nil {
		hwAddr, _ = wiredDevice.GetPropertyHwAddress()
		speed, _ = wiredDevice.GetPropertySpeed()
	}
	var ipv4Config WiredIPConfig
	var ipv6Config WiredIPConfig

	activeConn, err := dev.GetPropertyActiveConnection()
	if err == nil && activeConn != nil {
		ip4Config, err := activeConn.GetPropertyIP4Config()
		if err == nil && ip4Config != nil {
			var ips []string
			addresses, err := ip4Config.GetPropertyAddressData()
			if err == nil && len(addresses) > 0 {
				for _, addr := range addresses {
					ips = append(ips, fmt.Sprintf("%s/%s", addr.Address, strconv.Itoa(int(addr.Prefix))))
				}
			}

			gateway, _ := ip4Config.GetPropertyGateway()
			dnsAddrs := ""
			dns, err := ip4Config.GetPropertyNameserverData()
			if err == nil && len(dns) > 0 {
				for _, d := range dns {
					if len(dnsAddrs) > 0 {
						dnsAddrs = strings.Join([]string{dnsAddrs, d.Address}, "; ")
					} else {
						dnsAddrs = d.Address
					}
				}
			}

			ipv4Config = WiredIPConfig{
				IPs:     ips,
				Gateway: gateway,
				DNS:     dnsAddrs,
			}
		}

		ip6Config, err := activeConn.GetPropertyIP6Config()
		if err == nil && ip6Config != nil {
			var ips []string
			addresses, err := ip6Config.GetPropertyAddressData()
			if err == nil && len(addresses) > 0 {
				for _, addr := range addresses {
					ips = append(ips, fmt.Sprintf("%s/%s", addr.Address, strconv.Itoa(int(addr.Prefix))))
				}
			}

			gateway, _ := ip6Config.GetPropertyGateway()
			dnsAddrs := ""
			dns, err := ip6Config.GetPropertyNameservers()
			if err == nil && len(dns) > 0 {
				for _, d := range dns {
					if len(d) == 16 {
						ip := net.IP(d)
						if len(dnsAddrs) > 0 {
							dnsAddrs = strings.Join([]string{dnsAddrs, ip.String()}, "; ")
						} else {
							dnsAddrs = ip.String()
						}
					}
				}
			}

			ipv6Config = WiredIPConfig{
				IPs:     ips,
				Gateway: gateway,
				DNS:     dnsAddrs,
			}
		}
	}

	return &WiredNetworkInfoResponse{
		UUID:   uuid,
		IFace:  iface,
		Driver: driver,
		HwAddr: hwAddr,
		Speed:  strconv.Itoa(int(speed)),
		IPv4:   ipv4Config,
		IPv6:   ipv6Config,
	}, nil
}

func (b *NetworkManagerBackend) ConnectEthernet() error {
	if b.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	dev := b.ethernetDevice.(gonetworkmanager.Device)

	settingsMgr, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections: %w", err)
	}

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		if connMeta, ok := connSettings["connection"]; ok {
			if connType, ok := connMeta["type"].(string); ok && connType == "802-3-ethernet" {
				_, err := nm.ActivateConnection(conn, dev, nil)
				if err != nil {
					return fmt.Errorf("failed to activate ethernet: %w", err)
				}

				b.updateEthernetState()
				b.listEthernetConnections()
				b.updatePrimaryConnection()

				if b.onStateChange != nil {
					b.onStateChange()
				}

				return nil
			}
		}
	}

	settings := make(map[string]map[string]interface{})
	settings["connection"] = map[string]interface{}{
		"id":   "Wired connection",
		"type": "802-3-ethernet",
	}

	_, err = nm.AddAndActivateConnection(settings, dev)
	if err != nil {
		return fmt.Errorf("failed to create and activate ethernet: %w", err)
	}

	b.updateEthernetState()
	b.listEthernetConnections()
	b.updatePrimaryConnection()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) DisconnectEthernet() error {
	if b.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	dev := b.ethernetDevice.(gonetworkmanager.Device)

	err := dev.Disconnect()
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	b.updateEthernetState()
	b.listEthernetConnections()
	b.updatePrimaryConnection()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) ActivateWiredConnection(uuid string) error {
	if b.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	dev := b.ethernetDevice.(gonetworkmanager.Device)

	settingsMgr, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections: %w", err)
	}

	var targetConnection gonetworkmanager.Connection
	for _, conn := range connections {
		settings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		if connectionSettings, ok := settings["connection"]; ok {
			if connUUID, ok := connectionSettings["uuid"].(string); ok && connUUID == uuid {
				targetConnection = conn
				break
			}
		}
	}

	if targetConnection == nil {
		return fmt.Errorf("connection with UUID %s not found", uuid)
	}

	_, err = nm.ActivateConnection(targetConnection, dev, nil)
	if err != nil {
		return fmt.Errorf("error activation connection: %w", err)
	}

	b.updateEthernetState()
	b.listEthernetConnections()
	b.updatePrimaryConnection()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *NetworkManagerBackend) GetCurrentState() (*BackendState, error) {
	b.stateMutex.RLock()
	defer b.stateMutex.RUnlock()

	state := *b.state
	state.WiFiNetworks = append([]WiFiNetwork(nil), b.state.WiFiNetworks...)
	state.WiredConnections = append([]WiredConnection(nil), b.state.WiredConnections...)
	state.VPNProfiles = append([]VPNProfile(nil), b.state.VPNProfiles...)
	state.VPNActive = append([]VPNActive(nil), b.state.VPNActive...)

	return &state, nil
}

func (b *NetworkManagerBackend) StartMonitoring(onStateChange func()) error {
	b.onStateChange = onStateChange

	if err := b.startSecretAgent(); err != nil {
		return fmt.Errorf("failed to start secret agent: %w", err)
	}

	if err := b.startSignalPump(); err != nil {
		return err
	}

	return nil
}

func (b *NetworkManagerBackend) StopMonitoring() {
	b.stopSignalPump()
}

func (b *NetworkManagerBackend) GetPromptBroker() PromptBroker {
	return b.promptBroker
}

func (b *NetworkManagerBackend) SetPromptBroker(broker PromptBroker) error {
	if broker == nil {
		return fmt.Errorf("broker cannot be nil")
	}

	hadAgent := b.secretAgent != nil

	b.promptBroker = broker

	if b.secretAgent != nil {
		b.secretAgent.Close()
		b.secretAgent = nil
	}

	if hadAgent {
		return b.startSecretAgent()
	}

	return nil
}

func (b *NetworkManagerBackend) SubmitCredentials(token string, secrets map[string]string, save bool) error {
	if b.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return b.promptBroker.Resolve(token, PromptReply{
		Secrets: secrets,
		Save:    save,
		Cancel:  false,
	})
}

func (b *NetworkManagerBackend) CancelCredentials(token string) error {
	if b.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return b.promptBroker.Resolve(token, PromptReply{
		Cancel: true,
	})
}

func (b *NetworkManagerBackend) IsConnectingTo(ssid string) bool {
	b.stateMutex.RLock()
	defer b.stateMutex.RUnlock()
	return b.state.IsConnecting && b.state.ConnectingSSID == ssid
}

func (b *NetworkManagerBackend) updateVPNConnectionState() {
	b.stateMutex.RLock()
	isConnectingVPN := b.state.IsConnectingVPN
	connectingVPNUUID := b.state.ConnectingVPNUUID
	b.stateMutex.RUnlock()

	if !isConnectingVPN || connectingVPNUUID == "" {
		return
	}

	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return
	}

	foundConnection := false
	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		uuid, err := activeConn.GetPropertyUUID()
		if err != nil {
			continue
		}

		state, _ := activeConn.GetPropertyState()
		stateReason, _ := activeConn.GetPropertyStateFlags()

		if uuid == connectingVPNUUID {
			foundConnection = true

			if state == 2 {
				log.Infof("[updateVPNConnectionState] VPN connection successful: %s", uuid)
				b.stateMutex.Lock()
				b.state.IsConnectingVPN = false
				b.state.ConnectingVPNUUID = ""
				b.state.LastError = ""
				b.stateMutex.Unlock()
				b.ListActiveVPN()
				return
			} else if state == 4 {
				log.Warnf("[updateVPNConnectionState] VPN connection failed/deactivated: %s (state=%d, flags=%d)", uuid, state, stateReason)
				b.stateMutex.Lock()
				b.state.IsConnectingVPN = false
				b.state.ConnectingVPNUUID = ""
				b.state.LastError = "VPN connection failed"
				b.stateMutex.Unlock()
				b.ListActiveVPN()
				return
			}
		}
	}

	if !foundConnection {
		log.Warnf("[updateVPNConnectionState] VPN connection no longer exists: %s", connectingVPNUUID)
		b.stateMutex.Lock()
		b.state.IsConnectingVPN = false
		b.state.ConnectingVPNUUID = ""
		b.state.LastError = "VPN connection failed"
		b.stateMutex.Unlock()
		b.ListActiveVPN()
	}
}

func (b *NetworkManagerBackend) ensureWiFiDevice() error {
	if b.wifiDev != nil {
		return nil
	}

	if b.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	dev := b.wifiDevice.(gonetworkmanager.Device)
	wifiDev, err := gonetworkmanager.NewDeviceWireless(dev.GetPath())
	if err != nil {
		return fmt.Errorf("failed to get wireless device: %w", err)
	}
	b.wifiDev = wifiDev
	return nil
}

func (b *NetworkManagerBackend) updatePrimaryConnection() error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return err
	}

	hasActiveVPN := false
	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}
		if connType == "vpn" || connType == "wireguard" {
			state, _ := activeConn.GetPropertyState()
			if state == 2 {
				hasActiveVPN = true
				break
			}
		}
	}

	if hasActiveVPN {
		b.stateMutex.Lock()
		b.state.NetworkStatus = StatusVPN
		b.stateMutex.Unlock()
		return nil
	}

	primaryConn, err := nm.GetPropertyPrimaryConnection()
	if err != nil {
		return err
	}

	if primaryConn == nil || primaryConn.GetPath() == "/" {
		b.stateMutex.Lock()
		b.state.NetworkStatus = StatusDisconnected
		b.stateMutex.Unlock()
		return nil
	}

	connType, err := primaryConn.GetPropertyType()
	if err != nil {
		return err
	}

	b.stateMutex.Lock()
	switch connType {
	case "802-3-ethernet":
		b.state.NetworkStatus = StatusEthernet
	case "802-11-wireless":
		b.state.NetworkStatus = StatusWiFi
	case "vpn", "wireguard":
		b.state.NetworkStatus = StatusVPN
	default:
		b.state.NetworkStatus = StatusDisconnected
	}
	b.stateMutex.Unlock()

	return nil
}

func (b *NetworkManagerBackend) updateEthernetState() error {
	if b.ethernetDevice == nil {
		return nil
	}

	dev := b.ethernetDevice.(gonetworkmanager.Device)

	iface, err := dev.GetPropertyInterface()
	if err != nil {
		return err
	}

	state, err := dev.GetPropertyState()
	if err != nil {
		return err
	}

	connected := state == gonetworkmanager.NmDeviceStateActivated

	var ip string
	if connected {
		ip = b.getDeviceIP(dev)
	}

	b.stateMutex.Lock()
	b.state.EthernetDevice = iface
	b.state.EthernetConnected = connected
	b.state.EthernetIP = ip
	b.stateMutex.Unlock()

	return nil
}

func (b *NetworkManagerBackend) getDeviceStateReason(dev gonetworkmanager.Device) uint32 {
	path := dev.GetPath()
	obj := b.dbusConn.Object("org.freedesktop.NetworkManager", path)

	variant, err := obj.GetProperty(dbusNMDeviceInterface + ".StateReason")
	if err != nil {
		return 0
	}

	// StateReason is a struct (uint32, uint32) representing (state, reason)
	// We need to extract the reason (second element)
	if stateReasonStruct, ok := variant.Value().([]interface{}); ok && len(stateReasonStruct) >= 2 {
		if reason, ok := stateReasonStruct[1].(uint32); ok {
			return reason
		}
	}

	return 0
}

func (b *NetworkManagerBackend) classifyNMStateReason(reason uint32) string {
	switch reason {
	case NmDeviceStateReasonWrongPassword,
		NmDeviceStateReasonSupplicantTimeout,
		NmDeviceStateReasonSupplicantFailed,
		NmDeviceStateReasonSecretsRequired:
		return errdefs.ErrBadCredentials
	case NmDeviceStateReasonNoSecrets:
		return errdefs.ErrUserCanceled
	case NmDeviceStateReasonNoSsid:
		return errdefs.ErrNoSuchSSID
	case NmDeviceStateReasonDhcpClientFailed,
		NmDeviceStateReasonIpConfigUnavailable:
		return errdefs.ErrDhcpTimeout
	case NmDeviceStateReasonSupplicantDisconnect,
		NmDeviceStateReasonCarrier:
		return errdefs.ErrAssocTimeout
	default:
		return errdefs.ErrConnectionFailed
	}
}

func (b *NetworkManagerBackend) updateWiFiState() error {
	if b.wifiDevice == nil {
		return nil
	}

	dev := b.wifiDevice.(gonetworkmanager.Device)

	iface, err := dev.GetPropertyInterface()
	if err != nil {
		return err
	}

	state, err := dev.GetPropertyState()
	if err != nil {
		return err
	}

	connected := state == gonetworkmanager.NmDeviceStateActivated
	failed := state == gonetworkmanager.NmDeviceStateFailed
	disconnected := state == gonetworkmanager.NmDeviceStateDisconnected

	var ip, ssid, bssid string
	var signal uint8

	if connected {
		if err := b.ensureWiFiDevice(); err == nil && b.wifiDev != nil {
			w := b.wifiDev.(gonetworkmanager.DeviceWireless)
			activeAP, err := w.GetPropertyActiveAccessPoint()
			if err == nil && activeAP != nil && activeAP.GetPath() != "/" {
				ssid, _ = activeAP.GetPropertySSID()
				signal, _ = activeAP.GetPropertyStrength()
				bssid, _ = activeAP.GetPropertyHWAddress()
			}
		}

		ip = b.getDeviceIP(dev)
	}

	b.stateMutex.RLock()
	wasConnecting := b.state.IsConnecting
	connectingSSID := b.state.ConnectingSSID
	b.stateMutex.RUnlock()

	var reasonCode string
	if wasConnecting && connectingSSID != "" && (failed || (disconnected && !connected)) {
		reason := b.getDeviceStateReason(dev)

		if reason == NmDeviceStateReasonNewActivation || reason == 0 {
			return nil
		}

		log.Warnf("[updateWiFiState] Connection failed: SSID=%s, state=%d, reason=%d", connectingSSID, state, reason)

		reasonCode = b.classifyNMStateReason(reason)

		if reasonCode == errdefs.ErrConnectionFailed {
			b.failedMutex.RLock()
			if b.lastFailedSSID == connectingSSID {
				elapsed := time.Now().Unix() - b.lastFailedTime
				if elapsed < 5 {
					reasonCode = errdefs.ErrBadCredentials
				}
			}
			b.failedMutex.RUnlock()
		}
	}

	b.stateMutex.Lock()
	defer b.stateMutex.Unlock()

	wasConnecting = b.state.IsConnecting
	connectingSSID = b.state.ConnectingSSID

	if wasConnecting && connectingSSID != "" {
		if connected && ssid == connectingSSID {
			log.Infof("[updateWiFiState] Connection successful: %s", ssid)
			b.state.IsConnecting = false
			b.state.ConnectingSSID = ""
			b.state.LastError = ""
		} else if failed || (disconnected && !connected) {
			log.Warnf("[updateWiFiState] Connection failed: SSID=%s, state=%d", connectingSSID, state)
			b.state.IsConnecting = false
			b.state.ConnectingSSID = ""
			b.state.LastError = reasonCode

			b.failedMutex.Lock()
			b.lastFailedSSID = connectingSSID
			b.lastFailedTime = time.Now().Unix()
			b.failedMutex.Unlock()
		}
	}

	b.state.WiFiDevice = iface
	b.state.WiFiConnected = connected
	b.state.WiFiIP = ip
	b.state.WiFiSSID = ssid
	b.state.WiFiBSSID = bssid
	b.state.WiFiSignal = signal

	return nil
}

func (b *NetworkManagerBackend) getDeviceIP(dev gonetworkmanager.Device) string {
	ip4Config, err := dev.GetPropertyIP4Config()
	if err != nil || ip4Config == nil {
		return ""
	}

	addresses, err := ip4Config.GetPropertyAddressData()
	if err != nil || len(addresses) == 0 {
		return ""
	}

	return addresses[0].Address
}

func (b *NetworkManagerBackend) updateWiFiNetworks() ([]WiFiNetwork, error) {
	if b.wifiDevice == nil {
		return nil, fmt.Errorf("no WiFi device available")
	}

	if err := b.ensureWiFiDevice(); err != nil {
		return nil, err
	}
	wifiDev := b.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	s := b.settings
	if s == nil {
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}

	savedSSIDs := make(map[string]bool)
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		if connMeta, ok := connSettings["connection"]; ok {
			if connType, ok := connMeta["type"].(string); ok && connType == "802-11-wireless" {
				if wifiSettings, ok := connSettings["802-11-wireless"]; ok {
					if ssidBytes, ok := wifiSettings["ssid"].([]byte); ok {
						ssid := string(ssidBytes)
						savedSSIDs[ssid] = true
					}
				}
			}
		}
	}

	b.stateMutex.RLock()
	currentSSID := b.state.WiFiSSID
	b.stateMutex.RUnlock()

	seenSSIDs := make(map[string]*WiFiNetwork)
	networks := []WiFiNetwork{}

	for _, ap := range apPaths {
		ssid, err := ap.GetPropertySSID()
		if err != nil || ssid == "" {
			continue
		}

		if existing, exists := seenSSIDs[ssid]; exists {
			strength, _ := ap.GetPropertyStrength()
			if strength > existing.Signal {
				existing.Signal = strength
				freq, _ := ap.GetPropertyFrequency()
				existing.Frequency = freq
				bssid, _ := ap.GetPropertyHWAddress()
				existing.BSSID = bssid
			}
			continue
		}

		strength, _ := ap.GetPropertyStrength()
		flags, _ := ap.GetPropertyFlags()
		wpaFlags, _ := ap.GetPropertyWPAFlags()
		rsnFlags, _ := ap.GetPropertyRSNFlags()
		freq, _ := ap.GetPropertyFrequency()
		maxBitrate, _ := ap.GetPropertyMaxBitrate()
		bssid, _ := ap.GetPropertyHWAddress()
		mode, _ := ap.GetPropertyMode()

		secured := flags != uint32(gonetworkmanager.Nm80211APFlagsNone) ||
			wpaFlags != uint32(gonetworkmanager.Nm80211APSecNone) ||
			rsnFlags != uint32(gonetworkmanager.Nm80211APSecNone)

		enterprise := (rsnFlags&uint32(gonetworkmanager.Nm80211APSecKeyMgmt8021X) != 0) ||
			(wpaFlags&uint32(gonetworkmanager.Nm80211APSecKeyMgmt8021X) != 0)

		var modeStr string
		switch mode {
		case gonetworkmanager.Nm80211ModeAdhoc:
			modeStr = "adhoc"
		case gonetworkmanager.Nm80211ModeInfra:
			modeStr = "infrastructure"
		case gonetworkmanager.Nm80211ModeAp:
			modeStr = "ap"
		default:
			modeStr = "unknown"
		}

		channel := frequencyToChannel(freq)

		network := WiFiNetwork{
			SSID:       ssid,
			BSSID:      bssid,
			Signal:     strength,
			Secured:    secured,
			Enterprise: enterprise,
			Connected:  ssid == currentSSID,
			Saved:      savedSSIDs[ssid],
			Frequency:  freq,
			Mode:       modeStr,
			Rate:       maxBitrate / 1000,
			Channel:    channel,
		}

		seenSSIDs[ssid] = &network
		networks = append(networks, network)
	}

	sortWiFiNetworks(networks)

	b.stateMutex.Lock()
	b.state.WiFiNetworks = networks
	b.stateMutex.Unlock()

	return networks, nil
}

func (b *NetworkManagerBackend) listEthernetConnections() ([]WiredConnection, error) {
	if b.ethernetDevice == nil {
		return nil, fmt.Errorf("no ethernet device available")
	}

	s := b.settings
	if s == nil {
		s, err := gonetworkmanager.NewSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}

	wiredConfigs := make([]WiredConnection, 0)
	activeUUIDs, err := b.getActiveConnections()

	if err != nil {
		return nil, fmt.Errorf("failed to get active wired connections: %w", err)
	}

	currentUuid := ""
	for _, connection := range connections {
		path := connection.GetPath()
		settings, err := connection.GetSettings()
		if err != nil {
			log.Errorf("unable to get settings for %s: %v", path, err)
			continue
		}

		connectionSettings := settings["connection"]
		connType, _ := connectionSettings["type"].(string)
		connID, _ := connectionSettings["id"].(string)
		connUUID, _ := connectionSettings["uuid"].(string)

		if connType == "802-3-ethernet" {
			wiredConfigs = append(wiredConfigs, WiredConnection{
				Path:     path,
				ID:       connID,
				UUID:     connUUID,
				Type:     connType,
				IsActive: activeUUIDs[connUUID],
			})
			if activeUUIDs[connUUID] {
				currentUuid = connUUID
			}
		}
	}

	b.stateMutex.Lock()
	b.state.EthernetConnectionUuid = currentUuid
	b.state.WiredConnections = wiredConfigs
	b.stateMutex.Unlock()

	return wiredConfigs, nil
}

func (b *NetworkManagerBackend) getActiveConnections() (map[string]bool, error) {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	activeUUIDs := make(map[string]bool)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return activeUUIDs, fmt.Errorf("failed to get active connections: %w", err)
	}

	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "802-3-ethernet" {
			continue
		}

		state, err := activeConn.GetPropertyState()
		if err != nil {
			continue
		}
		if state < 1 || state > 2 {
			continue
		}

		uuid, err := activeConn.GetPropertyUUID()
		if err != nil {
			continue
		}
		activeUUIDs[uuid] = true
	}
	return activeUUIDs, nil
}

func (b *NetworkManagerBackend) findConnection(ssid string) (gonetworkmanager.Connection, error) {
	s := b.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, err
		}
		b.settings = s
	}

	settings := s.(gonetworkmanager.Settings)
	connections, err := settings.ListConnections()
	if err != nil {
		return nil, err
	}

	ssidBytes := []byte(ssid)
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		if connMeta, ok := connSettings["connection"]; ok {
			if connType, ok := connMeta["type"].(string); ok && connType == "802-11-wireless" {
				if wifiSettings, ok := connSettings["802-11-wireless"]; ok {
					if candidateSSID, ok := wifiSettings["ssid"].([]byte); ok {
						if bytes.Equal(candidateSSID, ssidBytes) {
							return conn, nil
						}
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("connection not found")
}

func (b *NetworkManagerBackend) createAndConnectWiFi(req ConnectionRequest) error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	dev := b.wifiDevice.(gonetworkmanager.Device)

	if err := b.ensureWiFiDevice(); err != nil {
		return err
	}
	wifiDev := b.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		return fmt.Errorf("failed to get access points: %w", err)
	}

	var targetAP gonetworkmanager.AccessPoint
	for _, ap := range apPaths {
		ssid, err := ap.GetPropertySSID()
		if err != nil || ssid != req.SSID {
			continue
		}
		targetAP = ap
		break
	}

	if targetAP == nil {
		return fmt.Errorf("access point not found: %s", req.SSID)
	}

	flags, _ := targetAP.GetPropertyFlags()
	wpaFlags, _ := targetAP.GetPropertyWPAFlags()
	rsnFlags, _ := targetAP.GetPropertyRSNFlags()

	const KeyMgmt8021x = uint32(512)
	const KeyMgmtPsk = uint32(256)
	const KeyMgmtSae = uint32(1024)

	isEnterprise := (wpaFlags&KeyMgmt8021x) != 0 || (rsnFlags&KeyMgmt8021x) != 0
	isPsk := (wpaFlags&KeyMgmtPsk) != 0 || (rsnFlags&KeyMgmtPsk) != 0
	isSae := (wpaFlags&KeyMgmtSae) != 0 || (rsnFlags&KeyMgmtSae) != 0

	secured := flags != uint32(gonetworkmanager.Nm80211APFlagsNone) ||
		wpaFlags != uint32(gonetworkmanager.Nm80211APSecNone) ||
		rsnFlags != uint32(gonetworkmanager.Nm80211APSecNone)

	if isEnterprise {
		log.Infof("[createAndConnectWiFi] Enterprise network detected (802.1x) - SSID: %s, interactive: %v",
			req.SSID, req.Interactive)
	}

	settings := make(map[string]map[string]interface{})

	settings["connection"] = map[string]interface{}{
		"id":          req.SSID,
		"type":        "802-11-wireless",
		"autoconnect": true,
	}

	settings["ipv4"] = map[string]interface{}{"method": "auto"}
	settings["ipv6"] = map[string]interface{}{"method": "auto"}

	if secured {
		settings["802-11-wireless"] = map[string]interface{}{
			"ssid":     []byte(req.SSID),
			"mode":     "infrastructure",
			"security": "802-11-wireless-security",
		}

		switch {
		case isEnterprise || req.Username != "":
			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "wpa-eap",
			}

			x := map[string]interface{}{
				"eap":             []string{"peap"},
				"phase2-auth":     "mschapv2",
				"system-ca-certs": false,
				"password-flags":  uint32(0),
			}

			if req.Username != "" {
				x["identity"] = req.Username
			}
			if req.Password != "" {
				x["password"] = req.Password
			}

			if req.AnonymousIdentity != "" {
				x["anonymous-identity"] = req.AnonymousIdentity
			}
			if req.DomainSuffixMatch != "" {
				x["domain-suffix-match"] = req.DomainSuffixMatch
			}

			settings["802-1x"] = x

			log.Infof("[createAndConnectWiFi] WPA-EAP settings: eap=peap, phase2-auth=mschapv2, identity=%s, interactive=%v, system-ca-certs=%v, domain-suffix-match=%q",
				req.Username, req.Interactive, x["system-ca-certs"], req.DomainSuffixMatch)

		case isPsk:
			sec := map[string]interface{}{
				"key-mgmt":  "wpa-psk",
				"psk-flags": uint32(0),
			}
			if !req.Interactive {
				sec["psk"] = req.Password
			}
			settings["802-11-wireless-security"] = sec

		case isSae:
			sec := map[string]interface{}{
				"key-mgmt":  "sae",
				"pmf":       int32(3),
				"psk-flags": uint32(0),
			}
			if !req.Interactive {
				sec["psk"] = req.Password
			}
			settings["802-11-wireless-security"] = sec

		default:
			return fmt.Errorf("secured network but not SAE/PSK/802.1X (rsn=0x%x wpa=0x%x)", rsnFlags, wpaFlags)
		}
	} else {
		settings["802-11-wireless"] = map[string]interface{}{
			"ssid": []byte(req.SSID),
			"mode": "infrastructure",
		}
	}

	if req.Interactive {
		s := b.settings
		if s == nil {
			var settingsErr error
			s, settingsErr = gonetworkmanager.NewSettings()
			if settingsErr != nil {
				return fmt.Errorf("failed to get settings manager: %w", settingsErr)
			}
			b.settings = s
		}

		settingsMgr := s.(gonetworkmanager.Settings)
		conn, err := settingsMgr.AddConnection(settings)
		if err != nil {
			return fmt.Errorf("failed to add connection: %w", err)
		}

		if isEnterprise {
			log.Infof("[createAndConnectWiFi] Enterprise connection added, activating (secret agent will be called)")
		}

		_, err = nm.ActivateWirelessConnection(conn, dev, targetAP)
		if err != nil {
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		log.Infof("[createAndConnectWiFi] Connection activation initiated, waiting for NetworkManager state changes...")
	} else {
		_, err = nm.AddAndActivateWirelessConnection(settings, dev, targetAP)
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		log.Infof("[createAndConnectWiFi] Connection activation initiated, waiting for NetworkManager state changes...")
	}

	return nil
}

func (b *NetworkManagerBackend) startSecretAgent() error {
	if b.promptBroker == nil {
		return fmt.Errorf("prompt broker not set")
	}

	agent, err := NewSecretAgent(b.promptBroker, nil, b)
	if err != nil {
		return err
	}

	b.secretAgent = agent
	return nil
}

func (b *NetworkManagerBackend) startSignalPump() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	b.dbusConn = conn

	signals := make(chan *dbus.Signal, 256)
	b.signals = signals
	conn.Signal(signals)

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
		dbus.WithMatchInterface(dbusPropsInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		conn.RemoveSignal(signals)
		conn.Close()
		return err
	}

	// Subscribe to Settings signals for connection add/remove
	// Signal names are "NewConnection" and "ConnectionRemoved"
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")),
		dbus.WithMatchInterface("org.freedesktop.NetworkManager.Settings"),
		dbus.WithMatchMember("NewConnection"),
	); err != nil {
		_ = conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		conn.RemoveSignal(signals)
		conn.Close()
		return err
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")),
		dbus.WithMatchInterface("org.freedesktop.NetworkManager.Settings"),
		dbus.WithMatchMember("ConnectionRemoved"),
	); err != nil {
		_ = conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		_ = conn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")),
			dbus.WithMatchInterface("org.freedesktop.NetworkManager.Settings"),
			dbus.WithMatchMember("NewConnection"),
		)
		conn.RemoveSignal(signals)
		conn.Close()
		return err
	}

	if b.wifiDevice != nil {
		dev := b.wifiDevice.(gonetworkmanager.Device)
		if err := conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		); err != nil {
			_ = conn.RemoveMatchSignal(
				dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
				dbus.WithMatchInterface(dbusPropsInterface),
				dbus.WithMatchMember("PropertiesChanged"),
			)
			conn.RemoveSignal(signals)
			conn.Close()
			return err
		}
	}

	if b.ethernetDevice != nil {
		dev := b.ethernetDevice.(gonetworkmanager.Device)
		if err := conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		); err != nil {
			_ = conn.RemoveMatchSignal(
				dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
				dbus.WithMatchInterface(dbusPropsInterface),
				dbus.WithMatchMember("PropertiesChanged"),
			)
			if b.wifiDevice != nil {
				dev := b.wifiDevice.(gonetworkmanager.Device)
				_ = conn.RemoveMatchSignal(
					dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
					dbus.WithMatchInterface(dbusPropsInterface),
					dbus.WithMatchMember("PropertiesChanged"),
				)
			}
			conn.RemoveSignal(signals)
			conn.Close()
			return err
		}
	}

	b.sigWG.Add(1)
	go func() {
		defer b.sigWG.Done()
		for {
			select {
			case <-b.stopChan:
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if sig == nil {
					continue
				}
				// Debug: log all signal names to find Settings signals
				if strings.Contains(string(sig.Name), "Settings") {
					log.Infof("[Signal Debug] Received signal: %s from %s path=%s", sig.Name, sig.Sender, sig.Path)
				}
				b.handleDBusSignal(sig)
			}
		}
	}()
	return nil
}

func (b *NetworkManagerBackend) stopSignalPump() {
	if b.dbusConn == nil {
		return
	}

	_ = b.dbusConn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
		dbus.WithMatchInterface(dbusPropsInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	)

	if b.wifiDevice != nil {
		dev := b.wifiDevice.(gonetworkmanager.Device)
		_ = b.dbusConn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
	}

	if b.ethernetDevice != nil {
		dev := b.ethernetDevice.(gonetworkmanager.Device)
		_ = b.dbusConn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
	}

	if b.signals != nil {
		b.dbusConn.RemoveSignal(b.signals)
		close(b.signals)
	}

	b.sigWG.Wait()

	b.dbusConn.Close()
}

func (b *NetworkManagerBackend) handleDBusSignal(sig *dbus.Signal) {
	// Handle Settings signals (NewConnection/ConnectionRemoved) which have different format
	if sig.Name == "org.freedesktop.NetworkManager.Settings.NewConnection" ||
		sig.Name == "org.freedesktop.NetworkManager.Settings.ConnectionRemoved" {
		log.Infof("[handleDBusSignal] Connection profile changed: %s", sig.Name)
		// Refresh VPN profiles list
		b.ListVPNProfiles()
		if b.onStateChange != nil {
			b.onStateChange()
		}
		return
	}

	if len(sig.Body) < 2 {
		return
	}

	iface, ok := sig.Body[0].(string)
	if !ok {
		return
	}

	changes, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}

	switch iface {
	case dbusNMInterface:
		b.handleNetworkManagerChange(changes)

	case dbusNMDeviceInterface:
		b.handleDeviceChange(changes)

	case dbusNMWirelessInterface:
		b.handleWiFiChange(changes)

	case dbusNMAccessPointInterface:
		b.handleAccessPointChange(changes)
	}
}

func (b *NetworkManagerBackend) handleNetworkManagerChange(changes map[string]dbus.Variant) {
	var needsUpdate bool

	for key := range changes {
		switch key {
		case "PrimaryConnection", "State", "ActiveConnections":
			needsUpdate = true
		case "WirelessEnabled":
			nm := b.nmConn.(gonetworkmanager.NetworkManager)
			if enabled, err := nm.GetPropertyWirelessEnabled(); err == nil {
				b.stateMutex.Lock()
				b.state.WiFiEnabled = enabled
				b.stateMutex.Unlock()
				needsUpdate = true
			}
		default:
			continue
		}
	}

	if needsUpdate {
		b.updatePrimaryConnection()
		if _, exists := changes["State"]; exists {
			b.updateEthernetState()
			b.updateWiFiState()
		}
		if _, exists := changes["ActiveConnections"]; exists {
			b.updateVPNConnectionState()
			b.ListActiveVPN()
		}
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}
}

func (b *NetworkManagerBackend) handleDeviceChange(changes map[string]dbus.Variant) {
	var needsUpdate bool
	var stateChanged bool

	for key := range changes {
		switch key {
		case "State":
			stateChanged = true
			needsUpdate = true
		case "Ip4Config":
			needsUpdate = true
		default:
			continue
		}
	}

	if needsUpdate {
		b.updateEthernetState()
		b.updateWiFiState()
		if stateChanged {
			b.updatePrimaryConnection()
		}
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}
}

func (b *NetworkManagerBackend) handleWiFiChange(changes map[string]dbus.Variant) {
	var needsStateUpdate bool
	var needsNetworkUpdate bool

	for key := range changes {
		switch key {
		case "ActiveAccessPoint":
			needsStateUpdate = true
			needsNetworkUpdate = true
		case "AccessPoints":
			needsNetworkUpdate = true
		default:
			continue
		}
	}

	if needsStateUpdate {
		b.updateWiFiState()
	}
	if needsNetworkUpdate {
		b.updateWiFiNetworks()
	}
	if needsStateUpdate || needsNetworkUpdate {
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}
}

func (b *NetworkManagerBackend) handleAccessPointChange(changes map[string]dbus.Variant) {
	_, hasStrength := changes["Strength"]
	if !hasStrength {
		return
	}

	b.stateMutex.RLock()
	oldSignal := b.state.WiFiSignal
	b.stateMutex.RUnlock()

	b.updateWiFiState()

	b.stateMutex.RLock()
	newSignal := b.state.WiFiSignal
	b.stateMutex.RUnlock()

	if signalChangeSignificant(oldSignal, newSignal) {
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}
}

func (b *NetworkManagerBackend) ListVPNProfiles() ([]VPNProfile, error) {
	s := b.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %w", err)
	}

	var profiles []VPNProfile
	for _, conn := range connections {
		settings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMeta, ok := settings["connection"]
		if !ok {
			continue
		}

		connType, _ := connMeta["type"].(string)
		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		connID, _ := connMeta["id"].(string)
		connUUID, _ := connMeta["uuid"].(string)

		profile := VPNProfile{
			Name: connID,
			UUID: connUUID,
			Type: connType,
		}

		if connType == "vpn" {
			if vpnSettings, ok := settings["vpn"]; ok {
				if svcType, ok := vpnSettings["service-type"].(string); ok {
					profile.ServiceType = svcType
				}
			}
		}

		profiles = append(profiles, profile)
	}

	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})

	b.stateMutex.Lock()
	b.state.VPNProfiles = profiles
	b.stateMutex.Unlock()

	return profiles, nil
}

func (b *NetworkManagerBackend) ListActiveVPN() ([]VPNActive, error) {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to get active connections: %w", err)
	}

	var active []VPNActive
	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		uuid, _ := activeConn.GetPropertyUUID()
		id, _ := activeConn.GetPropertyID()
		state, _ := activeConn.GetPropertyState()

		var stateStr string
		switch state {
		case 0:
			stateStr = "unknown"
		case 1:
			stateStr = "activating"
		case 2:
			stateStr = "activated"
		case 3:
			stateStr = "deactivating"
		case 4:
			stateStr = "deactivated"
		}

		vpnActive := VPNActive{
			Name:   id,
			UUID:   uuid,
			State:  stateStr,
			Type:   connType,
			Plugin: "",
		}

		if connType == "vpn" {
			conn, _ := activeConn.GetPropertyConnection()
			if conn != nil {
				connSettings, err := conn.GetSettings()
				if err == nil {
					if vpnSettings, ok := connSettings["vpn"]; ok {
						if svcType, ok := vpnSettings["service-type"].(string); ok {
							vpnActive.Plugin = svcType
						}
					}
				}
			}
		}

		active = append(active, vpnActive)
	}

	b.stateMutex.Lock()
	b.state.VPNActive = active
	b.stateMutex.Unlock()

	return active, nil
}

func (b *NetworkManagerBackend) ConnectVPN(uuidOrName string, singleActive bool) error {
	if singleActive {
		active, err := b.ListActiveVPN()
		if err == nil && len(active) > 0 {
			// If we're already connected to the requested VPN, nothing to do
			alreadyConnected := false
			for _, vpn := range active {
				if vpn.UUID == uuidOrName || vpn.Name == uuidOrName {
					alreadyConnected = true
					break
				}
			}

			// If requesting a different VPN, disconnect all others first
			if !alreadyConnected {
				if err := b.DisconnectAllVPN(); err != nil {
					log.Warnf("Failed to disconnect existing VPNs: %v", err)
				}
				// Give NetworkManager a moment to process the disconnect
				time.Sleep(500 * time.Millisecond)
			} else {
				// Already connected to this VPN, nothing to do
				return nil
			}
		}
	}

	s := b.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections: %w", err)
	}

	var targetConn gonetworkmanager.Connection
	for _, conn := range connections {
		settings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMeta, ok := settings["connection"]
		if !ok {
			continue
		}

		connType, _ := connMeta["type"].(string)
		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		connID, _ := connMeta["id"].(string)
		connUUID, _ := connMeta["uuid"].(string)

		if connUUID == uuidOrName || connID == uuidOrName {
			targetConn = conn
			break
		}
	}

	if targetConn == nil {
		return fmt.Errorf("VPN connection not found: %s", uuidOrName)
	}

	targetSettings, err := targetConn.GetSettings()
	if err != nil {
		return fmt.Errorf("failed to get connection settings: %w", err)
	}

	var targetUUID string
	if connMeta, ok := targetSettings["connection"]; ok {
		if uuid, ok := connMeta["uuid"].(string); ok {
			targetUUID = uuid
		}
	}

	b.stateMutex.Lock()
	b.state.IsConnectingVPN = true
	b.state.ConnectingVPNUUID = targetUUID
	b.stateMutex.Unlock()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	nm := b.nmConn.(gonetworkmanager.NetworkManager)
	activeConn, err := nm.ActivateConnection(targetConn, nil, nil)
	if err != nil {
		b.stateMutex.Lock()
		b.state.IsConnectingVPN = false
		b.state.ConnectingVPNUUID = ""
		b.stateMutex.Unlock()

		if b.onStateChange != nil {
			b.onStateChange()
		}

		return fmt.Errorf("failed to activate VPN: %w", err)
	}

	if activeConn != nil {
		state, _ := activeConn.GetPropertyState()
		if state == 2 {
			b.stateMutex.Lock()
			b.state.IsConnectingVPN = false
			b.state.ConnectingVPNUUID = ""
			b.stateMutex.Unlock()
			b.ListActiveVPN()
			if b.onStateChange != nil {
				b.onStateChange()
			}
		}
	}

	return nil
}

func (b *NetworkManagerBackend) DisconnectVPN(uuidOrName string) error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return fmt.Errorf("failed to get active connections: %w", err)
	}

	log.Debugf("[DisconnectVPN] Looking for VPN: %s", uuidOrName)

	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		uuid, _ := activeConn.GetPropertyUUID()
		id, _ := activeConn.GetPropertyID()
		state, _ := activeConn.GetPropertyState()

		log.Debugf("[DisconnectVPN] Found active VPN: uuid=%s id=%s state=%d", uuid, id, state)

		if uuid == uuidOrName || id == uuidOrName {
			log.Infof("[DisconnectVPN] Deactivating VPN: %s (state=%d)", id, state)
			if err := nm.DeactivateConnection(activeConn); err != nil {
				return fmt.Errorf("failed to deactivate VPN: %w", err)
			}
			b.ListActiveVPN()
			if b.onStateChange != nil {
				b.onStateChange()
			}
			return nil
		}
	}

	log.Warnf("[DisconnectVPN] VPN not found in active connections: %s", uuidOrName)

	s := b.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return fmt.Errorf("VPN connection not active and cannot access settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("VPN connection not active: %s", uuidOrName)
	}

	for _, conn := range connections {
		settings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMeta, ok := settings["connection"]
		if !ok {
			continue
		}

		connType, _ := connMeta["type"].(string)
		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		connID, _ := connMeta["id"].(string)
		connUUID, _ := connMeta["uuid"].(string)

		if connUUID == uuidOrName || connID == uuidOrName {
			log.Infof("[DisconnectVPN] VPN connection exists but not active: %s", connID)
			return nil
		}
	}

	return fmt.Errorf("VPN connection not found: %s", uuidOrName)
}

func (b *NetworkManagerBackend) DisconnectAllVPN() error {
	nm := b.nmConn.(gonetworkmanager.NetworkManager)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return fmt.Errorf("failed to get active connections: %w", err)
	}

	var lastErr error
	var disconnected bool
	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		if err := nm.DeactivateConnection(activeConn); err != nil {
			lastErr = err
			log.Warnf("Failed to deactivate VPN connection: %v", err)
		} else {
			disconnected = true
		}
	}

	if disconnected {
		b.ListActiveVPN()
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}

	return lastErr
}

func (b *NetworkManagerBackend) ClearVPNCredentials(uuidOrName string) error {
	s := b.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		b.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections: %w", err)
	}

	for _, conn := range connections {
		settings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMeta, ok := settings["connection"]
		if !ok {
			continue
		}

		connType, _ := connMeta["type"].(string)
		if connType != "vpn" && connType != "wireguard" {
			continue
		}

		connID, _ := connMeta["id"].(string)
		connUUID, _ := connMeta["uuid"].(string)

		if connUUID == uuidOrName || connID == uuidOrName {
			if connType == "vpn" {
				if vpnSettings, ok := settings["vpn"]; ok {
					delete(vpnSettings, "secrets")

					if dataMap, ok := vpnSettings["data"].(map[string]string); ok {
						dataMap["password-flags"] = "1"
						vpnSettings["data"] = dataMap
					}

					vpnSettings["password-flags"] = uint32(1)
				}

				settings["vpn-secrets"] = make(map[string]interface{})
			}

			if err := conn.Update(settings); err != nil {
				return fmt.Errorf("failed to update connection: %w", err)
			}

			if err := conn.ClearSecrets(); err != nil {
				log.Warnf("ClearSecrets call failed (may not be critical): %v", err)
			}

			log.Infof("Cleared credentials for VPN: %s", connID)
			return nil
		}
	}

	return fmt.Errorf("VPN connection not found: %s", uuidOrName)
}
