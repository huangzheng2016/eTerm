package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
)

func enterMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
}

func TestImportHostList_EnterTransitions(t *testing.T) {
	tests := []struct {
		name       string
		aliases    []string
		exportMode bool
		want       hostListState
	}{
		{name: "single alias goes to rename", aliases: []string{"web"}, want: hostListStateRename},
		{name: "multi alias goes to alias select", aliases: []string{"web", "web2"}, want: hostListStateAlias},
		{name: "noop in export mode", aliases: []string{"web"}, exportMode: true, want: hostListStateList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newImportHostList([]importHostEntry{{
				rec:         parser.HostRecord{Aliases: tt.aliases, Host: "1.2.3.4", Port: 22, Username: "root"},
				chosenAlias: "web",
			}})
			m.exportMode = tt.exportMode

			m.Update(enterMsg())
			if m.state != tt.want {
				t.Fatalf("state = %d, want %d", m.state, tt.want)
			}
		})
	}
}

func TestImportHostList_RenameSavesAlias(t *testing.T) {
	m := newImportHostList([]importHostEntry{{
		rec:         parser.HostRecord{Aliases: []string{"web"}, Host: "1.2.3.4", Port: 22, Username: "root"},
		chosenAlias: "web",
	}})

	m.Update(enterMsg())
	m.Update(keyMsg("2"))
	m.Update(enterMsg())
	if m.state != hostListStateList {
		t.Fatalf("expected list state after save, got %d", m.state)
	}
	if m.items[0].chosenAlias != "web2" {
		t.Fatalf("expected chosenAlias web2, got %q", m.items[0].chosenAlias)
	}
}
