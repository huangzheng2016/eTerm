package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/remote"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/remotemenu"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/ui/tmuxmenu"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestRemoteTmuxKillRequestRequiresConfirm(t *testing.T) {
	a := App{}
	next, cmd := a.Update(types.RemoteTmuxKillRequestMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		SessionID: "work",
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
	msg, ok := cmd().(types.RemoteTmuxKillMsg)
	if !ok {
		t.Fatalf("got %T want RemoteTmuxKillMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.SessionID != "work" {
		t.Fatalf("bad kill msg %+v", msg)
	}
}

func TestRemoteTmuxRenameRequestOpensPrompt(t *testing.T) {
	a := App{}
	next, cmd := a.Update(types.RemoteTmuxRenameRequestMsg{
		Peer:        types.RemotePeer{ID: "p1", Name: "peer"},
		SessionID:   "work",
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
	msg, ok := cmd().(types.RemoteTmuxRenameMsg)
	if !ok {
		t.Fatalf("got %T want RemoteTmuxRenameMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.SessionID != "work" || msg.Name != "ops" {
		t.Fatalf("bad rename msg %+v", msg)
	}
}

func TestRenameRemoteTmuxUpdatesOpenTabTitle(t *testing.T) {
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Tmux:      true,
		SessionID: "work",
	})
	a := App{
		viewState: MainView,
		tabs:      []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}},
	}

	a.renameRemoteTmuxTabs("p1", "work", "ops")

	if a.tabs[0].Title != "[T]peer-ops" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}
	if !a.tabs[0].userRenamed {
		t.Fatal("remote tmux rename did not set userRenamed")
	}
	spec := tab.RemoteReconnect()
	if spec == nil || spec.SessionID != "ops" || spec.Target != relay.TargetTmuxAttach {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestRenameRemoteTmuxDoesNotUpdateTabWhenRemoteRenameFails(t *testing.T) {
	oldRename := remoteRenameTmuxSession
	t.Cleanup(func() { remoteRenameTmuxSession = oldRename })
	remoteRenameTmuxSession = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, sessionID, name string) error {
		return errors.New("rename failed")
	}
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Tmux:      true,
		SessionID: "work",
	})
	a := remoteHTTPTestApp(t)
	a.viewState = MainView
	a.tabs = []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}}

	next, cmd := a.renameRemoteTmuxSession(types.RemoteTmuxRenameMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		SessionID: "work",
		Name:      "ops",
	})
	a = next
	msg := cmd()
	if _, ok := msg.(types.RemoteTmuxSessionsLoadedMsg); !ok {
		t.Fatalf("got %T want RemoteTmuxSessionsLoadedMsg", msg)
	}
	if a.tabs[0].Title != "[T]peer-work" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}
	spec := tab.RemoteReconnect()
	if spec == nil || spec.SessionID != "work" {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestRemoteTmuxTabTitle(t *testing.T) {
	title := remoteTmuxTabTitle("peer", "tmux-abcdef")

	if title != "[T]peer-tmux-abcdef" {
		t.Fatalf("title = %q", title)
	}
}

func TestOpenRemoteTmuxNewUsesReturnedSessionID(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	var gotTarget, gotSession string
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		gotTarget = target
		gotSession = sessionID
		return &internalssh.InteractiveSession{}, "tmux-newid", nil
	}
	a := remoteHTTPTestApp(t)

	_, cmd := a.openRemoteShell(types.RemoteShellOpenMsg{
		Peer:   types.RemotePeer{ID: "p1", Name: "peer"},
		Target: relay.TargetTmuxNew,
		Tmux:   true,
	})
	msg := lastBatchMessage(t, cmd)

	opened, ok := msg.(remoteTerminalOpenedMsg)
	if !ok {
		t.Fatalf("got %T want remoteTerminalOpenedMsg", msg)
	}
	if gotTarget != relay.TargetTmuxNew || gotSession != "" {
		t.Fatalf("open target=%q session=%q", gotTarget, gotSession)
	}
	if opened.title != "[T]peer-tmux-newid" || opened.reconnect == nil || opened.reconnect.SessionID != "tmux-newid" || opened.reconnect.Target != relay.TargetTmuxAttach {
		t.Fatalf("opened = %+v reconnect=%+v", opened, opened.reconnect)
	}
}

