package remotemenu

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestRemoteMenuDoesNotUseTabPrefixesOrListTags(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, []types.RemoteHost{{Alias: "prod", Hostname: "prod.example", Username: "root", Port: 22, Tags: "web", Group: "Prod"}})
	m.Update(keyMsg("tab"))

	view := m.View()

	for _, want := range []string{"peer", "LocalShell", "prod [Prod] [web]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, bad := range []string{"[Default]", "[Daemon]", "[L]", "[S]", "[R]"} {
		if strings.Contains(view, bad) {
			t.Fatalf("view contains %s:\n%s", bad, view)
		}
	}
}

func TestRemoteMenuSearchFiltersHosts(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, []types.RemoteHost{
		{Alias: "prod", Hostname: "prod.example", Username: "root", Port: 22, Tags: "web"},
		{Alias: "db", Hostname: "db.example", Username: "root", Port: 22, Tags: "data"},
	})
	m.Update(keyMsg("tab"))

	m.Update(keyMsg("/"))
	m.Update(keyText("d"))
	m.Update(keyText("a"))
	m.Update(keyText("t"))
	m.Update(keyText("a"))
	view := m.View()

	if !strings.Contains(view, "db") {
		t.Fatalf("view missing db:\n%s", view)
	}
	if strings.Contains(view, "prod") {
		t.Fatalf("view contains filtered host:\n%s", view)
	}
}

func TestRemoteMenuPaginatesHosts(t *testing.T) {
	var hosts []types.RemoteHost
	for i := 0; i < 10; i++ {
		hosts = append(hosts, types.RemoteHost{Alias: fmt.Sprintf("host-%02d", i), Hostname: "example", Username: "root", Port: 22})
	}
	m := New(types.RemotePeer{Name: "peer"}, hosts)
	m.Update(keyMsg("tab"))

	first := m.View()
	m.Update(keyMsg("pgdown"))
	second := m.View()

	if !strings.Contains(first, "host-00") || strings.Contains(first, "host-09") {
		t.Fatalf("first page wrong:\n%s", first)
	}
	if !strings.Contains(second, "host-08") || !strings.Contains(second, "host-09") {
		t.Fatalf("second page wrong:\n%s", second)
	}
}

func TestTabDefaultsToTmux(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	if m.tab != tabTmux {
		t.Fatal("default tab should be tmux")
	}
	view := m.View()
	for _, want := range []string{"tmux", "+ New session"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tmux view missing %q:\n%s", want, view)
		}
	}
	for _, bad := range []string{"Active", "daemon-resident shell"} {
		if strings.Contains(view, bad) {
			t.Fatalf("tmux view contains %q:\n%s", bad, view)
		}
	}
}

func TestTmuxTabShowsLoadingEmptyAndError(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxLoading(true)
	if !strings.Contains(m.View(), "Loading tmux sessions") {
		t.Fatalf("missing loading state:\n%s", m.View())
	}

	m.SetTmuxLoading(false)
	if !strings.Contains(m.View(), "No tmux sessions") {
		t.Fatalf("missing empty state:\n%s", m.View())
	}

	m.SetTmuxError("tmux not found in PATH")
	if !strings.Contains(m.View(), "tmux not found in PATH") {
		t.Fatalf("missing error state:\n%s", m.View())
	}
}

func TestTmuxTabRefreshEmitsPeerMenuMsg(t *testing.T) {
	peer := types.RemotePeer{ID: "p1", Name: "peer"}
	m := New(peer, nil)

	done, cmd := m.Update(keyText("R"))

	if done || cmd == nil {
		t.Fatal("refresh should keep menu open and emit cmd")
	}
	msg, ok := cmd().(types.RemotePeerMenuMsg)
	if !ok {
		t.Fatalf("got %T want RemotePeerMenuMsg", cmd())
	}
	if msg.Peer.ID != "p1" {
		t.Fatalf("bad refresh msg %+v", msg)
	}
}

func TestRelayCursorSurvivesAsyncTmuxList(t *testing.T) {
	var hosts []types.RemoteHost
	for i := 0; i < 5; i++ {
		hosts = append(hosts, types.RemoteHost{Alias: fmt.Sprintf("host-%02d", i), Hostname: "example", Username: "root", Port: 22})
	}
	m := New(types.RemotePeer{Name: "peer"}, hosts)
	m.Update(keyMsg("tab"))
	m.Update(keyMsg("down"))
	m.Update(keyMsg("down"))
	if m.cursor != 2 {
		t.Fatalf("cursor before list = %d", m.cursor)
	}

	m.SetTmuxSessions(nil)

	if m.cursor != 2 || m.tab != tabRelay {
		t.Fatalf("relay cursor changed after tmux list: tab=%d cursor=%d", m.tab, m.cursor)
	}
}

