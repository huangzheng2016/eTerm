package settingsview

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func testSettingsDB(t *testing.T) *Model {
	t.Helper()
	database, err := db.InitDB(t.TempDir() + "/settings.db")
	if err != nil {
		t.Fatal(err)
	}
	return New(database, nil, nil, false)
}

func TestTmuxConfigFileLoadsAndDisplaysBuiltIn(t *testing.T) {
	m := testSettingsDB(t)
	if err := db.SetSetting(m.db, "tmux_config_file", "/tmp/tmux.conf"); err != nil {
		t.Fatal(err)
	}
	m = New(m.db, nil, nil, false)
	if m.tmuxConfigFile != "/tmp/tmux.conf" {
		t.Fatalf("got %q", m.tmuxConfigFile)
	}

	m.tmuxConfigFile = ""
	if got := m.View().Content; !strings.Contains(got, "tmux config file") || !strings.Contains(got, "(built-in)") {
		t.Fatalf("view missing tmux built-in row: %q", got)
	}
}

func TestTmuxConfigFileEditAcceptAndCancel(t *testing.T) {
	m := testSettingsDB(t)
	m.tmuxConfigFile = "/old.conf"
	m.cursor = cursorTmuxConfigFile
	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.tmuxConfigInput.SetValue("  /new.conf  ")
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.tmuxConfigFile != "/new.conf" || !m.modified || m.state != stateNormal {
		t.Fatalf("value=%q modified=%v state=%v", m.tmuxConfigFile, m.modified, m.state)
	}

	m.modified = false
	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m.tmuxConfigInput.SetValue("/cancelled.conf")
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.tmuxConfigFile != "/new.conf" || m.modified || m.state != stateNormal {
		t.Fatalf("value=%q modified=%v state=%v", m.tmuxConfigFile, m.modified, m.state)
	}
}

func TestTmuxConfigFileMouseEdit(t *testing.T) {
	m := testSettingsDB(t)
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 6, Button: tea.MouseLeft}))
	if m.cursor != cursorTmuxConfigFile || m.state != stateTmuxConfig {
		t.Fatalf("cursor=%d state=%v", m.cursor, m.state)
	}
}

func TestTmuxConfigFileSaveEmptyAndReset(t *testing.T) {
	m := testSettingsDB(t)
	m.tmuxConfigFile = ""
	_, cmd := m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	msg := cmd()
	if saved, ok := msg.(types.SettingsSavedMsg); !ok || saved.Err != nil {
		t.Fatalf("save message = %#v", msg)
	}
	got, err := db.GetSetting(m.db, "tmux_config_file")
	if err != nil || got != "" {
		t.Fatalf("saved value=%q err=%v", got, err)
	}

	m.tmuxConfigFile = "/custom.conf"
	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if m.tmuxConfigFile != "" || !m.modified {
		t.Fatalf("value=%q modified=%v", m.tmuxConfigFile, m.modified)
	}
}

func TestBuildEntriesIncludesTabPageKeys(t *testing.T) {
	data, err := json.Marshal(map[string][]string{
		"tab_page_left":  {"alt+shift+left"},
		"tab_page_right": {"alt+shift+right"},
	})
	if err != nil {
		t.Fatal(err)
	}

	entries := buildEntries(data)
	want := map[string]string{
		"tab_page_left":  "Tab Page Left",
		"tab_page_right": "Tab Page Right",
	}
	for _, entry := range entries {
		if label, ok := want[entry.Field]; ok && entry.Category == "Global" && entry.Label == label {
			delete(want, entry.Field)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing settings entries: %#v", want)
	}
}

func TestKeyStringPrefersPrintableString(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: '/', Text: "?", Mod: tea.ModShift})

	if got := keyString(msg); got != "?" {
		t.Fatalf("got %q want ?", got)
	}
}

func TestKeyStringKeepsModifierKeystroke(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: 'h', Mod: tea.ModCtrl | tea.ModShift})

	if got := keyString(msg); got != "ctrl+shift+h" {
		t.Fatalf("got %q want ctrl+shift+h", got)
	}
}
