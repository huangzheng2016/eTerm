package viewkeys

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMatchKeyMatchesPrintableString(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: '/', Text: "?", Mod: tea.ModShift})

	if !MatchKey(msg, []string{"?"}) {
		t.Fatal("expected ? to match printable key string")
	}
}

func TestMatchKeyMatchesKeystrokeString(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: '/', Text: "?", Mod: tea.ModShift})

	if !MatchKey(msg, []string{"shift+/"}) {
		t.Fatal("expected ? to match shift+/ keystroke")
	}
}

func TestMatchKeyMatchesCtrlShiftLetter(t *testing.T) {
	msg := tea.KeyPressMsg(tea.Key{Code: 'h', Mod: tea.ModCtrl | tea.ModShift})

	if !MatchKey(msg, []string{"ctrl+shift+h"}) {
		t.Fatal("expected ctrl+shift+h to match")
	}
}

func TestHelpLabelUsesFirstTwoKeys(t *testing.T) {
	got := HelpLabel([]string{"ctrl+f", "s", "alt+s"})
	if got != "ctrl+f/s" {
		t.Fatalf("got %q", got)
	}
}
