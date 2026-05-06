package settingsview

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
)

type editState int

const (
	stateNormal  editState = iota
	stateCapture           // waiting for user to press a key (replaces)
	stateAppend            // waiting for user to press a key (appends)
)

// bindingEntry represents one configurable keybinding row.
type bindingEntry struct {
	Category string   // e.g. "Global", "Home", "SFTP"
	Label    string   // e.g. "Quit App"
	Field    string   // JSON field name, e.g. "quit_app"
	Keys     []string // current key bindings
}

type Model struct {
	db           *gorm.DB
	entries      []bindingEntry
	cursor       int // 0,1 = prefs; 2+ = entries[cursor-2]
	state        editState
	width        int
	height       int
	scroll       int // scroll offset for long lists
	modified     bool
	defaultsJSON []byte // default config for reset

	saveSessionTranscript bool
	gridStatusWords       bool
	noPasswordMode        bool
	pwd                   *passwordOverlay
}

func New(database *gorm.DB, configJSON []byte, defaultsJSON []byte, noPasswordMode bool) *Model {
	m := &Model{
		db:             database,
		defaultsJSON:   defaultsJSON,
		noPasswordMode: noPasswordMode,
	}
	m.entries = buildEntries(configJSON)
	m.saveSessionTranscript = loadSaveSessionTranscript(database)
	m.gridStatusWords = loadGridStatusWords(database)
	return m
}

func (m *Model) SetNoPasswordMode(v bool) {
	m.noPasswordMode = v
}

func loadSaveSessionTranscript(gdb *gorm.DB) bool {
	s, err := db.GetSetting(gdb, "save_session_transcript")
	if err != nil {
		return true
	}
	return s != "false"
}

func loadGridStatusWords(gdb *gorm.DB) bool {
	s, err := db.GetSetting(gdb, "grid_status_words")
	if err != nil {
		return false
	}
	return s == "true"
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// buildEntries creates the flat list of binding entries from config JSON.
func buildEntries(configJSON []byte) []bindingEntry {
	var cfg map[string]json.RawMessage
	_ = json.Unmarshal(configJSON, &cfg)

	type fieldDef struct {
		Category string
		Label    string
		Field    string
	}

	defs := []fieldDef{
		// Global
		{"Global", "Quit App", "quit_app"},
		{"Global", "Quit", "quit"},
		{"Global", "Help", "help"},
		{"Global", "SSH Keys Tab", "new_tab"},
		{"Global", "Close Tab", "close_tab"},
		{"Global", "Close Tab (Safe)", "close_tab_safe"},
		{"Global", "Next Tab", "next_tab"},
		{"Global", "Prev Tab", "prev_tab"},
		{"Global", "Lock", "lock"},
		{"Global", "Lock App", "lock_app"},
		{"Global", "Forwards Tab", "forward_tab"},
		{"Global", "Snippets Tab", "snippets_tab"},
		// Home
		{"Home", "SSH Connect", "ssh_connect"},
		{"Home", "SFTP Open", "sftp_open"},
		{"Home", "New Host", "new_host"},
		{"Home", "Edit Host", "edit_host"},
		{"Home", "Delete Host", "delete_host"},
		{"Home", "Search", "search"},
		{"Home", "Copy SSH", "copy_ssh"},
		{"Home", "Clone Host", "clone_host"},
		{"Home", "Toggle View", "toggle_view"},
		{"Home", "Quick Connect", "quick_connect"},
		{"Home", "Import SSH", "import_ssh"},
		{"Home", "Export Config", "export_config"},
		{"Home", "Show Hidden", "show_hidden"},
		{"Home", "Hide Host", "hide_host"},
		{"Home", "Snippet Picker", "snippet_picker"},
		{"Home", "Session Log", "session_history"},
		{"Home", "Toggle Select", "toggle_select"},
		{"Home", "Batch Tag", "batch_tag"},
		{"Home", "Batch Actions", "batch_actions"},
		// SFTP
		{"SFTP", "Upload", "sftp_upload"},
		{"SFTP", "Download", "sftp_download"},
		{"SFTP", "Delete", "sftp_delete"},
		{"SFTP", "Mkdir", "sftp_mkdir"},
		{"SFTP", "Rename", "sftp_rename"},
		{"SFTP", "Chmod", "sftp_chmod"},
		{"SFTP", "Switch Left", "sftp_switch_left"},
		{"SFTP", "Switch Right", "sftp_switch_right"},
		// Keys
		{"Keys", "New Key", "key_new"},
		{"Keys", "Import Key", "key_import"},
		{"Keys", "Export Key", "key_export"},
		{"Keys", "Delete Key", "key_delete"},
		{"Keys", "Copy Fingerprint", "key_copy"},
		// Forward
		{"Forward", "Start", "fwd_start"},
		{"Forward", "Stop", "fwd_stop"},
		{"Forward", "New Rule", "fwd_new"},
		{"Forward", "Edit Rule", "fwd_edit"},
		{"Forward", "Delete Rule", "fwd_delete"},
		// Snippet
		{"Snippet", "New Snippet", "snip_new"},
		{"Snippet", "Edit Snippet", "snip_edit"},
		{"Snippet", "Delete Snippet", "snip_delete"},
		// SSH
		{"SSH", "Reconnect", "ssh_reconnect"},
		{"SSH", "Snippet Picker", "ssh_snippet_picker"},
	}

	entries := make([]bindingEntry, 0, len(defs))
	for _, d := range defs {
		var keys []string
		if raw, ok := cfg[d.Field]; ok {
			_ = json.Unmarshal(raw, &keys)
		}
		entries = append(entries, bindingEntry{
			Category: d.Category,
			Label:    d.Label,
			Field:    d.Field,
			Keys:     keys,
		})
	}
	return entries
}

// ConfigJSON returns the current config as JSON bytes.
func (m *Model) ConfigJSON() []byte {
	result := make(map[string][]string)
	for _, e := range m.entries {
		result[e.Field] = e.Keys
	}
	data, _ := json.Marshal(result)
	return data
}

// keyString converts a tea.KeyPressMsg to a human-readable key string.
func keyString(msg tea.KeyPressMsg) string {
	k := msg.Key()
	ks := k.Keystroke()
	if ks != "" {
		return ks
	}
	return msg.String()
}

// visibleRows returns how many rows fit in the viewport.
func (m *Model) visibleRows() int {
	// Reserve 4 lines for header + footer
	rows := m.height - 4
	if rows < 5 {
		rows = 5
	}
	return rows
}

// formatKeys formats a key list for display.
func formatKeys(keys []string) string {
	if len(keys) == 0 {
		return "(none)"
	}
	return strings.Join(keys, ", ")
}
