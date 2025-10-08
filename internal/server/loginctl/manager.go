package loginctl

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

func NewManager() (*Manager, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	sessionID := os.Getenv("XDG_SESSION_ID")
	if sessionID == "" {
		sessionID = "self"
	}

	m := &Manager{
		state: &SessionState{
			SessionID: sessionID,
		},
		stateMutex:  sync.RWMutex{},
		subscribers: make(map[string]chan SessionState),
		subMutex:    sync.RWMutex{},
		stopChan:    make(chan struct{}),
		conn:        conn,
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
	m.managerObj = m.conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")

	var sessionPath dbus.ObjectPath
	err := m.managerObj.Call("org.freedesktop.login1.Manager.GetSession", 0, m.state.SessionID).Store(&sessionPath)
	if err != nil {
		return fmt.Errorf("failed to get session path: %w", err)
	}

	m.stateMutex.Lock()
	m.state.SessionPath = string(sessionPath)
	m.stateMutex.Unlock()

	m.sessionObj = m.conn.Object("org.freedesktop.login1", sessionPath)

	if err := m.updateSessionState(); err != nil {
		return err
	}

	return nil
}

func (m *Manager) updateSessionState() error {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	if err := m.getProperty("Active", &m.state.Active); err != nil {
		return err
	}
	if err := m.getProperty("IdleHint", &m.state.IdleHint); err != nil {
		return err
	}
	if err := m.getProperty("IdleSinceHint", &m.state.IdleSinceHint); err != nil {
		return err
	}
	if err := m.getProperty("LockedHint", &m.state.LockedHint); err != nil {
		return err
	}
	if err := m.getProperty("Type", &m.state.SessionType); err != nil {
		return err
	}
	if err := m.getProperty("Class", &m.state.SessionClass); err != nil {
		return err
	}

	var user struct {
		UID  uint32
		Path dbus.ObjectPath
	}
	if err := m.getProperty("User", &user); err != nil {
		return err
	}
	m.state.User = user.UID

	if err := m.getProperty("Name", &m.state.UserName); err != nil {
		return err
	}
	if err := m.getProperty("RemoteHost", &m.state.RemoteHost); err != nil {
		return err
	}
	if err := m.getProperty("Service", &m.state.Service); err != nil {
		return err
	}
	if err := m.getProperty("TTY", &m.state.TTY); err != nil {
		return err
	}
	if err := m.getProperty("Display", &m.state.Display); err != nil {
		return err
	}
	if err := m.getProperty("Remote", &m.state.Remote); err != nil {
		return err
	}

	var seat struct {
		ID   string
		Path dbus.ObjectPath
	}
	if err := m.getProperty("Seat", &seat); err == nil {
		m.state.Seat = seat.ID
	}

	if err := m.getProperty("VTNr", &m.state.VTNr); err != nil {
		m.state.VTNr = 0
	}

	m.state.Locked = m.state.LockedHint

	return nil
}

func (m *Manager) getProperty(prop string, dest interface{}) error {
	variant, err := m.sessionObj.GetProperty("org.freedesktop.login1.Session." + prop)
	if err != nil {
		return err
	}
	return variant.Store(dest)
}

func (m *Manager) snapshotState() SessionState {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return *m.state
}

func stateChangedMeaningfully(old, new *SessionState) bool {
	if old.Locked != new.Locked {
		return true
	}
	if old.LockedHint != new.LockedHint {
		return true
	}
	if old.Active != new.Active {
		return true
	}
	if old.IdleHint != new.IdleHint {
		return true
	}
	if old.PreparingForSleep != new.PreparingForSleep {
		return true
	}
	return false
}

func (m *Manager) GetState() SessionState {
	return m.snapshotState()
}

func (m *Manager) Subscribe(id string) chan SessionState {
	ch := make(chan SessionState, 64)
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

func (m *Manager) Close() {
	close(m.stopChan)
	m.notifierWg.Wait()
	m.subMutex.Lock()
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = make(map[string]chan SessionState)
	m.subMutex.Unlock()
}
