package network

import (
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/godbus/dbus/v5"
)

func (m *Manager) monitorChanges() {

	conn, err := dbus.SystemBus()
	if err != nil {
		return
	}

	err = conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/NetworkManager"),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	if err != nil {
		return
	}

	if m.wifiDevice != nil {
		dev := m.wifiDevice.(gonetworkmanager.Device)
		err = conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		if err != nil {
			return
		}
	}

	if m.ethernetDevice != nil {
		dev := m.ethernetDevice.(gonetworkmanager.Device)
		err = conn.AddMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		if err != nil {
			return
		}
	}

	signals := make(chan *dbus.Signal, 10)
	conn.Signal(signals)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return

		case sig := <-signals:
			if sig == nil {
				continue
			}

			m.handleDBusSignal(sig)

		case <-ticker.C:
			go func() {
				m.stateMutex.RLock()
				enabled := m.state.WiFiEnabled
				m.stateMutex.RUnlock()

				if enabled {
					m.updateWiFiNetworks()
				}
			}()
		}
	}
}

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
	}
}

func (m *Manager) handleNetworkManagerChange(changes map[string]dbus.Variant) {
	for key := range changes {
		switch key {
		case "PrimaryConnection":
			m.updatePrimaryConnection()
			m.notifySubscribers()

		case "WirelessEnabled":
			nm := m.nmConn.(gonetworkmanager.NetworkManager)
			if enabled, err := nm.GetPropertyWirelessEnabled(); err == nil {
				m.stateMutex.Lock()
				m.state.WiFiEnabled = enabled
				m.stateMutex.Unlock()
				m.notifySubscribers()
			}

		case "State":
			m.updatePrimaryConnection()
			m.updateEthernetState()
			m.updateWiFiState()
			m.notifySubscribers()
		}
	}
}

func (m *Manager) handleDeviceChange(changes map[string]dbus.Variant) {
	for key := range changes {
		switch key {
		case "State":
			m.updateEthernetState()
			m.updateWiFiState()
			m.updatePrimaryConnection()
			m.notifySubscribers()

		case "Ip4Config":
			m.updateEthernetState()
			m.updateWiFiState()
			m.notifySubscribers()
		}
	}
}

func (m *Manager) handleWiFiChange(changes map[string]dbus.Variant) {
	for key := range changes {
		switch key {
		case "ActiveAccessPoint":
			m.updateWiFiState()
			m.updateWiFiNetworks()
			m.notifySubscribers()

		case "AccessPoints":
			m.updateWiFiNetworks()
		}
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
