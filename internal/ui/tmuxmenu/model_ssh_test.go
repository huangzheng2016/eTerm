package tmuxmenu

import (
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestTmuxMenuSSHModeView(t *testing.T) {
	m := NewSSH(7, "prod")
	m.SetSessions([]types.TmuxSession{{Name: "work"}})
	view := m.View()

	for _, want := range []string{"tmux @ prod", "work", "start a remote tmux session"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"rename", "kill"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("ssh mode view must not show %q:\n%s", unwanted, view)
		}
	}
}

func TestTmuxMenuSSHModeEnterCarriesHostID(t *testing.T) {
	m := NewSSH(7, "prod")
	m.SetSessions([]types.TmuxSession{{Name: "work"}})

	done, cmd := m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on new should close menu and emit cmd")
	}
	msg := cmd().(types.TmuxOpenMsg)
	if !msg.New || msg.HostID != 7 {
		t.Fatalf("bad open msg %+v", msg)
	}

	m = NewSSH(7, "prod")
	m.SetSessions([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1
	done, cmd = m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on session should close menu and emit cmd")
	}
	msg = cmd().(types.TmuxOpenMsg)
	if msg.New || msg.Name != "work" || msg.HostID != 7 {
		t.Fatalf("bad open msg %+v", msg)
	}
}

func TestTmuxMenuSSHModeRefreshCarriesHostID(t *testing.T) {
	m := NewSSH(7, "prod")

	done, cmd := m.Update(keyText("R"))

	if done || cmd == nil {
		t.Fatal("refresh should keep menu open and emit cmd")
	}
	msg, ok := cmd().(types.TmuxMenuMsg)
	if !ok {
		t.Fatalf("got %T want TmuxMenuMsg", cmd())
	}
	if msg.HostID != 7 {
		t.Fatalf("HostID = %d want 7", msg.HostID)
	}
}

func TestTmuxMenuSSHModeIgnoresKillRename(t *testing.T) {
	m := NewSSH(7, "prod")
	m.SetSessions([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1

	for _, key := range []string{"d", "r"} {
		done, cmd := m.Update(keyText(key))
		if done || cmd != nil {
			t.Fatalf("%q must be inert in ssh mode (done=%v cmd=%v)", key, done, cmd)
		}
	}
}
