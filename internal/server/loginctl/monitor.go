package loginctl

import (
	"github.com/godbus/dbus/v5"
)

func (m *Manager) monitorChanges() {
	err := m.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(m.state.SessionPath)),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	)
	if err != nil {
		return
	}

	err = m.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(m.state.SessionPath)),
		dbus.WithMatchInterface("org.freedesktop.login1.Session"),
		dbus.WithMatchMember("Lock"),
	)
	if err != nil {
		return
	}

	err = m.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(dbus.ObjectPath(m.state.SessionPath)),
		dbus.WithMatchInterface("org.freedesktop.login1.Session"),
		dbus.WithMatchMember("Unlock"),
	)
	if err != nil {
		return
	}

	err = m.conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	)
	if err != nil {
		return
	}

	signals := make(chan *dbus.Signal, 10)
	m.conn.Signal(signals)

	for {
		select {
		case <-m.stopChan:
			return

		case sig := <-signals:
			if sig == nil {
				continue
			}

			m.handleDBusSignal(sig)
		}
	}
}

func (m *Manager) handleDBusSignal(sig *dbus.Signal) {
	switch sig.Name {
	case "org.freedesktop.login1.Session.Lock":
		m.stateMutex.Lock()
		m.state.Locked = true
		m.state.LockedHint = true
		m.stateMutex.Unlock()
		m.notifySubscribers()

	case "org.freedesktop.login1.Session.Unlock":
		m.stateMutex.Lock()
		m.state.Locked = false
		m.state.LockedHint = false
		m.stateMutex.Unlock()
		m.notifySubscribers()

	case "org.freedesktop.login1.Manager.PrepareForSleep":
		if len(sig.Body) > 0 {
			if preparing, ok := sig.Body[0].(bool); ok {
				m.stateMutex.Lock()
				m.state.PreparingForSleep = preparing
				m.stateMutex.Unlock()
				m.notifySubscribers()
			}
		}

	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		m.handlePropertiesChanged(sig)
	}
}

func (m *Manager) handlePropertiesChanged(sig *dbus.Signal) {
	if len(sig.Body) < 2 {
		return
	}

	iface, ok := sig.Body[0].(string)
	if !ok || iface != "org.freedesktop.login1.Session" {
		return
	}

	changes, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return
	}

	var needsUpdate bool

	for key, variant := range changes {
		switch key {
		case "Active":
			if val, ok := variant.Value().(bool); ok {
				m.stateMutex.Lock()
				m.state.Active = val
				m.stateMutex.Unlock()
				needsUpdate = true
			}

		case "IdleHint":
			if val, ok := variant.Value().(bool); ok {
				m.stateMutex.Lock()
				m.state.IdleHint = val
				m.stateMutex.Unlock()
				needsUpdate = true
			}

		case "IdleSinceHint":
			if val, ok := variant.Value().(uint64); ok {
				m.stateMutex.Lock()
				m.state.IdleSinceHint = val
				m.stateMutex.Unlock()
				needsUpdate = true
			}

		case "LockedHint":
			if val, ok := variant.Value().(bool); ok {
				m.stateMutex.Lock()
				m.state.LockedHint = val
				m.state.Locked = val
				m.stateMutex.Unlock()
				needsUpdate = true
			}
		}
	}

	if needsUpdate {
		m.notifySubscribers()
	}
}
