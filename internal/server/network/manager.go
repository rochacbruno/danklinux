package network

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/godbus/dbus/v5"
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
		stateMutex:            sync.RWMutex{},
		subscribers:           make(map[string]chan NetworkState),
		subMutex:              sync.RWMutex{},
		stopChan:              make(chan struct{}),
		nmConn:                nm,
		dirty:                 make(chan struct{}, 1),
		credentialSubscribers: make(map[string]chan CredentialPrompt),
		credSubMutex:          sync.RWMutex{},
	}

	broker := NewSubscriptionBroker(m.broadcastCredentialPrompt)
	m.promptBroker = broker

	if err := m.initialize(); err != nil {
		return nil, err
	}

	if err := m.startSecretAgent(); err != nil {
		return nil, fmt.Errorf("failed to start secret agent: %w", err)
	}

	m.notifierWg.Add(1)
	go m.notifier()

	if err := m.startSignalPump(); err != nil {
		m.Close()
		return nil, err
	}

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
			if managed, _ :=  dev.GetPropertyManaged(); !managed {
				continue
			} 
			m.ethernetDevice = dev
			if err := m.updateEthernetState(); err != nil {
				continue
			}
			err := m.listEthernetConnections()
			if err != nil {
				return fmt.Errorf("failed to get wired configurations: %w", err)
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

	var ip string
	if connected {
		ip = m.getDeviceIP(dev)
	}

	m.stateMutex.Lock()
	m.state.EthernetDevice = iface
	m.state.EthernetConnected = connected
	m.state.EthernetIP = ip
	m.stateMutex.Unlock()

	return nil
}

func (m *Manager) ensureWiFiDevice() error {
	if m.wifiDev != nil {
		return nil
	}

	if m.wifiDevice == nil {
		return fmt.Errorf("no WiFi device available")
	}

	dev := m.wifiDevice.(gonetworkmanager.Device)
	wifiDev, err := gonetworkmanager.NewDeviceWireless(dev.GetPath())
	if err != nil {
		return fmt.Errorf("failed to get wireless device: %w", err)
	}
	m.wifiDev = wifiDev
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
	failed := state == gonetworkmanager.NmDeviceStateFailed
	disconnected := state == gonetworkmanager.NmDeviceStateDisconnected

	var ip, ssid, bssid string
	var signal uint8

	if connected {
		if err := m.ensureWiFiDevice(); err == nil && m.wifiDev != nil {
			w := m.wifiDev.(gonetworkmanager.DeviceWireless)
			activeAP, err := w.GetPropertyActiveAccessPoint()
			if err == nil && activeAP != nil && activeAP.GetPath() != "/" {
				ssid, _ = activeAP.GetPropertySSID()
				signal, _ = activeAP.GetPropertyStrength()
				bssid, _ = activeAP.GetPropertyHWAddress()
			}
		}

		ip = m.getDeviceIP(dev)
	}

	m.stateMutex.Lock()
	wasConnecting := m.state.IsConnecting
	connectingSSID := m.state.ConnectingSSID

	if wasConnecting && connectingSSID != "" {
		if connected && ssid == connectingSSID {
			log.Printf("[updateWiFiState] Connection successful: %s", ssid)
			m.state.IsConnecting = false
			m.state.ConnectingSSID = ""
			m.state.LastError = ""
		} else if failed || (disconnected && !connected) {
			log.Printf("[updateWiFiState] Connection failed: SSID=%s, state=%d", connectingSSID, state)
			m.state.IsConnecting = false
			m.state.ConnectingSSID = ""
			m.state.LastError = "connection-failed"

			m.failedMutex.Lock()
			m.lastFailedSSID = connectingSSID
			m.lastFailedTime = time.Now().Unix()
			m.failedMutex.Unlock()
		}
	}

	m.state.WiFiDevice = iface
	m.state.WiFiConnected = connected
	m.state.WiFiIP = ip
	m.state.WiFiSSID = ssid
	m.state.WiFiBSSID = bssid
	m.state.WiFiSignal = signal
	m.stateMutex.Unlock()

	return nil
}

func signalChangeSignificant(old, new uint8) bool {
	if old == 0 || new == 0 {
		return true
	}
	diff := int(new) - int(old)
	if diff < 0 {
		diff = -diff
	}
	return diff >= 5
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
	s.WiredConnections = append([]WiredConnection(nil), m.state.WiredConnections...)
	return s
}

