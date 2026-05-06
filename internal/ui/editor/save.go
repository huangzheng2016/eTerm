package editor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m Model) save() tea.Cmd {
	hostname := m.inputs[inputIndexForField(hostnameField)].Value()
	if hostname == "" {
		m.err = "Hostname is required"
		return nil
	}

	portStr := m.inputs[inputIndexForField(portField)].Value()
	port := 22
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			m.err = "Invalid port number"
			return nil
		}
		port = p
	}

	authMethod := authOptions[m.authIdx]

	password := m.inputs[inputIndexForField(passwordField)].Value()
	var encryptedPassword string
	if password != "" && (authMethod == "password" || authMethod == "interactive") {
		k := m.masterKey.GetKey()
		if k != nil {
			enc, err := security.Encrypt([]byte(password), k.Bytes())
			k.Clear()
			if err != nil {
				m.err = "Failed to encrypt password"
				return nil
			}
			encryptedPassword = enc
		}
	}

	var keyID *uint
	if authMethod == "key" && m.keyIdx >= 0 && m.keyIdx < len(m.keyOptions) {
		uid := m.keyOptions[m.keyIdx].ID
		keyID = &uid
	}

	proxyType := ""
	var proxyHost, proxyUser string
	var proxyPort int
	var proxyPassEnc string

	if m.proxyTypeIdx > 0 {
		proxyType = proxyOptions[m.proxyTypeIdx]
		proxyHost = strings.TrimSpace(m.inputs[inputIndexForField(proxyHostField)].Value())
		if proxyHost == "" {
			m.err = "Proxy host is required"
			return nil
		}
		ppStr := strings.TrimSpace(m.inputs[inputIndexForField(proxyPortField)].Value())
		pp, err := strconv.Atoi(ppStr)
		if err != nil || pp <= 0 || pp > 65535 {
			m.err = "Invalid proxy port"
			return nil
		}
		proxyPort = pp
		proxyUser = strings.TrimSpace(m.inputs[inputIndexForField(proxyUserField)].Value())
		pw := m.inputs[inputIndexForField(proxyPasswordField)].Value()
		if pw != "" {
			k := m.masterKey.GetKey()
			if k != nil {
				enc, err := security.Encrypt([]byte(pw), k.Bytes())
				k.Clear()
				if err != nil {
					m.err = "Failed to encrypt proxy password"
					return nil
				}
				proxyPassEnc = enc
			}
		}
	}

	var jumpHostID *uint
	if m.jumpIdx >= 0 && m.jumpIdx < len(m.jumpHostOptions) {
		id := m.jumpHostOptions[m.jumpIdx].ID
		jumpHostID = &id
	}
	if m.host != nil && m.host.ID > 0 && jumpHostID != nil {
		if db.JumpChainPointsBackToHost(m.db, m.host.ID, *jumpHostID) {
			m.err = "Jump host chain would cycle"
			return nil
		}
	}

	// GSSAPI fields
	gssapiSource := ""
	krbPrincipal := ""
	gssapiKeytab := ""
	if authMethod == "gssapi" {
		gssapiSource = gssapiSourceOptions[m.gssapiSourceIdx]
		if m.gssapiSourceIdx == 1 { // keytab
			krbPrincipal = strings.TrimSpace(m.inputs[inputIndexForField(krbPrincipalField)].Value())
			if krbPrincipal == "" {
				m.err = "Kerberos principal is required for keytab mode"
				return nil
			}
			gssapiKeytab = strings.TrimSpace(m.inputs[inputIndexForField(gssapiKeytabField)].Value())
			if gssapiKeytab == "" {
				m.err = "Keytab path is required"
				return nil
			}
		}
	}

	// ProxyCommand (mutually exclusive with proxy type)
	proxyCommand := ""
	if m.proxyTypeIdx == 0 {
		proxyCommand = strings.TrimSpace(m.inputs[inputIndexForField(proxyCommandField)].Value())
	}
	forwardAgent := boolOptions[m.forwardAgentIdx]
	remoteCommand := strings.TrimSpace(m.remoteCommand.Value())
	extraSSHOptions := strings.TrimSpace(m.extraOptions.Value())
	if err := sshconfig.ValidateExtraOptions(extraSSHOptions); err != nil {
		m.err = err.Error()
		return nil
	}

	host := db.Host{
		Alias:           m.inputs[inputIndexForField(aliasField)].Value(),
		Hostname:        hostname,
		Port:            port,
		Username:        m.inputs[inputIndexForField(usernameField)].Value(),
		AuthMethod:      authMethod,
		Password:        encryptedPassword,
		KeyID:           keyID,
		Group:           m.inputs[inputIndexForField(groupField)].Value(),
		Tags:            m.inputs[inputIndexForField(tagsField)].Value(),
		Description:     m.inputs[inputIndexForField(descriptionField)].Value(),
		JumpHostID:      jumpHostID,
		ProxyType:       proxyType,
		ProxyHost:       proxyHost,
		ProxyPort:       proxyPort,
		ProxyUser:       proxyUser,
		ProxyPassword:   proxyPassEnc,
		ProxyCommand:    proxyCommand,
		GSSAPISource:    gssapiSource,
		GSSAPIKeytab:    gssapiKeytab,
		KrbPrincipal:    krbPrincipal,
		ForwardAgent:    forwardAgent,
		RemoteCommand:   remoteCommand,
		ExtraSSHOptions: extraSSHOptions,
	}

	database := m.db

	if m.host != nil && m.host.ID > 0 {
		host.Model = m.host.Model
		return func() tea.Msg {
			if err := database.Save(&host).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
			return types.HostSavedMsg{Host: host}
		}
	}

	return func() tea.Msg {
		if err := database.Create(&host).Error; err != nil {
			return types.ErrorMsg{Err: err}
		}
		return types.HostSavedMsg{Host: host}
	}
}
