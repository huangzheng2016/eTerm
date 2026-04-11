package home

import (
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keymatch"
	"github.com/eterm/eterm/internal/types"
)

func firstMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	return unwrapBatchFirstMsg(msg)
}

func unwrapBatchFirstMsg(msg tea.Msg) tea.Msg {
	if msg == nil {
		return nil
	}
	bm, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, c := range bm {
		if m := firstMsg(c); m != nil {
			return m
		}
	}
	return nil
}

// loadedModel returns a home Model with one host loaded from the same DB the host was stored in.
func loadedModel(t *testing.T) (Model, *db.Host) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	gdb, err := db.InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	h := db.Host{
		Hostname:   "example.com",
		Port:       22,
		Username:   "alice",
		AuthMethod: "key",
	}
	if err := gdb.Create(&h).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	m := New(gdb, nil)
	m.SetSize(80, 24)
	out, cmd := m.Update(hostsLoadedMsg{hosts: []db.Host{h}})
	m = out.(Model)
	if cmd != nil {
		_ = firstMsg(cmd)
	}
	return m, &h
}

func TestHomeShortcut_SSHConnect_Enter(t *testing.T) {
	m, h := loadedModel(t)

	out, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	_ = out
	msg := firstMsg(cmd)
	cm, ok := msg.(types.SSHConnectMsg)
	if !ok {
		t.Fatalf("want SSHConnectMsg, got %T %#v", msg, msg)
	}
	if cm.HostID != h.ID {
		t.Fatalf("HostID: got %d want %d", cm.HostID, h.ID)
	}
}

func TestHomeShortcut_SFTPOpen_plainS(t *testing.T) {
	m, h := loadedModel(t)

	out, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	_ = out
	msg := firstMsg(cmd)
	sm, ok := msg.(types.SFTPOpenMsg)
	if !ok {
		t.Fatalf("want SFTPOpenMsg, got %T %#v", msg, msg)
	}
	if sm.HostID != h.ID {
		t.Fatalf("HostID: got %d want %d", sm.HostID, h.ID)
	}
}

func TestHomeShortcut_Edit_NewTab(t *testing.T) {
	m, _ := loadedModel(t)

	out, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'e', Text: "e"}))
	_ = out
	msg := firstMsg(cmd)
	tm, ok := msg.(types.NewTabMsg)
	if !ok {
		t.Fatalf("want NewTabMsg, got %T %#v", msg, msg)
	}
	if tm.Type != "editor" {
		t.Fatalf("tab type: %q", tm.Type)
	}
	if tm.Data == nil {
		t.Fatal("expected editor data for edit shortcut")
	}
}

func TestHomeShortcut_NewHost(t *testing.T) {
	m, _ := loadedModel(t)

	out, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Text: "n"}))
	_ = out
	msg := firstMsg(cmd)
	tm, ok := msg.(types.NewTabMsg)
	if !ok {
		t.Fatalf("want NewTabMsg, got %T %#v", msg, msg)
	}
	if tm.Title != "New Host" {
		t.Fatalf("title: %q", tm.Title)
	}
}

func TestHomeShortcut_FilteringEnterNotSSH(t *testing.T) {
	m, _ := loadedModel(t)

	out, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: '/', Text: "/"}))
	m = out.(Model)
	if m.list.FilterState() != list.Filtering {
		t.Fatalf("expected list.Filtering after '/', got %v", m.list.FilterState())
	}

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg := firstMsg(cmd)
	if _, ok := msg.(types.SSHConnectMsg); ok {
		t.Fatalf("Enter while filtering must not emit SSHConnectMsg, got %#v", msg)
	}
}

// At least one of keymatch or bubbles key.Matches should recognize a plain Enter for connect.
func TestDualPath_EnterMatchesKeyOrKeymatch(t *testing.T) {
	m, _ := loadedModel(t)
	msg := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	if !keymatch.MatchConnect(msg) && !key.Matches(msg, m.keys.SSHConnect) {
		t.Fatal("expected KeyEnter to match via keymatch or key.Matches(SSHConnect)")
	}
}
