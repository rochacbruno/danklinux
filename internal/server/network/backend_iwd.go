package network

import (
	"fmt"
	"sync"
	"time"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/godbus/dbus/v5"
)

const (
	iwdBusName               = "net.connman.iwd"
	iwdObjectPath            = "/"
	iwdAdapterInterface      = "net.connman.iwd.Adapter"
	iwdDeviceInterface       = "net.connman.iwd.Device"
	iwdStationInterface      = "net.connman.iwd.Station"
	iwdNetworkInterface      = "net.connman.iwd.Network"
	iwdKnownNetworkInterface = "net.connman.iwd.KnownNetwork"
	dbusObjectManager        = "org.freedesktop.DBus.ObjectManager"
	dbusPropertiesInterface  = "org.freedesktop.DBus.Properties"
)

type connectAttempt struct {
	ssid           string
	netPath        dbus.ObjectPath
	start          time.Time
	deadline       time.Time
	sawAuthish     bool
	connectedAt    time.Time
	sawIPConfig    bool
	sawPromptRetry bool
	finalized      bool
	mu             sync.Mutex
}

type IWDBackend struct {
	conn          *dbus.Conn
	state         *BackendState
	stateMutex    sync.RWMutex
	promptBroker  PromptBroker
	onStateChange func()

	devicePath  dbus.ObjectPath
	stationPath dbus.ObjectPath
	adapterPath dbus.ObjectPath

	iwdAgent *IWDAgent

	stopChan      chan struct{}
	sigWG         sync.WaitGroup
	curAttempt    *connectAttempt
	attemptMutex  sync.RWMutex
	recentScans   map[string]time.Time
	recentScansMu sync.Mutex
}

func NewIWDBackend() (*IWDBackend, error) {
	backend := &IWDBackend{
		state: &BackendState{
			Backend:     "iwd",
			WiFiEnabled: true,
		},
		stopChan:    make(chan struct{}),
		recentScans: make(map[string]time.Time),
	}

	return backend, nil
}

func (b *IWDBackend) Initialize() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}
	b.conn = conn

	if err := b.discoverDevices(); err != nil {
		conn.Close()
		return fmt.Errorf("failed to discover iwd devices: %w", err)
	}

	if err := b.updateState(); err != nil {
		conn.Close()
		return fmt.Errorf("failed to get initial state: %w", err)
	}

	return nil
}

func (b *IWDBackend) Close() {
	close(b.stopChan)
	b.sigWG.Wait()

	if b.iwdAgent != nil {
		b.iwdAgent.Close()
	}

	if b.conn != nil {
		b.conn.Close()
	}
}

func (b *IWDBackend) discoverDevices() error {
	obj := b.conn.Object(iwdBusName, iwdObjectPath)

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := obj.Call(dbusObjectManager+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return fmt.Errorf("failed to get managed objects: %w", err)
	}

	for path, interfaces := range objects {
		if _, hasStation := interfaces[iwdStationInterface]; hasStation {
			b.stationPath = path
		}
		if _, hasDevice := interfaces[iwdDeviceInterface]; hasDevice {
			b.devicePath = path

			if devProps, ok := interfaces[iwdDeviceInterface]; ok {
				if nameVar, ok := devProps["Name"]; ok {
					if name, ok := nameVar.Value().(string); ok {
						b.stateMutex.Lock()
						b.state.WiFiDevice = name
						b.stateMutex.Unlock()
					}
				}
			}
		}
		if _, hasAdapter := interfaces[iwdAdapterInterface]; hasAdapter {
			b.adapterPath = path
		}
	}

	if b.stationPath == "" || b.devicePath == "" {
		return fmt.Errorf("no WiFi device found")
	}

	return nil
}

