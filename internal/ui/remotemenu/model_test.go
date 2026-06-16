package remotemenu

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestRemoteMenuDoesNotUseTabPrefixesOrListTags(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, []types.RemoteHost{{Alias: "prod", Hostname: "prod.example", Username: "root", Port: 22, Tags: "web", Group: "Prod"}})

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

func keyMsg(s string) tea.KeyPressMsg {
	if s == "pgdown" {
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	}
	return tea.KeyPressMsg(tea.Key{Text: s})
}

func keyText(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(s)[0], Text: s})
}
