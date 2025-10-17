package network

import (
	"fmt"
	"strings"
	"net"
	"strconv"
	
	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) GetWiredConfigs() []WiredConnection {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	configs := make([]WiredConnection, len(m.state.WiredConnections))
	copy(configs, m.state.WiredConnections)
	return configs
}

func (m *Manager) getActiveConnections() (map[string]bool, error) {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	activeUUIDs := make(map[string]bool)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return activeUUIDs, fmt.Errorf("failed to get active connections: %w", err)
	}

	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "802-3-ethernet" {
			continue
		}
		
		state, err := activeConn.GetPropertyState()
		if err != nil {
			continue
		}
		if state < 1 || state > 2 {
			continue
		}

		uuid, err := activeConn.GetPropertyUUID()
		if err != nil {
			continue
		}
		activeUUIDs[uuid] = true
	}
	return activeUUIDs, nil
}

func (m *Manager) listEthernetConnections() error {
	if m.ethernetDevice == nil {
		return fmt.Errorf("no ethernet device available")
	}

	s := m.settings
	if s == nil {
		s, err := gonetworkmanager.NewSettings()
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

	wiredConfigs := make([]WiredConnection, 0)
	activeUUIDs, err := m.getActiveConnections()

	if err != nil {
		return fmt.Errorf("failed to get active wired connections: %w", err)
	}

	currentUuid := ""
	for _, connection := range connections {
		path := connection.GetPath()
		settings, err := connection.GetSettings()
		if err != nil {
			log.Errorf("unable to get settings for %s: %v", path, err)
			continue
		}

		connectionSettings := settings["connection"]
		connType, _ := connectionSettings["type"].(string)
		connID, _ := connectionSettings["id"].(string)
		connUUID, _ := connectionSettings["uuid"].(string)
		
		if connType == "802-3-ethernet" {
			wiredConfigs = append(wiredConfigs, WiredConnection{
				Path: path,
				ID:   connID,
				UUID: connUUID,
				Type: connType,
				IsActive: activeUUIDs[connUUID],
			})
			if activeUUIDs[connUUID] {
				currentUuid = connUUID
			}
		}
	}

	m.stateMutex.Lock()
	m.state.EthernetConnectionUuid = currentUuid
	m.state.WiredConnections = wiredConfigs
	m.stateMutex.Unlock()

	return nil
}

func (m *Manager) activateConnection(uuid string) error {
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
    
    var targetConnection gonetworkmanager.Connection
    for _, conn := range connections {
        settings, err := conn.GetSettings()
        if err != nil {
            continue
        }
        
        if connectionSettings, ok := settings["connection"]; ok {
            if connUUID, ok := connectionSettings["uuid"].(string); ok && connUUID == uuid {
                targetConnection = conn
                break
            }
        }
    }
    
    if targetConnection == nil {
        return fmt.Errorf("connection with UUID %s not found", uuid)
    }
    
    _, err = nm.ActivateConnection(targetConnection, dev, nil)
    if err != nil {
        return fmt.Errorf("error activation connection: %w", err)
    }

	m.updateEthernetState()
	m.listEthernetConnections()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}

type WiredNetworkInfoResponse struct {
	UUID   string          `json:"uuid"`
	IFace  string          `json:"iface"`
	Driver string          `json:"driver"`
	HwAddr string          `json:"hwAddr"`
	Speed  string          `json:"speed"`
	IPv4   WiredIPConfig   `json:"IPv4s"`
	IPv6   WiredIPConfig   `json:"IPv6s"`
}

type WiredIPConfig struct {
	IPs     []string        `json:"ips"`
	Gateway string          `json:"gateway"`
	DNS     string          `json:"dns"`
}

func (m *Manager) GetWiredNetworkInfoDetailed(uuid string) (*WiredNetworkInfoResponse, error) {
	if m.ethernetDevice == nil {
		return nil, fmt.Errorf("no ethernet device available")
	}

	dev := m.ethernetDevice.(gonetworkmanager.Device)
	
	iface, _ := dev.GetPropertyInterface()
	driver, _ := dev.GetPropertyDriver()
	
	hwAddr := "Not available"
	var speed uint32 = 0
	wiredDevice, err := gonetworkmanager.NewDeviceWired(dev.GetPath())
	if err == nil {
		hwAddr, _ = wiredDevice.GetPropertyHwAddress()
		speed, _ = wiredDevice.GetPropertySpeed()
	}
	var ipv4Config WiredIPConfig
	var ipv6Config WiredIPConfig
	
	activeConn, err := dev.GetPropertyActiveConnection()
	if err == nil && activeConn != nil {
		ip4Config, err := activeConn.GetPropertyIP4Config()
		if err == nil && ip4Config != nil {
			var ips []string
			addresses, err := ip4Config.GetPropertyAddressData()
			if err == nil && len(addresses) > 0 {
				for _, addr := range addresses {
					ips = append(ips, fmt.Sprintf("%s/%s", addr.Address, strconv.Itoa(int(addr.Prefix))))
				}
			}

			gateway, _ := ip4Config.GetPropertyGateway()
			dnsAddrs := ""
			dns, err := ip4Config.GetPropertyNameserverData()
			if err == nil && len(dns) > 0 {
				for _, d := range dns {
					if len(dnsAddrs) > 0 {
						dnsAddrs = strings.Join([]string{dnsAddrs, d.Address}, "; ")
					} else {
						dnsAddrs = d.Address
					} 
				}
			}

			ipv4Config = WiredIPConfig {
				IPs: ips,
				Gateway: gateway,
				DNS: dnsAddrs,
			}
		}

		ip6Config, err := activeConn.GetPropertyIP6Config()
		if err == nil && ip6Config != nil {
			var ips []string
			addresses, err := ip6Config.GetPropertyAddressData()
			if err == nil && len(addresses) > 0 {
				for _, addr := range addresses {
					ips = append(ips, fmt.Sprintf("%s/%s", addr.Address, strconv.Itoa(int(addr.Prefix))))
				}
			}

			gateway, _ := ip6Config.GetPropertyGateway()
			dnsAddrs := ""
			dns, err := ip6Config.GetPropertyNameservers()
			if err == nil && len(dns) > 0 {
				fmt.Println("DNS IPv6:")
				for _, d := range dns {
					if len(d) == 16 {
						ip := net.IP(d)
						if len(dnsAddrs) > 0 {
							dnsAddrs = strings.Join([]string{dnsAddrs, ip.String()}, "; ")
						} else {
							dnsAddrs = ip.String()
						} 
					}
				}
			}
			
			ipv6Config = WiredIPConfig {
				IPs: ips,
				Gateway: gateway,
				DNS: dnsAddrs,
			}
		}
	} else {
		fmt.Println("no active connection on this device")
	}

	return &WiredNetworkInfoResponse{
		UUID:  uuid,
		IFace: iface,
		Driver: driver,
		HwAddr: hwAddr,
		Speed: strconv.Itoa(int(speed)),
		IPv4:  ipv4Config,
		IPv6:  ipv6Config,
	}, nil
}