func (b *IWDBackend) updateState() error {
	if b.devicePath == "" {
		return nil
	}

	obj := b.conn.Object(iwdBusName, b.devicePath)

	poweredVar, err := obj.GetProperty(iwdDeviceInterface + ".Powered")
	if err == nil {
		if powered, ok := poweredVar.Value().(bool); ok {
			b.stateMutex.Lock()
			b.state.WiFiEnabled = powered
			b.stateMutex.Unlock()
		}
	}

	if b.stationPath == "" {
		return nil
	}

	stationObj := b.conn.Object(iwdBusName, b.stationPath)

	stateVar, err := stationObj.GetProperty(iwdStationInterface + ".State")
	if err == nil {
		if state, ok := stateVar.Value().(string); ok {
			b.stateMutex.Lock()
			b.state.WiFiConnected = (state == "connected")
			if state == "connected" {
				b.state.NetworkStatus = StatusWiFi
			} else {
				b.state.NetworkStatus = StatusDisconnected
			}
			b.stateMutex.Unlock()
		}
	}

	connNetVar, err := stationObj.GetProperty(iwdStationInterface + ".ConnectedNetwork")
	if err == nil && connNetVar.Value() != nil {
		if netPath, ok := connNetVar.Value().(dbus.ObjectPath); ok && netPath != "/" {
			netObj := b.conn.Object(iwdBusName, netPath)

			nameVar, err := netObj.GetProperty(iwdNetworkInterface + ".Name")
			if err == nil {
				if name, ok := nameVar.Value().(string); ok {
					b.stateMutex.Lock()
					b.state.WiFiSSID = name
					b.stateMutex.Unlock()
				}
			}

			var orderedNetworks [][]dbus.Variant
			err = stationObj.Call(iwdStationInterface+".GetOrderedNetworks", 0).Store(&orderedNetworks)
			if err == nil {
				for _, netData := range orderedNetworks {
					if len(netData) < 2 {
						continue
					}
					currentNetPath, ok := netData[0].Value().(dbus.ObjectPath)
					if !ok || currentNetPath != netPath {
						continue
					}
					signalStrength, ok := netData[1].Value().(int16)
					if !ok {
						continue
					}
					signalDbm := signalStrength / 100
					signal := uint8(signalDbm + 100)
					if signalDbm > 0 {
						signal = 100
					} else if signalDbm < -100 {
						signal = 0
					}
					b.stateMutex.Lock()
					b.state.WiFiSignal = signal
					b.stateMutex.Unlock()
					break
				}
			}
		}
	}

	networks, err := b.updateWiFiNetworks()
	if err == nil {
		b.stateMutex.Lock()
		b.state.WiFiNetworks = networks
		b.stateMutex.Unlock()
	}

	return nil
}

func (b *IWDBackend) GetWiFiEnabled() (bool, error) {
	b.stateMutex.RLock()
	defer b.stateMutex.RUnlock()
	return b.state.WiFiEnabled, nil
}

func (b *IWDBackend) SetWiFiEnabled(enabled bool) error {
	if b.devicePath == "" {
		return fmt.Errorf("no WiFi device available")
	}

	obj := b.conn.Object(iwdBusName, b.devicePath)
	call := obj.Call(dbusPropertiesInterface+".Set", 0, iwdDeviceInterface, "Powered", dbus.MakeVariant(enabled))
	if call.Err != nil {
		return fmt.Errorf("failed to set WiFi enabled: %w", call.Err)
	}

	b.stateMutex.Lock()
	b.state.WiFiEnabled = enabled
	b.stateMutex.Unlock()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *IWDBackend) ScanWiFi() error {
	if b.stationPath == "" {
		return fmt.Errorf("no WiFi device available")
	}

	obj := b.conn.Object(iwdBusName, b.stationPath)

	scanningVar, err := obj.GetProperty(iwdStationInterface + ".Scanning")
	if err != nil {
		return fmt.Errorf("failed to check scanning state: %w", err)
	}

	if scanning, ok := scanningVar.Value().(bool); ok && scanning {
		return fmt.Errorf("scan already in progress")
	}

	call := obj.Call(iwdStationInterface+".Scan", 0)
	if call.Err != nil {
		return fmt.Errorf("scan request failed: %w", call.Err)
	}

	return nil
}

func (b *IWDBackend) UpdateWiFiNetworks() ([]WiFiNetwork, error) {
	return b.updateWiFiNetworks()
}

