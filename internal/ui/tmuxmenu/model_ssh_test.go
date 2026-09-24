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
	for _, want := range []string{"rename", "kill"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ssh mode view missing %q:\n%s", want, view)
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

func TestTmuxMenuSSHModeSupportsKillRename(t *testing.T) {
	m := NewSSH(7, "prod")
	m.SetSessions([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1

	done, cmd := m.Update(keyText("d"))
	if done || cmd == nil {
		t.Fatal("kill should keep menu open and emit a command")
	}
	kill := cmd().(types.TmuxKillRequestMsg)
	if kill.HostID != 7 || kill.Name != "work" {
		t.Fatalf("kill msg = %+v", kill)
	}

	done, cmd = m.Update(keyText("r"))
	if done || cmd == nil {
		t.Fatal("rename should keep menu open and emit a command")
	}
	rename := cmd().(types.TmuxRenameRequestMsg)
	if rename.HostID != 7 || rename.Name != "work" {
		t.Fatalf("rename msg = %+v", rename)
	}

	for _, key := range []string{"x"} {
		done, cmd := m.Update(keyText(key))
		if done || cmd != nil {
			t.Fatalf("%q must be inert (done=%v cmd=%v)", key, done, cmd)
		}
	}
}
