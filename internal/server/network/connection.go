package network

import (
	"bytes"
	"fmt"

	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) ConnectWiFi(req ConnectionRequest) error {
	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
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
			m.stateMutex.Lock()
			m.state.IsConnecting = false
			m.state.ConnectingSSID = ""
			m.state.LastError = fmt.Sprintf("failed to activate connection: %v", err)
			m.stateMutex.Unlock()
			m.notifySubscribers()
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		m.stateMutex.Lock()
		m.state.IsConnecting = false
		m.state.ConnectingSSID = ""
		m.stateMutex.Unlock()

		m.updateWiFiState()
		m.updatePrimaryConnection()
		m.notifySubscribers()

		return nil
	}

	if err := m.createAndConnectWiFi(req); err != nil {
		m.stateMutex.Lock()
		m.state.IsConnecting = false
		m.state.ConnectingSSID = ""
		m.state.LastError = err.Error()
		m.stateMutex.Unlock()
		m.notifySubscribers()
		return err
	}

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

	settings := make(map[string]map[string]interface{})

	settings["connection"] = map[string]interface{}{
		"id":   req.SSID,
		"type": "802-11-wireless",
	}

	settings["802-11-wireless"] = map[string]interface{}{
		"ssid": []byte(req.SSID),
		"mode": "infrastructure",
	}

	flags, _ := targetAP.GetPropertyFlags()
	wpaFlags, _ := targetAP.GetPropertyWPAFlags()
	rsnFlags, _ := targetAP.GetPropertyRSNFlags()

	secured := flags != uint32(gonetworkmanager.Nm80211APFlagsNone) ||
		wpaFlags != uint32(gonetworkmanager.Nm80211APSecNone) ||
		rsnFlags != uint32(gonetworkmanager.Nm80211APSecNone)

	if secured {
		if req.Username != "" {
			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "wpa-eap",
			}

			settings["802-1x"] = map[string]interface{}{
				"eap":      []string{"peap"},
				"identity": req.Username,
				"password": req.Password,
			}
		} else if req.Password != "" {
			settings["802-11-wireless-security"] = map[string]interface{}{
				"key-mgmt": "wpa-psk",
				"psk":      req.Password,
			}
		} else {
			return fmt.Errorf("network is secured but no password provided")
		}
	}

	_, err = nm.AddAndActivateConnection(settings, dev)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
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
