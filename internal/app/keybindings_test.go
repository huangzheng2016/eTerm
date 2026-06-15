package app

import "testing"

func TestBuildKeyMapNewTabHelpUsesConfig(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	cfg.NewTab = []string{"ctrl+shift+t"}

	km := BuildKeyMap(cfg)

	if got := km.NewTab.Help().Key; got != "ctrl+shift+t" {
		t.Fatalf("got %q want ctrl+shift+t", got)
	}
}

func TestDefaultLocalTerminalKey(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	if len(cfg.LocalTerminal) != 1 || cfg.LocalTerminal[0] != "ctrl+shift+t" {
		t.Fatalf("LocalTerminal = %#v", cfg.LocalTerminal)
	}
}
