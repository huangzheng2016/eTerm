package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
)

func enterMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
}

func TestImportHostList_EnterRenameSingleAlias(t *testing.T) {
	items := []importHostEntry{
		{
			rec:         parser.HostRecord{Aliases: []string{"web"}, Host: "1.2.3.4", Port: 22, Username: "root"},
			chosenAlias: "web",
		},
	}
	m := newImportHostList(items)

	m.Update(enterMsg())
	if m.state != hostListStateRename {
		t.Fatalf("expected rename state, got %d", m.state)
	}

	m.Update(keyMsg("2"))
	m.Update(enterMsg())
	if m.state != hostListStateList {
		t.Fatalf("expected list state after save, got %d", m.state)
	}
	if m.items[0].chosenAlias != "web2" {
		t.Fatalf("expected chosenAlias web2, got %q", m.items[0].chosenAlias)
	}
}

func TestImportHostList_EnterMultiAliasGoesToAliasSelect(t *testing.T) {
	items := []importHostEntry{
		{
			rec:         parser.HostRecord{Aliases: []string{"web", "web2"}, Host: "1.2.3.4", Port: 22, Username: "root"},
			chosenAlias: "web",
		},
	}
	m := newImportHostList(items)

	m.Update(enterMsg())
	if m.state != hostListStateAlias {
		t.Fatalf("expected alias state, got %d", m.state)
	}
}

func TestImportHostList_EnterNoopInExportMode(t *testing.T) {
	items := []importHostEntry{
		{
			rec:         parser.HostRecord{Aliases: []string{"web"}, Host: "1.2.3.4", Port: 22, Username: "root"},
			chosenAlias: "web",
		},
	}
	m := newImportHostList(items)
	m.exportMode = true

	m.Update(enterMsg())
	if m.state != hostListStateList {
		t.Fatalf("expected list state in exportMode, got %d", m.state)
	}
}