func (b *IWDBackend) updateWiFiNetworks() ([]WiFiNetwork, error) {
	if b.stationPath == "" {
		return nil, fmt.Errorf("no WiFi device available")
	}

	obj := b.conn.Object(iwdBusName, b.stationPath)

	var orderedNetworks [][]dbus.Variant
	err := obj.Call(iwdStationInterface+".GetOrderedNetworks", 0).Store(&orderedNetworks)
	if err != nil {
		return nil, fmt.Errorf("failed to get networks: %w", err)
	}

	knownNetworks, err := b.getKnownNetworks()
	if err != nil {
		knownNetworks = make(map[string]bool)
	}

	b.stateMutex.RLock()
	currentSSID := b.state.WiFiSSID
	wifiConnected := b.state.WiFiConnected
	b.stateMutex.RUnlock()

	networks := make([]WiFiNetwork, 0, len(orderedNetworks))
	for _, netData := range orderedNetworks {
		if len(netData) < 2 {
			continue
		}

		networkPath, ok := netData[0].Value().(dbus.ObjectPath)
		if !ok {
			continue
		}

		signalStrength, ok := netData[1].Value().(int16)
		if !ok {
			continue
		}

		netObj := b.conn.Object(iwdBusName, networkPath)

		nameVar, err := netObj.GetProperty(iwdNetworkInterface + ".Name")
		if err != nil {
			continue
		}
		name, ok := nameVar.Value().(string)
		if !ok {
			continue
		}

		typeVar, err := netObj.GetProperty(iwdNetworkInterface + ".Type")
		if err != nil {
			continue
		}
		netType, ok := typeVar.Value().(string)
		if !ok {
			continue
		}

		signalDbm := signalStrength / 100
		signal := uint8(signalDbm + 100)
		if signalDbm > 0 {
			signal = 100
		} else if signalDbm < -100 {
			signal = 0
		}

		secured := netType != "open"

		network := WiFiNetwork{
			SSID:       name,
			Signal:     signal,
			Secured:    secured,
			Connected:  wifiConnected && name == currentSSID,
			Saved:      knownNetworks[name],
			Enterprise: netType == "8021x",
		}

		networks = append(networks, network)
	}

	sortWiFiNetworks(networks, currentSSID)

	b.stateMutex.Lock()
	b.state.WiFiNetworks = networks
	b.stateMutex.Unlock()

	// Update recent scans map for classification
	now := time.Now()
	b.recentScansMu.Lock()
	for _, net := range networks {
		b.recentScans[net.SSID] = now
	}
	b.recentScansMu.Unlock()

	return networks, nil
}

func (b *IWDBackend) getKnownNetworks() (map[string]bool, error) {
	obj := b.conn.Object(iwdBusName, iwdObjectPath)

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := obj.Call(dbusObjectManager+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return nil, err
	}

	known := make(map[string]bool)
	for _, interfaces := range objects {
		if knownProps, ok := interfaces[iwdKnownNetworkInterface]; ok {
			if nameVar, ok := knownProps["Name"]; ok {
				if name, ok := nameVar.Value().(string); ok {
					known[name] = true
				}
			}
		}
	}

	return known, nil
}

func (b *IWDBackend) GetWiFiNetworkDetails(ssid string) (*NetworkInfoResponse, error) {
	b.stateMutex.RLock()
	networks := b.state.WiFiNetworks
	b.stateMutex.RUnlock()

	var found *WiFiNetwork
	for i := range networks {
		if networks[i].SSID == ssid {
			found = &networks[i]
			break
		}
	}

	if found == nil {
		return nil, fmt.Errorf("network not found: %s", ssid)
	}

	return &NetworkInfoResponse{
		SSID:  ssid,
		Bands: []WiFiNetwork{*found},
	}, nil
}

func (b *IWDBackend) setConnectError(code string) {
	b.stateMutex.Lock()
	b.state.IsConnecting = false
	b.state.ConnectingSSID = ""
	b.state.LastError = code
	b.stateMutex.Unlock()
}

func (b *IWDBackend) seenInRecentScan(ssid string) bool {
	b.recentScansMu.Lock()
	defer b.recentScansMu.Unlock()
	lastSeen, ok := b.recentScans[ssid]
	return ok && time.Since(lastSeen) < 30*time.Second
}

func (b *IWDBackend) classifyAttempt(att *connectAttempt) string {
	att.mu.Lock()
	defer att.mu.Unlock()

	if att.sawPromptRetry {
		return errdefs.ErrBadCredentials
	}

	// Short-lived connection without IP config indicates auth failure
	if !att.connectedAt.IsZero() && !att.sawIPConfig {
		connDuration := time.Since(att.connectedAt)
		if connDuration > 500*time.Millisecond && connDuration < 3*time.Second {
			return errdefs.ErrBadCredentials
		}
	}

	// Authentication succeeded but no IP obtained
	if (att.sawAuthish || !att.connectedAt.IsZero()) && !att.sawIPConfig {
		if time.Since(att.start) > 12*time.Second {
			return errdefs.ErrDhcpTimeout
		}
	}

	// No authentication progress at all
	if !att.sawAuthish && att.connectedAt.IsZero() {
		if !b.seenInRecentScan(att.ssid) {
			return errdefs.ErrNoSuchSSID
		}
		return errdefs.ErrAssocTimeout
	}

	return errdefs.ErrAssocTimeout
}