func TestTmuxTabPaginatesSessions(t *testing.T) {
	var sessions []relay.TmuxSessionInfo
	for i := 0; i < 10; i++ {
		sessions = append(sessions, relay.TmuxSessionInfo{Name: fmt.Sprintf("tmux-%02d", i)})
	}
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxSessions(sessions)

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

func TestTabToggle(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.Update(keyMsg("tab"))
	if m.tab != tabRelay {
		t.Fatal("tab key should switch to Relay")
	}
	m.Update(keyMsg("tab"))
	if m.tab != tabTmux {
		t.Fatal("tab key should switch back to tmux")
	}
}

func TestTmuxNewEmitsOpen(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 0
	done, cmd := m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on new should close menu and emit cmd")
	}
	msg := cmd().(types.RemoteShellOpenMsg)
	if msg.Target != relay.TargetTmuxNew || !msg.Tmux || msg.HostLabel != "" {
		t.Fatalf("bad new msg %+v", msg)
	}
}

func TestTmuxAttachEmitsOpen(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 1
	_, cmd := m.Update(keyMsg("enter"))
	msg := cmd().(types.RemoteShellOpenMsg)
	if msg.Target != relay.TargetTmuxAttach || msg.SessionID != "work" || !msg.Tmux {
		t.Fatalf("bad attach msg %+v", msg)
	}
}

func TestTmuxLabelUsesName(t *testing.T) {
	got := tmuxLabel(relay.TmuxSessionInfo{Name: "work"})
	if got != "work" {
		t.Fatalf("label = %q", got)
	}
}

func TestTmuxDescHidesMissingCreatedTime(t *testing.T) {
	got := tmuxDesc(relay.TmuxSessionInfo{Name: "work"})
	if strings.Contains(got, "00:00:00") {
		t.Fatalf("desc = %q", got)
	}
}

func TestTmuxDescMarksDaemonSession(t *testing.T) {
	got := tmuxDesc(relay.TmuxSessionInfo{Name: "work", Daemon: true, Attached: true})
	if !strings.Contains(got, "daemon session") || !strings.Contains(got, "attached") {
		t.Fatalf("desc = %q", got)
	}
	if strings.Contains(tmuxDesc(relay.TmuxSessionInfo{Name: "work"}), "daemon session") {
		t.Fatal("real tmux session should not be marked daemon")
	}
}

func TestTmuxTabShowsDaemonSessionEntries(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{
		{Name: "work"},
		{Name: "win", Daemon: true},
	})
	view := m.View()
	if !strings.Contains(view, "daemon session") {
		t.Fatalf("view missing daemon marker:\n%s", view)
	}
	if strings.Contains(view, "work daemon session") {
		t.Fatalf("real tmux entry marked as daemon:\n%s", view)
	}
}

func TestTmuxRenameRequestsPrompt(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("r"))
	if done || cmd == nil {
		t.Fatal("rename request should keep menu open")
	}
	msg := cmd().(types.RemoteTmuxRenameRequestMsg)
	if msg.Peer.ID != "p1" || msg.SessionID != "work" || msg.CurrentName != "work" {
		t.Fatalf("bad rename msg %+v", msg)
	}
}

func TestTmuxKillRequestsConfirmationAndKeepsMenu(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("d"))
	if done {
		t.Fatal("kill request should keep menu open")
	}
	if _, ok := cmd().(types.RemoteTmuxKillRequestMsg); !ok {
		t.Fatal("d should emit kill confirmation request")
	}
}

func TestShareKeyOnTmuxSessionEmitsAttachShare(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("s"))
	if !done || cmd == nil {
		t.Fatal("s on session should close menu and emit cmd")
	}
	msg := cmd().(types.RemoteShareMsg)
	if msg.Peer.ID != "p1" || msg.Target != relay.TargetTmuxAttach || msg.SessionID != "work" || msg.Label != "work" {
		t.Fatalf("bad share msg %+v", msg)
	}
}

func TestShareKeyOnNewSessionEmitsLocalShare(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, nil)
	m.SetTmuxSessions([]relay.TmuxSessionInfo{{Name: "work"}})
	m.cursor = 0
	done, cmd := m.Update(keyText("s"))
	if !done || cmd == nil {
		t.Fatal("s on + New session should close menu and emit cmd")
	}
	msg := cmd().(types.RemoteShareMsg)
	if msg.Target != "" || msg.SessionID != "" || msg.Label != "peer" {
		t.Fatalf("bad share msg %+v", msg)
	}
}

func TestShareKeyOnRelayTabEmitsLocalShare(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, []types.RemoteHost{{Alias: "prod"}})
	m.Update(keyMsg("tab"))
	m.Update(keyMsg("down"))
	done, cmd := m.Update(keyText("s"))
	if !done || cmd == nil {
		t.Fatal("s on relay tab should close menu and emit cmd")
	}
	msg := cmd().(types.RemoteShareMsg)
	if msg.Target != "" || msg.SessionID != "" || msg.Label != "peer" {
		t.Fatalf("bad share msg %+v", msg)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "pgdown":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	}
	return tea.KeyPressMsg(tea.Key{Text: s})
}

func keyText(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(s)[0], Text: s})
}
