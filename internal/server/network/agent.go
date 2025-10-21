package network

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/godbus/dbus/v5"
)

const (
	nmAgentManagerPath  = "/org/freedesktop/NetworkManager/AgentManager"
	nmAgentManagerIface = "org.freedesktop.NetworkManager.AgentManager"
	nmSecretAgentIface  = "org.freedesktop.NetworkManager.SecretAgent"
	agentObjectPath     = "/org/freedesktop/NetworkManager/SecretAgent"
	agentIdentifier     = "com.danklinux.NMAgent"
)

type SecretAgent struct {
	conn     *dbus.Conn
	objPath  dbus.ObjectPath
	id       string
	prompts  PromptBroker
	manager  *Manager
}

type nmVariantMap map[string]dbus.Variant
type nmSettingMap map[string]nmVariantMap

const introspectXML = `
<node>
	<interface name="org.freedesktop.NetworkManager.SecretAgent">
		<method name="GetSecrets">
			<arg type="a{sa{sv}}" name="connection" direction="in"/>
			<arg type="o" name="connection_path" direction="in"/>
			<arg type="s" name="setting_name" direction="in"/>
			<arg type="as" name="hints" direction="in"/>
			<arg type="u" name="flags" direction="in"/>
			<arg type="a{sa{sv}}" name="secrets" direction="out"/>
		</method>
		<method name="SaveSecrets">
			<arg type="a{sa{sv}}" name="connection" direction="in"/>
			<arg type="o" name="connection_path" direction="in"/>
		</method>
		<method name="DeleteSecrets">
			<arg type="a{sa{sv}}" name="connection" direction="in"/>
			<arg type="o" name="connection_path" direction="in"/>
		</method>
		<method name="DeleteSecrets2">
			<arg type="o" name="connection_path" direction="in"/>
			<arg type="s" name="setting" direction="in"/>
		</method>
		<method name="CancelGetSecrets">
			<arg type="o" name="connection_path" direction="in"/>
			<arg type="s" name="setting_name" direction="in"/>
		</method>
	</interface>
	<interface name="org.freedesktop.DBus.Introspectable">
		<method name="Introspect">
			<arg name="data" type="s" direction="out"/>
		</method>
	</interface>
</node>`

func NewSecretAgent(prompts PromptBroker, manager *Manager) (*SecretAgent, error) {
	c, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system bus: %w", err)
	}

	sa := &SecretAgent{
		conn:    c,
		objPath: dbus.ObjectPath(agentObjectPath),
		id:      agentIdentifier,
		prompts: prompts,
		manager: manager,
	}

	if err := c.Export(sa, sa.objPath, nmSecretAgentIface); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to export secret agent: %w", err)
	}

	if err := c.Export(sa, sa.objPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to export introspection: %w", err)
	}

	mgr := c.Object("org.freedesktop.NetworkManager", dbus.ObjectPath(nmAgentManagerPath))
	call := mgr.Call(nmAgentManagerIface+".Register", 0, sa.id)
	if call.Err != nil {
		c.Close()
		return nil, fmt.Errorf("failed to register agent with NetworkManager: %w", call.Err)
	}

	log.Printf("[SecretAgent] Registered with NetworkManager (id=%s, unique name=%s, fixed path=%s)", sa.id, c.Names()[0], sa.objPath)
	return sa, nil
}

func (a *SecretAgent) Close() {
	if a.conn != nil {
		mgr := a.conn.Object("org.freedesktop.NetworkManager", dbus.ObjectPath(nmAgentManagerPath))
		_ = mgr.Call(nmAgentManagerIface+".Unregister", 0, a.id).Err
		a.conn.Close()
	}
}

