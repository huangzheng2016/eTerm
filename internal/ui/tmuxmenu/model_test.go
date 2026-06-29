package tmuxmenu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestTmuxMenuShowsSessions(t *testing.T) {
	m := New([]types.TmuxSession{{Name: "work", CreatedUnix: 1710000000, Attached: true}})
	view := m.View()

	for _, want := range []string{"tmux", "+ New session", "work", "attached"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestTmuxMenuNewEmitsOpen(t *testing.T) {
	m := New(nil)
	done, cmd := m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on new should close menu and emit cmd")
	}
	msg := cmd().(types.TmuxOpenMsg)
	if !msg.New || msg.Name != "" {
		t.Fatalf("bad open msg %+v", msg)
	}
}

func TestTmuxMenuAttachEmitsOpen(t *testing.T) {
	m := New([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on session should close menu and emit cmd")
	}
	msg := cmd().(types.TmuxOpenMsg)
	if msg.New || msg.Name != "work" {
		t.Fatalf("bad open msg %+v", msg)
	}
}

func TestTmuxMenuRenameRequestsPrompt(t *testing.T) {
	m := New([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("r"))
	if done || cmd == nil {
		t.Fatal("rename should keep menu open and emit cmd")
	}
	msg := cmd().(types.TmuxRenameRequestMsg)
	if msg.Name != "work" {
		t.Fatalf("bad rename msg %+v", msg)
	}
}

func TestTmuxMenuKillRequestsConfirmation(t *testing.T) {
	m := New([]types.TmuxSession{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("d"))
	if done || cmd == nil {
		t.Fatal("kill should keep menu open and emit cmd")
	}
	msg := cmd().(types.TmuxKillRequestMsg)
	if msg.Name != "work" {
		t.Fatalf("bad kill msg %+v", msg)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	}
	return tea.KeyPressMsg(tea.Key{Text: s})
}

func keyText(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(s)[0], Text: s})
}
