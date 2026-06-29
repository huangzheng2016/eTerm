package app

import (
	"encoding/json"
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/keymatch"
	"github.com/huangzheng2016/eTerm/internal/ui/home"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
	"gorm.io/gorm"
)

const keybindingsSettingKey = "keybindings"

// KeyBindingConfig holds all configurable keybindings as string slices (each entry is a key combo).
type KeyBindingConfig struct {
	// Global
	QuitApp        []string `json:"quit_app"`
	Quit           []string `json:"quit"`
	Help           []string `json:"help"`
	NewTab         []string `json:"new_tab"`
	CloseTab       []string `json:"close_tab"`
	CloseTabSafe   []string `json:"close_tab_safe"`
	NextTab        []string `json:"next_tab"`
	PrevTab        []string `json:"prev_tab"`
	Lock           []string `json:"lock"`
	LockApp        []string `json:"lock_app"`
	ForwardTab     []string `json:"forward_tab"`
	SnippetsTab    []string `json:"snippets_tab"`
	CommandPalette []string `json:"command_palette"`
	LocalTerminal  []string `json:"local_terminal"`
	PasteImageURL  []string `json:"paste_image_url"`

	// Home
	SSHConnect     []string `json:"ssh_connect"`
	SFTPOpen       []string `json:"sftp_open"`
	NewHost        []string `json:"new_host"`
	EditHost       []string `json:"edit_host"`
	DeleteHost     []string `json:"delete_host"`
	Search         []string `json:"search"`
	CopySSH        []string `json:"copy_ssh"`
	CloneHost      []string `json:"clone_host"`
	ToggleView     []string `json:"toggle_view"`
	QuickConnect   []string `json:"quick_connect"`
	ImportSSH      []string `json:"import_ssh"`
	ExportConfig   []string `json:"export_config"`
	ShowHidden     []string `json:"show_hidden"`
	HideHost       []string `json:"hide_host"`
	SnippetPicker  []string `json:"snippet_picker"`
	SessionHistory []string `json:"session_history"`
	ToggleSelect   []string `json:"toggle_select"`
	BatchTag       []string `json:"batch_tag"`
	BatchActions   []string `json:"batch_actions"`
	TmuxMenu       []string `json:"tmux_menu"`

	// SFTP
	SFTPUpload      []string `json:"sftp_upload"`
	SFTPDownload    []string `json:"sftp_download"`
	SFTPDelete      []string `json:"sftp_delete"`
	SFTPMkdir       []string `json:"sftp_mkdir"`
	SFTPRename      []string `json:"sftp_rename"`
	SFTPChmod       []string `json:"sftp_chmod"`
	SFTPSwitchLeft  []string `json:"sftp_switch_left"`
	SFTPSwitchRight []string `json:"sftp_switch_right"`

	// Key management
	KeyNew    []string `json:"key_new"`
	KeyImport []string `json:"key_import"`
	KeyExport []string `json:"key_export"`
	KeyDelete []string `json:"key_delete"`
	KeyCopy   []string `json:"key_copy"`

	// Forward
	FwdStart  []string `json:"fwd_start"`
	FwdStop   []string `json:"fwd_stop"`
	FwdNew    []string `json:"fwd_new"`
	FwdEdit   []string `json:"fwd_edit"`
	FwdDelete []string `json:"fwd_delete"`

	// Snippet
	SnipNew    []string `json:"snip_new"`
	SnipEdit   []string `json:"snip_edit"`
	SnipDelete []string `json:"snip_delete"`

	// SSH
	SSHReconnect     []string `json:"ssh_reconnect"`
	SSHSnippetPicker []string `json:"ssh_snippet_picker"`
}

