package network

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
)

func NewManager() (*Manager, error) {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	m := &Manager{
		state: &NetworkState{
			NetworkStatus: StatusDisconnected,
			Preference:    PreferenceAuto,
			WiFiNetworks:  []WiFiNetwork{},
		},
		stateMutex:  sync.RWMutex{},
		subscribers: make(map[string]chan NetworkState),
		subMutex:    sync.RWMutex{},
		stopChan:    make(chan struct{}),
		nmConn:      nm,
		dirty:       make(chan struct{}, 1),
	}

	if err := m.initialize(); err != nil {
		return nil, err
	}

	m.notifierWg.Add(1)
	go m.notifier()
	go m.monitorChanges()

	return m, nil
}

func (m *Manager) initialize() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	if s, err := gonetworkmanager.NewSettings(); err == nil {
		m.settings = s
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
			m.ethernetDevice = dev
			if err := m.updateEthernetState(); err != nil {
				continue
			}

		case gonetworkmanager.NmDeviceTypeWifi:
			m.wifiDevice = dev
			if w, err := gonetworkmanager.NewDeviceWireless(dev.GetPath()); err == nil {
				m.wifiDev = w
			}
			wifiEnabled, err := nm.GetPropertyWirelessEnabled()
			if err == nil {
				m.stateMutex.Lock()
				m.state.WiFiEnabled = wifiEnabled
				m.stateMutex.Unlock()
			}
			if err := m.updateWiFiState(); err != nil {
				continue
			}
		}
	}

	if err := m.updatePrimaryConnection(); err != nil {
		return err
	}

	return nil
}

func (m *Manager) updatePrimaryConnection() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	primaryConn, err := nm.GetPropertyPrimaryConnection()
	if err != nil {
		return err
	}

	if primaryConn == nil || primaryConn.GetPath() == "/" {
		m.stateMutex.Lock()
		m.state.NetworkStatus = StatusDisconnected
		m.stateMutex.Unlock()
		return nil
	}

	connType, err := primaryConn.GetPropertyType()
	if err != nil {
		return err
	}

	m.stateMutex.Lock()
	switch connType {
	case "802-3-ethernet":
		m.state.NetworkStatus = StatusEthernet
	case "802-11-wireless":
		m.state.NetworkStatus = StatusWiFi
	default:
		m.state.NetworkStatus = StatusDisconnected
	}
	m.stateMutex.Unlock()

	return nil
}

func (m *Manager) updateEthernetState() error {
	if m.ethernetDevice == nil {
		return nil
	}

	dev := m.ethernetDevice.(gonetworkmanager.Device)

	iface, err := dev.GetPropertyInterface()
	if err != nil {
		return err
	}

	state, err := dev.GetPropertyState()
	if err != nil {
		return err
	}

	connected := state == gonetworkmanager.NmDeviceStateActivated

	m.stateMutex.Lock()
	m.state.EthernetDevice = iface
	m.state.EthernetConnected = connected
	m.stateMutex.Unlock()

	if connected {
		if ip := m.getDeviceIP(dev); ip != "" {
			m.stateMutex.Lock()
			m.state.EthernetIP = ip
			m.stateMutex.Unlock()
		}
	} else {
		m.stateMutex.Lock()
		m.state.EthernetIP = ""
		m.stateMutex.Unlock()
	}

	return nil
}

func (m *Manager) updateWiFiState() error {
	if m.wifiDevice == nil {
		return nil
	}

	dev := m.wifiDevice.(gonetworkmanager.Device)

	iface, err := dev.GetPropertyInterface()
	if err != nil {
		return err
	}

	state, err := dev.GetPropertyState()
	if err != nil {
		return err
	}

	connected := state == gonetworkmanager.NmDeviceStateActivated

	m.stateMutex.Lock()
	m.state.WiFiDevice = iface
	m.state.WiFiConnected = connected
	m.stateMutex.Unlock()

	if connected {
		wifiDev := m.wifiDev
		if wifiDev == nil {
			var err error
			wifiDev, err = gonetworkmanager.NewDeviceWireless(dev.GetPath())
			if err == nil {
				m.wifiDev = wifiDev
			}
		}

		if wifiDev != nil {
			w := wifiDev.(gonetworkmanager.DeviceWireless)
			activeAP, err := w.GetPropertyActiveAccessPoint()
			if err == nil && activeAP != nil && activeAP.GetPath() != "/" {
				ssid, _ := activeAP.GetPropertySSID()
				strength, _ := activeAP.GetPropertyStrength()
				bssid, _ := activeAP.GetPropertyHWAddress()

				m.stateMutex.Lock()
				m.state.WiFiSSID = ssid
				m.state.WiFiBSSID = bssid
				m.state.WiFiSignal = strength
				m.stateMutex.Unlock()
			}
		}

		if ip := m.getDeviceIP(dev); ip != "" {
			m.stateMutex.Lock()
			m.state.WiFiIP = ip
			m.stateMutex.Unlock()
		}
	} else {
		m.stateMutex.Lock()
		m.state.WiFiIP = ""
		m.state.WiFiSSID = ""
		m.state.WiFiBSSID = ""
		m.state.WiFiSignal = 0
		m.stateMutex.Unlock()
	}

	return nil
}

func (m *Manager) getDeviceIP(dev gonetworkmanager.Device) string {
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

func (m *Manager) snapshotState() NetworkState {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	s := *m.state
	s.WiFiNetworks = append([]WiFiNetwork(nil), m.state.WiFiNetworks...)
	return s
}

func (m *Manager) GetState() NetworkState {
	return m.snapshotState()
}

func (m *Manager) Subscribe(id string) chan NetworkState {
	ch := make(chan NetworkState, 64)
	m.subMutex.Lock()
	m.subscribers[id] = ch
	m.subMutex.Unlock()
	return ch
}

func (m *Manager) Unsubscribe(id string) {
	m.subMutex.Lock()
	if ch, ok := m.subscribers[id]; ok {
		close(ch)
		delete(m.subscribers, id)
	}
	m.subMutex.Unlock()
}

func (m *Manager) notifier() {
	defer m.notifierWg.Done()
	const minGap = 100 * time.Millisecond
	var timer *time.Timer
	var pending bool
	for {
		select {
		case <-m.stopChan:
			return
		case <-m.dirty:
			if pending {
				continue
			}
			pending = true
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(minGap, func() {
				state := m.snapshotState()
				m.subMutex.RLock()
				for _, ch := range m.subscribers {
					select {
					case ch <- state:
					default:
					}
				}
				m.subMutex.RUnlock()
				pending = false
			})
		}
	}
}

func (m *Manager) notifySubscribers() {
	select {
	case m.dirty <- struct{}{}:
	default:
	}
}

func (m *Manager) Close() {
	close(m.stopChan)
	m.notifierWg.Wait()
	m.subMutex.Lock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = make(map[string]chan NetworkState)
	m.subMutex.Unlock()
}

func getIPv4Address(iface string) string {
	netIface, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}

	addrs, err := netIface.Addrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}

	return ""
}
