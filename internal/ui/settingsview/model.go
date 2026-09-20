package settingsview

import (
	"encoding/json"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

type editState int

const (
	stateNormal editState = iota
	stateCapture
	stateAppend
	stateShell
	stateTmuxConfig
	stateShareHours
)

type bindingEntry struct {
	Category string
	Label    string
	Field    string
	Keys     []string
}

type Model struct {
	db       *gorm.DB
	cursor   int
	state    editState
	width    int
	height   int
	scroll   int
	modified bool

	confirmReset components.ConfirmModel

	saveSessionTranscript bool
	replaySessions        bool
	gridStatusWords       bool
	localTerminalShell    string
	shellInput            textinput.Model
	tmuxConfigFile        string
	tmuxConfigInput       textinput.Model
	shareMaxHours         string
	shareHoursInput       textinput.Model
	shareHoursErr         string
	noPasswordMode        bool
	pwd                   *passwordOverlay
}

func New(database *gorm.DB, noPasswordMode bool) *Model {
	m := &Model{
		db:             database,
		noPasswordMode: noPasswordMode,
		confirmReset:   components.NewConfirm("Reset settings", "Restore all preferences to factory defaults?"),
	}
	m.saveSessionTranscript = loadSaveSessionTranscript(database)
	m.replaySessions = loadReplaySessions(database)
	m.gridStatusWords = loadGridStatusWords(database)
	m.localTerminalShell = loadLocalTerminalShell(database)
	m.tmuxConfigFile = loadTmuxConfigFile(database)
	m.shareMaxHours = loadShareMaxHours(database)
	ti := textinput.New()
	ti.Placeholder = localterm.DefaultShell("")
	ti.CharLimit = 512
	m.shellInput = ti
	ti = textinput.New()
	ti.CharLimit = 512
	m.tmuxConfigInput = ti
	ti = textinput.New()
	ti.Placeholder = "4"
	ti.CharLimit = 3
	m.shareHoursInput = ti
	return m
}

func loadReplaySessions(gdb *gorm.DB) bool {
	s, err := db.GetSetting(gdb, "session_capture_mode")
	return err != nil || s != "transcript"
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

func loadLocalTerminalShell(gdb *gorm.DB) string {
	s, err := db.GetSetting(gdb, localterm.SettingShell)
	if err != nil {
		return ""
	}
	return s
}

func loadTmuxConfigFile(gdb *gorm.DB) string {
	s, err := db.GetSetting(gdb, "tmux_config_file")
	if err != nil {
		return ""
	}
	return s
}

func loadShareMaxHours(gdb *gorm.DB) string {
	s, err := db.GetSetting(gdb, "share_max_hours")
	if err != nil {
		return "4"
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 168 {
		return "4"
	}
	return strconv.Itoa(n)
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func bindingDefs() []bindingEntry {
	return []bindingEntry{
		{Category: "Global", Label: "Quit App", Field: "quit_app"},
		{Category: "Global", Label: "Quit", Field: "quit"},
		{Category: "Global", Label: "Help", Field: "help"},
		{Category: "Global", Label: "SSH Keys Tab", Field: "new_tab"},
		{Category: "Global", Label: "Close Tab", Field: "close_tab"},
		{Category: "Global", Label: "Close Tab (Safe)", Field: "close_tab_safe"},
		{Category: "Global", Label: "Next Tab", Field: "next_tab"},
		{Category: "Global", Label: "Prev Tab", Field: "prev_tab"},
		{Category: "Global", Label: "Tab Page Left", Field: "tab_page_left"},
		{Category: "Global", Label: "Tab Page Right", Field: "tab_page_right"},
		{Category: "Global", Label: "Lock", Field: "lock"},
		{Category: "Global", Label: "Lock App", Field: "lock_app"},
		{Category: "Global", Label: "Repaint", Field: "repaint"},
		{Category: "Global", Label: "Forwards Tab", Field: "forward_tab"},
		{Category: "Global", Label: "Snippets Tab", Field: "snippets_tab"},
		{Category: "Global", Label: "Command Palette", Field: "command_palette"},
		{Category: "Global", Label: "AI Overlay", Field: "ai_overlay"},
		{Category: "Global", Label: "Voice Input", Field: "voice_input"},
		{Category: "Global", Label: "Local Terminal", Field: "local_terminal"},
		{Category: "Global", Label: "Rename Tab", Field: "rename_tab"},
		{Category: "Global", Label: "Paste Blob URL", Field: "paste_blob_url"},
		{Category: "Global", Label: "Toggle Chrome", Field: "toggle_chrome"},
		{Category: "Home", Label: "SSH Connect", Field: "ssh_connect"},
		{Category: "Home", Label: "SFTP Open", Field: "sftp_open"},
		{Category: "Home", Label: "New Host", Field: "new_host"},
		{Category: "Home", Label: "Edit Host", Field: "edit_host"},
		{Category: "Home", Label: "Delete Host", Field: "delete_host"},
		{Category: "Home", Label: "Search", Field: "search"},
		{Category: "Home", Label: "Copy SSH", Field: "copy_ssh"},
		{Category: "Home", Label: "Clone Host", Field: "clone_host"},
		{Category: "Home", Label: "Toggle View", Field: "toggle_view"},
		{Category: "Home", Label: "Quick Connect", Field: "quick_connect"},
		{Category: "Home", Label: "Show Hidden", Field: "show_hidden"},
		{Category: "Home", Label: "Hide Host", Field: "hide_host"},
		{Category: "Home", Label: "Snippet Picker", Field: "snippet_picker"},
		{Category: "Home", Label: "Session Log", Field: "session_history"},
		{Category: "Home", Label: "Toggle Select", Field: "toggle_select"},
		{Category: "Home", Label: "Batch Tag", Field: "batch_tag"},
		{Category: "Home", Label: "Batch Actions", Field: "batch_actions"},
		{Category: "Home", Label: "tmux Menu", Field: "tmux_menu"},
		{Category: "Home", Label: "SSH tmux", Field: "ssh_tmux"},
		{Category: "SFTP", Label: "Upload", Field: "sftp_upload"},
		{Category: "SFTP", Label: "Download", Field: "sftp_download"},
		{Category: "SFTP", Label: "Delete", Field: "sftp_delete"},
		{Category: "SFTP", Label: "Mkdir", Field: "sftp_mkdir"},
		{Category: "SFTP", Label: "Rename", Field: "sftp_rename"},
		{Category: "SFTP", Label: "Chmod", Field: "sftp_chmod"},
		{Category: "SFTP", Label: "Switch Left", Field: "sftp_switch_left"},
		{Category: "SFTP", Label: "Switch Right", Field: "sftp_switch_right"},
		{Category: "Keys", Label: "New Key", Field: "key_new"},
		{Category: "Keys", Label: "Import Key", Field: "key_import"},
		{Category: "Keys", Label: "Edit Key", Field: "key_edit"},
		{Category: "Keys", Label: "Delete Key", Field: "key_delete"},
		{Category: "Keys", Label: "Copy Fingerprint", Field: "key_copy"},
		{Category: "Forward", Label: "Start", Field: "fwd_start"},
		{Category: "Forward", Label: "Stop", Field: "fwd_stop"},
		{Category: "Forward", Label: "New Rule", Field: "fwd_new"},
		{Category: "Forward", Label: "Edit Rule", Field: "fwd_edit"},
		{Category: "Forward", Label: "Delete Rule", Field: "fwd_delete"},
		{Category: "Snippet", Label: "New Snippet", Field: "snip_new"},
		{Category: "Snippet", Label: "Edit Snippet", Field: "snip_edit"},
		{Category: "Snippet", Label: "Delete Snippet", Field: "snip_delete"},
		{Category: "SSH", Label: "Reconnect", Field: "ssh_reconnect"},
		{Category: "SSH", Label: "Snippet Picker", Field: "ssh_snippet_picker"},
	}
}

func buildEntries(configJSON []byte) []bindingEntry {
	var cfg map[string]json.RawMessage
	_ = json.Unmarshal(configJSON, &cfg)

	defs := bindingDefs()
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

func keyString(msg tea.KeyPressMsg) string {
	k := msg.Key()
	if s := msg.String(); s != "" && s != " " {
		return s
	}
	ks := k.Keystroke()
	if ks != "" {
		return ks
	}
	return msg.String()
}

func visibleRowsFor(height int) int {
	rows := height - 4
	if rows < 5 {
		rows = 5
	}
	return rows
}

func formatKeys(keys []string) string {
	if len(keys) == 0 {
		return "(none)"
	}
	return strings.Join(keys, ", ")
}