func DefaultKeyBindingConfig() KeyBindingConfig {
	return KeyBindingConfig{
		// Global
		QuitApp:        []string{"ctrl+shift+q", "ctrl+shift+c"},
		Quit:           []string{"ctrl+c"},
		Help:           []string{"?"},
		NewTab:         []string{"ctrl+t"},
		CloseTab:       []string{"ctrl+w"},
		CloseTabSafe:   []string{"ctrl+shift+w"},
		NextTab:        []string{"ctrl+tab", "ctrl+pgdown", "alt+n", "ctrl+right"},
		PrevTab:        []string{"ctrl+shift+tab", "ctrl+pgup", "alt+p", "ctrl+left"},
		Lock:           []string{"ctrl+l"},
		LockApp:        []string{"ctrl+shift+l"},
		ForwardTab:     []string{"ctrl+p"},
		SnippetsTab:    []string{"ctrl+shift+b"},
		CommandPalette: []string{"ctrl+k"},
		LocalTerminal:  []string{"ctrl+shift+t"},
		PasteImageURL:  []string{"ctrl+shift+i"},

		// Home
		SSHConnect:     []string{"enter"},
		SFTPOpen:       []string{"ctrl+f", "s"},
		NewHost:        []string{"n"},
		EditHost:       []string{"e"},
		DeleteHost:     []string{"d"},
		Search:         []string{"/"},
		CopySSH:        []string{"c"},
		CloneHost:      []string{"C"},
		ToggleView:     []string{"t"},
		QuickConnect:   []string{"q"},
		ImportSSH:      []string{"I"},
		ExportConfig:   []string{"E"},
		ShowHidden:     []string{"h"},
		HideHost:       []string{"H"},
		SnippetPicker:  []string{"ctrl+shift+s"},
		SessionHistory: []string{"ctrl+shift+h"},
		ToggleSelect:   []string{"ctrl+space"},
		BatchTag:       []string{"ctrl+shift+g"},
		BatchActions:   []string{"ctrl+shift+m"},
		TmuxMenu:       []string{"m"},

		// SFTP
		SFTPUpload:      []string{"u"},
		SFTPDownload:    []string{"d"},
		SFTPDelete:      []string{"delete", "x"},
		SFTPMkdir:       []string{"m"},
		SFTPRename:      []string{"r"},
		SFTPChmod:       []string{"p"},
		SFTPSwitchLeft:  []string{"left", "h"},
		SFTPSwitchRight: []string{"right", "l"},

		// Key management
		KeyNew:    []string{"n"},
		KeyImport: []string{"i"},
		KeyExport: []string{"e"},
		KeyDelete: []string{"d"},
		KeyCopy:   []string{"c"},

		// Forward
		FwdStart:  []string{"enter"},
		FwdStop:   []string{"x"},
		FwdNew:    []string{"n"},
		FwdEdit:   []string{"e"},
		FwdDelete: []string{"d"},

		// Snippet
		SnipNew:    []string{"n"},
		SnipEdit:   []string{"e"},
		SnipDelete: []string{"d"},

		// SSH
		SSHReconnect:     []string{"r"},
		SSHSnippetPicker: []string{"ctrl+shift+s", "ctrl+S"},
	}
}

func LoadKeyBindingConfig(database *gorm.DB) KeyBindingConfig {
	cfg := DefaultKeyBindingConfig()
	val, err := db.GetSetting(database, keybindingsSettingKey)
	if err != nil || val == "" {
		return cfg
	}
	// Unmarshal on top of defaults so missing fields keep their default values.
	_ = json.Unmarshal([]byte(val), &cfg)
	return cfg
}

func SaveKeyBindingConfig(database *gorm.DB, cfg KeyBindingConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return db.SetSetting(database, keybindingsSettingKey, string(data))
}

func firstKey(keys []string) string {
	if len(keys) > 0 {
		return keys[0]
	}
	return ""
}

func helpLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return shortKeyLabel(keys[0])
	}
	return shortKeyLabel(keys[0]) + "/" + shortKeyLabel(keys[1])
}

func shortKeyLabel(k string) string {
	parts := strings.Split(k, "+")
	for i, p := range parts {
		switch strings.ToLower(p) {
		case "ctrl":
			parts[i] = "C"
		case "shift":
			parts[i] = "S"
		case "alt":
			parts[i] = "A"
		case "meta":
			parts[i] = "M"
		default:
			parts[i] = p
		}
	}
	return strings.Join(parts, "-")
}

