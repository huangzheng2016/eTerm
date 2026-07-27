package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/remote"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestTmuxRestoreFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux_restore.json")
	want := []tmuxRestoreEntry{
		{Kind: tmuxRestoreLocal, Session: "work", Title: "[T]work"},
		{Kind: tmuxRestoreRemote, Session: "ops", Title: "[T]peer-ops", PeerID: "p1", PeerName: "peer"},
	}

	if err := writeTmuxRestoreFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := readTmuxRestoreFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestWriteTmuxRestoreFileRemovesEmptySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux_restore.json")
	if err := writeTmuxRestoreFile(path, []tmuxRestoreEntry{{Kind: tmuxRestoreLocal, Session: "work"}}); err != nil {
		t.Fatal(err)
	}

	if err := writeTmuxRestoreFile(path, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat err = %v, want not exist", err)
	}
}

func TestTmuxRestoreSnapshotOnlyIncludesTmuxTabsInOrder(t *testing.T) {
	localTmux := sshview.New(&internalssh.InteractiveSession{}, "[T]work", 0, viewkeys.SSHKeys{})
	plainLocal := sshview.New(&internalssh.InteractiveSession{}, "[T]plain", 0, viewkeys.SSHKeys{})
	remoteTmux := sshview.New(&internalssh.InteractiveSession{}, "[T]peer-ops", 0, viewkeys.SSHKeys{})
	remoteTmux.SetRemoteReconnect(&types.RemoteReconnect{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		Tmux:      true,
		SessionID: "ops",
	})
	plainSSH := sshview.New(&internalssh.InteractiveSession{}, "ssh", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() {
		_ = localTmux.Close()
		_ = plainLocal.Close()
		_ = remoteTmux.Close()
		_ = plainSSH.Close()
	})
	a := App{
		tabs: []Tab{
			{Type: HomeTab, Title: "List"},
			{Type: LocalTab, Title: "[T]work", Model: localTmux, TmuxSession: "work"},
			{Type: LocalTab, Title: "[T]plain", Model: plainLocal},
			{Type: SSHTab, Title: "[T]peer-ops", Model: remoteTmux},
			{Type: SSHTab, Title: "ssh", Model: plainSSH},
		},
	}

	got := a.tmuxRestoreEntries()

	want := []tmuxRestoreEntry{
		{Kind: tmuxRestoreLocal, Session: "work", Title: "[T]work"},
		{Kind: tmuxRestoreRemote, Session: "ops", Title: "[T]peer-ops", PeerID: "p1", PeerName: "peer"},
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestUnlockPromptsForSavedTmuxRestore(t *testing.T) {
	a := restoreTestApp(t)
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	if err := writeTmuxRestoreFile(a.tmuxRestorePath, []tmuxRestoreEntry{{Kind: tmuxRestoreLocal, Session: "work"}}); err != nil {
		t.Fatal(err)
	}

	next, _ := a.Update(types.MasterKeyUnlockedMsg{NoPassword: true})
	a = next.(App)

	if !a.confirm.IsActive() {
		t.Fatal("expected restore confirmation")
	}
	if len(a.pendingTmuxRestore) != 1 || a.pendingTmuxRestore[0].Session != "work" {
		t.Fatalf("pending restore = %#v", a.pendingTmuxRestore)
	}
}

func TestUpgradeCommandDefersTmuxRestoreUntilUpdatePromptFinishes(t *testing.T) {
	a := restoreTestApp(t)
	a.forceUpdateCheck = true
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	if err := writeTmuxRestoreFile(a.tmuxRestorePath, []tmuxRestoreEntry{{Kind: tmuxRestoreLocal, Session: "work"}}); err != nil {
		t.Fatal(err)
	}

	next, _ := a.Update(types.MasterKeyUnlockedMsg{NoPassword: true})
	a = next.(App)
	if a.confirm.IsActive() || !a.tmuxRestoreDeferred || len(a.pendingTmuxRestore) != 0 {
		t.Fatalf("restore was not deferred: confirm=%v deferred=%v pending=%#v", a.confirm.IsActive(), a.tmuxRestoreDeferred, a.pendingTmuxRestore)
	}

	next, _ = a.Update(types.UpdateCheckDoneMsg{Version: "v9.9.9", URL: "https://example.com"})
	a = next.(App)
	if a.upgradePrompt == nil || a.confirm.IsActive() || !a.tmuxRestoreDeferred {
		t.Fatalf("upgrade prompt ordering is wrong: upgrade=%v confirm=%v deferred=%v", a.upgradePrompt != nil, a.confirm.IsActive(), a.tmuxRestoreDeferred)
	}

	a, _ = a.dismissUpgradePrompt(true)
	if a.upgradePrompt != nil || !a.confirm.IsActive() || a.tmuxRestoreDeferred || len(a.pendingTmuxRestore) != 1 {
		t.Fatalf("restore was not prompted after upgrade: upgrade=%v confirm=%v deferred=%v pending=%#v", a.upgradePrompt != nil, a.confirm.IsActive(), a.tmuxRestoreDeferred, a.pendingTmuxRestore)
	}
}

func TestUpgradeCommandPromptsTmuxRestoreAfterNoUpdate(t *testing.T) {
	a := restoreTestApp(t)
	a.forceUpdateCheck = true
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	if err := writeTmuxRestoreFile(a.tmuxRestorePath, []tmuxRestoreEntry{{Kind: tmuxRestoreLocal, Session: "work"}}); err != nil {
		t.Fatal(err)
	}

	next, _ := a.Update(types.MasterKeyUnlockedMsg{NoPassword: true})
	a = next.(App)
	next, _ = a.Update(types.UpdateCheckDoneMsg{})
	a = next.(App)
	if !a.confirm.IsActive() || a.tmuxRestoreDeferred || len(a.pendingTmuxRestore) != 1 {
		t.Fatalf("restore missing after update check: confirm=%v deferred=%v pending=%#v", a.confirm.IsActive(), a.tmuxRestoreDeferred, a.pendingTmuxRestore)
	}
}

func TestDecliningTmuxRestoreClearsSnapshot(t *testing.T) {
	a := restoreTestApp(t)
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	if err := writeTmuxRestoreFile(a.tmuxRestorePath, []tmuxRestoreEntry{{Kind: tmuxRestoreLocal, Session: "work"}}); err != nil {
		t.Fatal(err)
	}
	next, _ := a.Update(types.MasterKeyUnlockedMsg{NoPassword: true})
	a = next.(App)

	a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Text: "n"}))
	cmd := a.processConfirmResult()

	if cmd != nil {
		t.Fatal("decline should not restore sessions")
	}
	if _, err := os.Stat(a.tmuxRestorePath); !os.IsNotExist(err) {
		t.Fatalf("stat err = %v, want restore file removed", err)
	}
}

