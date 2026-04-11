package editor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/types"
)

type editorDataLoadedMsg struct {
	keys  []db.SSHKey
	hosts []db.Host
	err   error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadEditorData(), textinput.Blink)
}

func (m Model) loadEditorData() tea.Cmd {
	return func() tea.Msg {
		var keys []db.SSHKey
		var hosts []db.Host
		errK := m.db.Order("name").Find(&keys).Error
		errH := m.db.Order("alias").Find(&hosts).Error
		err := errK
		if errH != nil {
			err = errH
		}
		return editorDataLoadedMsg{keys: keys, hosts: hosts, err: err}
	}
}

func (m Model) currentField() int {
	vf := m.visibleFields()
	if m.focused >= 0 && m.focused < len(vf) {
		return vf[m.focused]
	}
	return -1
}

func (m *Model) blurCurrent() {
	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		m.inputs[idx].Blur()
	}
}

func (m *Model) focusCurrent() tea.Cmd {
	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}

func (m *Model) clampFocus() {
	vf := m.visibleFields()
	if len(vf) == 0 {
		return
	}
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
	if m.focused < 0 {
		m.focused = 0
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editorDataLoadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.keyOptions = msg.keys
			if m.host != nil && m.host.KeyID != nil {
				for i, k := range m.keyOptions {
					if k.ID == *m.host.KeyID {
						m.keyIdx = i
						break
					}
				}
			}
			var opts []db.Host
			for _, h := range msg.hosts {
				if m.host != nil && h.ID > 0 && h.ID == m.host.ID {
					continue
				}
				opts = append(opts, h)
			}
			m.jumpHostOptions = opts
			m.jumpIdx = -1
			if m.host != nil && m.host.JumpHostID != nil {
				for i, jh := range m.jumpHostOptions {
					if jh.ID == *m.host.JumpHostID {
						m.jumpIdx = i
						break
					}
				}
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncInputWidths()
		return m, nil

	case tea.KeyPressMsg:
		vf := m.visibleFields()
		field := m.currentField()

		switch msg.String() {
		case "tab", "down":
			m.blurCurrent()
			m.focused = (m.focused + 1) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "shift+tab", "up":
			m.blurCurrent()
			m.focused = (m.focused - 1 + len(vf)) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "left":
			if field == authMethodField {
				m.authIdx = (m.authIdx - 1 + len(authOptions)) % len(authOptions)
				return m, nil
			}
			if field == keyIDField && len(m.keyOptions) > 0 {
				m.keyIdx = (m.keyIdx - 1 + len(m.keyOptions)) % len(m.keyOptions)
				return m, nil
			}
			if field == jumpHostField {
				n := len(m.jumpHostOptions)
				if n == 0 {
					m.jumpIdx = -1
					return m, nil
				}
				if m.jumpIdx < 0 {
					m.jumpIdx = n - 1
				} else {
					m.jumpIdx--
				}
				return m, nil
			}
			if field == proxyTypeField {
				m.proxyTypeIdx = (m.proxyTypeIdx - 1 + len(proxyOptions)) % len(proxyOptions)
				m.clampFocus()
				return m, nil
			}
			if field == gssapiSourceField {
				m.gssapiSourceIdx = (m.gssapiSourceIdx - 1 + len(gssapiSourceOptions)) % len(gssapiSourceOptions)
				m.clampFocus()
				return m, nil
			}

		case "right":
			if field == authMethodField {
				m.authIdx = (m.authIdx + 1) % len(authOptions)
				return m, nil
			}
			if field == keyIDField && len(m.keyOptions) > 0 {
				m.keyIdx = (m.keyIdx + 1) % len(m.keyOptions)
				return m, nil
			}
			if field == jumpHostField {
				n := len(m.jumpHostOptions)
				if n == 0 {
					m.jumpIdx = -1
					return m, nil
				}
				if m.jumpIdx < 0 {
					m.jumpIdx = 0
				} else if m.jumpIdx < n-1 {
					m.jumpIdx++
				} else {
					m.jumpIdx = -1
				}
				return m, nil
			}
			if field == proxyTypeField {
				m.proxyTypeIdx = (m.proxyTypeIdx + 1) % len(proxyOptions)
				m.clampFocus()
				return m, nil
			}
			if field == gssapiSourceField {
				m.gssapiSourceIdx = (m.gssapiSourceIdx + 1) % len(gssapiSourceOptions)
				m.clampFocus()
				return m, nil
			}

		case "enter":
			if m.focused == len(vf)-1 {
				return m, m.save()
			}
			m.blurCurrent()
			m.focused = (m.focused + 1) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "ctrl+s":
			return m, m.save()

		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}
	}

	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}

	return m, nil
}

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

	host := db.Host{
		Alias:         m.inputs[inputIndexForField(aliasField)].Value(),
		Hostname:      hostname,
		Port:          port,
		Username:      m.inputs[inputIndexForField(usernameField)].Value(),
		AuthMethod:    authMethod,
		Password:      encryptedPassword,
		KeyID:         keyID,
		Group:         m.inputs[inputIndexForField(groupField)].Value(),
		Tags:          m.inputs[inputIndexForField(tagsField)].Value(),
		Description:   m.inputs[inputIndexForField(descriptionField)].Value(),
		JumpHostID:    jumpHostID,
		ProxyType:     proxyType,
		ProxyHost:     proxyHost,
		ProxyPort:     proxyPort,
		ProxyUser:     proxyUser,
		ProxyPassword: proxyPassEnc,
		ProxyCommand:  proxyCommand,
		GSSAPISource:  gssapiSource,
		GSSAPIKeytab:  gssapiKeytab,
		KrbPrincipal:  krbPrincipal,
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
