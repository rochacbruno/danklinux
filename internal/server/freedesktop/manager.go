package freedesktop

import (
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

func NewManager() (*Manager, error) {
	systemConn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	sessionConn, err := dbus.ConnectSessionBus()
	if err != nil {
		sessionConn = nil
	}

	m := &Manager{
		state: &FreedeskState{
			Accounts: AccountsState{},
			Settings: SettingsState{},
		},
		stateMutex:  sync.RWMutex{},
		systemConn:  systemConn,
		sessionConn: sessionConn,
		currentUID:  uint64(os.Getuid()),
	}

	m.initializeAccounts()
	m.initializeSettings()

	return m, nil
}

func (m *Manager) initializeAccounts() error {
	accountsManager := m.systemConn.Object("org.freedesktop.Accounts", "/org/freedesktop/Accounts")

	var userPath dbus.ObjectPath
	err := accountsManager.Call("org.freedesktop.Accounts.FindUserById", 0, int64(m.currentUID)).Store(&userPath)
	if err != nil {
		m.stateMutex.Lock()
		m.state.Accounts.Available = false
		m.stateMutex.Unlock()
		return err
	}

	m.accountsObj = m.systemConn.Object("org.freedesktop.Accounts", userPath)

	m.stateMutex.Lock()
	m.state.Accounts.Available = true
	m.state.Accounts.UserPath = string(userPath)
	m.state.Accounts.UID = m.currentUID
	m.stateMutex.Unlock()

	m.updateAccountsState()

	return nil
}

func (m *Manager) initializeSettings() error {
	if m.sessionConn == nil {
		m.stateMutex.Lock()
		m.state.Settings.Available = false
		m.stateMutex.Unlock()
		return fmt.Errorf("no session bus connection")
	}

	m.settingsObj = m.sessionConn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

	var variant dbus.Variant
	err := m.settingsObj.Call("org.freedesktop.portal.Settings.ReadOne", 0, "org.freedesktop.appearance", "color-scheme").Store(&variant)
	if err != nil {
		m.stateMutex.Lock()
		m.state.Settings.Available = false
		m.stateMutex.Unlock()
		return err
	}

	m.stateMutex.Lock()
	m.state.Settings.Available = true
	m.stateMutex.Unlock()

	m.updateSettingsState()

	return nil
}

func (m *Manager) updateAccountsState() error {
	if !m.state.Accounts.Available {
		return fmt.Errorf("accounts service not available")
	}

	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	m.getAccountProperty("IconFile", &m.state.Accounts.IconFile)
	m.getAccountProperty("RealName", &m.state.Accounts.RealName)
	m.getAccountProperty("UserName", &m.state.Accounts.UserName)
	m.getAccountProperty("AccountType", &m.state.Accounts.AccountType)
	m.getAccountProperty("HomeDirectory", &m.state.Accounts.HomeDirectory)
	m.getAccountProperty("Shell", &m.state.Accounts.Shell)
	m.getAccountProperty("Email", &m.state.Accounts.Email)
	m.getAccountProperty("Language", &m.state.Accounts.Language)
	m.getAccountProperty("Location", &m.state.Accounts.Location)
	m.getAccountProperty("Locked", &m.state.Accounts.Locked)
	m.getAccountProperty("PasswordMode", &m.state.Accounts.PasswordMode)

	return nil
}

func (m *Manager) updateSettingsState() error {
	if !m.state.Settings.Available {
		return fmt.Errorf("settings portal not available")
	}

	var variant dbus.Variant
	err := m.settingsObj.Call("org.freedesktop.portal.Settings.ReadOne", 0, "org.freedesktop.appearance", "color-scheme").Store(&variant)
	if err != nil {
		return err
	}

	if colorScheme, ok := variant.Value().(uint32); ok {
		m.stateMutex.Lock()
		m.state.Settings.ColorScheme = colorScheme
		m.stateMutex.Unlock()
	}

	return nil
}

func (m *Manager) getAccountProperty(prop string, dest interface{}) error {
	variant, err := m.accountsObj.GetProperty("org.freedesktop.Accounts.User." + prop)
	if err != nil {
		return err
	}
	return variant.Store(dest)
}

func (m *Manager) GetState() FreedeskState {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return *m.state
}

func (m *Manager) Close() {
	if m.systemConn != nil {
		m.systemConn.Close()
	}
	if m.sessionConn != nil {
		m.sessionConn.Close()
	}
}
