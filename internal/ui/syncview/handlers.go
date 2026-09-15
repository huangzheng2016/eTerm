package syncview

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.editing >= 0 {
		return m.handleSecretEdit(msg)
	}
	f := m.currentField()
	ks := msg.String()

	switch ks {
	case "tab", "down":
		vf := m.visibleFields()
		m.focused = (m.focused + 1) % len(vf)
		return m, m.focusCurrent()
	case "shift+tab", "up":
		vf := m.visibleFields()
		m.focused = (m.focused - 1 + len(vf)) % len(vf)
		return m, m.focusCurrent()
	case "left":
		if m.isSelector(f) {
			m.handleSelectorLeft(f)
			return m, m.focusCurrent()
		}
	case "right":
		if m.isSelector(f) {
			m.handleSelectorRight(f)
			return m, m.focusCurrent()
		}
	case "enter":
		if isSecretField(f) {
			return m, m.startSecretEdit(m.inputIdxForField(f))
		}
	case "ctrl+s":
		return m, m.save()
	case "f5":
		m.testing = true
		m.err = "Testing..."
		return m, m.testConnection()
	case "ctrl+y":
		return m, m.saveAndSync()
	case "esc":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}

	idx := m.inputIdxForField(f)
	if idx >= 0 && !isSecretField(f) {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) startSecretEdit(idx int) tea.Cmd {
	m.editing = idx
	m.inputs[idx].SetValue("")
	return m.inputs[idx].Focus()
}

func (m *Model) handleSecretEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		v := m.inputs[m.editing].Value()
		if m.editing == inAPIKey {
			m.pendAPIKey, m.apiKeyDirty = v, true
		} else {
			m.pendPass, m.passDirty = v, true
		}
		m.endSecretEdit()
		return m, nil
	case "esc":
		m.endSecretEdit()
		return m, nil
	}
	var cmd tea.Cmd
	m.inputs[m.editing], cmd = m.inputs[m.editing].Update(msg)
	return m, cmd
}

func (m *Model) endSecretEdit() {
	m.inputs[m.editing].SetValue("")
	m.inputs[m.editing].Blur()
	m.editing = -1
}

func (m *Model) saveAndSync() tea.Cmd {
	saveCmd := m.save()
	if saveCmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := saveCmd()
		if _, ok := msg.(types.SuccessMsg); ok {
			return types.SyncStartMsg{}
		}
		return msg
	}
}

func (m *Model) handleSelectorLeft(f int) {
	switch f {
	case fieldEnabled:
		m.enableIdx = (m.enableIdx - 1 + len(enableOptions)) % len(enableOptions)
	case fieldMode:
		m.modeIdx = (m.modeIdx - 1 + len(modeOptions)) % len(modeOptions)
		m.clampFocus()
	case fieldInsecureTLS:
		m.insecureIdx = (m.insecureIdx - 1 + len(insecureOptions)) % len(insecureOptions)
	case fieldSSHHost:
		if m.hostIdx > -1 {
			m.hostIdx--
		}
	}
}

func (m *Model) handleSelectorRight(f int) {
	switch f {
	case fieldEnabled:
		m.enableIdx = (m.enableIdx + 1) % len(enableOptions)
	case fieldMode:
		m.modeIdx = (m.modeIdx + 1) % len(modeOptions)
		m.clampFocus()
	case fieldInsecureTLS:
		m.insecureIdx = (m.insecureIdx + 1) % len(insecureOptions)
	case fieldSSHHost:
		if m.hostIdx < len(m.hostOpts)-1 {
			m.hostIdx++
		}
	}
}

func (m *Model) clampFocus() {
	vf := m.visibleFields()
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
}