func stateChangedMeaningfully(old, new *NetworkState) bool {
	if old.NetworkStatus != new.NetworkStatus {
		return true
	}
	if old.Preference != new.Preference {
		return true
	}
	if old.EthernetConnected != new.EthernetConnected {
		return true
	}
	if old.EthernetIP != new.EthernetIP {
		return true
	}
	if old.WiFiConnected != new.WiFiConnected {
		return true
	}
	if old.WiFiEnabled != new.WiFiEnabled {
		return true
	}
	if old.WiFiSSID != new.WiFiSSID {
		return true
	}
	if old.WiFiBSSID != new.WiFiBSSID {
		return true
	}
	if old.WiFiIP != new.WiFiIP {
		return true
	}
	if !signalChangeSignificant(old.WiFiSignal, new.WiFiSignal) {
		if old.WiFiSignal != new.WiFiSignal {
			return false
		}
	} else if old.WiFiSignal != new.WiFiSignal {
		return true
	}
	if old.IsConnecting != new.IsConnecting {
		return true
	}
	if old.ConnectingSSID != new.ConnectingSSID {
		return true
	}
	if old.LastError != new.LastError {
		return true
	}
	if len(old.WiFiNetworks) != len(new.WiFiNetworks) {
		return true
	}
	if len(old.WiredConnections) != len(new.WiredConnections) {
		return true
	}

	for i := range old.WiFiNetworks {
		oldNet := &old.WiFiNetworks[i]
		newNet := &new.WiFiNetworks[i]
		if oldNet.SSID != newNet.SSID {
			return true
		}
		if oldNet.Connected != newNet.Connected {
			return true
		}
		if oldNet.Saved != newNet.Saved {
			return true
		}
	}

	for i := range old.WiredConnections {
		oldNet := &old.WiredConnections[i]
		newNet := &new.WiredConnections[i]
		if oldNet.ID != newNet.ID {
			return true
		}
		if oldNet.IsActive != newNet.IsActive {
			return true
		}
	}

	return false
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

func (m *Manager) SubscribeCredentials(id string) chan CredentialPrompt {
	ch := make(chan CredentialPrompt, 16)
	m.credSubMutex.Lock()
	m.credentialSubscribers[id] = ch
	m.credSubMutex.Unlock()
	return ch
}

func (m *Manager) UnsubscribeCredentials(id string) {
	m.credSubMutex.Lock()
	if ch, ok := m.credentialSubscribers[id]; ok {
		close(ch)
		delete(m.credentialSubscribers, id)
	}
	m.credSubMutex.Unlock()
}

func (m *Manager) broadcastCredentialPrompt(prompt CredentialPrompt) {
	m.credSubMutex.RLock()
	defer m.credSubMutex.RUnlock()

	for _, ch := range m.credentialSubscribers {
		select {
		case ch <- prompt:
		default:
		}
	}
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
				m.subMutex.RLock()
				if len(m.subscribers) == 0 {
					m.subMutex.RUnlock()
					pending = false
					return
				}

				currentState := m.snapshotState()

				if m.lastNotifiedState != nil && !stateChangedMeaningfully(m.lastNotifiedState, &currentState) {
					m.subMutex.RUnlock()
					pending = false
					return
				}

				for _, ch := range m.subscribers {
					select {
					case ch <- currentState:
					default:
					}
				}
				m.subMutex.RUnlock()

				stateCopy := currentState
				m.lastNotifiedState = &stateCopy
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

func (m *Manager) startSignalPump() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return err
	}
	m.dbusConn = conn

	signals := make(chan *dbus.Signal, 256)
	m.signals = signals
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

	if m.wifiDevice != nil {
		dev := m.wifiDevice.(gonetworkmanager.Device)
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

	if m.ethernetDevice != nil {
		dev := m.ethernetDevice.(gonetworkmanager.Device)
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
			if m.wifiDevice != nil {
				dev := m.wifiDevice.(gonetworkmanager.Device)
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

	m.sigWG.Add(1)
	go func() {
		defer m.sigWG.Done()
		for {
			select {
			case <-m.stopChan:
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if sig == nil {
					continue
				}
				m.handleDBusSignal(sig)
			}
		}
	}()
	return nil
}

func (m *Manager) stopSignalPump() {
	if m.dbusConn == nil {
		return
	}

	_ = m.dbusConn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(dbusNMPath)),
		dbus.WithMatchInterface(dbusPropsInterface),
		dbus.WithMatchMember("PropertiesChanged"),
	)

	if m.wifiDevice != nil {
		dev := m.wifiDevice.(gonetworkmanager.Device)
		_ = m.dbusConn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
	}

	if m.ethernetDevice != nil {
		dev := m.ethernetDevice.(gonetworkmanager.Device)
		_ = m.dbusConn.RemoveMatchSignal(
			dbus.WithMatchObjectPath(dbus.ObjectPath(dev.GetPath())),
			dbus.WithMatchInterface(dbusPropsInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
	}

	if m.signals != nil {
		m.dbusConn.RemoveSignal(m.signals)
		close(m.signals)
	}

	m.sigWG.Wait()

	m.dbusConn.Close()
}

func (m *Manager) startSecretAgent() error {
	if m.promptBroker == nil {
		return fmt.Errorf("prompt broker not set")
	}

	agent, err := NewSecretAgent(m.promptBroker, m)
	if err != nil {
		return err
	}

	m.secretAgent = agent
	return nil
}

func (m *Manager) SetPromptBroker(broker PromptBroker) error {
	if broker == nil {
		return fmt.Errorf("broker cannot be nil")
	}

	m.promptBroker = broker

	if m.secretAgent != nil {
		m.secretAgent.Close()
		m.secretAgent = nil
	}

	return m.startSecretAgent()
}

func (m *Manager) SubmitCredentials(token string, secrets map[string]string, save bool) error {
	if m.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return m.promptBroker.Resolve(token, PromptReply{
		Secrets: secrets,
		Save:    save,
		Cancel:  false,
	})
}

func (m *Manager) CancelCredentials(token string) error {
	if m.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return m.promptBroker.Resolve(token, PromptReply{
		Cancel: true,
	})
}

func (m *Manager) GetPromptBroker() PromptBroker {
	return m.promptBroker
}

func (m *Manager) Close() {
	close(m.stopChan)
	m.notifierWg.Wait()

	m.stopSignalPump()

	if m.secretAgent != nil {
		m.secretAgent.Close()
	}

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
