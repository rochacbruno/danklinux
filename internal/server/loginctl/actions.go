package loginctl

import (
	"fmt"
)

func (m *Manager) Lock() error {
	err := m.sessionObj.Call("org.freedesktop.login1.Session.Lock", 0).Err
	if err != nil {
		return fmt.Errorf("failed to lock session: %w", err)
	}
	return nil
}

func (m *Manager) Unlock() error {
	err := m.sessionObj.Call("org.freedesktop.login1.Session.Unlock", 0).Err
	if err != nil {
		return fmt.Errorf("failed to unlock session: %w", err)
	}
	return nil
}

func (m *Manager) Activate() error {
	err := m.sessionObj.Call("org.freedesktop.login1.Session.Activate", 0).Err
	if err != nil {
		return fmt.Errorf("failed to activate session: %w", err)
	}
	return nil
}

func (m *Manager) SetIdleHint(idle bool) error {
	err := m.sessionObj.Call("org.freedesktop.login1.Session.SetIdleHint", 0, idle).Err
	if err != nil {
		return fmt.Errorf("failed to set idle hint: %w", err)
	}
	return nil
}

func (m *Manager) Terminate() error {
	err := m.sessionObj.Call("org.freedesktop.login1.Session.Terminate", 0).Err
	if err != nil {
		return fmt.Errorf("failed to terminate session: %w", err)
	}
	return nil
}

func (m *Manager) SetLockBeforeSuspend(enabled bool) {
	m.lockBeforeSuspend.Store(enabled)
}
