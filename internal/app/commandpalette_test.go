package app

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestCommandPaletteFiltersAndSelectsHostConnect(t *testing.T) {
	p := newCommandPalette([]commandPaletteItem{
		{Title: "Connect prod", Subtitle: "host", Search: "prod web", Msg: types.SSHConnectMsg{HostID: 42}},
		{Title: "Open settings", Subtitle: "app", Search: "settings", Msg: types.OpenSettingsMsg{}},
	})
	p.input.SetValue("prod")
	p.refresh()

	if len(p.filtered) != 1 {
		t.Fatalf("got %d filtered items, want 1", len(p.filtered))
	}
	msg := p.selectedMsg()
	if got, ok := msg.(types.SSHConnectMsg); !ok || got.HostID != 42 {
		t.Fatalf("got %#v, want SSHConnectMsg host 42", msg)
	}
}

func TestCommandPaletteMouseSelectsItem(t *testing.T) {
	a := App{commandPalette: newCommandPalette([]commandPaletteItem{
		{Title: "Connect prod", Subtitle: "host", Search: "prod", Msg: types.SSHConnectMsg{HostID: 7}},
	})}

	_, cmd := a.commandPaletteMouse(2, 4)
	if cmd == nil {
		t.Fatal("expected selection command")
	}
	msg := cmd()
	if got, ok := msg.(types.SSHConnectMsg); !ok || got.HostID != 7 {
		t.Fatalf("got %#v, want SSHConnectMsg host 7", msg)
	}
}
