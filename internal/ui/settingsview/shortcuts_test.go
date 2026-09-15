package settingsview

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func testShortcutsDB(t *testing.T, configJSON []byte) *ShortcutsModel {
	t.Helper()
	database, err := db.InitDB(t.TempDir() + "/shortcuts.db")
	if err != nil {
		t.Fatal(err)
	}
	defaults, _ := json.Marshal(map[string][]string{
		"quit_app": {"ctrl+shift+q"},
		"repaint":  {"f5"},
	})
	return NewShortcuts(database, configJSON, defaults)
}

func entryByField(m *ShortcutsModel, field string) *bindingEntry {
	for i := range m.entries {
		if m.entries[i].Field == field {
			return &m.entries[i]
		}
	}
	return nil
}

func TestBuildEntriesIncludesFormerlyMissingBindings(t *testing.T) {
	data, _ := json.Marshal(map[string][]string{
		"repaint":     {"f5"},
		"ai_overlay":  {"ctrl+k"},
		"voice_input": {"ctrl+r"},
		"ssh_tmux":    {"m"},
	})
	entries := buildEntries(data)
	want := map[string]string{
		"repaint":     "Global",
		"ai_overlay":  "Global",
		"voice_input": "Global",
		"ssh_tmux":    "Home",
	}
	for _, e := range entries {
		if cat, ok := want[e.Field]; ok && e.Category == cat && len(e.Keys) > 0 {
			delete(want, e.Field)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing bindings: %#v", want)
	}
}

func TestShortcutsConfigJSONPreservesUnknownFields(t *testing.T) {
	configJSON := []byte(`{"quit_app":["ctrl+shift+q"],"repaint":["f6"],"ai_overlay":["ctrl+k"],"voice_input":["ctrl+r"],"ssh_tmux":["m"],"future_binding":["f9"]}`)
	m := testShortcutsDB(t, configJSON)

	e := entryByField(m, "quit_app")
	if e == nil {
		t.Fatal("quit_app entry missing")
	}
	e.Keys = []string{"ctrl+shift+x"}

	out := m.ConfigJSON()
	var got map[string][]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"repaint", "ai_overlay", "voice_input", "ssh_tmux", "future_binding"} {
		if _, ok := got[field]; !ok {
			t.Fatalf("ConfigJSON dropped field %q: %s", field, out)
		}
	}
	if len(got["repaint"]) != 1 || got["repaint"][0] != "f6" {
		t.Fatalf("repaint value lost: %v", got["repaint"])
	}
	if len(got["quit_app"]) != 1 || got["quit_app"][0] != "ctrl+shift+x" {
		t.Fatalf("edit not applied: %v", got["quit_app"])
	}
}

func TestShortcutsSaveRoundTripKeepsBindings(t *testing.T) {
	configJSON := []byte(`{"repaint":["f6"],"ai_overlay":["ctrl+k"],"voice_input":["ctrl+r"],"ssh_tmux":["m"]}`)
	m := testShortcutsDB(t, configJSON)

	msg := m.save()()
	if saved, ok := msg.(types.SettingsSavedMsg); !ok || saved.Err != nil {
		t.Fatalf("save message = %#v", msg)
	}
	stored, err := db.GetSetting(m.db, "keybindings")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"repaint", "ai_overlay", "voice_input", "ssh_tmux"} {
		if !strings.Contains(stored, `"`+field+`"`) {
			t.Fatalf("stored keybindings lost %q: %s", field, stored)
		}
	}
}

func TestShortcutsCtrlRRequiresConfirm(t *testing.T) {
	configJSON := []byte(`{"quit_app":["ctrl+shift+x"]}`)
	m := testShortcutsDB(t, configJSON)

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if !m.confirmReset.IsActive() {
		t.Fatal("ctrl+r must ask for confirmation before resetting")
	}
	if got := entryByField(m, "quit_app").Keys; len(got) != 1 || got[0] != "ctrl+shift+x" {
		t.Fatalf("reset applied before confirm: %v", got)
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	if !m.modified {
		t.Fatal("confirmed reset should mark modified")
	}
	if got := entryByField(m, "quit_app").Keys; len(got) != 1 || got[0] != "ctrl+shift+q" {
		t.Fatalf("defaults not restored: %v", got)
	}
	if got := entryByField(m, "repaint").Keys; len(got) != 1 || got[0] != "f5" {
		t.Fatalf("defaults not restored for repaint: %v", got)
	}
}

func TestShortcutsCtrlRConfirmMouseYes(t *testing.T) {
	configJSON := []byte(`{"quit_app":["ctrl+shift+x"]}`)
	m := testShortcutsDB(t, configJSON)
	m.SetSize(80, 24)

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))
	if !m.confirmReset.IsActive() {
		t.Fatal("ctrl+r must ask for confirmation before resetting")
	}
	ox, oy := dialogOrigin(m.confirmReset.View(), m.width, m.height)
	m.Update(tea.MouseClickMsg(tea.Mouse{X: ox + 4, Y: oy + 6, Button: tea.MouseLeft}))
	if m.confirmReset.IsActive() {
		t.Fatal("Yes click did not close the dialog")
	}
	if got := entryByField(m, "quit_app").Keys; len(got) != 1 || got[0] != "ctrl+shift+q" {
		t.Fatalf("Yes click did not restore defaults: %v", got)
	}
}

func TestShortcutsGroupJump(t *testing.T) {
	m := testShortcutsDB(t, nil)
	starts := m.groupStarts()
	if len(starts) != 7 {
		t.Fatalf("group count = %d, want 7", len(starts))
	}

	m.jumpGroupIndex(1)
	if m.cursor != starts[1] || m.entries[m.cursor].Category != "Home" {
		t.Fatalf("digit jump landed on %d (%s)", m.cursor, m.entries[m.cursor].Category)
	}

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: ']', Text: "]"}))
	if m.cursor != starts[2] || m.entries[m.cursor].Category != "SFTP" {
		t.Fatalf("] jump landed on %d (%s)", m.cursor, m.entries[m.cursor].Category)
	}

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: '[', Text: "["}))
	if m.cursor != starts[1] {
		t.Fatalf("[ from group start should go to previous group, landed on %d", m.cursor)
	}

	m.cursor = starts[2] + 1
	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: '[', Text: "["}))
	if m.cursor != starts[2] {
		t.Fatalf("[ inside group should go to group start, landed on %d", m.cursor)
	}
}

func TestShortcutsCaptureRebindsKey(t *testing.T) {
	configJSON := []byte(`{"quit_app":["ctrl+shift+q"]}`)
	m := testShortcutsDB(t, configJSON)

	m.handleNormal(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.state != stateCapture {
		t.Fatalf("state = %v", m.state)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF9}))
	if got := entryByField(m, "quit_app").Keys; len(got) != 1 || got[0] != "f9" {
		t.Fatalf("keys = %v", got)
	}
	if !m.modified {
		t.Fatal("capture should mark modified")
	}
}
