package tmuxmenu

import (
	"fmt"
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

func TestTmuxMenuShowsLoadingEmptyAndError(t *testing.T) {
	m := New(nil)
	m.SetLoading(true)
	if !strings.Contains(m.View(), "Loading tmux sessions") {
		t.Fatalf("missing loading state:\n%s", m.View())
	}

	m.SetLoading(false)
	if !strings.Contains(m.View(), "No tmux sessions") {
		t.Fatalf("missing empty state:\n%s", m.View())
	}

	m.SetError("tmux not found in PATH")
	if !strings.Contains(m.View(), "tmux not found in PATH") {
		t.Fatalf("missing error state:\n%s", m.View())
	}
}

func TestTmuxMenuRefreshEmitsMenuMsg(t *testing.T) {
	m := New(nil)

	done, cmd := m.Update(keyText("R"))

	if done || cmd == nil {
		t.Fatal("refresh should keep menu open and emit cmd")
	}
	if _, ok := cmd().(types.TmuxMenuMsg); !ok {
		t.Fatalf("got %T want TmuxMenuMsg", cmd())
	}
}

func TestTmuxMenuPaginatesSessions(t *testing.T) {
	var sessions []types.TmuxSession
	for i := 0; i < 10; i++ {
		sessions = append(sessions, types.TmuxSession{Name: fmt.Sprintf("tmux-%02d", i)})
	}
	m := New(sessions)

	first := m.View()
	for i := 0; i < pageSize+1; i++ {
		m.Update(keyMsg("down"))
	}
	second := m.View()

	if !strings.Contains(first, "tmux-00") || strings.Contains(first, "tmux-09") {
		t.Fatalf("first page wrong:\n%s", first)
	}
	if !strings.Contains(second, "tmux-08") || !strings.Contains(second, "tmux-09") {
		t.Fatalf("second page wrong:\n%s", second)
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