func (b *IWDBackend) finalizeAttempt(att *connectAttempt, code string) {
	att.mu.Lock()
	if att.finalized {
		att.mu.Unlock()
		return
	}
	att.finalized = true
	att.mu.Unlock()

	b.stateMutex.Lock()
	b.state.IsConnecting = false
	b.state.ConnectingSSID = ""
	b.state.LastError = code
	b.stateMutex.Unlock()

	b.updateState()

	if b.onStateChange != nil {
		b.onStateChange()
	}
}

func (b *IWDBackend) startAttemptWatchdog(att *connectAttempt) {
	b.sigWG.Add(1)
	go func() {
		defer b.sigWG.Done()

		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				att.mu.Lock()
				finalized := att.finalized
				att.mu.Unlock()

				if finalized || time.Now().After(att.deadline) {
					if !finalized {
						b.finalizeAttempt(att, b.classifyAttempt(att))
					}
					return
				}

				station := b.conn.Object(iwdBusName, b.stationPath)
				stVar, err := station.GetProperty(iwdStationInterface + ".State")
				if err != nil {
					continue
				}
				state, _ := stVar.Value().(string)

				cnVar, err := station.GetProperty(iwdStationInterface + ".ConnectedNetwork")
				if err != nil {
					continue
				}
				var connPath dbus.ObjectPath
				if cnVar.Value() != nil {
					connPath, _ = cnVar.Value().(dbus.ObjectPath)
				}

				att.mu.Lock()
				if connPath == att.netPath && state == "connected" && att.connectedAt.IsZero() {
					att.connectedAt = time.Now()
				}
				if state == "configuring" {
					att.sawIPConfig = true
				}
				att.mu.Unlock()

			case <-b.stopChan:
				return
			}
		}
	}()
}

func (b *IWDBackend) mapIwdDBusError(name string) string {
	switch name {
	case "net.connman.iwd.Error.AlreadyConnected":
		return errdefs.ErrAlreadyConnected
	case "net.connman.iwd.Error.AuthenticationFailed",
		"net.connman.iwd.Error.InvalidKey",
		"net.connman.iwd.Error.IncorrectPassphrase":
		return errdefs.ErrBadCredentials
	case "net.connman.iwd.Error.NotFound":
		return errdefs.ErrNoSuchSSID
	case "net.connman.iwd.Error.NotSupported":
		return errdefs.ErrConnectionFailed
	case "net.connman.iwd.Agent.Error.Canceled":
		return errdefs.ErrUserCanceled
	default:
		return errdefs.ErrConnectionFailed
	}
}

func (b *IWDBackend) classifyIwdImmediateError(name string) {
	b.setConnectError(b.mapIwdDBusError(name))
}

func (b *IWDBackend) ConnectWiFi(req ConnectionRequest) error {
	if b.stationPath == "" {
		b.setConnectError(errdefs.ErrWifiDisabled)
		if b.onStateChange != nil {
			b.onStateChange()
		}
		return fmt.Errorf("no WiFi device available")
	}

	networkPath, err := b.findNetworkPath(req.SSID)
	if err != nil {
		b.setConnectError(errdefs.ErrNoSuchSSID)
		if b.onStateChange != nil {
			b.onStateChange()
		}
		return fmt.Errorf("network not found: %w", err)
	}

	// Create new attempt
	att := &connectAttempt{
		ssid:     req.SSID,
		netPath:  networkPath,
		start:    time.Now(),
		deadline: time.Now().Add(15 * time.Second),
	}

	b.attemptMutex.Lock()
	b.curAttempt = att
	b.attemptMutex.Unlock()

	b.stateMutex.Lock()
	b.state.IsConnecting = true
	b.state.ConnectingSSID = req.SSID
	b.state.LastError = ""
	b.stateMutex.Unlock()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	netObj := b.conn.Object(iwdBusName, networkPath)
	go func() {
		call := netObj.Call(iwdNetworkInterface+".Connect", 0)
		if call.Err != nil {
			var code string
			if dbusErr, ok := call.Err.(dbus.Error); ok {
				code = b.mapIwdDBusError(dbusErr.Name)
			} else if dbusErrPtr, ok := call.Err.(*dbus.Error); ok {
				code = b.mapIwdDBusError(dbusErrPtr.Name)
			} else {
				code = errdefs.ErrConnectionFailed
			}

			// If we saw a re-prompt, it means bad credentials regardless of the actual error
			att.mu.Lock()
			if att.sawPromptRetry {
				code = errdefs.ErrBadCredentials
			}
			att.mu.Unlock()

			b.finalizeAttempt(att, code)
			return
		}

		b.startAttemptWatchdog(att)
	}()

	return nil
}