func TestOpenRemoteTmuxAttachPreservesSessionID(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	var gotTarget, gotSession string
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		gotTarget = target
		gotSession = sessionID
		return &internalssh.InteractiveSession{}, "", nil
	}
	a := remoteHTTPTestApp(t)

	_, cmd := a.openRemoteShell(types.RemoteShellOpenMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "work",
	})
	msg := lastBatchMessage(t, cmd)

	opened, ok := msg.(remoteTerminalOpenedMsg)
	if !ok {
		t.Fatalf("got %T want remoteTerminalOpenedMsg", msg)
	}
	if gotTarget != relay.TargetTmuxAttach || gotSession != "work" {
		t.Fatalf("open target=%q session=%q", gotTarget, gotSession)
	}
	if opened.title != "[T]peer-work" || opened.reconnect == nil || opened.reconnect.SessionID != "work" {
		t.Fatalf("opened = %+v reconnect=%+v", opened, opened.reconnect)
	}
}

func TestApplyRemoteTmuxReconnectReopensTmuxSession(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	var gotTarget, gotSession string
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		gotTarget = target
		gotSession = sessionID
		return &internalssh.InteractiveSession{}, "", nil
	}
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "work",
	})
	updated, _ := tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	tab = updated.(*sshview.Model)
	a := remoteHTTPTestApp(t)
	a.tabs = []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}}

	next, cmd := a.applyRemoteShellReconnect(types.RemoteShellReconnectMsg{
		StreamID: tab.StreamID(),
		Spec: types.RemoteReconnect{
			Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
			Target:    relay.TargetTmuxAttach,
			Tmux:      true,
			SessionID: "work",
		},
	})
	a = next
	msg := lastBatchMessage(t, cmd)

	opened, ok := msg.(remoteTerminalOpenedMsg)
	if !ok {
		t.Fatalf("got %T want remoteTerminalOpenedMsg", msg)
	}
	if gotTarget != relay.TargetTmuxAttach || gotSession != "work" {
		t.Fatalf("open target=%q session=%q", gotTarget, gotSession)
	}
	if opened.replaceTabAt != 0 || opened.reconnect == nil || !opened.reconnect.Tmux {
		t.Fatalf("opened = %+v reconnect=%+v", opened, opened.reconnect)
	}
}

func TestApplyRemoteTmuxAutoReconnectRetriesBeforeConnError(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		return nil, "", errors.New("dial failed")
	}
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "work",
	})
	updated, _ := tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	tab = updated.(*sshview.Model)
	a := remoteHTTPTestApp(t)
	a.tabs = []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}}

	_, cmd := a.applyRemoteShellReconnect(types.RemoteShellReconnectMsg{
		StreamID:    tab.StreamID(),
		Spec:        *tab.RemoteReconnect(),
		Auto:        true,
		Attempt:     1,
		MaxAttempts: 3,
	})
	msg := lastBatchMessage(t, cmd)
	next, ok := msg.(types.RemoteShellReconnectMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShellReconnectMsg", msg)
	}
	if !next.Auto || next.Attempt != 2 || next.MaxAttempts != 3 {
		t.Fatalf("next reconnect = %+v", next)
	}

	_, cmd = a.applyRemoteShellReconnect(types.RemoteShellReconnectMsg{
		StreamID:    tab.StreamID(),
		Spec:        *tab.RemoteReconnect(),
		Auto:        true,
		Attempt:     3,
		MaxAttempts: 3,
	})
	msg = lastBatchMessage(t, cmd)
	if _, ok := msg.(types.ConnErrorMsg); !ok {
		t.Fatalf("got %T want ConnErrorMsg", msg)
	}
}

