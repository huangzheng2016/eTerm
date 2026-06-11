package settingsview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestKeyStringPrefersPrintableString(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: '/', Text: "?", Mod: tea.ModShift})

	if got := keyString(msg); got != "?" {
		t.Fatalf("got %q want ?", got)
	}
}

func TestKeyStringKeepsModifierKeystroke(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: 'h', Mod: tea.ModCtrl | tea.ModShift})

	if got := keyString(msg); got != "ctrl+shift+h" {
		t.Fatalf("got %q want ctrl+shift+h", got)
	}
}