func (b *IWDBackend) watchIwdConnect(ssid string) {
	deadline := time.Now().Add(15 * time.Second)
	seen := make(map[string]bool)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-b.stopChan:
			return
		case <-ticker.C:
			stationObj := b.conn.Object(iwdBusName, b.stationPath)
			stateVar, err := stationObj.GetProperty(iwdStationInterface + ".State")
			if err != nil {
				continue
			}
			state, ok := stateVar.Value().(string)
			if !ok {
				continue
			}
			seen[state] = true

			if state == "connected" {
				b.stateMutex.Lock()
				b.state.IsConnecting = false
				b.state.ConnectingSSID = ""
				b.state.LastError = ""
				b.stateMutex.Unlock()
				if b.onStateChange != nil {
					b.onStateChange()
				}
				return
			}

			if state == "disconnected" && (seen["authenticating"] || seen["associating"]) {
				if !seen["ip-config"] && !seen["connected"] {
					b.setConnectError(errdefs.ErrBadCredentials)
					if b.onStateChange != nil {
						b.onStateChange()
					}
					return
				}
			}

			if (seen["authenticating"] || seen["associated"] || seen["roaming"]) && time.Now().After(deadline.Add(-3*time.Second)) {
				if !seen["ip-config"] && !seen["connected"] {
					b.setConnectError(errdefs.ErrDhcpTimeout)
					if b.onStateChange != nil {
						b.onStateChange()
					}
					return
				}
			}
		}
	}

	b.stateMutex.RLock()
	stillConnecting := b.state.ConnectingSSID == ssid
	currentError := b.state.LastError
	b.stateMutex.RUnlock()

	if currentError == "" && stillConnecting {
		if seen["associating"] || seen["authenticating"] {
			b.setConnectError(errdefs.ErrAssocTimeout)
		} else {
			b.setConnectError(errdefs.ErrNoSuchSSID)
		}
		if b.onStateChange != nil {
			b.onStateChange()
		}
	}
}

func (b *IWDBackend) findNetworkPath(ssid string) (dbus.ObjectPath, error) {
	obj := b.conn.Object(iwdBusName, iwdObjectPath)

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := obj.Call(dbusObjectManager+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return "", err
	}

	for path, interfaces := range objects {
		if netProps, ok := interfaces[iwdNetworkInterface]; ok {
			if nameVar, ok := netProps["Name"]; ok {
				if name, ok := nameVar.Value().(string); ok && name == ssid {
					return path, nil
				}
			}
		}
	}

	return "", fmt.Errorf("network not found")
}

func (b *IWDBackend) DisconnectWiFi() error {
	if b.stationPath == "" {
		return fmt.Errorf("no WiFi device available")
	}

	obj := b.conn.Object(iwdBusName, b.stationPath)
	call := obj.Call(iwdStationInterface+".Disconnect", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to disconnect: %w", call.Err)
	}

	b.updateState()

	if b.onStateChange != nil {
		b.onStateChange()
	}

	return nil
}

func (b *IWDBackend) ForgetWiFiNetwork(ssid string) error {
	obj := b.conn.Object(iwdBusName, iwdObjectPath)

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := obj.Call(dbusObjectManager+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return err
	}

	for path, interfaces := range objects {
		if knownProps, ok := interfaces[iwdKnownNetworkInterface]; ok {
			if nameVar, ok := knownProps["Name"]; ok {
				if name, ok := nameVar.Value().(string); ok && name == ssid {
					knownObj := b.conn.Object(iwdBusName, path)
					call := knownObj.Call(iwdKnownNetworkInterface+".Forget", 0)
					if call.Err != nil {
						return fmt.Errorf("failed to forget network: %w", call.Err)
					}

					if b.onStateChange != nil {
						b.onStateChange()
					}

					return nil
				}
			}
		}
	}

	return fmt.Errorf("network not found")
}

