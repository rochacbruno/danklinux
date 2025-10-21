package network

import (
	"bytes"
	"fmt"
	"log"

	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) ConnectWiFi(req ConnectionRequest) error {
	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	m.stateMutex.RLock()
	alreadyConnected := m.state.WiFiConnected && m.state.WiFiSSID == req.SSID
	m.stateMutex.RUnlock()

	if alreadyConnected && !req.Interactive {
		return nil
	}

	m.stateMutex.Lock()
	m.state.IsConnecting = true
	m.state.ConnectingSSID = req.SSID
	m.state.LastError = ""
	m.stateMutex.Unlock()

	m.notifySubscribers()

	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	existingConn, err := m.findConnection(req.SSID)
	if err == nil && existingConn != nil {
		dev := m.wifiDevice.(gonetworkmanager.Device)

		_, err := nm.ActivateConnection(existingConn, dev, nil)
		if err != nil {
			log.Printf("[ConnectWiFi] Failed to activate existing connection: %v", err)
			m.stateMutex.Lock()
			m.state.IsConnecting = false
			m.state.ConnectingSSID = ""
			m.state.LastError = fmt.Sprintf("failed to activate connection: %v", err)
			m.stateMutex.Unlock()
			m.notifySubscribers()
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		return nil
	}

	if err := m.createAndConnectWiFi(req); err != nil {
		log.Printf("[ConnectWiFi] Failed to create and connect: %v", err)
		m.stateMutex.Lock()
		m.state.IsConnecting = false
		m.state.ConnectingSSID = ""
		m.state.LastError = err.Error()
		m.stateMutex.Unlock()
		m.notifySubscribers()
		return err
	}

	return nil
}

func (m *Manager) createAndConnectWiFi(req ConnectionRequest) error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)
	dev := m.wifiDevice.(gonetworkmanager.Device)

	if err := m.ensureWiFiDevice(); err != nil {
		return err
	}
	wifiDev := m.wifiDev

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
		log.Printf("[createAndConnectWiFi] Enterprise network detected (802.1x) - SSID: %s, interactive: %v",
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
			}

			if req.Interactive {
				x["password-flags"] = uint32(1)
				if req.Username != "" {
					x["identity"] = req.Username
				}
			} else {
				x["identity"] = req.Username
				x["password"] = req.Password
				x["password-flags"] = uint32(0)
			}

			if req.AnonymousIdentity != "" {
				x["anonymous-identity"] = req.AnonymousIdentity
			}
			if req.DomainSuffixMatch != "" {
				x["domain-suffix-match"] = req.DomainSuffixMatch
			}

			settings["802-1x"] = x

			log.Printf("[createAndConnectWiFi] WPA-EAP settings: eap=peap, phase2-auth=mschapv2, identity=%s, interactive=%v, system-ca-certs=%v, domain-suffix-match=%q",
				req.Username, req.Interactive, x["system-ca-certs"], req.DomainSuffixMatch)

		case isPsk:
			sec := map[string]interface{}{
				"key-mgmt": "wpa-psk",
			}
			if req.Interactive {
				sec["psk-flags"] = uint32(1)
			} else {
				sec["psk"] = req.Password
				sec["psk-flags"] = uint32(0)
			}
			settings["802-11-wireless-security"] = sec

		case isSae:
			sec := map[string]interface{}{
				"key-mgmt": "sae",
			}
			if req.Interactive {
				sec["psk-flags"] = uint32(1)
			} else {
				sec["psk"] = req.Password
				sec["psk-flags"] = uint32(0)
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
		s := m.settings
		if s == nil {
			var settingsErr error
			s, settingsErr = gonetworkmanager.NewSettings()
			if settingsErr != nil {
				return fmt.Errorf("failed to get settings manager: %w", settingsErr)
			}
			m.settings = s
		}

		settingsMgr := s.(gonetworkmanager.Settings)
		conn, err := settingsMgr.AddConnection(settings)
		if err != nil {
			return fmt.Errorf("failed to add connection: %w", err)
		}

		if isEnterprise {
			log.Printf("[createAndConnectWiFi] Enterprise connection added, activating (secret agent will be called)")
		}

		_, err = nm.ActivateWirelessConnection(conn, dev, targetAP)
		if err != nil {
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		log.Printf("[createAndConnectWiFi] Connection activation initiated, waiting for NetworkManager state changes...")
	} else {
		_, err = nm.AddAndActivateWirelessConnection(settings, dev, targetAP)
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		log.Printf("[createAndConnectWiFi] Connection activation initiated, waiting for NetworkManager state changes...")
	}

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
				m.listEthernetConnections()
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
	m.listEthernetConnections()
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
	m.listEthernetConnections()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}
