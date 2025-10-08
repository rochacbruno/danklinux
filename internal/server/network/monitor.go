package network

import (
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/godbus/dbus/v5"
)

func (m *Manager) handleDBusSignal(sig *dbus.Signal) {
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
	case "org.freedesktop.NetworkManager":
		m.handleNetworkManagerChange(changes)

	case "org.freedesktop.NetworkManager.Device":
		m.handleDeviceChange(changes)

	case "org.freedesktop.NetworkManager.Device.Wireless":
		m.handleWiFiChange(changes)

	case "org.freedesktop.NetworkManager.AccessPoint":
		m.handleAccessPointChange(changes)
	}
}

func (m *Manager) handleNetworkManagerChange(changes map[string]dbus.Variant) {
	var needsUpdate bool

	for key := range changes {
		switch key {
		case "PrimaryConnection", "State", "ActiveConnections":
			needsUpdate = true
		case "WirelessEnabled":
			nm := m.nmConn.(gonetworkmanager.NetworkManager)
			if enabled, err := nm.GetPropertyWirelessEnabled(); err == nil {
				m.stateMutex.Lock()
				m.state.WiFiEnabled = enabled
				m.stateMutex.Unlock()
				needsUpdate = true
			}
		default:
			// Ignore irrelevant properties
			continue
		}
	}

	if needsUpdate {
		m.updatePrimaryConnection()
		if _, exists := changes["State"]; exists {
			m.updateEthernetState()
			m.updateWiFiState()
		}
		m.notifySubscribers()
	}
}

func (m *Manager) handleDeviceChange(changes map[string]dbus.Variant) {
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
			// Ignore irrelevant properties
			continue
		}
	}

	if needsUpdate {
		m.updateEthernetState()
		m.updateWiFiState()
		if stateChanged {
			m.updatePrimaryConnection()
		}
		m.notifySubscribers()
	}
}

func (m *Manager) handleWiFiChange(changes map[string]dbus.Variant) {
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
			// Ignore irrelevant properties
			continue
		}
	}

	if needsStateUpdate {
		m.updateWiFiState()
	}
	if needsNetworkUpdate {
		m.updateWiFiNetworks()
	}
	if needsStateUpdate || needsNetworkUpdate {
		m.notifySubscribers()
	}
}

func (m *Manager) handleAccessPointChange(changes map[string]dbus.Variant) {
	_, hasStrength := changes["Strength"]
	if !hasStrength {
		return
	}

	m.stateMutex.RLock()
	oldSignal := m.state.WiFiSignal
	m.stateMutex.RUnlock()

	m.updateWiFiState()

	m.stateMutex.RLock()
	newSignal := m.state.WiFiSignal
	m.stateMutex.RUnlock()

	if signalChangeSignificant(oldSignal, newSignal) {
		m.notifySubscribers()
	}
}

func (m *Manager) StartAutoScan(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopChan:
				return
			case <-ticker.C:
				m.stateMutex.RLock()
				enabled := m.state.WiFiEnabled
				m.stateMutex.RUnlock()

				if enabled {
					m.ScanWiFi()
				}
			}
		}
	}()
}