func (b *IWDBackend) GetWiredConnections() ([]WiredConnection, error) {
	return nil, fmt.Errorf("wired connections not supported by iwd")
}

func (b *IWDBackend) GetWiredNetworkDetails(uuid string) (*WiredNetworkInfoResponse, error) {
	return nil, fmt.Errorf("wired connections not supported by iwd")
}

func (b *IWDBackend) ConnectEthernet() error {
	return fmt.Errorf("wired connections not supported by iwd")
}

func (b *IWDBackend) DisconnectEthernet() error {
	return fmt.Errorf("wired connections not supported by iwd")
}

func (b *IWDBackend) ActivateWiredConnection(uuid string) error {
	return fmt.Errorf("wired connections not supported by iwd")
}

func (b *IWDBackend) GetCurrentState() (*BackendState, error) {
	state := *b.state
	state.WiFiNetworks = append([]WiFiNetwork(nil), b.state.WiFiNetworks...)
	state.WiredConnections = append([]WiredConnection(nil), b.state.WiredConnections...)

	return &state, nil
}

func (b *IWDBackend) OnUserCanceledPrompt() {
	b.setConnectError(errdefs.ErrUserCanceled)
	if b.onStateChange != nil {
		b.onStateChange()
	}
}

func (b *IWDBackend) OnPromptRetry(ssid string) {
	b.attemptMutex.RLock()
	att := b.curAttempt
	b.attemptMutex.RUnlock()

	if att != nil && att.ssid == ssid {
		att.mu.Lock()
		att.sawPromptRetry = true
		att.mu.Unlock()
	}
}

