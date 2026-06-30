package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestRemoteShellKillRequestRequiresConfirm(t *testing.T) {
	a := App{}
	next, cmd := a.Update(types.RemoteShellKillRequestMsg{
		Peer:    types.RemotePeer{ID: "p1", Name: "peer"},
		ShellID: "ab",
	})
	a = next.(App)

	if cmd != nil {
		t.Fatal("request should not kill immediately")
	}
	if !a.confirm.IsActive() {
		t.Fatal("expected confirm dialog")
	}

	a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	cmd = a.processConfirmResult()
	if cmd == nil {
		t.Fatal("expected confirmed kill command")
	}
	msg, ok := cmd().(types.RemoteShellKillMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShellKillMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.ShellID != "ab" {
		t.Fatalf("bad kill msg %+v", msg)
	}
}

func TestRemoteShellRenameRequestOpensPrompt(t *testing.T) {
	a := App{}
	next, cmd := a.Update(types.RemoteShellRenameRequestMsg{
		Peer:        types.RemotePeer{ID: "p1", Name: "peer"},
		ShellID:     "ab",
		CurrentName: "work",
	})
	a = next.(App)

	if cmd == nil {
		t.Fatal("expected blink command")
	}
	if a.renamePrompt == nil {
		t.Fatal("expected rename prompt")
	}

	a.renamePrompt.input.SetValue("ops")
	_, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok := cmd().(types.RemoteShellRenameMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShellRenameMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.ShellID != "ab" || msg.Name != "ops" {
		t.Fatalf("bad rename msg %+v", msg)
	}
}

func TestRenameActiveShellUpdatesOpenTabTitle(t *testing.T) {
	tab := sshview.New(&internalssh.InteractiveSession{}, "[A]peer-ab", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:    types.RemotePeer{ID: "p1", Name: "peer"},
		Active:  true,
		ShellID: "ab",
	})
	a := App{
		viewState: MainView,
		tabs:      []Tab{{Type: SSHTab, Title: "[A]peer-ab", Model: tab}},
	}

	a.renameActiveShellTabs("p1", "ab", "ops")

	if a.tabs[0].Title != "[A]peer-ops" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}
}

func TestTmuxKillRequestRequiresConfirm(t *testing.T) {
	a := App{}
	next, cmd := a.Update(types.TmuxKillRequestMsg{Name: "work"})
	a = next.(App)

	if cmd != nil {
		t.Fatal("request should not kill immediately")
	}
	if !a.confirm.IsActive() {
		t.Fatal("expected confirm dialog")
	}

	a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	cmd = a.processConfirmResult()
	msg, ok := cmd().(types.TmuxKillMsg)
	if !ok {
		t.Fatalf("got %T want TmuxKillMsg", cmd())
	}
	if msg.Name != "work" {
		t.Fatalf("bad kill msg %+v", msg)
	}
}

func TestTabStripDoesNotPrefixTmuxTabsAsLocal(t *testing.T) {
	a := App{tabs: []Tab{{Type: LocalTab, Title: "[T]work"}}}

	items := a.tabStripItems()

	if items[0].Title != "1:[T]work" {
		t.Fatalf("title = %q", items[0].Title)
	}
}

func TestRenameTabShortcutRenamesPlainTab(t *testing.T) {
	a := renameShortcutTestApp([]Tab{{Type: HomeTab, Title: "List"}})

	next, cmd := a.Update(ctrlShiftR())
	a = next.(App)

	if cmd == nil {
		t.Fatal("expected blink command")
	}
	if a.renamePrompt == nil {
		t.Fatal("expected rename prompt")
	}
	a.renamePrompt.input.SetValue("Home")
	_, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok := cmd().(tabRenameMsg)
	if !ok {
		t.Fatalf("got %T want tabRenameMsg", cmd())
	}

	next, _ = a.Update(msg)
	a = next.(App)
	if a.tabs[0].Title != "Home" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}
}

func TestRenameTabShortcutUsesTmuxRename(t *testing.T) {
	a := renameShortcutTestApp([]Tab{{Type: LocalTab, Title: "[T]work"}})

	next, _ := a.Update(ctrlShiftR())
	a = next.(App)
	if a.renamePrompt == nil {
		t.Fatal("expected rename prompt")
	}
	a.renamePrompt.input.SetValue("ops")
	_, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok := cmd().(types.TmuxRenameMsg)
	if !ok {
		t.Fatalf("got %T want TmuxRenameMsg", cmd())
	}
	if msg.OldName != "work" || msg.NewName != "ops" {
		t.Fatalf("bad tmux rename msg %+v", msg)
	}
}

func TestRenameTabShortcutUsesActiveShellRename(t *testing.T) {
	tab := sshview.New(&internalssh.InteractiveSession{}, "[A]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:    types.RemotePeer{ID: "p1", Name: "peer"},
		Active:  true,
		ShellID: "ab",
	})
	a := renameShortcutTestApp([]Tab{{Type: SSHTab, Title: "[A]peer-work", Model: tab}})

	next, _ := a.Update(ctrlShiftR())
	a = next.(App)
	if a.renamePrompt == nil {
		t.Fatal("expected rename prompt")
	}
	a.renamePrompt.input.SetValue("ops")
	_, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok := cmd().(types.RemoteShellRenameMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShellRenameMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.ShellID != "ab" || msg.Name != "ops" {
		t.Fatalf("bad remote rename msg %+v", msg)
	}
}

func renameShortcutTestApp(tabs []Tab) App {
	cfg := DefaultKeyBindingConfig()
	return App{
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
		viewState: MainView,
		tabs:      tabs,
		activeTab: 0,
		keyMap:    BuildKeyMap(cfg),
		kbConfig:  cfg,
	}
}

func ctrlShiftR() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: 'r', Text: "R", Mod: tea.ModCtrl | tea.ModShift})
}
