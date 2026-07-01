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

func TestTabDefaultsToActive(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	if m.tab != tabActive {
		t.Fatal("default tab should be Active")
	}
	if !strings.Contains(m.View(), "+ New shell") {
		t.Fatalf("active view missing new shell:\n%s", m.View())
	}
}

func TestTabToggle(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.Update(keyMsg("tab"))
	if m.tab != tabRelay {
		t.Fatal("tab key should switch to Relay")
	}
	m.Update(keyMsg("tab"))
	if m.tab != tabActive {
		t.Fatal("tab key should switch back to Active")
	}
}

func TestActiveNewEmitsOpen(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetShells([]relay.ActiveShellInfo{{ID: "ab", Shell: "zsh"}})
	m.cursor = 0
	done, cmd := m.Update(keyMsg("enter"))
	if !done || cmd == nil {
		t.Fatal("enter on new should close menu and emit cmd")
	}
	msg := cmd().(types.RemoteShellOpenMsg)
	if msg.Target != relay.TargetActiveNew || !msg.Active || msg.HostLabel != "" {
		t.Fatalf("bad new msg %+v", msg)
	}
}

func TestActiveAttachEmitsOpen(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetShells([]relay.ActiveShellInfo{{ID: "ab", Shell: "zsh"}})
	m.cursor = 1
	_, cmd := m.Update(keyMsg("enter"))
	msg := cmd().(types.RemoteShellOpenMsg)
	if msg.Target != relay.TargetActiveAttach || msg.ShellID != "ab" || !msg.Active {
		t.Fatalf("bad attach msg %+v", msg)
	}
}

func TestActiveLabelPrefersName(t *testing.T) {
	got := activeLabel(relay.ActiveShellInfo{ID: "ab", Shell: "zsh", Name: "work"})
	if got != "work" {
		t.Fatalf("label = %q", got)
	}
}

func TestActiveRenameRequestsPrompt(t *testing.T) {
	m := New(types.RemotePeer{ID: "p1", Name: "peer"}, nil)
	m.SetShells([]relay.ActiveShellInfo{{ID: "ab", Shell: "zsh", Name: "work"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("r"))
	if done || cmd == nil {
		t.Fatal("rename request should keep menu open")
	}
	msg := cmd().(types.RemoteShellRenameRequestMsg)
	if msg.Peer.ID != "p1" || msg.ShellID != "ab" || msg.CurrentName != "work" {
		t.Fatalf("bad rename msg %+v", msg)
	}
}

func TestActiveKillRequestsConfirmationAndKeepsMenu(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, nil)
	m.SetShells([]relay.ActiveShellInfo{{ID: "ab", Shell: "zsh"}})
	m.cursor = 1
	done, cmd := m.Update(keyText("d"))
	if done {
		t.Fatal("kill request should keep menu open")
	}
	if _, ok := cmd().(types.RemoteShellKillRequestMsg); !ok {
		t.Fatal("d should emit kill confirmation request")
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
