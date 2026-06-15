package syncview

import (
	"strconv"

	"charm.land/bubbles/v2/textinput"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

func New(database *gorm.DB, mk *security.MasterKeyManager) *Model {
	m := &Model{db: database, masterKey: mk, hostIdx: -1}

	m.inputs[inRemoteBin] = textinput.New()
	m.inputs[inRemoteBin].Placeholder = "etermsyncd"

	m.inputs[inRemoteDB] = textinput.New()
	m.inputs[inRemoteDB].Placeholder = "~/.config/etermsyncd/sync.db"

	m.inputs[inServerURL] = textinput.New()
	m.inputs[inServerURL].Placeholder = "https://sync.example.com"

	m.inputs[inAPIKey] = textinput.New()
	m.inputs[inAPIKey].Placeholder = "Bearer token"
	m.inputs[inAPIKey].EchoMode = textinput.EchoPassword
	m.inputs[inAPIKey].EchoCharacter = '*'

	m.inputs[inPassphrase] = textinput.New()
	m.inputs[inPassphrase].Placeholder = "sync encryption passphrase"
	m.inputs[inPassphrase].EchoMode = textinput.EchoPassword
	m.inputs[inPassphrase].EchoCharacter = '*'

	m.inputs[inInterval] = textinput.New()
	m.inputs[inInterval].Placeholder = "300"

	m.syncInputWidths()
	m.loadFromDB()
	return m
}

func (m *Model) syncInputWidths() {
	w := inputInnerWidth
	if m.width > 0 && m.width < 58 {
		w = max(12, m.width-22)
	}
	for i := range m.inputs {
		m.inputs[i].SetWidth(w)
	}
}

func (m *Model) loadFromDB() {
	get := func(key, def string) string {
		v, err := db.GetSetting(m.db, key)
		if err != nil || v == "" {
			return def
		}
		return v
	}
	decrypt := func(key string) string {
		v, err := db.GetSetting(m.db, key)
		if err != nil || v == "" {
			return ""
		}
		k := m.masterKey.GetKey()
		if k == nil {
			return ""
		}
		defer k.Clear()
		plain, err := security.Decrypt(v, k.Bytes())
		if err != nil {
			return ""
		}
		return string(plain)
	}

	if get("sync_enabled", "false") == "true" {
		m.enableIdx = 1
	}
	switch get("sync_mode", "http") {
	case "https":
		m.modeIdx = 1
	case "ssh":
		m.modeIdx = 2
	default:
		m.modeIdx = 0
	}

	m.inputs[inRemoteBin].SetValue(get("sync_remote_bin", ""))
	m.inputs[inRemoteDB].SetValue(get("sync_remote_db", ""))
	m.inputs[inServerURL].SetValue(get("sync_server_url", ""))
	m.inputs[inAPIKey].SetValue(decrypt("sync_api_key"))
	m.inputs[inPassphrase].SetValue(decrypt("sync_passphrase"))
	m.inputs[inInterval].SetValue(get("sync_interval", ""))

	// Load hosts for selector
	m.db.Order("alias").Find(&m.hostOpts)
	hostIDStr := get("sync_ssh_host_id", "0")
	if hid, err := strconv.ParseUint(hostIDStr, 10, 64); err == nil && hid > 0 {
		for i, h := range m.hostOpts {
			if h.ID == uint(hid) {
				m.hostIdx = i
				break
			}
		}
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.syncInputWidths()
}
