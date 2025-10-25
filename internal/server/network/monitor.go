package network

import (
	"time"
)

func (m *Manager) StartAutoScan(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopChan:
				return
			case <-ticker.C:
				m.stateMutex.RLock()
				enabled := m.state.WiFiEnabled
				m.stateMutex.RUnlock()

				if enabled {
					m.ScanWiFi()
				}
			}
		}
	}()
}
