package app

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestRemoteShareMsgOpensPromptWithDefaultHours(t *testing.T) {
	a := remoteHTTPTestApp(t)
	_ = db.SetSetting(a.db, "share_max_hours", "8")

	next, cmd := a.Update(types.RemoteShareMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		SessionID: "work",
		Label:     "work",
	})
	a = next.(App)

	if a.sharePrompt == nil {
		t.Fatal("expected share prompt")
	}
	if cmd == nil {
		t.Fatal("expected blink command")
	}

	// first enter moves focus to name field, second enter submits
	next, _ = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	a = next.(App)
	if a.sharePrompt == nil {
		t.Fatal("first enter should switch field, not close")
	}
	next, cmd = a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	a = next.(App)
	if a.sharePrompt != nil {
		t.Fatal("submit should close prompt")
	}
	msg, ok := cmd().(types.RemoteShareSubmitMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShareSubmitMsg", cmd())
	}
	if msg.MaxHours != 8 || msg.Target != relay.TargetTmuxAttach || msg.SessionID != "work" || msg.Label != "work" {
		t.Fatalf("bad submit msg %+v", msg)
	}
}

func TestSharePromptEscCancels(t *testing.T) {
	a := remoteHTTPTestApp(t)
	next, _ := a.Update(types.RemoteShareMsg{Peer: types.RemotePeer{ID: "p1", Name: "peer"}, Label: "peer"})
	a = next.(App)
	if a.sharePrompt == nil {
		t.Fatal("expected share prompt")
	}

	next, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	a = next.(App)
	if a.sharePrompt != nil {
		t.Fatal("esc should close prompt")
	}
	if cmd != nil {
		t.Fatal("esc should not emit a command")
	}
}

func TestSharePromptKeysAreIntercepted(t *testing.T) {
	a := remoteHTTPTestApp(t)
	a.viewState = MainView
	next, _ := a.Update(types.RemoteShareMsg{Peer: types.RemotePeer{ID: "p1", Name: "peer"}, Label: "peer"})
	a = next.(App)

	next, cmd := a.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	a = next.(App)
	if a.sharePrompt == nil {
		t.Fatal("prompt should stay open")
	}
	if cmd != nil {
		if _, ok := cmd().(types.QuitRequestMsg); ok {
			t.Fatal("keys should not reach global handlers while prompt is open")
		}
	}
}

func TestShareRemoteShellUsesPromptValues(t *testing.T) {
	old := syncshareCreate
	t.Cleanup(func() { syncshareCreate = old })
	var gotPeerID, gotName, gotTarget, gotSessionID string
	var gotHours int
	syncshareCreate = func(ctx context.Context, cfg esync.Config, peerID, name string, maxHours int, target, sessionID string) (string, time.Time, error) {
		gotPeerID, gotName, gotTarget, gotSessionID, gotHours = peerID, name, target, sessionID, maxHours
		return "http://example/x/tok", time.Now(), nil
	}
	a := remoteHTTPTestApp(t)
	a.masterKey.UnlockNoPassword()
	k := a.masterKey.GetKey()
	enc, err := security.Encrypt([]byte("apikey"), k.Bytes())
	k.Clear()
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting(a.db, "sync_server_url", "http://127.0.0.1:1")
	_ = db.SetSetting(a.db, "sync_api_key", enc)

	_, cmd := a.shareRemoteShell(types.RemoteShareSubmitMsg{
		Peer:      types.RemotePeer{ID: "p1", Name: "peer"},
		Target:    relay.TargetTmuxAttach,
		SessionID: "work",
		Label:     "work",
		Name:      "demo note",
		MaxHours:  6,
	})
	out, ok := cmd().(remoteShareLinkMsg)
	if !ok {
		t.Fatalf("got %T want remoteShareLinkMsg", cmd())
	}
	if out.err != nil {
		t.Fatal(out.err)
	}
	if gotPeerID != "p1" || gotName != "demo note" || gotHours != 6 || gotTarget != relay.TargetTmuxAttach || gotSessionID != "work" {
		t.Fatalf("create got peer=%q name=%q hours=%d target=%q session=%q", gotPeerID, gotName, gotHours, gotTarget, gotSessionID)
	}
}

func TestShareRemoteShellEmptyNameFallsBackToPeerName(t *testing.T) {
	old := syncshareCreate
	t.Cleanup(func() { syncshareCreate = old })
	var gotName string
	syncshareCreate = func(ctx context.Context, cfg esync.Config, peerID, name string, maxHours int, target, sessionID string) (string, time.Time, error) {
		gotName = name
		return "http://example/x/tok", time.Now(), nil
	}
	a := remoteHTTPTestApp(t)
	a.masterKey.UnlockNoPassword()
	k := a.masterKey.GetKey()
	enc, err := security.Encrypt([]byte("apikey"), k.Bytes())
	k.Clear()
	if err != nil {
		t.Fatal(err)
	}
	_ = db.SetSetting(a.db, "sync_server_url", "http://127.0.0.1:1")
	_ = db.SetSetting(a.db, "sync_api_key", enc)

	_, cmd := a.shareRemoteShell(types.RemoteShareSubmitMsg{
		Peer:     types.RemotePeer{ID: "p1", Name: "peer"},
		Label:    "peer",
		MaxHours: 4,
	})
	out := cmd().(remoteShareLinkMsg)
	if out.err != nil {
		t.Fatal(out.err)
	}
	if gotName != "peer" {
		t.Fatalf("name = %q want peer fallback", gotName)
	}
}

func TestShareRemoteShellUnconfigured(t *testing.T) {
	a := remoteHTTPTestApp(t)
	_, cmd := a.shareRemoteShell(types.RemoteShareSubmitMsg{
		Peer:     types.RemotePeer{ID: "p1", Name: "peer"},
		MaxHours: 4,
	})
	out := cmd().(remoteShareLinkMsg)
	if out.err == nil {
		t.Fatal("expected error when sync server is not configured")
	}
}
