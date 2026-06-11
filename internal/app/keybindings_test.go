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
