package network

import (
	"bytes"
	"fmt"
	"log"

	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) ConnectWiFi(req ConnectionRequest) error {
	log.Printf("[ConnectWiFi] Starting connection to SSID: %s, hasUsername: %v, hasPassword: %v",
		req.SSID, req.Username != "", req.Password != "")

	if m.wifiDevice == nil {
		log.Printf("[ConnectWiFi] ERROR: No WiFi device available")
		return fmt.Errorf("no WiFi device available")
	}

	m.stateMutex.Lock()
	m.state.IsConnecting = true
	m.state.ConnectingSSID = req.SSID
	m.state.LastError = ""
	m.stateMutex.Unlock()

	m.notifySubscribers()

	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	log.Printf("[ConnectWiFi] Searching for existing connection for SSID: %s", req.SSID)
	existingConn, err := m.findConnection(req.SSID)
	if err == nil && existingConn != nil {
		log.Printf("[ConnectWiFi] Found existing connection, attempting to activate")
		dev := m.wifiDevice.(gonetworkmanager.Device)

		_, err := nm.ActivateConnection(existingConn, dev, nil)
		if err != nil {
			log.Printf("[ConnectWiFi] ERROR: Failed to activate existing connection: %v", err)
			m.stateMutex.Lock()
			m.state.IsConnecting = false
			m.state.ConnectingSSID = ""
			m.state.LastError = fmt.Sprintf("failed to activate connection: %v", err)
			m.stateMutex.Unlock()
			m.notifySubscribers()
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		log.Printf("[ConnectWiFi] Successfully activated existing connection")
		m.stateMutex.Lock()
		m.state.IsConnecting = false
		m.state.ConnectingSSID = ""
		m.stateMutex.Unlock()

		m.updateWiFiState()
		m.updatePrimaryConnection()
		m.notifySubscribers()

		return nil
	}
	log.Printf("[ConnectWiFi] No existing connection found (or error: %v), creating new connection", err)

	if err := m.createAndConnectWiFi(req); err != nil {
		log.Printf("[ConnectWiFi] ERROR: Failed to create and connect: %v", err)
		m.stateMutex.Lock()
		m.state.IsConnecting = false
		m.state.ConnectingSSID = ""
		m.state.LastError = err.Error()
		m.stateMutex.Unlock()
		m.notifySubscribers()
		return err
	}

	log.Printf("[ConnectWiFi] Successfully created and connected to new network")
	m.stateMutex.Lock()
	m.state.IsConnecting = false
	m.state.ConnectingSSID = ""
	m.stateMutex.Unlock()

	m.updateWiFiState()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}

func (m *Manager) createAndConnectWiFi(req ConnectionRequest) error {
	log.Printf("[createAndConnectWiFi] Starting for SSID: %s", req.SSID)

	nm := m.nmConn.(gonetworkmanager.NetworkManager)
	dev := m.wifiDevice.(gonetworkmanager.Device)

	log.Printf("[createAndConnectWiFi] Ensuring WiFi device is available")
	if err := m.ensureWiFiDevice(); err != nil {
		log.Printf("[createAndConnectWiFi] ERROR: Failed to ensure WiFi device: %v", err)
		return err
	}
	wifiDev := m.wifiDev

	log.Printf("[createAndConnectWiFi] Getting access points")
	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		log.Printf("[createAndConnectWiFi] ERROR: Failed to get access points: %v", err)
		return fmt.Errorf("failed to get access points: %w", err)
	}
	log.Printf("[createAndConnectWiFi] Found %d access points", len(apPaths))

	var targetAP gonetworkmanager.AccessPoint
	for _, ap := range apPaths {
		ssid, err := ap.GetPropertySSID()
		if err != nil || ssid != req.SSID {
			continue
		}

		targetAP = ap
		log.Printf("[createAndConnectWiFi] Found target access point for SSID: %s", req.SSID)
		break
	}

	if targetAP == nil {
		log.Printf("[createAndConnectWiFi] ERROR: Access point not found: %s", req.SSID)
		return fmt.Errorf("access point not found: %s", req.SSID)
	}

	flags, _ := targetAP.GetPropertyFlags()
	wpaFlags, _ := targetAP.GetPropertyWPAFlags()
	rsnFlags, _ := targetAP.GetPropertyRSNFlags()

	log.Printf("[createAndConnectWiFi] AP Security flags - flags: 0x%x, wpaFlags: 0x%x, rsnFlags: 0x%x",
		flags, wpaFlags, rsnFlags)

	// NM80211ApSecurityFlags constants
	const KeyMgmt8021x = uint32(512) // Enterprise WiFi (802.1X)
	const KeyMgmtPsk = uint32(256)   // WPA2-Personal (PSK)
	const KeyMgmtSae = uint32(1024)  // WPA3-Personal (SAE)

	isEnterprise := (wpaFlags&KeyMgmt8021x) != 0 || (rsnFlags&KeyMgmt8021x) != 0
	isPsk := (wpaFlags&KeyMgmtPsk) != 0 || (rsnFlags&KeyMgmtPsk) != 0
	isSae := (wpaFlags&KeyMgmtSae) != 0 || (rsnFlags&KeyMgmtSae) != 0

	secured := flags != uint32(gonetworkmanager.Nm80211APFlagsNone) ||
		wpaFlags != uint32(gonetworkmanager.Nm80211APSecNone) ||
		rsnFlags != uint32(gonetworkmanager.Nm80211APSecNone)

	log.Printf("[createAndConnectWiFi] Network analysis - secured: %v, isEnterprise: %v, isPsk: %v, isSae: %v",
		secured, isEnterprise, isPsk, isSae)

	settings := make(map[string]map[string]interface{})

	settings["connection"] = map[string]interface{}{
		"id":   req.SSID,
		"type": "802-11-wireless",
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
			log.Printf("[createAndConnectWiFi] Configuring WPA-EAP (enterprise) with username: %s", req.Username)

			if req.Username == "" {
				log.Printf("[createAndConnectWiFi] ERROR: Enterprise network detected but no username provided")
				return fmt.Errorf("enterprise network requires username")
			}

			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "wpa-eap",
			}

			settings["802-1x"] = map[string]interface{}{
				"eap":             []string{"peap"},
				"phase2-autheap":  "mschapv2",
				"identity":        req.Username,
				"password":        req.Password,
				"system-ca-certs": true,
			}
			log.Printf("[createAndConnectWiFi] WPA-EAP settings configured: eap=peap, phase2-auth=mschapv2, identity=%s, system-ca-certs=true", req.Username)

		case isSae:
			log.Printf("[createAndConnectWiFi] Configuring WPA3-SAE (personal)")
			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "sae",
				"psk":      req.Password,
				"pmf":      int32(2), // WPA3 requires PMF: 2=required, 1=optional, 0=disabled
			}

		case isPsk:
			log.Printf("[createAndConnectWiFi] Configuring WPA2-PSK (personal)")
			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "wpa-psk",
				"psk":      req.Password,
			}

		default:
			log.Printf("[createAndConnectWiFi] ERROR: Secured network with unknown key management (rsn=0x%x, wpa=0x%x)", rsnFlags, wpaFlags)
			return fmt.Errorf("secured network but not SAE/PSK/802.1X (rsn=0x%x wpa=0x%x)", rsnFlags, wpaFlags)
		}
	} else {
		// Open network
		settings["802-11-wireless"] = map[string]interface{}{
			"ssid": []byte(req.SSID),
			"mode": "infrastructure",
		}
		log.Printf("[createAndConnectWiFi] Network is open (no security)")
	}

	log.Printf("[createAndConnectWiFi] Calling AddAndActivateWirelessConnection with settings: %+v", settings)
	log.Printf("[createAndConnectWiFi] Using access point: %s", targetAP.GetPath())

	_, err = nm.AddAndActivateWirelessConnection(settings, dev, targetAP)
	if err != nil {
		log.Printf("[createAndConnectWiFi] ERROR: AddAndActivateWirelessConnection failed: %v", err)
		log.Printf("[createAndConnectWiFi] ERROR: Connection settings that failed: %+v", settings)
		return fmt.Errorf("failed to connect: %w", err)
	}

	log.Printf("[createAndConnectWiFi] Successfully added and activated wireless connection")
	return nil
}

func (m *Manager) findConnection(ssid string) (gonetworkmanager.Connection, error) {
	s := m.settings
	if s == nil {
		var err error
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, err
		}
		m.settings = s
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

func (m *Manager) DisconnectWiFi() error {
	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	dev := m.wifiDevice.(gonetworkmanager.Device)

	err := dev.Disconnect()
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	m.updateWiFiState()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}

func (m *Manager) ForgetWiFiNetwork(ssid string) error {
	conn, err := m.findConnection(ssid)
	if err != nil {
		return fmt.Errorf("connection not found: %w", err)
	}

	err = conn.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete connection: %w", err)
	}

	m.updateWiFiNetworks()
	m.notifySubscribers()

	return nil
}

func (m *Manager) ConnectEthernet() error {
	if m.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	nm := m.nmConn.(gonetworkmanager.NetworkManager)
	dev := m.ethernetDevice.(gonetworkmanager.Device)

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

				m.updateEthernetState()
				m.updatePrimaryConnection()
				m.notifySubscribers()

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

	m.updateEthernetState()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}

func (m *Manager) DisconnectEthernet() error {
	if m.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	dev := m.ethernetDevice.(gonetworkmanager.Device)

	err := dev.Disconnect()
	if err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	m.updateEthernetState()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}
