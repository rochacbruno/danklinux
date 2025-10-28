package network

import (
	"testing"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/stretchr/testify/assert"
)

func TestNetworkManagerBackend_UpdatePrimaryConnection(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	err = backend.updatePrimaryConnection()
	assert.NoError(t, err)
}

func TestNetworkManagerBackend_UpdateEthernetState_NoDevice(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	backend.ethernetDevice = nil
	err = backend.updateEthernetState()
	assert.NoError(t, err)
}

func TestNetworkManagerBackend_UpdateWiFiState_NoDevice(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	backend.wifiDevice = nil
	err = backend.updateWiFiState()
	assert.NoError(t, err)
}

func TestNetworkManagerBackend_ClassifyNMStateReason(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	testCases := []struct {
		reason   uint32
		expected string
	}{
		{NmDeviceStateReasonWrongPassword, errdefs.ErrBadCredentials},
		{NmDeviceStateReasonSupplicantTimeout, errdefs.ErrBadCredentials},
		{NmDeviceStateReasonSupplicantFailed, errdefs.ErrBadCredentials},
		{NmDeviceStateReasonSecretsRequired, errdefs.ErrBadCredentials},
		{NmDeviceStateReasonNoSecrets, errdefs.ErrUserCanceled},
		{NmDeviceStateReasonNoSsid, errdefs.ErrNoSuchSSID},
		{NmDeviceStateReasonDhcpClientFailed, errdefs.ErrDhcpTimeout},
		{NmDeviceStateReasonIpConfigUnavailable, errdefs.ErrDhcpTimeout},
		{NmDeviceStateReasonSupplicantDisconnect, errdefs.ErrAssocTimeout},
		{NmDeviceStateReasonCarrier, errdefs.ErrAssocTimeout},
		{999, errdefs.ErrConnectionFailed},
	}

	for _, tc := range testCases {
		result := backend.classifyNMStateReason(tc.reason)
		assert.Equal(t, tc.expected, result, "Failed for reason %d", tc.reason)
	}
}

func TestNetworkManagerBackend_GetDeviceIP_NoConfig(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	if backend.ethernetDevice == nil && backend.wifiDevice == nil {
		t.Skip("No network devices available")
	}
}

func TestNetworkManagerBackend_GetDeviceStateReason_NoDBusConn(t *testing.T) {
	backend, err := NewNetworkManagerBackend()
	if err != nil {
		t.Skipf("NetworkManager not available: %v", err)
	}

	if backend.ethernetDevice == nil && backend.wifiDevice == nil {
		t.Skip("No network devices available")
	}

	backend.dbusConn = nil
}
