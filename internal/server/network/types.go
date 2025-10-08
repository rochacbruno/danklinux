package network

import (
	"sync"
)

type NetworkStatus string

const (
	StatusDisconnected NetworkStatus = "disconnected"
	StatusEthernet     NetworkStatus = "ethernet"
	StatusWiFi         NetworkStatus = "wifi"
)

type ConnectionPreference string

const (
	PreferenceAuto     ConnectionPreference = "auto"
	PreferenceWiFi     ConnectionPreference = "wifi"
	PreferenceEthernet ConnectionPreference = "ethernet"
)

type WiFiNetwork struct {
	SSID       string `json:"ssid"`
	BSSID      string `json:"bssid"`
	Signal     uint8  `json:"signal"`
	Secured    bool   `json:"secured"`
	Enterprise bool   `json:"enterprise"`
	Connected  bool   `json:"connected"`
	Saved      bool   `json:"saved"`
	Frequency  uint32 `json:"frequency"`
	Mode       string `json:"mode"`
	Rate       uint32 `json:"rate"`
	Channel    uint32 `json:"channel"`
}

type NetworkState struct {
	NetworkStatus     NetworkStatus        `json:"networkStatus"`
	Preference        ConnectionPreference `json:"preference"`
	EthernetIP        string               `json:"ethernetIP"`
	EthernetDevice    string               `json:"ethernetDevice"`
	EthernetConnected bool                 `json:"ethernetConnected"`
	WiFiIP            string               `json:"wifiIP"`
	WiFiDevice        string               `json:"wifiDevice"`
	WiFiConnected     bool                 `json:"wifiConnected"`
	WiFiEnabled       bool                 `json:"wifiEnabled"`
	WiFiSSID          string               `json:"wifiSSID"`
	WiFiBSSID         string               `json:"wifiBSSID"`
	WiFiSignal        uint8                `json:"wifiSignal"`
	WiFiNetworks      []WiFiNetwork        `json:"wifiNetworks"`
	IsConnecting      bool                 `json:"isConnecting"`
	ConnectingSSID    string               `json:"connectingSSID"`
	LastError         string               `json:"lastError"`
}

type ConnectionRequest struct {
	SSID     string `json:"ssid"`
	Password string `json:"password,omitempty"`
	Username string `json:"username,omitempty"`
}

type PriorityUpdate struct {
	Preference ConnectionPreference `json:"preference"`
}

type Manager struct {
	state          *NetworkState
	stateMutex     sync.RWMutex
	subscribers    map[string]chan NetworkState
	subMutex       sync.RWMutex
	stopChan       chan struct{}
	nmConn         interface{}
	ethernetDevice interface{}
	wifiDevice     interface{}
	settings       interface{}
	wifiDev        interface{}
	dirty          chan struct{}
	notifierWg     sync.WaitGroup
}

type EventType string

const (
	EventStateChanged    EventType = "state_changed"
	EventNetworksUpdated EventType = "networks_updated"
	EventConnecting      EventType = "connecting"
	EventConnected       EventType = "connected"
	EventDisconnected    EventType = "disconnected"
	EventError           EventType = "error"
)

type NetworkEvent struct {
	Type EventType    `json:"type"`
	Data NetworkState `json:"data"`
}