func TestRemoteTmuxAutoReconnectDoesNotStealActiveTab(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		return &internalssh.InteractiveSession{}, "", nil
	}
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "work",
	})
	updated, _ := tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	tab = updated.(*sshview.Model)
	a := remoteHTTPTestApp(t)
	a.tabs = []Tab{
		{Type: SSHTab, Title: "[T]peer-work", Model: tab},
		{Type: HomeTab, Title: "Home", Model: nil},
	}
	a.activeTab = 1

	next, cmd := a.applyRemoteShellReconnect(types.RemoteShellReconnectMsg{
		StreamID:    tab.StreamID(),
		Spec:        *tab.RemoteReconnect(),
		Auto:        true,
		Attempt:     1,
		MaxAttempts: 3,
	})
	a = next
	msg := lastBatchMessage(t, cmd)
	opened, ok := msg.(remoteTerminalOpenedMsg)
	if !ok {
		t.Fatalf("got %T want remoteTerminalOpenedMsg", msg)
	}
	a, _ = a.applyRemoteTerminalOpened(opened)
	if a.activeTab != 1 {
		t.Fatalf("activeTab = %d want 1", a.activeTab)
	}
}

func TestRemoteTmuxRenameAppliedUpdatesTabAndRefreshesList(t *testing.T) {
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Tmux:      true,
		SessionID: "work",
	})
	a := remoteHTTPTestApp(t)
	a.tabs = []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}}

	next, cmd := a.Update(remoteTmuxRenameAppliedMsg{Peer: types.RemotePeer{ID: "p1", Name: "peer"}, OldSessionID: "work", Name: "ops"})
	a = next.(App)

	if a.tabs[0].Title != "[T]peer-ops" {
		t.Fatalf("title = %q", a.tabs[0].Title)
	}
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
}

func TestRemoteTmuxKillSuccessRefreshesList(t *testing.T) {
	oldKill := remoteKillTmuxSession
	oldList := remoteListTmuxSessions
	t.Cleanup(func() {
		remoteKillTmuxSession = oldKill
		remoteListTmuxSessions = oldList
	})
	var killed string
	remoteKillTmuxSession = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, sessionID string) error {
		killed = sessionID
		return nil
	}
	remoteListTmuxSessions = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID string) ([]relay.TmuxSessionInfo, error) {
		return []relay.TmuxSessionInfo{{Name: "ops"}}, nil
	}

	cmd := remoteHTTPTestApp(t).killRemoteTmuxSession(types.RemoteTmuxKillMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		SessionID: "work",
	})
	msg := cmd()

	loaded, ok := msg.(types.RemoteTmuxSessionsLoadedMsg)
	if !ok {
		t.Fatalf("got %T want RemoteTmuxSessionsLoadedMsg", msg)
	}
	if killed != "work" || len(loaded.Sessions) != 1 || loaded.Sessions[0].Name != "ops" {
		t.Fatalf("killed=%q loaded=%+v", killed, loaded.Sessions)
	}
}

func TestRemoteTmuxSessionsLoadedIgnoresStalePeer(t *testing.T) {
	a := App{remoteMenu: remotemenu.New(types.RemotePeer{ID: "p2", Name: "peer2"}, nil)}

	next, _ := a.Update(types.RemoteTmuxSessionsLoadedMsg{
		Peer:     types.RemotePeer{ID: "p1", Name: "peer1"},
		Sessions: []relay.TmuxSessionInfo{{Name: "work"}},
	})
	a = next.(App)

	if strings.Contains(a.remoteMenu.View(), "work") {
		t.Fatalf("stale peer updated menu:\n%s", a.remoteMenu.View())
	}
}

func lastBatchMessage(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected batch command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("got %T want tea.BatchMsg", cmd())
	}
	if len(batch) == 0 {
		t.Fatal("empty batch")
	}
	return batch[len(batch)-1]()
}

func TestRemoteTmuxSessionsLoadedErrorShowsError(t *testing.T) {
	a := App{remoteMenu: remotemenu.New(types.RemotePeer{ID: "p1", Name: "peer"}, nil)}

	next, cmd := a.Update(types.RemoteTmuxSessionsLoadedMsg{
		Peer: types.RemotePeer{ID: "p1", Name: "peer"},
		Err:  errors.New("tmux not found in PATH"),
	})
	a = next.(App)

	if cmd != nil {
		t.Fatal("tmux menu error should render inline")
	}
	if !strings.Contains(a.remoteMenu.View(), "tmux not found in PATH") {
		t.Fatalf("menu missing inline error:\n%s", a.remoteMenu.View())
	}
}

