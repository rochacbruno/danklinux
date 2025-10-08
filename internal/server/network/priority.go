package network

import (
	"fmt"

	"github.com/Wifx/gonetworkmanager/v2"
)

func (m *Manager) SetConnectionPreference(pref ConnectionPreference) error {
	m.stateMutex.Lock()
	m.state.Preference = pref
	m.stateMutex.Unlock()

	switch pref {
	case PreferenceWiFi:
		return m.prioritizeWiFi()
	case PreferenceEthernet:
		return m.prioritizeEthernet()
	case PreferenceAuto:
		return m.balancePriorities()
	default:
		return fmt.Errorf("invalid preference: %s", pref)
	}
}

func (m *Manager) prioritizeWiFi() error {
	if err := m.setConnectionMetrics("802-11-wireless", 50); err != nil {
		return err
	}

	if err := m.setConnectionMetrics("802-3-ethernet", 100); err != nil {
		return err
	}

	return m.reactivateConnections()
}

func (m *Manager) prioritizeEthernet() error {
	if err := m.setConnectionMetrics("802-3-ethernet", 50); err != nil {
		return err
	}

	if err := m.setConnectionMetrics("802-11-wireless", 100); err != nil {
		return err
	}

	return m.reactivateConnections()
}

func (m *Manager) balancePriorities() error {
	if err := m.setConnectionMetrics("802-3-ethernet", 50); err != nil {
		return err
	}

	if err := m.setConnectionMetrics("802-11-wireless", 50); err != nil {
		return err
	}

	return m.reactivateConnections()
}

func (m *Manager) setConnectionMetrics(connType string, metric uint32) error {
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
			if cType, ok := connMeta["type"].(string); ok && cType == connType {
				if connSettings["ipv4"] == nil {
					connSettings["ipv4"] = make(map[string]interface{})
				}
				if ipv4Map := connSettings["ipv4"]; ipv4Map != nil {
					ipv4Map["route-metric"] = int64(metric)
				}

				if connSettings["ipv6"] == nil {
					connSettings["ipv6"] = make(map[string]interface{})
				}
				if ipv6Map := connSettings["ipv6"]; ipv6Map != nil {
					ipv6Map["route-metric"] = int64(metric)
				}

				err = conn.Update(connSettings)
				if err != nil {
					continue
				}
			}
		}
	}

	return nil
}

func (m *Manager) reactivateConnections() error {
	nm := m.nmConn.(gonetworkmanager.NetworkManager)

	activeConns, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return fmt.Errorf("failed to get active connections: %w", err)
	}

	for _, activeConn := range activeConns {
		connType, err := activeConn.GetPropertyType()
		if err != nil {
			continue
		}

		if connType != "802-11-wireless" && connType != "802-3-ethernet" {
			continue
		}

		devices, err := activeConn.GetPropertyDevices()
		if err != nil || len(devices) == 0 {
			continue
		}

		connection, err := activeConn.GetPropertyConnection()
		if err != nil {
			continue
		}

		err = nm.DeactivateConnection(activeConn)
		if err != nil {
			continue
		}

		_, err = nm.ActivateConnection(connection, devices[0], nil)
		if err != nil {
			continue
		}
	}

	m.updateEthernetState()
	m.updateWiFiState()
	m.updatePrimaryConnection()
	m.notifySubscribers()

	return nil
}

func (m *Manager) GetConnectionPreference() ConnectionPreference {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.state.Preference
}
