package app

import (
	"strings"
	"testing"
)

func TestListStatusBarsMatchDefaultActions(t *testing.T) {
	cfg := DefaultKeyBindingConfig()
	km := BuildKeyMap(cfg)
	tests := []struct {
		name      string
		tab       TabType
		want      []string
		forbidden []string
	}{
		{name: "hosts", tab: HomeTab, want: []string{"enter connect", "n new", "e edit", "d delete", "/ search"}},
		{name: "keys", tab: KeyTab, want: []string{"enter details", "n generate", "i import", "e edit", "d delete", "c copy pubkey"}, forbidden: []string{"export"}},
		{name: "forwards", tab: ForwardTab, want: []string{"enter start", "x stop", "n new", "e edit", "d delete"}},
		{name: "snippets", tab: SnippetTab, want: []string{"n new", "e edit", "d delete"}, forbidden: []string{"run"}},
		{name: "sessions", tab: SessionListTab, want: []string{"enter read", "/ search", "c copy transcript"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint := mainViewStatusBarHint(km, cfg, tt.tab, false, false)
			for _, want := range tt.want {
				if !strings.Contains(hint, want) {
					t.Fatalf("status %q missing %q", hint, want)
				}
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(hint, forbidden) {
					t.Fatalf("status %q contains invalid action %q", hint, forbidden)
				}
			}
		})
	}
}
