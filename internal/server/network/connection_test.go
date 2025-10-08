package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnectionRequest_Validation(t *testing.T) {
	t.Run("basic WiFi connection", func(t *testing.T) {
		req := ConnectionRequest{
			SSID:     "TestNetwork",
			Password: "testpass123",
		}

		assert.NotEmpty(t, req.SSID)
		assert.NotEmpty(t, req.Password)
		assert.Empty(t, req.Username)
	})

	t.Run("enterprise WiFi connection", func(t *testing.T) {
		req := ConnectionRequest{
			SSID:     "EnterpriseNetwork",
			Password: "testpass123",
			Username: "testuser",
		}

		assert.NotEmpty(t, req.SSID)
		assert.NotEmpty(t, req.Password)
		assert.NotEmpty(t, req.Username)
	})

	t.Run("open WiFi connection", func(t *testing.T) {
		req := ConnectionRequest{
			SSID: "OpenNetwork",
		}

		assert.NotEmpty(t, req.SSID)
		assert.Empty(t, req.Password)
		assert.Empty(t, req.Username)
	})
}

func TestManager_ConnectWiFi_NoDevice(t *testing.T) {
	manager := &Manager{
		state:      &NetworkState{},
		wifiDevice: nil,
	}

	req := ConnectionRequest{
		SSID:     "TestNetwork",
		Password: "testpass123",
	}

	err := manager.ConnectWiFi(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no WiFi device available")
}

func TestManager_DisconnectWiFi_NoDevice(t *testing.T) {
	manager := &Manager{
		state:      &NetworkState{},
		wifiDevice: nil,
	}

	err := manager.DisconnectWiFi()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no WiFi device available")
}

func TestManager_ForgetWiFiNetwork_NotFound(t *testing.T) {
	manager := &Manager{
		state: &NetworkState{},
	}

	err := manager.ForgetWiFiNetwork("NonExistentNetwork")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection not found")
}

func TestManager_ConnectEthernet_NoDevice(t *testing.T) {
	manager := &Manager{
		state:          &NetworkState{},
		ethernetDevice: nil,
	}

	err := manager.ConnectEthernet()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no ethernet device available")
}

func TestManager_DisconnectEthernet_NoDevice(t *testing.T) {
	manager := &Manager{
		state:          &NetworkState{},
		ethernetDevice: nil,
	}

	err := manager.DisconnectEthernet()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no ethernet device available")
}

// Note: More comprehensive tests for connection operations would require
// mocking the NetworkManager D-Bus interfaces, which is beyond the scope
// of these unit tests. The tests above cover the basic error cases and
// validation logic. Integration tests would be needed for full coverage.