func TestConfirmingTmuxRestoreCreatesTabsBeforeOpeningInSavedOrder(t *testing.T) {
	oldAttach := appAttachTmuxSession
	oldRemoteOpen := remoteOpenTmuxSessionWithProgress
	t.Cleanup(func() {
		appAttachTmuxSession = oldAttach
		remoteOpenTmuxSessionWithProgress = oldRemoteOpen
	})
	var opened []string
	appAttachTmuxSession = func(ctx context.Context, _ string, name string, rows, cols int) (*internalssh.InteractiveSession, error) {
		opened = append(opened, "local:"+name)
		return &internalssh.InteractiveSession{}, nil
	}
	remoteOpenTmuxSessionWithProgress = func(ctx context.Context, serverURL, apiKey, tenant string, insecureTLS bool, peerID, target, sessionID string, rows, cols int, progress remote.ProgressFunc) (*internalssh.InteractiveSession, string, error) {
		opened = append(opened, "remote:"+peerID+":"+sessionID)
		return &internalssh.InteractiveSession{}, "", nil
	}
	a := restoreTestApp(t)
	a.viewState = MainView
	a.tabs = []Tab{{Type: HomeTab, Title: "List"}}
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	a.pendingTmuxRestore = []tmuxRestoreEntry{
		{Kind: tmuxRestoreLocal, Session: "work", Title: "[T]work"},
		{Kind: tmuxRestoreRemote, Session: "ops", Title: "[T]peer-ops", PeerID: "p1", PeerName: "peer"},
		{Kind: tmuxRestoreLocal, Session: "logs", Title: "[T]logs"},
	}
	a.confirm = a.confirm.Show()

	a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	cmd := a.processConfirmResult()
	if cmd == nil {
		t.Fatal("expected restore command")
	}
	if len(a.tabs) != 4 {
		t.Fatalf("tabs before attach = %#v, want home + 3 restoring tabs", a.tabs)
	}
	if a.activeTab != 0 {
		t.Fatalf("active tab = %d want unchanged home tab", a.activeTab)
	}
	for i := 1; i < len(a.tabs); i++ {
		sm := a.tabs[i].Model.(*sshview.Model)
		if sm.ReconnectingLabel() == "" {
			t.Fatalf("tab %d is not marked reconnecting", i)
		}
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 3 {
		t.Fatalf("restore command = %T len=%d, want 3-command batch", batch, len(batch))
	}
	for i := len(batch) - 1; i >= 0; i-- {
		next, _ := a.Update(batch[i]())
		a = next.(App)
	}

	wantOpened := []string{"local:logs", "remote:p1:ops", "local:work"}
	if len(opened) != len(wantOpened) {
		t.Fatalf("opened = %#v, want %#v", opened, wantOpened)
	}
	for i := range wantOpened {
		if opened[i] != wantOpened[i] {
			t.Fatalf("opened = %#v, want %#v", opened, wantOpened)
		}
	}
	if len(a.tabs) != 4 {
		t.Fatalf("tabs = %#v, want home + 3 restored tabs", a.tabs)
	}
	if a.tabs[1].Type != LocalTab || a.tabs[1].TmuxSession != "work" {
		t.Fatalf("tab1 = %#v", a.tabs[1])
	}
	if a.tabs[2].Type != SSHTab || a.tabs[2].Title != "[T]peer-ops" {
		t.Fatalf("tab2 = %#v", a.tabs[2])
	}
	if a.tabs[3].Type != LocalTab || a.tabs[3].TmuxSession != "logs" {
		t.Fatalf("tab3 = %#v", a.tabs[3])
	}
}

func TestTmuxRestoreMissingSessionClosesOnlyPlaceholder(t *testing.T) {
	a := restoreTestApp(t)
	a.viewState = MainView
	a.tabs = []Tab{{Type: HomeTab, Title: "List"}}
	a.activeTab = 0

	cmd := (&a).restoreTmuxSessions([]tmuxRestoreEntry{
		{Kind: tmuxRestoreLocal, Session: "gone", Title: "[T]gone"},
		{Kind: tmuxRestoreLocal, Session: "work", Title: "[T]work"},
	})
	if cmd == nil || len(a.tabs) != 3 {
		t.Fatalf("tabs before result = %#v", a.tabs)
	}
	missingID := a.tabs[1].tmuxRestoreID
	next, _ := a.Update(tmuxRestoreOpenedMsg{
		id:    missingID,
		entry: tmuxRestoreEntry{Kind: tmuxRestoreLocal, Session: "gone", Title: "[T]gone"},
		err:   errors.New("tmux attach-session: exit status 1: can't find session: gone"),
	})
	a = next.(App)

	if len(a.tabs) != 2 || a.tabs[1].Title != "[T]work" {
		t.Fatalf("tabs after missing session = %#v", a.tabs)
	}
	if a.activeTab != 0 || a.connError != nil {
		t.Fatalf("active tab=%d connError=%v", a.activeTab, a.connError)
	}
}

func TestApplyTmuxTerminalOpenedPersistsRestoreSnapshot(t *testing.T) {
	a := restoreTestApp(t)
	a.viewState = MainView
	a.tabs = []Tab{{Type: HomeTab, Title: "List"}}
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")

	next, _ := a.applyTmuxTerminalOpened(tmuxTerminalOpenedMsg{
		is:      &internalssh.InteractiveSession{},
		title:   "[T]work",
		session: "work",
	})
	a = next

	entries, err := readTmuxRestoreFile(a.tmuxRestorePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != tmuxRestoreLocal || entries[0].Session != "work" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestCloseTmuxTabRemovesRestoreSnapshot(t *testing.T) {
	a := restoreTestApp(t)
	a.viewState = MainView
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]work", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	a.tabs = []Tab{
		{Type: HomeTab, Title: "List"},
		{Type: LocalTab, Title: "[T]work", Model: tab, TmuxSession: "work"},
	}
	a.activeTab = 1
	a.persistTmuxRestoreSnapshot()

	next, _ := a.closeCurrentTabIfAllowed()
	a = next

	if _, err := os.Stat(a.tmuxRestorePath); !os.IsNotExist(err) {
		t.Fatalf("stat err = %v, want restore file removed", err)
	}
}

func TestConfirmedQuitPersistsTmuxRestoreSnapshot(t *testing.T) {
	a := restoreTestApp(t)
	a.viewState = MainView
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	tab := sshview.New(&internalssh.InteractiveSession{}, "[T]work", 0, viewkeys.SSHKeys{})
	history := db.ConnectionHistory{Label: "[T]work", ConnectedAt: time.Now()}
	if err := a.db.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	tab.SetHistoryID(history.ID)
	tab.EnableReplayRecording()
	tab.Update(sshview.ChunkMsg{StreamID: tab.StreamID(), Data: []byte("quit output")})
	t.Cleanup(func() { _ = tab.Close() })
	a.tabs = []Tab{
		{Type: HomeTab, Title: "List"},
		{Type: LocalTab, Title: "[T]work", Model: tab, TmuxSession: "work"},
	}
	a.pendingQuit = true
	a.confirm = a.confirm.Show()
	a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))

	cmd := a.processConfirmResult()

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	entries, err := readTmuxRestoreFile(a.tmuxRestorePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Session != "work" {
		t.Fatalf("entries = %#v", entries)
	}
	if err := a.db.First(&history, history.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(history.ReplayData) == 0 || history.Transcript == "" {
		t.Fatalf("quit did not finalize session: replay=%d transcript=%q", len(history.ReplayData), history.Transcript)
	}
}

func restoreTestApp(t *testing.T) App {
	t.Helper()
	gdb, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting(gdb, "sync_mode", "http")
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	return NewApp(gdb, mk).SetNoUpdateCheck(true)
}
