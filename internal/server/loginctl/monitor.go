package loginctl

import (
	"time"

	"github.com/godbus/dbus/v5"
)

func (m *Manager) handleDBusSignal(sig *dbus.Signal) {
	switch sig.Name {
	case dbusSessionInterface + ".Lock":
		m.stateMutex.Lock()
		m.state.Locked = true
		m.state.LockedHint = true
		m.stateMutex.Unlock()
		m.notifySubscribers()

		// Start a 1-second timer to release the inhibitor
		// This allows lockerReady to be called, or releases it automatically for custom lock screens
		m.lockTimerMu.Lock()
		if m.lockTimer != nil {
			m.lockTimer.Stop()
		}
		m.lockTimer = time.AfterFunc(1*time.Second, func() {
			m.releaseSleepInhibitor()
		})
		m.lockTimerMu.Unlock()

	case dbusSessionInterface + ".Unlock":
		m.stateMutex.Lock()
		m.state.Locked = false
		m.state.LockedHint = false
		m.stateMutex.Unlock()
		m.notifySubscribers()

		// Cancel the lock timer if it's still running
		m.lockTimerMu.Lock()
		if m.lockTimer != nil {
			m.lockTimer.Stop()
			m.lockTimer = nil
		}
		m.lockTimerMu.Unlock()

		// Re-acquire the sleep inhibitor
		_ = m.acquireSleepInhibitor()

	case dbusManagerInterface + ".PrepareForSleep":
		if len(sig.Body) == 0 {
			return
		}
		preparing, _ := sig.Body[0].(bool)

		if preparing {
			m.inSleepCycle.Store(true)

			if m.lockBeforeSuspend.Load() {
				_ = m.Lock()
			}

			readyCh := m.newLockerReadyCh()
			go func() {
				select {
				case <-readyCh:
				}
				m.releaseSleepInhibitor()
			}()
		} else {
			m.inSleepCycle.Store(false)
			_ = m.acquireSleepInhibitor()
		}

		m.stateMutex.Lock()
		m.state.PreparingForSleep = preparing
		m.stateMutex.Unlock()
		m.notifySubscribers()

	case dbusPropsInterface + ".PropertiesChanged":
		m.handlePropertiesChanged(sig)

	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) == 3 {
			name, _ := sig.Body[0].(string)
			oldOwner, _ := sig.Body[1].(string)
			newOwner, _ := sig.Body[2].(string)
			if name == dbusDest && oldOwner != "" && newOwner != "" {
				_ = m.updateSessionState()
				if !m.inSleepCycle.Load() {
					_ = m.acquireSleepInhibitor()
				}
				m.notifySubscribers()
			}
		}
	}
}

func (m *Manager) handlePropertiesChanged(sig *dbus.Signal) {
	if len(sig.Body) < 2 {
		return
	}

	iface, ok := sig.Body[0].(string)
	if !ok || iface != dbusSessionInterface {
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