func (m *Model) save() tea.Cmd {
	if m.enableIdx == 1 {
		if m.modeIdx == 1 && m.hostIdx < 0 {
			m.err = "SSH Host is required"
			return nil
		}
		if m.modeIdx == 0 && m.inputs[inServerURL].Value() == "" {
			m.err = "Server URL is required"
			return nil
		}
		if m.effectivePass() == "" {
			m.err = "Passphrase is required"
			return nil
		}
		if m.modeIdx == 1 {
			if p := m.inputs[inRemotePort].Value(); p != "" {
				if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
					m.err = "Remote Port must be 1-65535"
					return nil
				}
			}
		}
	}
	m.err = ""

	database := m.db
	mk := m.masterKey

	enabled := "false"
	if m.enableIdx == 1 {
		enabled = "true"
	}
	mode := "http"
	if m.modeIdx == 1 {
		mode = "ssh"
	}
	insecureTLS := "false"
	if m.insecureIdx == 1 {
		insecureTLS = "true"
	}

	hostID := ""
	if m.hostIdx >= 0 && m.hostIdx < len(m.hostOpts) {
		hostID = strconv.Itoa(int(m.hostOpts[m.hostIdx].ID))
	}
	remotePort := m.inputs[inRemotePort].Value()
	if remotePort == "" {
		remotePort = "18443"
	}
	serverURL := m.inputs[inServerURL].Value()
	apiKeyDirty, passDirty := m.apiKeyDirty, m.passDirty
	apiKeyPend, passPend := m.pendAPIKey, m.pendPass
	interval := m.inputs[inInterval].Value()

	return func() tea.Msg {
		set := func(key, value string) error {
			return db.SetSetting(database, key, value)
		}
		for _, kv := range [][2]string{
			{"sync_enabled", enabled},
			{"sync_mode", mode},
			{"sync_ssh_host_id", hostID},
			{"sync_remote_port", remotePort},
			{"sync_server_url", serverURL},
			{"sync_insecure_tls", insecureTLS},
			{"sync_interval", interval},
		} {
			if err := set(kv[0], kv[1]); err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("save sync settings: %w", err)}
			}
		}

		if apiKeyDirty || passDirty {
			k := mk.GetKey()
			if k == nil {
				return types.ErrorMsg{Err: fmt.Errorf("master key required to save secrets")}
			}
			defer k.Clear()
			for _, kv := range []struct {
				key   string
				value string
				dirty bool
			}{
				{"sync_api_key", apiKeyPend, apiKeyDirty},
				{"sync_passphrase", passPend, passDirty},
			} {
				if !kv.dirty {
					continue
				}
				if kv.value == "" {
					if err := set(kv.key, ""); err != nil {
						return types.ErrorMsg{Err: fmt.Errorf("save %s: %w", kv.key, err)}
					}
					continue
				}
				enc, err := security.Encrypt([]byte(kv.value), k.Bytes())
				if err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("encrypt %s: %w", kv.key, err)}
				}
				if err := set(kv.key, enc); err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("save %s: %w", kv.key, err)}
				}
			}
		}

		devID, _ := db.GetSetting(database, "sync_device_id")
		if devID == "" {
			if err := set("sync_device_id", uuid.New().String()); err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("save sync_device_id: %w", err)}
			}
		}

		return types.SuccessMsg{Message: "Sync settings saved"}
	}
}

func (m *Model) testConnection() tea.Cmd {
	serverURL := m.inputs[inServerURL].Value()
	apiKey := m.effectiveAPIKey()
	insecureTLS := m.insecureIdx == 1
	mode := m.modeIdx
	remotePort, _ := strconv.Atoi(m.inputs[inRemotePort].Value())
	if remotePort <= 0 {
		remotePort = 18443
	}
	passphrase := m.effectivePass()
	hostIdx := m.hostIdx
	hostOpts := m.hostOpts
	database := m.db
	mk := m.masterKey

	return func() tea.Msg {
		if mode == 1 {
			if hostIdx < 0 || hostIdx >= len(hostOpts) {
				return types.SyncTestResultMsg{OK: false, Err: fmt.Errorf("no SSH host selected")}
			}
			if mk.IsLocked() {
				return types.SyncTestResultMsg{OK: false, Err: fmt.Errorf("master key locked")}
			}
			tunnel, err := esync.OpenTunnel(database, mk, hostOpts[hostIdx].ID, remotePort)
			if err != nil {
				return types.SyncTestResultMsg{OK: false, Err: err}
			}
			defer tunnel.Close()
			tr := esync.NewHTTPTransportWithOptions(tunnel.BaseURL(), apiKey, esync.TenantIDFromPassphrase(passphrase), false)
			defer tr.Close()
			if err := tr.Ping(); err != nil {
				return types.SyncTestResultMsg{OK: false, Err: fmt.Errorf("ping: %w", err)}
			}
			return types.SyncTestResultMsg{OK: true}
		}
		tr := esync.NewHTTPTransportWithOptions(serverURL, apiKey, "", insecureTLS)
		defer tr.Close()
		if err := tr.Ping(); err != nil {
			return types.SyncTestResultMsg{Err: err}
		}
		return types.SyncTestResultMsg{OK: true}
	}
}
