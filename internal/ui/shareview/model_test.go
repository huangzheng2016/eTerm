package shareview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	}
	return tea.KeyPressMsg(tea.Key{Text: s})
}

func TestPrefillsDefaultHours(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, "", "", "peer", 8)
	if m.hours.Value() != "8" {
		t.Fatalf("hours = %q", m.hours.Value())
	}

	m = New(types.RemotePeer{ID: "p1", Name: "peer"}, "", "", "peer", 0)
	if m.hours.Value() != "4" {
		t.Fatalf("invalid default should fall back to 4, got %q", m.hours.Value())
	}
}

func TestInvalidHoursBlocked(t *testing.T) {
	for _, bad := range []string{"abc", "0", "169", ""} {
		m := New(types.RemotePeer{ID: "p1", Name: "peer"}, "", "", "peer", 4)
		m.hours.SetValue(bad)
		closed, _ := m.Update(key("enter")) // focus hours -> name
		if closed {
			t.Fatal("first enter should switch field")
		}
		closed, cmd := m.Update(key("enter")) // submit
		if closed || cmd != nil {
			t.Fatalf("hours %q should block submit", bad)
		}
		if m.err == "" {
			t.Fatalf("hours %q should set inline error", bad)
		}
	}
}

func TestSubmitEmitsShareSubmitMsg(t *testing.T) {
	peer := types.RemotePeer{ID: "p1", Name: "peer"}
	m := New(peer, relay.TargetTmuxAttach, "work", "work", 4)
	m.hours.SetValue("12")
	m.Update(key("tab"))
	m.name.SetValue("demo note")

	closed, cmd := m.Update(key("enter"))
	if !closed || cmd == nil {
		t.Fatal("enter on name field should submit and close")
	}
	msg, ok := cmd().(types.RemoteShareSubmitMsg)
	if !ok {
		t.Fatalf("got %T want RemoteShareSubmitMsg", cmd())
	}
	if msg.Peer.ID != "p1" || msg.Target != relay.TargetTmuxAttach || msg.SessionID != "work" || msg.Label != "work" {
		t.Fatalf("bad msg %+v", msg)
	}
	if msg.MaxHours != 12 || msg.Name != "demo note" {
		t.Fatalf("bad msg %+v", msg)
	}
}

func TestEscCancels(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, "", "", "peer", 4)
	closed, cmd := m.Update(key("esc"))
	if !closed || cmd != nil {
		t.Fatal("esc should cancel without emitting")
	}
}
