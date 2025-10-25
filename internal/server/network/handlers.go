package network

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/server/models"
)

type Request struct {
	ID     int                    `json:"id,omitempty"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type SuccessResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func HandleRequest(conn net.Conn, req Request, manager *Manager) {
	switch req.Method {
	case "network.getState":
		handleGetState(conn, req, manager)
	case "network.wifi.scan":
		handleScanWiFi(conn, req, manager)
	case "network.wifi.networks":
		handleGetWiFiNetworks(conn, req, manager)
	case "network.wifi.connect":
		handleConnectWiFi(conn, req, manager)
	case "network.wifi.disconnect":
		handleDisconnectWiFi(conn, req, manager)
	case "network.wifi.forget":
		handleForgetWiFi(conn, req, manager)
	case "network.wifi.toggle":
		handleToggleWiFi(conn, req, manager)
	case "network.wifi.enable":
		handleEnableWiFi(conn, req, manager)
	case "network.wifi.disable":
		handleDisableWiFi(conn, req, manager)
	case "network.ethernet.connect.config":
		handleConnectEthernetSpecificConfig(conn, req, manager)
	case "network.ethernet.connect":
		handleConnectEthernet(conn, req, manager)
	case "network.ethernet.disconnect":
		handleDisconnectEthernet(conn, req, manager)
	case "network.preference.set":
		handleSetPreference(conn, req, manager)
	case "network.info":
		handleGetNetworkInfo(conn, req, manager)
	case "network.ethernet.info":
		handleGetWiredNetworkInfo(conn, req, manager)
	case "network.subscribe":
		handleSubscribe(conn, req, manager)
	case "network.credentials.submit":
		handleCredentialsSubmit(conn, req, manager)
	case "network.credentials.cancel":
		handleCredentialsCancel(conn, req, manager)
	default:
		models.RespondError(conn, req.ID, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

func handleCredentialsSubmit(conn net.Conn, req Request, manager *Manager) {
	token, ok := req.Params["token"].(string)
	if !ok {
		log.Warnf("handleCredentialsSubmit: missing or invalid token parameter")
		models.RespondError(conn, req.ID, "missing or invalid 'token' parameter")
		return
	}

	secretsRaw, ok := req.Params["secrets"].(map[string]interface{})
	if !ok {
		log.Warnf("handleCredentialsSubmit: missing or invalid secrets parameter")
		models.RespondError(conn, req.ID, "missing or invalid 'secrets' parameter")
		return
	}

	secrets := make(map[string]string)
	for k, v := range secretsRaw {
		if str, ok := v.(string); ok {
			secrets[k] = str
		}
	}

	save := false
	if saveParam, ok := req.Params["save"].(bool); ok {
		save = saveParam
	}

	if err := manager.SubmitCredentials(token, secrets, save); err != nil {
		log.Warnf("handleCredentialsSubmit: failed to submit credentials: %v", err)
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	log.Infof("handleCredentialsSubmit: credentials submitted successfully")
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "credentials submitted"})
}

func handleCredentialsCancel(conn net.Conn, req Request, manager *Manager) {
	token, ok := req.Params["token"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'token' parameter")
		return
	}

	if err := manager.CancelCredentials(token); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "credentials cancelled"})
}

func handleGetState(conn net.Conn, req Request, manager *Manager) {
	state := manager.GetState()
	models.Respond(conn, req.ID, state)
}

func handleScanWiFi(conn net.Conn, req Request, manager *Manager) {
	if err := manager.ScanWiFi(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "scanning"})
}

func handleGetWiFiNetworks(conn net.Conn, req Request, manager *Manager) {
	networks := manager.GetWiFiNetworks()
	models.Respond(conn, req.ID, networks)
}

func handleConnectWiFi(conn net.Conn, req Request, manager *Manager) {
	ssid, ok := req.Params["ssid"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'ssid' parameter")
		return
	}

	var connReq ConnectionRequest
	connReq.SSID = ssid

	if password, ok := req.Params["password"].(string); ok {
		connReq.Password = password
	}
	if username, ok := req.Params["username"].(string); ok {
		connReq.Username = username
	}

	if interactive, ok := req.Params["interactive"].(bool); ok {
		connReq.Interactive = interactive
	} else {
		state := manager.GetState()
		alreadyConnected := state.WiFiConnected && state.WiFiSSID == ssid

		if alreadyConnected {
			connReq.Interactive = false
		} else {
			networkInfo, err := manager.GetNetworkInfo(ssid)
			isSaved := err == nil && networkInfo.Saved

			if isSaved {
				connReq.Interactive = false
			} else if err == nil && networkInfo.Secured && connReq.Password == "" && connReq.Username == "" {
				connReq.Interactive = true
			}
		}
	}

	if anonymousIdentity, ok := req.Params["anonymousIdentity"].(string); ok {
		connReq.AnonymousIdentity = anonymousIdentity
	}
	if domainSuffixMatch, ok := req.Params["domainSuffixMatch"].(string); ok {
		connReq.DomainSuffixMatch = domainSuffixMatch
	}

	if err := manager.ConnectWiFi(connReq); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "connecting"})
}

func handleDisconnectWiFi(conn net.Conn, req Request, manager *Manager) {
	if err := manager.DisconnectWiFi(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "disconnected"})
}

func handleForgetWiFi(conn net.Conn, req Request, manager *Manager) {
	ssid, ok := req.Params["ssid"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'ssid' parameter")
		return
	}

	if err := manager.ForgetWiFiNetwork(ssid); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "forgotten"})
}

func handleToggleWiFi(conn net.Conn, req Request, manager *Manager) {
	if err := manager.ToggleWiFi(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	state := manager.GetState()
	models.Respond(conn, req.ID, map[string]bool{"enabled": state.WiFiEnabled})
}

func handleEnableWiFi(conn net.Conn, req Request, manager *Manager) {
	if err := manager.EnableWiFi(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, map[string]bool{"enabled": true})
}

func handleDisableWiFi(conn net.Conn, req Request, manager *Manager) {
	if err := manager.DisableWiFi(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, map[string]bool{"enabled": false})
}

func handleConnectEthernetSpecificConfig(conn net.Conn, req Request, manager *Manager) {
	uuid, ok := req.Params["uuid"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'uuid' parameter")
		return
	}
	if err := manager.activateConnection(uuid); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "connecting"})
}

func handleConnectEthernet(conn net.Conn, req Request, manager *Manager) {
	if err := manager.ConnectEthernet(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "connecting"})
}

func handleDisconnectEthernet(conn net.Conn, req Request, manager *Manager) {
	if err := manager.DisconnectEthernet(); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}
	models.Respond(conn, req.ID, SuccessResult{Success: true, Message: "disconnected"})
}

func handleSetPreference(conn net.Conn, req Request, manager *Manager) {
	preference, ok := req.Params["preference"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'preference' parameter")
		return
	}

	if err := manager.SetConnectionPreference(ConnectionPreference(preference)); err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, map[string]string{"preference": preference})
}

func handleGetNetworkInfo(conn net.Conn, req Request, manager *Manager) {
	ssid, ok := req.Params["ssid"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'ssid' parameter")
		return
	}

	network, err := manager.GetNetworkInfoDetailed(ssid)
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, network)
}

func handleGetWiredNetworkInfo(conn net.Conn, req Request, manager *Manager) {
	uuid, ok := req.Params["uuid"].(string)
	if !ok {
		models.RespondError(conn, req.ID, "missing or invalid 'uuid' parameter")
		return
	}

	network, err := manager.GetWiredNetworkInfoDetailed(uuid)
	if err != nil {
		models.RespondError(conn, req.ID, err.Error())
		return
	}

	models.Respond(conn, req.ID, network)
}

func handleSubscribe(conn net.Conn, req Request, manager *Manager) {
	clientID := fmt.Sprintf("client-%p", conn)
	stateChan := manager.Subscribe(clientID)
	defer manager.Unsubscribe(clientID)

	initialState := manager.GetState()
	event := NetworkEvent{
		Type: EventStateChanged,
		Data: initialState,
	}
	if err := json.NewEncoder(conn).Encode(models.Response[NetworkEvent]{
		ID:     req.ID,
		Result: &event,
	}); err != nil {
		return
	}

	for state := range stateChan {
		event := NetworkEvent{
			Type: EventStateChanged,
			Data: state,
		}
		if err := json.NewEncoder(conn).Encode(models.Response[NetworkEvent]{
			Result: &event,
		}); err != nil {
			return
		}
	}
}
