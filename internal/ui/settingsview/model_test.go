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
	return New(database, false)
}

func triggerReset(t *testing.T, m *Model) {
	t.Helper()
	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if !m.confirmReset.IsActive() {
		t.Fatal("ctrl+r must ask for confirmation before resetting")
	}
}

func TestTmuxConfigFileLoadsAndDisplaysBuiltIn(t *testing.T) {
	m := testSettingsDB(t)
	if err := db.SetSetting(m.db, "tmux_config_file", "/tmp/tmux.conf"); err != nil {
		t.Fatal(err)
	}
	m = New(m.db, false)
	if m.tmuxConfigFile != "/tmp/tmux.conf" {
		t.Fatalf("got %q", m.tmuxConfigFile)
	}

	m.tmuxConfigFile = ""
	m.SetSize(80, 40)
	if got := m.View().Content; !strings.Contains(got, "tmux config file") || !strings.Contains(got, "(built-in)") {
		t.Fatalf("view missing tmux built-in row: %q", got)
	}
}

func TestReplayRecordingDefaultsOnAndSavesMode(t *testing.T) {
	m := testSettingsDB(t)
	if !m.replaySessions {
		t.Fatal("replay recording is not enabled by default")
	}
	m.replaySessions = false
	msg := m.save()()
	if saved, ok := msg.(types.SettingsSavedMsg); !ok || saved.Err != nil {
		t.Fatalf("save message = %#v", msg)
	}
	got, err := db.GetSetting(m.db, "session_capture_mode")
	if err != nil || got != "transcript" {
		t.Fatalf("mode=%q err=%v", got, err)
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
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 11, Button: tea.MouseLeft}))
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
	triggerReset(t, m)
	if m.tmuxConfigFile != "/custom.conf" {
		t.Fatalf("reset applied before confirm: %q", m.tmuxConfigFile)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	if m.tmuxConfigFile != "" || !m.modified {
		t.Fatalf("value=%q modified=%v", m.tmuxConfigFile, m.modified)
	}
}

func TestCtrlRResetCancelledKeepsValues(t *testing.T) {
	m := testSettingsDB(t)
	m.gridStatusWords = true
	triggerReset(t, m)
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Text: "n"}))
	if !m.gridStatusWords {
		t.Fatal("cancelled reset changed preferences")
	}
}

func TestCtrlRConfirmMouseYesAndNo(t *testing.T) {
	m := testSettingsDB(t)
	m.SetSize(80, 24)
	m.gridStatusWords = true
	triggerReset(t, m)
	ox, oy := dialogOrigin(m.confirmReset.View(), m.width, m.height)

	m.Update(tea.MouseClickMsg(tea.Mouse{X: ox + 16, Y: oy + 6, Button: tea.MouseLeft}))
	if m.confirmReset.IsActive() {
		t.Fatal("No click did not close the dialog")
	}
	if !m.gridStatusWords {
		t.Fatal("No click applied the reset")
	}

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	m.Update(tea.MouseClickMsg(tea.Mouse{X: ox + 4, Y: oy + 6, Button: tea.MouseLeft}))
	if m.confirmReset.IsActive() {
		t.Fatal("Yes click did not close the dialog")
	}
	if m.gridStatusWords {
		t.Fatal("Yes click did not apply the reset")
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