func TestRemoteTmuxKillErrorShowsError(t *testing.T) {
	oldKill := remoteKillTmuxSession
	t.Cleanup(func() { remoteKillTmuxSession = oldKill })
	remoteKillTmuxSession = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, sessionID string) error {
		return errors.New("kill failed")
	}

	cmd := remoteHTTPTestApp(t).killRemoteTmuxSession(types.RemoteTmuxKillMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		SessionID: "work",
	})
	msg := cmd()
	if _, ok := msg.(types.RemoteTmuxSessionsLoadedMsg); !ok {
		t.Fatalf("got %T want RemoteTmuxSessionsLoadedMsg", msg)
	}
}

func remoteHTTPTestApp(t *testing.T) App {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting(database, "sync_mode", "http")
	return App{
		db:        database,
		masterKey: security.NewMasterKeyManager(nil, nil, time.Minute),
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

func TestTmuxSessionsLoadedErrorShowsInlineMenuError(t *testing.T) {
	a := App{tmuxMenu: tmuxmenu.New(nil)}

	next, cmd := a.Update(types.TmuxSessionsLoadedMsg{Err: errors.New("tmux not found in PATH")})
	a = next.(App)

	if cmd != nil {
		t.Fatal("tmux menu error should render inline")
	}
	if !strings.Contains(a.tmuxMenu.View(), "tmux not found in PATH") {
		t.Fatalf("menu missing inline error:\n%s", a.tmuxMenu.View())
	}
}

func TestRenameTmuxSessionDoesNotUpdateTabBeforeCommandSucceeds(t *testing.T) {
	a := App{
		tabs: []Tab{{Type: LocalTab, Title: "[T]work", TmuxSession: "work"}},
	}

	next, cmd := a.renameTmuxSession(types.TmuxRenameMsg{OldName: "work", NewName: "ops"})

	if cmd == nil {
		t.Fatal("expected rename command")
	}
	if next.tabs[0].Title != "[T]work" || next.tabs[0].TmuxSession != "work" {
		t.Fatalf("tab updated before command succeeded: %+v", next.tabs[0])
	}
}

func TestTmuxRenameAppliedUpdatesLocalTab(t *testing.T) {
	a := App{tabs: []Tab{{Type: LocalTab, Title: "[T]work", TmuxSession: "work"}}}

	next, _ := a.Update(tmuxRenameAppliedMsg{OldName: "work", NewName: "ops"})
	a = next.(App)

	if a.tabs[0].Title != "[T]ops" || a.tabs[0].TmuxSession != "ops" {
		t.Fatalf("tab not renamed: %+v", a.tabs[0])
	}
}

func TestConfirmKeysTakePriorityOverRemoteMenu(t *testing.T) {
	peer := types.RemotePeer{ID: "p1", Name: "peer"}
	a := App{
		viewState:             MainView,
		remoteMenu:            remotemenu.New(peer, nil),
		confirm:               components.NewConfirm("Kill tmux session", "Kill?").Show(),
		pendingRemoteTmuxKill: &types.RemoteTmuxKillMsg{Peer: peer, SessionID: "work"},
	}

	next, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	a = next.(App)
	if cmd != nil {
		t.Fatal("left should only select yes")
	}
	next, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	a = next.(App)
	if cmd == nil {
		t.Fatal("expected confirmed kill command")
	}
	msg, ok := cmd().(types.RemoteTmuxKillMsg)
	if !ok {
		t.Fatalf("got %T want RemoteTmuxKillMsg", cmd())
	}
	if msg.SessionID != "work" {
		t.Fatalf("bad kill msg %+v", msg)
	}
	if a.remoteMenu == nil {
		t.Fatal("remote menu should stay open after delete confirmation")
	}
}

func TestConfirmKeysTakePriorityOverTmuxMenu(t *testing.T) {
	a := App{
		viewState:       MainView,
		tmuxMenu:        tmuxmenu.New([]types.TmuxSession{{Name: "work"}}),
		confirm:         components.NewConfirm("Kill tmux session", "Kill?").Show(),
		pendingTmuxKill: &types.TmuxKillMsg{Name: "work"},
	}

	next, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	a = next.(App)
	if cmd != nil {
		t.Fatal("left should only select yes")
	}
	next, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	a = next.(App)
	if cmd == nil {
		t.Fatal("expected confirmed kill command")
	}
	msg, ok := cmd().(types.TmuxKillMsg)
	if !ok {
		t.Fatalf("got %T want TmuxKillMsg", cmd())
	}
	if msg.Name != "work" {
		t.Fatalf("bad kill msg %+v", msg)
	}
	if a.tmuxMenu == nil {
		t.Fatal("tmux menu should stay open after delete confirmation")
	}
}

func TestTabStripDoesNotPrefixTmuxTabsAsLocal(t *testing.T) {
	a := App{tabs: []Tab{{Type: LocalTab, Title: "[T]work", TmuxSession: "work"}}}

	items := a.tabStripItems()

	if items[0].Title != "1:[T]work" {
		t.Fatalf("title = %q", items[0].Title)
	}
}

func TestTabStripPrefixesPlainLocalTabRenamedWithTmuxPrefix(t *testing.T) {
	a := App{tabs: []Tab{{Type: LocalTab, Title: "[T]plain"}}}

	items := a.tabStripItems()

	if items[0].Title != "1:[L] [T]plain" {
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
	a := renameShortcutTestApp([]Tab{{Type: LocalTab, Title: "[T]work", TmuxSession: "work"}})

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

func TestRenameTabShortcutDoesNotUseTmuxRenameForPlainPrefix(t *testing.T) {
	a := renameShortcutTestApp([]Tab{{Type: LocalTab, Title: "[T]plain"}})

	next, _ := a.Update(ctrlShiftR())
	a = next.(App)
	a.renamePrompt.input.SetValue("Plain")
	_, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if _, ok := cmd().(tabRenameMsg); !ok {
		t.Fatalf("got %T want tabRenameMsg", cmd())
	}
}

func TestRenameTabShortcutUsesRemoteTmuxRename(t *testing.T) {
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Tmux:      true,
		SessionID: "work",
	})
	a := renameShortcutTestApp([]Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}})

	next, _ := a.Update(ctrlShiftR())
	a = next.(App)
	if a.renamePrompt == nil {
		t.Fatal("expected rename prompt")
	}
	a.renamePrompt.input.SetValue("ops")
	_, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	msg, ok := cmd().(types.RemoteTmuxRenameMsg)
	if !ok {
		t.Fatalf("got %T want RemoteTmuxRenameMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.SessionID != "work" || msg.Name != "ops" {
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

func TestRemoteTmuxAutoReconnectExhaustedSurfacesOpenError(t *testing.T) {
	oldOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() { remoteOpenTmuxSessionWithProgress = oldOpen })
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		return nil, "", errors.New("no such session")
	}
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-work", 0, viewkeys.SSHKeys{})
	tab.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "work",
	})
	updated, _ := tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	tab = updated.(*sshview.Model)
	a := remoteHTTPTestApp(t)
	a.tabs = []Tab{{Type: SSHTab, Title: "[T]peer-work", Model: tab}}

	_, cmd := a.applyRemoteShellReconnect(types.RemoteShellReconnectMsg{
		StreamID:    tab.StreamID(),
		Spec:        *tab.RemoteReconnect(),
		Auto:        true,
		Attempt:     3,
		MaxAttempts: 3,
	})
	msg := lastBatchMessage(t, cmd)

	connErr, ok := msg.(types.ConnErrorMsg)
	if !ok {
		t.Fatalf("got %T want ConnErrorMsg", msg)
	}
	if connErr.Err == nil || !strings.Contains(connErr.Err.Error(), "no such session") {
		t.Fatalf("err = %v", connErr.Err)
	}
	retry, ok := connErr.Retry.(types.RemoteShellReconnectMsg)
	if !ok || retry.Auto {
		t.Fatalf("retry = %+v, want manual RemoteShellReconnectMsg", connErr.Retry)
	}
}
