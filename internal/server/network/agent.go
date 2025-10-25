package network

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AvengeMedia/danklinux/internal/errdefs"
	"github.com/AvengeMedia/danklinux/internal/log"
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
	conn    *dbus.Conn
	objPath dbus.ObjectPath
	id      string
	prompts PromptBroker
	manager *Manager
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

	log.Infof("[SecretAgent] Registered with NetworkManager (id=%s, unique name=%s, fixed path=%s)", sa.id, c.Names()[0], sa.objPath)
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
	log.Infof("[SecretAgent] GetSecrets called: path=%s, setting=%s, hints=%v, flags=%d",
		path, settingName, hints, flags)

	const (
		NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION = 0x1
		NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW       = 0x2
		NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED    = 0x4
	)

	connType, displayName, vpnSvc := readConnTypeAndName(conn)
	ssid := readSSID(conn)
	fields := fieldsNeeded(settingName, conn, hints)

	log.Infof("[SecretAgent] connType=%s, name=%s, vpnSvc=%s, fields=%v, flags=%d", connType, displayName, vpnSvc, fields, flags)

	if len(fields) == 0 {
		allowInteraction := flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION != 0
		userRequested := flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED != 0

		if settingName == "vpn" && (allowInteraction || userRequested) {
			log.Infof("[SecretAgent] VPN with empty hints but interaction allowed/requested - using fallback fields")
			fields = []string{"password"}
		} else {
			const (
				NM_SETTING_SECRET_FLAG_NONE         = 0
				NM_SETTING_SECRET_FLAG_AGENT_OWNED  = 1
				NM_SETTING_SECRET_FLAG_NOT_SAVED    = 2
				NM_SETTING_SECRET_FLAG_NOT_REQUIRED = 4
			)

			var passwordFlags uint32 = 0xFFFF
			if settingName == "vpn" {
				if vpnSettings, ok := conn["vpn"]; ok {
					if flagsVariant, ok := vpnSettings["password-flags"]; ok {
						if pwdFlags, ok := flagsVariant.Value().(uint32); ok {
							passwordFlags = pwdFlags
							log.Infof("[SecretAgent] Parsed VPN password-flags directly: %d", passwordFlags)
						}
					}

					if passwordFlags == 0xFFFF {
						if dataVariant, ok := vpnSettings["data"]; ok {
							dataValue := dataVariant.Value()
							log.Debugf("[SecretAgent] vpn.data type: %T", dataValue)
							if dataMap, ok := dataValue.(map[string]string); ok {
								if flagsStr, ok := dataMap["password-flags"]; ok {
									var flagsInt int
									if _, err := fmt.Sscanf(flagsStr, "%d", &flagsInt); err == nil {
										passwordFlags = uint32(flagsInt)
										log.Infof("[SecretAgent] Parsed VPN password-flags from data: %d", passwordFlags)
									} else {
										log.Warnf("[SecretAgent] Failed to parse password-flags '%s': %v", flagsStr, err)
									}
								} else {
									log.Warnf("[SecretAgent] No password-flags in vpn.data map")
								}
							} else {
								log.Warnf("[SecretAgent] vpn.data is not map[string]string")
							}
						} else {
							log.Warnf("[SecretAgent] No vpn.data field")
						}
					}
				}
			} else if settingName == "802-11-wireless-security" {
				if wifiSecSettings, ok := conn["802-11-wireless-security"]; ok {
					if flagsVariant, ok := wifiSecSettings["psk-flags"]; ok {
						if pwdFlags, ok := flagsVariant.Value().(uint32); ok {
							passwordFlags = pwdFlags
						}
					}
				}
			} else if settingName == "802-1x" {
				if dot1xSettings, ok := conn["802-1x"]; ok {
					if flagsVariant, ok := dot1xSettings["password-flags"]; ok {
						if pwdFlags, ok := flagsVariant.Value().(uint32); ok {
							passwordFlags = pwdFlags
						}
					}
				}
			}

			if passwordFlags == 0xFFFF {
				log.Warnf("[SecretAgent] Could not determine password-flags for empty hints - returning NoSecrets error")
				return nil, dbus.NewError("org.freedesktop.NetworkManager.SecretAgent.Error.NoSecrets", nil)
			} else if passwordFlags&NM_SETTING_SECRET_FLAG_NOT_REQUIRED != 0 {
				log.Infof("[SecretAgent] Secrets not required (flags=%d)", passwordFlags)
				out := nmSettingMap{}
				out[settingName] = nmVariantMap{}
				return out, nil
			} else if passwordFlags&NM_SETTING_SECRET_FLAG_AGENT_OWNED != 0 {
				log.Warnf("[SecretAgent] Secrets are agent-owned but we don't store secrets (flags=%d) - returning NoSecrets error", passwordFlags)
				return nil, dbus.NewError("org.freedesktop.NetworkManager.SecretAgent.Error.NoSecrets", nil)
			} else {
				log.Infof("[SecretAgent] No secrets needed, using system stored secrets (flags=%d)", passwordFlags)
				out := nmSettingMap{}
				out[settingName] = nmVariantMap{}
				return out, nil
			}
		}
	}

	reason := reasonFromFlags(flags)
	if a.manager != nil && connType == "802-11-wireless" && a.manager.WasRecentlyFailed(ssid) {
		reason = "wrong-password"
	}

	var connId, connUuid string
	if c, ok := conn["connection"]; ok {
		if v, ok := c["id"]; ok {
			if s, ok2 := v.Value().(string); ok2 {
				connId = s
			}
		}
		if v, ok := c["uuid"]; ok {
			if s, ok2 := v.Value().(string); ok2 {
				connUuid = s
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	token, err := a.prompts.Ask(ctx, PromptRequest{
		Name:           displayName,
		SSID:           ssid,
		ConnType:       connType,
		VpnService:     vpnSvc,
		SettingName:    settingName,
		Fields:         fields,
		Hints:          hints,
		Reason:         reason,
		ConnectionId:   connId,
		ConnectionUuid: connUuid,
	})
	if err != nil {
		log.Warnf("[SecretAgent] Failed to create prompt: %v", err)
		return nil, dbus.MakeFailedError(err)
	}

	log.Infof("[SecretAgent] Waiting for user input (token=%s)", token)
	reply, err := a.prompts.Wait(ctx, token)
	if err != nil {
		log.Warnf("[SecretAgent] Prompt failed or cancelled: %v", err)
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

	if settingName == "802-1x" {
		log.Infof("[SecretAgent] Returning 802-1x enterprise secrets with %d fields", len(sec))
	} else if settingName == "vpn" {
		log.Infof("[SecretAgent] Returning VPN secrets with %d fields for %s", len(sec), vpnSvc)
	}
	return out, nil
}

func (a *SecretAgent) SaveSecrets(conn map[string]nmVariantMap, path dbus.ObjectPath) *dbus.Error {
	log.Infof("[SecretAgent] SaveSecrets called: path=%s (handled in GetSecrets)", path)
	return nil
}

func (a *SecretAgent) DeleteSecrets(conn map[string]nmVariantMap, path dbus.ObjectPath) *dbus.Error {
	ssid := readSSID(conn)
	log.Infof("[SecretAgent] DeleteSecrets called: path=%s, SSID=%s", path, ssid)
	return nil
}

func (a *SecretAgent) DeleteSecrets2(path dbus.ObjectPath, setting string) *dbus.Error {
	log.Infof("[SecretAgent] DeleteSecrets2 (alternate) called: path=%s, setting=%s", path, setting)
	return nil
}

func (a *SecretAgent) CancelGetSecrets(path dbus.ObjectPath, settingName string) *dbus.Error {
	log.Infof("[SecretAgent] CancelGetSecrets called: path=%s, setting=%s", path, settingName)
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

func readConnTypeAndName(conn map[string]nmVariantMap) (string, string, string) {
	var connType, name, svc string
	if c, ok := conn["connection"]; ok {
		if v, ok := c["type"]; ok {
			if s, ok2 := v.Value().(string); ok2 {
				connType = s
			}
		}
		if v, ok := c["id"]; ok {
			if s, ok2 := v.Value().(string); ok2 {
				name = s
			}
		}
	}
	if vpn, ok := conn["vpn"]; ok {
		if v, ok := vpn["service-type"]; ok {
			if s, ok2 := v.Value().(string); ok2 {
				svc = s
			}
		}
	}
	if name == "" && connType == "802-11-wireless" {
		name = readSSID(conn)
	}
	return connType, name, svc
}

func fieldsNeeded(setting string, conn map[string]nmVariantMap, hints []string) []string {
	switch setting {
	case "802-11-wireless-security":
		return []string{"psk"}
	case "802-1x":
		return []string{"identity", "password"}
	case "vpn":
		return hints
	default:
		return []string{}
	}
}

func reasonFromFlags(flags uint32) string {
	const (
		NM_SECRET_AGENT_GET_SECRETS_FLAG_NONE              = 0x0
		NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION = 0x1
		NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW       = 0x2
		NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED    = 0x4
		NM_SECRET_AGENT_GET_SECRETS_FLAG_WPS_PBC_ACTIVE    = 0x8
		NM_SECRET_AGENT_GET_SECRETS_FLAG_ONLY_SYSTEM       = 0x80000000
		NM_SECRET_AGENT_GET_SECRETS_FLAG_NO_ERRORS         = 0x40000000
	)

	if flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW != 0 {
		return "wrong-password"
	}
	if flags&NM_SECRET_AGENT_GET_SECRETS_FLAG_USER_REQUESTED != 0 {
		return "user-requested"
	}
	return "required"
}
