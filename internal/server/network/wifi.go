package network

import (
	"fmt"
	"sort"

	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) ScanWiFi() error {
	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	m.stateMutex.RLock()
	enabled := m.state.WiFiEnabled
	m.stateMutex.RUnlock()

	if !enabled {
		return fmt.Errorf("WiFi is disabled")
	}

	if err := m.ensureWiFiDevice(); err != nil {
		return err
	}
	wifiDev := m.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	err := w.RequestScan()
	if err != nil {
		return fmt.Errorf("scan request failed: %w", err)
	}

	return m.updateWiFiNetworks()
}

func (m *Manager) updateWiFiNetworks() error {
	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	if err := m.ensureWiFiDevice(); err != nil {
		return err
	}
	wifiDev := m.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		return fmt.Errorf("failed to get access points: %w", err)
	}

	s := m.settings
	if s == nil {
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		m.settings = s
	}

	settingsMgr := s.(gonetworkmanager.Settings)
	connections, err := settingsMgr.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to get connections: %w", err)
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

	m.stateMutex.RLock()
	currentSSID := m.state.WiFiSSID
	m.stateMutex.RUnlock()

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

	sortWiFiNetworks(networks, currentSSID)

	m.stateMutex.Lock()
	m.state.WiFiNetworks = networks
	m.stateMutex.Unlock()

	return nil
}

func sortWiFiNetworks(networks []WiFiNetwork, currentSSID string) {
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].Connected && !networks[j].Connected {
			return true
		}
		if !networks[i].Connected && networks[j].Connected {
			return false
		}

		if !networks[i].Secured && networks[j].Secured {
			if networks[i].Signal >= 50 {
				return true
			}
		}
		if networks[i].Secured && !networks[j].Secured {
			if networks[j].Signal >= 50 {
				return false
			}
		}

		return networks[i].Signal > networks[j].Signal
	})
}

func frequencyToChannel(freq uint32) uint32 {
	if freq >= 2412 && freq <= 2484 {
		if freq == 2484 {
			return 14
		}
		return (freq-2412)/5 + 1
	}

	if freq >= 5170 && freq <= 5825 {
		return (freq-5170)/5 + 34
	}

	if freq >= 5955 && freq <= 7115 {
		return (freq-5955)/5 + 1
	}

	return 0
}

func (m *Manager) GetWiFiNetworks() []WiFiNetwork {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	networks := make([]WiFiNetwork, len(m.state.WiFiNetworks))
	copy(networks, m.state.WiFiNetworks)
	return networks
}

func (m *Manager) GetNetworkInfo(ssid string) (*WiFiNetwork, error) {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()

	for _, network := range m.state.WiFiNetworks {
		if network.SSID == ssid {
			return &network, nil
		}
	}

	return nil, fmt.Errorf("network not found: %s", ssid)
}

func (m *Manager) ToggleWiFi() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	enabled, err := nm.GetPropertyWirelessEnabled()
	if err != nil {
		return fmt.Errorf("failed to get WiFi state: %w", err)
	}

	err = nm.SetPropertyWirelessEnabled(!enabled)
	if err != nil {
		return fmt.Errorf("failed to toggle WiFi: %w", err)
	}

	m.stateMutex.Lock()
	m.state.WiFiEnabled = !enabled
	m.stateMutex.Unlock()

	m.notifySubscribers()

	return nil
}

func (m *Manager) EnableWiFi() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	err := nm.SetPropertyWirelessEnabled(true)
	if err != nil {
		return fmt.Errorf("failed to enable WiFi: %w", err)
	}

	m.stateMutex.Lock()
	m.state.WiFiEnabled = true
	m.stateMutex.Unlock()

	m.notifySubscribers()

	return nil
}

func (m *Manager) DisableWiFi() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	err := nm.SetPropertyWirelessEnabled(false)
	if err != nil {
		return fmt.Errorf("failed to disable WiFi: %w", err)
	}

	m.stateMutex.Lock()
	m.state.WiFiEnabled = false
	m.stateMutex.Unlock()

	m.notifySubscribers()

	return nil
}

type NetworkInfoResponse struct {
	SSID  string        `json:"ssid"`
	Bands []WiFiNetwork `json:"bands"`
}

func (m *Manager) GetNetworkInfoDetailed(ssid string) (*NetworkInfoResponse, error) {
	if m.wifiDevice == nil {
		return nil, fmt.Errorf("no WiFi device available")
	}

	if err := m.ensureWiFiDevice(); err != nil {
		return nil, err
	}
	wifiDev := m.wifiDev

	w := wifiDev.(gonetworkmanager.DeviceWireless)
	apPaths, err := w.GetAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	s := m.settings
	if s == nil {
		s, err = gonetworkmanager.NewSettings()
		if err != nil {
			return nil, fmt.Errorf("failed to get settings: %w", err)
		}
		m.settings = s
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

	m.stateMutex.RLock()
	currentSSID := m.state.WiFiSSID
	currentBSSID := m.state.WiFiBSSID
	m.stateMutex.RUnlock()

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
