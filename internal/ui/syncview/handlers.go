package syncview

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "ctrl+s":
		return m, m.save()
	case "f5":
		m.testing = true
		m.err = "Testing..."
		return m, m.testConnection()
	case "ctrl+y":
		return m, func() tea.Msg { return types.SyncStartMsg{} }
	case "esc":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}

	idx := m.inputIdxForField(f)
	if idx >= 0 {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleSelectorLeft(f int) {
	switch f {
	case fieldEnabled:
		m.enableIdx = (m.enableIdx - 1 + len(enableOptions)) % len(enableOptions)
	case fieldMode:
		m.modeIdx = (m.modeIdx - 1 + len(modeOptions)) % len(modeOptions)
		m.clampFocus()
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
		if m.modeIdx == 0 && m.hostIdx < 0 {
			m.err = "SSH Host is required"
			return nil
		}
		if m.modeIdx > 0 && m.inputs[inServerURL].Value() == "" {
			m.err = "Server URL is required"
			return nil
		}
		if m.inputs[inPassphrase].Value() == "" {
			m.err = "Passphrase is required"
			return nil
		}
	}
	m.err = ""

	database := m.db
	mk := m.masterKey

	enabled := "false"
	if m.enableIdx == 1 {
		enabled = "true"
	}
	mode := "ssh"
	if m.modeIdx == 1 {
		mode = "http"
	} else if m.modeIdx == 2 {
		mode = "https"
	}

	hostID := ""
	if m.hostIdx >= 0 && m.hostIdx < len(m.hostOpts) {
		hostID = strconv.Itoa(int(m.hostOpts[m.hostIdx].ID))
	}
	remoteBin := m.inputs[inRemoteBin].Value()
	remoteDB := m.inputs[inRemoteDB].Value()
	serverURL := m.inputs[inServerURL].Value()
	apiKeyPlain := m.inputs[inAPIKey].Value()
	passPlain := m.inputs[inPassphrase].Value()
	interval := m.inputs[inInterval].Value()

	return func() tea.Msg {
		set := func(key, value string) error {
			return db.SetSetting(database, key, value)
		}
		for _, kv := range [][2]string{
			{"sync_enabled", enabled},
			{"sync_mode", mode},
			{"sync_ssh_host_id", hostID},
			{"sync_remote_bin", remoteBin},
			{"sync_remote_db", remoteDB},
			{"sync_server_url", serverURL},
			{"sync_interval", interval},
		} {
			if err := set(kv[0], kv[1]); err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("save sync settings: %w", err)}
			}
		}

		if apiKeyPlain != "" || passPlain != "" {
			k := mk.GetKey()
			if k == nil {
				return types.ErrorMsg{Err: fmt.Errorf("master key required to save secrets")}
			}
			defer k.Clear()
			for _, kv := range [][2]string{
				{"sync_api_key", apiKeyPlain},
				{"sync_passphrase", passPlain},
			} {
				if kv[1] == "" {
					continue
				}
				enc, err := security.Encrypt([]byte(kv[1]), k.Bytes())
				if err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("encrypt %s: %w", kv[0], err)}
				}
				if err := set(kv[0], enc); err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("save %s: %w", kv[0], err)}
				}
			}
		} else {
			k := mk.GetKey()
			if k != nil {
				defer k.Clear()
				if err := set("sync_api_key", ""); err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("save sync_api_key: %w", err)}
				}
				if err := set("sync_passphrase", ""); err != nil {
					return types.ErrorMsg{Err: fmt.Errorf("save sync_passphrase: %w", err)}
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
	apiKey := m.inputs[inAPIKey].Value()
	mode := m.modeIdx

	return func() tea.Msg {
		if mode == 0 {
			return types.SyncTestResultMsg{OK: false, Err: fmt.Errorf("SSH test: use F5 after saving, then trigger manual sync with Ctrl+Y")}
		}
		url := serverURL + "/api/v1/ping"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return types.SyncTestResultMsg{Err: err}
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return types.SyncTestResultMsg{Err: err}
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return types.SyncTestResultMsg{OK: true}
		}
		return types.SyncTestResultMsg{Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
}
