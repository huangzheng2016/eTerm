package app

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestBuildKeyMapNewTabHelpUsesConfig(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	cfg.NewTab = []string{"ctrl+shift+t"}

	km := BuildKeyMap(cfg)

	if got := km.NewTab.Help().Key; got != "C-S-t" {
		t.Fatalf("got %q want C-S-t", got)
	}
}

func TestDefaultLocalTerminalKey(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	if len(cfg.LocalTerminal) != 1 || cfg.LocalTerminal[0] != "ctrl+shift+t" {
		t.Fatalf("LocalTerminal = %#v", cfg.LocalTerminal)
	}
}

func TestDefaultPasteImageURLKey(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	if len(cfg.PasteImageURL) != 1 || cfg.PasteImageURL[0] != "ctrl+shift+i" {
		t.Fatalf("PasteImageURL = %#v", cfg.PasteImageURL)
	}
}

func TestDefaultTabPageKeys(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	if len(cfg.TabPageLeft) != 1 || cfg.TabPageLeft[0] != "alt+shift+left" {
		t.Fatalf("TabPageLeft = %#v", cfg.TabPageLeft)
	}
	if len(cfg.TabPageRight) != 1 || cfg.TabPageRight[0] != "alt+shift+right" {
		t.Fatalf("TabPageRight = %#v", cfg.TabPageRight)
	}
}

func TestBuildKeyMapTabPageKeysUseConfig(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	cfg.TabPageLeft = []string{"alt+shift+up"}
	cfg.TabPageRight = []string{"alt+shift+down"}

	km := BuildKeyMap(cfg)
	if !key.Matches(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp, Mod: tea.ModAlt | tea.ModShift}), km.TabPageLeft) {
		t.Fatal("TabPageLeft does not use config")
	}
	if !key.Matches(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown, Mod: tea.ModAlt | tea.ModShift}), km.TabPageRight) {
		t.Fatal("TabPageRight does not use config")
	}
}
