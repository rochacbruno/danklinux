package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkManagerBackend_ListVPNProfiles(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	profiles, err := backend.ListVPNProfiles()
	assert.NoError(t, err)
	assert.NotNil(t, profiles)
}

func TestNetworkManagerBackend_ListActiveVPN(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	_, err = backend.ListActiveVPN()
	assert.NoError(t, err)
}

func TestNetworkManagerBackend_ConnectVPN_NotFound(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.ConnectVPN("non-existent-vpn-12345", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestNetworkManagerBackend_ConnectVPN_SingleActive_NoActiveVPN(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.ConnectVPN("non-existent-vpn-12345", true)
	assert.Error(t, err)
}

func TestNetworkManagerBackend_DisconnectVPN_NotActive(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.DisconnectVPN("non-existent-vpn-12345")
	assert.Error(t, err)
}

func TestNetworkManagerBackend_DisconnectAllVPN(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.DisconnectAllVPN()
	assert.NoError(t, err)
}

func TestNetworkManagerBackend_ClearVPNCredentials_NotFound(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.ClearVPNCredentials("non-existent-vpn-12345")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestNetworkManagerBackend_UpdateVPNConnectionState_NotConnecting(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	backend.stateMutex.Lock()
	backend.state.IsConnectingVPN = false
	backend.state.ConnectingVPNUUID = ""
	backend.stateMutex.Unlock()

	assert.NotPanics(t, func() {
		backend.updateVPNConnectionState()
	})
}

func TestNetworkManagerBackend_UpdateVPNConnectionState_EmptyUUID(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	backend.stateMutex.Lock()
	backend.state.IsConnectingVPN = true
	backend.state.ConnectingVPNUUID = ""
	backend.stateMutex.Unlock()

	assert.NotPanics(t, func() {
		backend.updateVPNConnectionState()
	})
}
