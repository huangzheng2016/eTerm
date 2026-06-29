package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
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