func (b *IWDBackend) StartMonitoring(onStateChange func()) error {
	b.onStateChange = onStateChange

	if b.promptBroker != nil {
		agent, err := NewIWDAgent(b.promptBroker)
		if err != nil {
			return fmt.Errorf("failed to start IWD agent: %w", err)
		}
		agent.onUserCanceled = b.OnUserCanceledPrompt
		agent.onPromptRetry = b.OnPromptRetry
		b.iwdAgent = agent
	}

	sigChan := make(chan *dbus.Signal, 100)
	b.conn.Signal(sigChan)

	if b.devicePath != "" {
		err := b.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(b.devicePath),
			dbus.WithMatchInterface(dbusPropertiesInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		if err != nil {
			return fmt.Errorf("failed to add device signal match: %w", err)
		}
	}

	if b.stationPath != "" {
		err := b.conn.AddMatchSignal(
			dbus.WithMatchObjectPath(b.stationPath),
			dbus.WithMatchInterface(dbusPropertiesInterface),
			dbus.WithMatchMember("PropertiesChanged"),
		)
		if err != nil {
			return fmt.Errorf("failed to add station signal match: %w", err)
		}
	}

	b.sigWG.Add(1)
	go b.signalHandler(sigChan)

	return nil
}

func (b *IWDBackend) signalHandler(sigChan chan *dbus.Signal) {
	defer b.sigWG.Done()

	for {
		select {
		case <-b.stopChan:
			b.conn.RemoveSignal(sigChan)
			close(sigChan)
			return

		case sig := <-sigChan:
			if sig == nil {
				return
			}

			if sig.Name != dbusPropertiesInterface+".PropertiesChanged" {
				continue
			}

			if len(sig.Body) < 2 {
				continue
			}

			iface, ok := sig.Body[0].(string)
			if !ok {
				continue
			}

			changed, ok := sig.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}

			stateChanged := false

			switch iface {
			case iwdDeviceInterface:
				if sig.Path == b.devicePath {
					if poweredVar, ok := changed["Powered"]; ok {
						if powered, ok := poweredVar.Value().(bool); ok {
							b.stateMutex.Lock()
							if b.state.WiFiEnabled != powered {
								b.state.WiFiEnabled = powered
								stateChanged = true
							}
							b.stateMutex.Unlock()
						}
					}
				}

			case iwdStationInterface:
				if sig.Path == b.stationPath {
					if scanningVar, ok := changed["Scanning"]; ok {
						if scanning, ok := scanningVar.Value().(bool); ok && !scanning {
							networks, err := b.updateWiFiNetworks()
							if err == nil {
								b.stateMutex.Lock()
								b.state.WiFiNetworks = networks
								b.stateMutex.Unlock()
								stateChanged = true
							}

							b.stateMutex.RLock()
							wifiConnected := b.state.WiFiConnected
							b.stateMutex.RUnlock()

							if wifiConnected {
								stationObj := b.conn.Object(iwdBusName, b.stationPath)
								connNetVar, err := stationObj.GetProperty(iwdStationInterface + ".ConnectedNetwork")
								if err == nil && connNetVar.Value() != nil {
									if netPath, ok := connNetVar.Value().(dbus.ObjectPath); ok && netPath != "/" {
										var orderedNetworks [][]dbus.Variant
										err = stationObj.Call(iwdStationInterface+".GetOrderedNetworks", 0).Store(&orderedNetworks)
										if err == nil {
											for _, netData := range orderedNetworks {
												if len(netData) < 2 {
													continue
												}
												currentNetPath, ok := netData[0].Value().(dbus.ObjectPath)
												if !ok || currentNetPath != netPath {
													continue
												}
												signalStrength, ok := netData[1].Value().(int16)
												if !ok {
													continue
												}
												signalDbm := signalStrength / 100
												signal := uint8(signalDbm + 100)
												if signalDbm > 0 {
													signal = 100
												} else if signalDbm < -100 {
													signal = 0
												}
												b.stateMutex.Lock()
												if b.state.WiFiSignal != signal {
													b.state.WiFiSignal = signal
													stateChanged = true
												}
												b.stateMutex.Unlock()
												break
											}
										}
									}
								}
							}
						}
					}

					if stateVar, ok := changed["State"]; ok {
						if state, ok := stateVar.Value().(string); ok {
							b.attemptMutex.RLock()
							att := b.curAttempt
							b.attemptMutex.RUnlock()

							var connPath dbus.ObjectPath
							if v, ok := changed["ConnectedNetwork"]; ok {
								if v.Value() != nil {
									if p, ok := v.Value().(dbus.ObjectPath); ok {
										connPath = p
									}
								}
							}
							if connPath == "" {
								station := b.conn.Object(iwdBusName, b.stationPath)
								if cnVar, err := station.GetProperty(iwdStationInterface + ".ConnectedNetwork"); err == nil && cnVar.Value() != nil {
									_ = cnVar.Store(&connPath)
								}
							}

							b.stateMutex.RLock()
							prevConnected := b.state.WiFiConnected
							prevSSID := b.state.WiFiSSID
							b.stateMutex.RUnlock()

							targetPath := dbus.ObjectPath("")
							if att != nil {
								targetPath = att.netPath
							}

							isTarget := att != nil && targetPath != "" && connPath == targetPath

							if att != nil {
								switch state {
								case "authenticating", "associating", "associated", "roaming":
									att.mu.Lock()
									att.sawAuthish = true
									att.mu.Unlock()
								}
							}

							if att != nil && state == "connected" && isTarget {
								att.mu.Lock()
								if att.connectedAt.IsZero() {
									att.connectedAt = time.Now()
								}
								att.mu.Unlock()
							}

							if att != nil && state == "configuring" {
								att.mu.Lock()
								att.sawIPConfig = true
								att.mu.Unlock()
							}

							switch state {
							case "connected":
								b.stateMutex.Lock()
								b.state.WiFiConnected = true
								b.state.NetworkStatus = StatusWiFi
								b.state.IsConnecting = false
								b.state.ConnectingSSID = ""
								b.state.LastError = ""
								b.stateMutex.Unlock()

								if connPath != "" && connPath != "/" {
									netObj := b.conn.Object(iwdBusName, connPath)
									if nameVar, err := netObj.GetProperty(iwdNetworkInterface + ".Name"); err == nil {
										if name, ok := nameVar.Value().(string); ok {
											b.stateMutex.Lock()
											b.state.WiFiSSID = name
											b.stateMutex.Unlock()
										}
									}
								}

								stateChanged = true

								// Wait for connection stability before finalizing success
								if att != nil && isTarget {
									go func(attLocal *connectAttempt, tgt dbus.ObjectPath) {
										time.Sleep(3 * time.Second)
										station := b.conn.Object(iwdBusName, b.stationPath)
										var nowState string
										if stVar, err := station.GetProperty(iwdStationInterface + ".State"); err == nil {
											_ = stVar.Store(&nowState)
										}
										var nowConn dbus.ObjectPath
										if cnVar, err := station.GetProperty(iwdStationInterface + ".ConnectedNetwork"); err == nil && cnVar.Value() != nil {
											_ = cnVar.Store(&nowConn)
										}

										if nowState == "connected" && nowConn == tgt {
											b.finalizeAttempt(attLocal, "")
											b.attemptMutex.Lock()
											if b.curAttempt == attLocal {
												b.curAttempt = nil
											}
											b.attemptMutex.Unlock()
										}
									}(att, targetPath)
								}

							case "disconnecting", "disconnected":
								if att != nil {
									wasConnectedToTarget := prevConnected && prevSSID == att.ssid
									if wasConnectedToTarget || isTarget {
										code := b.classifyAttempt(att)
										b.finalizeAttempt(att, code)
										b.attemptMutex.Lock()
										if b.curAttempt == att {
											b.curAttempt = nil
										}
										b.attemptMutex.Unlock()
									}
								}

								b.stateMutex.Lock()
								b.state.WiFiConnected = false
								if state == "disconnected" {
									b.state.NetworkStatus = StatusDisconnected
								}
								b.stateMutex.Unlock()
								stateChanged = true
							}
						}
					}

					if connNetVar, ok := changed["ConnectedNetwork"]; ok {
						if netPath, ok := connNetVar.Value().(dbus.ObjectPath); ok && netPath != "/" {
							netObj := b.conn.Object(iwdBusName, netPath)
							nameVar, err := netObj.GetProperty(iwdNetworkInterface + ".Name")
							if err == nil {
								if name, ok := nameVar.Value().(string); ok {
									b.stateMutex.Lock()
									if b.state.WiFiSSID != name {
										b.state.WiFiSSID = name
										stateChanged = true
									}
									b.stateMutex.Unlock()
								}
							}

							stationObj := b.conn.Object(iwdBusName, b.stationPath)
							var orderedNetworks [][]dbus.Variant
							err = stationObj.Call(iwdStationInterface+".GetOrderedNetworks", 0).Store(&orderedNetworks)
							if err == nil {
								for _, netData := range orderedNetworks {
									if len(netData) < 2 {
										continue
									}
									currentNetPath, ok := netData[0].Value().(dbus.ObjectPath)
									if !ok || currentNetPath != netPath {
										continue
									}
									signalStrength, ok := netData[1].Value().(int16)
									if !ok {
										continue
									}
									signalDbm := signalStrength / 100
									signal := uint8(signalDbm + 100)
									if signalDbm > 0 {
										signal = 100
									} else if signalDbm < -100 {
										signal = 0
									}
									b.stateMutex.Lock()
									if b.state.WiFiSignal != signal {
										b.state.WiFiSignal = signal
										stateChanged = true
									}
									b.stateMutex.Unlock()
									break
								}
							}
						} else {
							b.stateMutex.Lock()
							if b.state.WiFiSSID != "" {
								b.state.WiFiSSID = ""
								b.state.WiFiSignal = 0
								stateChanged = true
							}
							b.stateMutex.Unlock()
						}
					}
				}
			}

			if stateChanged && b.onStateChange != nil {
				b.onStateChange()
			}
		}
	}
}

func (b *IWDBackend) StopMonitoring() {
	select {
	case <-b.stopChan:
		return
	default:
		close(b.stopChan)
	}
	b.sigWG.Wait()
}

func (b *IWDBackend) GetPromptBroker() PromptBroker {
	return b.promptBroker
}

func (b *IWDBackend) SetPromptBroker(broker PromptBroker) error {
	if broker == nil {
		return fmt.Errorf("broker cannot be nil")
	}

	b.promptBroker = broker
	return nil
}

func (b *IWDBackend) SubmitCredentials(token string, secrets map[string]string, save bool) error {
	if b.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return b.promptBroker.Resolve(token, PromptReply{
		Secrets: secrets,
		Save:    save,
		Cancel:  false,
	})
}

func (b *IWDBackend) CancelCredentials(token string) error {
	if b.promptBroker == nil {
		return fmt.Errorf("prompt broker not initialized")
	}

	return b.promptBroker.Resolve(token, PromptReply{
		Cancel: true,
	})
}
