package app

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func keyMsg(s string) tea.KeyPressMsg {
	if s == "esc" {
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg(tea.Key{Code: r, Text: s})
}

func newTestCard(retry tea.Msg) *connErrorModel {
	ce := &internalssh.ConnectError{Kind: internalssh.ErrKindAuth, Summary: "Authentication failed", Hint: "h", Err: errors.New("raw detail")}
	return newConnErrorModel(ce, "user@host:22", retry)
}

func TestConnErrorToggleDetails(t *testing.T) {
	a := App{connError: newTestCard(nil)}
	m, _ := a.handleConnErrorKey(keyMsg("d"))
	if !m.(App).connError.expanded {
		t.Fatal("d should expand details")
	}
	m2, _ := m.(App).handleConnErrorKey(keyMsg("d"))
	if m2.(App).connError.expanded {
		t.Fatal("d again should collapse details")
	}
}

func TestConnErrorEscCloses(t *testing.T) {
	a := App{connError: newTestCard(nil)}
	m, _ := a.handleConnErrorKey(keyMsg("esc"))
	if m.(App).connError != nil {
		t.Fatal("esc should close the card")
	}
}

func TestConnErrorRetryEmitsMsg(t *testing.T) {
	want := types.SSHConnectMsg{HostID: 7}
	a := App{connError: newTestCard(want)}
	m, cmd := a.handleConnErrorKey(keyMsg("r"))
	if m.(App).connError != nil {
		t.Fatal("r should close the card")
	}
	if cmd == nil {
		t.Fatal("r with retry should return a cmd")
	}
	if got := cmd(); got != want {
		t.Fatalf("retry cmd = %#v, want %#v", got, want)
	}
}

func TestConnErrorRetryNilNoCmd(t *testing.T) {
	a := App{connError: newTestCard(nil)}
	m, cmd := a.handleConnErrorKey(keyMsg("r"))
	if m.(App).connError != nil {
		t.Fatal("r should close the card")
	}
	if cmd != nil {
		t.Fatal("r with nil retry should not emit a cmd")
	}
}

func TestConnErrorViewRendersDetailWhenExpanded(t *testing.T) {
	c := newTestCard(types.SSHConnectMsg{HostID: 1})
	collapsed := c.View()
	c.expanded = true
	expanded := c.View()
	if len(expanded) <= len(collapsed) {
		t.Fatal("expanded view should include raw detail")
	}
}