func (a *SecretAgent) GetSecrets(
	conn map[string]nmVariantMap,
	path dbus.ObjectPath,
	settingName string,
	hints []string,
	flags uint32,
) (nmSettingMap, *dbus.Error) {
	log.Printf("[SecretAgent] GetSecrets called: path=%s, setting=%s, hints=%v, flags=%d",
		path, settingName, hints, flags)

	ssid := readSSID(conn)
	fields := fieldsNeeded(settingName, conn, hints)

	log.Printf("[SecretAgent] SSID=%s, fields=%v", ssid, fields)

	reason := reasonFromFlags(flags)
	if a.manager != nil && a.manager.WasRecentlyFailed(ssid) {
		reason = "wrong-password"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	token, err := a.prompts.Ask(ctx, PromptRequest{
		SSID:        ssid,
		SettingName: settingName,
		Fields:      fields,
		Hints:       hints,
		Reason:      reason,
	})
	if err != nil {
		log.Printf("[SecretAgent] Failed to create prompt: %v", err)
		return nil, dbus.MakeFailedError(err)
	}

	log.Printf("[SecretAgent] Waiting for user input (token=%s)", token)
	reply, err := a.prompts.Wait(ctx, token)
	if err != nil {
		log.Printf("[SecretAgent] Prompt failed or cancelled: %v", err)
		if errors.Is(err, errdefs.ErrSecretPromptTimeout) {
			return nil, dbus.NewError("org.freedesktop.NetworkManager.SecretAgent.Error.Failed", nil)
		}
		if reply.Cancel || errors.Is(err, errdefs.ErrSecretPromptCancelled) {
			return nil, dbus.NewError("org.freedesktop.NetworkManager.SecretAgent.Error.UserCanceled", nil)
		}
		return nil, dbus.NewError("org.freedesktop.NetworkManager.SecretAgent.Error.Failed", nil)
	}

	out := nmSettingMap{}
	sec := nmVariantMap{}
	for k, v := range reply.Secrets {
		sec[k] = dbus.MakeVariant(v)
	}
	out[settingName] = sec

	if reply.Save {
		if err := a.saveConnectionSecrets(path, settingName, reply.Secrets); err != nil {
			log.Printf("[SecretAgent] Warning: failed to save secrets to connection: %v", err)
		}
	}

	if settingName == "802-1x" {
		log.Printf("[SecretAgent] Returning 802-1x enterprise secrets with %d fields", len(sec))
	}
	return out, nil
}

func (a *SecretAgent) saveConnectionSecrets(path dbus.ObjectPath, settingName string, secrets map[string]string) error {
	nmConn := a.conn.Object("org.freedesktop.NetworkManager", path)

	var settings map[string]map[string]dbus.Variant
	if call := nmConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0); call.Err != nil {
		return fmt.Errorf("GetSettings: %w", call.Err)
	} else if err := call.Store(&settings); err != nil {
		return fmt.Errorf("GetSettings decode: %w", err)
	}

	switch settingName {
	case "802-11-wireless-security":
		sec := settings["802-11-wireless-security"]
		if sec == nil {
			sec = map[string]dbus.Variant{}
		}
		if psk, ok := secrets["psk"]; ok {
			sec["psk"] = dbus.MakeVariant(psk)
			sec["psk-flags"] = dbus.MakeVariant(uint32(0))
		}
		settings["802-11-wireless-security"] = sec

	case "802-1x":
		sec := settings["802-1x"]
		if sec == nil {
			sec = map[string]dbus.Variant{}
		}
		if id, ok := secrets["identity"]; ok {
			sec["identity"] = dbus.MakeVariant(id)
		}
		if pw, ok := secrets["password"]; ok {
			sec["password"] = dbus.MakeVariant(pw)
			sec["password-flags"] = dbus.MakeVariant(uint32(0))
		}
		settings["802-1x"] = sec
	}

	if ipv6 := settings["ipv6"]; ipv6 != nil {
		delete(ipv6, "addresses")
		delete(ipv6, "routes")
		delete(ipv6, "address-data")
		delete(ipv6, "route-data")
		settings["ipv6"] = ipv6
	}
	if ipv4 := settings["ipv4"]; ipv4 != nil {
		delete(ipv4, "addresses")
		delete(ipv4, "routes")
		delete(ipv4, "address-data")
		delete(ipv4, "route-data")
		settings["ipv4"] = ipv4
	}

	if call := nmConn.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings); call.Err != nil {
		return fmt.Errorf("Update: %w", call.Err)
	}

	log.Printf("[SecretAgent] Successfully saved secrets to connection: %s", path)
	return nil
}

func (a *SecretAgent) SaveSecrets(conn map[string]nmVariantMap, path dbus.ObjectPath) *dbus.Error {
	log.Printf("[SecretAgent] SaveSecrets called: path=%s", path)
	return nil
}

func (a *SecretAgent) DeleteSecrets(conn map[string]nmVariantMap, path dbus.ObjectPath) *dbus.Error {
	log.Printf("[SecretAgent] DeleteSecrets called: path=%s", path)
	return nil
}

func (a *SecretAgent) DeleteSecrets2(path dbus.ObjectPath, setting string) *dbus.Error {
	log.Printf("[SecretAgent] DeleteSecrets2 (alternate) called: path=%s, setting=%s", path, setting)
	return nil
}

func (a *SecretAgent) CancelGetSecrets(path dbus.ObjectPath, settingName string) *dbus.Error {
	log.Printf("[SecretAgent] CancelGetSecrets called: path=%s, setting=%s", path, settingName)
	return nil
}

func (a *SecretAgent) Introspect() (string, *dbus.Error) {
	return introspectXML, nil
}

func readSSID(conn map[string]nmVariantMap) string {
	if w, ok := conn["802-11-wireless"]; ok {
		if v, ok := w["ssid"]; ok {
			if b, ok := v.Value().([]byte); ok {
				return string(b)
			}
			if s, ok := v.Value().(string); ok {
				return s
			}
		}
	}
	return ""
}

func fieldsNeeded(setting string, conn map[string]nmVariantMap, hints []string) []string {
	switch setting {
	case "802-11-wireless-security":
		return []string{"psk"}
	case "802-1x":
		fields := []string{"identity", "password"}
		return fields
	default:
		return []string{}
	}
}

func reasonFromFlags(flags uint32) string {
	const (
		NM_SECRET_AGENT_GET_SECRETS_FLAG_NONE                  = 0x0
		NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION     = 0x1
		NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW           = 0x2
		NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED        = 0x4
		NM_SECRET_AGENT_GET_SECRETS_FLAG_WPS_PBC_ACTIVE        = 0x8
		NM_SECRET_AGENT_GET_SECRETS_FLAG_ONLY_SYSTEM           = 0x80000000
		NM_SECRET_AGENT_GET_SECRETS_FLAG_NO_ERRORS             = 0x40000000
	)

	if flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW != 0 {
		return "wrong-password"
	}
	if flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED != 0 {
		return "user-requested"
	}
	return "required"
}