// BuildKeyMap constructs the global KeyMap from a KeyBindingConfig.
func BuildKeyMap(cfg KeyBindingConfig) KeyMap {
	return KeyMap{
		QuitApp: key.NewBinding(
			key.WithKeys(cfg.QuitApp...),
			key.WithHelp(helpLabel(cfg.QuitApp), "quit"),
		),
		Quit: key.NewBinding(
			key.WithKeys(cfg.Quit...),
			key.WithHelp(helpLabel(cfg.Quit), "quit (list only; SSH sends to host)"),
		),
		Help: key.NewBinding(
			key.WithKeys(cfg.Help...),
			key.WithHelp(helpLabel(cfg.Help), "help"),
		),
		NewTab: key.NewBinding(
			key.WithKeys(cfg.NewTab...),
			key.WithHelp(helpLabel(cfg.NewTab), "SSH keys"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys(cfg.CloseTab...),
			key.WithHelp(helpLabel(cfg.CloseTab), "close tab"),
		),
		CloseTabSafe: key.NewBinding(
			key.WithKeys(cfg.CloseTabSafe...),
			key.WithHelp(helpLabel(cfg.CloseTabSafe), "close tab"),
		),
		NextTab: key.NewBinding(
			key.WithKeys(cfg.NextTab...),
			key.WithHelp(helpLabel(cfg.NextTab), "next"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys(cfg.PrevTab...),
			key.WithHelp(helpLabel(cfg.PrevTab), "prev"),
		),
		SSHConnect: key.NewBinding(
			key.WithKeys(cfg.SSHConnect...),
			key.WithHelp(helpLabel(cfg.SSHConnect), "connect"),
		),
		SFTPOpen: key.NewBinding(
			key.WithKeys(cfg.SFTPOpen...),
			key.WithHelp(helpLabel(cfg.SFTPOpen), "open sftp"),
		),
		NewHost: key.NewBinding(
			key.WithKeys(cfg.NewHost...),
			key.WithHelp(helpLabel(cfg.NewHost), "new host"),
		),
		EditHost: key.NewBinding(
			key.WithKeys(cfg.EditHost...),
			key.WithHelp(helpLabel(cfg.EditHost), "edit host"),
		),
		DeleteHost: key.NewBinding(
			key.WithKeys(cfg.DeleteHost...),
			key.WithHelp(helpLabel(cfg.DeleteHost), "delete host"),
		),
		Search: key.NewBinding(
			key.WithKeys(cfg.Search...),
			key.WithHelp(helpLabel(cfg.Search), "search"),
		),
		Lock: key.NewBinding(
			key.WithKeys(cfg.Lock...),
			key.WithHelp(helpLabel(cfg.Lock), "lock"),
		),
		LockApp: key.NewBinding(
			key.WithKeys(cfg.LockApp...),
			key.WithHelp(helpLabel(cfg.LockApp), "lock"),
		),
		ForwardTab: key.NewBinding(
			key.WithKeys(cfg.ForwardTab...),
			key.WithHelp(helpLabel(cfg.ForwardTab), "port fwds"),
		),
		SnippetsTab: key.NewBinding(
			key.WithKeys(cfg.SnippetsTab...),
			key.WithHelp(helpLabel(cfg.SnippetsTab), "snippets"),
		),
		CommandPalette: key.NewBinding(
			key.WithKeys(cfg.CommandPalette...),
			key.WithHelp(helpLabel(cfg.CommandPalette), "commands"),
		),
		LocalTerminal: key.NewBinding(
			key.WithKeys(cfg.LocalTerminal...),
			key.WithHelp(helpLabel(cfg.LocalTerminal), "local shell"),
		),
		PasteImageURL: key.NewBinding(
			key.WithKeys(cfg.PasteImageURL...),
			key.WithHelp(helpLabel(cfg.PasteImageURL), "paste image url"),
		),
	}
}

// BuildKeymatchConfig constructs a keymatch.Config from a KeyBindingConfig.
func BuildKeymatchConfig(cfg KeyBindingConfig) keymatch.Config {
	return keymatch.Config{
		ConnectKeys: cfg.SSHConnect,
		SFTPKeys:    cfg.SFTPOpen,
		NewHostKey:  firstRune(cfg.NewHost),
		NewHostName: firstKey(cfg.NewHost),
		EditKey:     firstRune(cfg.EditHost),
		EditName:    firstKey(cfg.EditHost),
		DeleteKey:   firstRune(cfg.DeleteHost),
		DeleteName:  firstKey(cfg.DeleteHost),
		CopyKey:     firstRune(cfg.CopySSH),
		CopyName:    firstKey(cfg.CopySSH),
		SearchKey:   firstRune(cfg.Search),
		SearchName:  firstKey(cfg.Search),
	}
}

func firstRune(keys []string) rune {
	if len(keys) > 0 && len(keys[0]) > 0 {
		runes := []rune(keys[0])
		return runes[0]
	}
	return 0
}

// BuildHomeKeyConfig constructs the home view key config from a KeyBindingConfig.
func BuildHomeKeyConfig(cfg KeyBindingConfig) home.HomeKeyConfig {
	return home.HomeKeyConfig{
		KmCfg: BuildKeymatchConfig(cfg),
		Keys: home.BuildListKeyMap(cfg.SSHConnect, cfg.SFTPOpen, cfg.NewHost, cfg.EditHost,
			cfg.DeleteHost, cfg.CopySSH, cfg.CloneHost, cfg.Search, cfg.ToggleView, cfg.TmuxMenu),
		Help:           cfg.Help,
		QuickConnect:   cfg.QuickConnect,
		ImportSSH:      cfg.ImportSSH,
		ExportConfig:   cfg.ExportConfig,
		ShowHidden:     cfg.ShowHidden,
		HideHost:       cfg.HideHost,
		SessionHistory: cfg.SessionHistory,
		ToggleSelect:   cfg.ToggleSelect,
		BatchTag:       cfg.BatchTag,
		BatchActions:   cfg.BatchActions,
		Tmux:           cfg.TmuxMenu,
	}
}

func BuildSFTPKeys(cfg KeyBindingConfig) viewkeys.SFTPKeys {
	return viewkeys.SFTPKeys{
		Upload:      cfg.SFTPUpload,
		Download:    cfg.SFTPDownload,
		Delete:      cfg.SFTPDelete,
		Mkdir:       cfg.SFTPMkdir,
		Rename:      cfg.SFTPRename,
		Chmod:       cfg.SFTPChmod,
		SwitchLeft:  cfg.SFTPSwitchLeft,
		SwitchRight: cfg.SFTPSwitchRight,
	}
}

func BuildKeyViewKeys(cfg KeyBindingConfig) viewkeys.KeyViewKeys {
	return viewkeys.KeyViewKeys{
		New:    cfg.KeyNew,
		Import: cfg.KeyImport,
		Export: cfg.KeyExport,
		Delete: cfg.KeyDelete,
		Copy:   cfg.KeyCopy,
	}
}

func BuildFwdKeys(cfg KeyBindingConfig) viewkeys.FwdKeys {
	return viewkeys.FwdKeys{
		Start:  cfg.FwdStart,
		Stop:   cfg.FwdStop,
		New:    cfg.FwdNew,
		Edit:   cfg.FwdEdit,
		Delete: cfg.FwdDelete,
	}
}

func BuildSnippetKeys(cfg KeyBindingConfig) viewkeys.SnippetKeys {
	return viewkeys.SnippetKeys{
		New:    cfg.SnipNew,
		Edit:   cfg.SnipEdit,
		Delete: cfg.SnipDelete,
	}
}

func BuildSSHKeys(cfg KeyBindingConfig) viewkeys.SSHKeys {
	return viewkeys.SSHKeys{
		Reconnect:     cfg.SSHReconnect,
		SnippetPicker: cfg.SSHSnippetPicker,
	}
}
