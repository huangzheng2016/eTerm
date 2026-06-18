package sshview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEncodeKeyModifiedSpecialKeys(t *testing.T) {
	m := &Model{}
	tests := []struct {
		name string
		key  tea.Key
		want string
	}{
		{"plain enter", tea.Key{Code: tea.KeyEnter}, "\r"},
		{"shift enter", tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}, "\x1b[13;2u"},
		{"ctrl enter", tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl}, "\x1b[13;5u"},
		{"alt enter", tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt}, "\x1b[13;3u"},
		{"ctrl shift enter", tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl | tea.ModShift}, "\x1b[13;6u"},
		{"plain tab", tea.Key{Code: tea.KeyTab}, "\t"},
		{"shift tab", tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, "\x1b[Z"},
		{"ctrl tab", tea.Key{Code: tea.KeyTab, Mod: tea.ModCtrl}, "\x1b[9;5u"},
		{"shift backspace", tea.Key{Code: tea.KeyBackspace, Mod: tea.ModShift}, "\x1b[127;2u"},
		{"shift up", tea.Key{Code: tea.KeyUp, Mod: tea.ModShift}, "\x1b[1;2A"},
		{"alt down", tea.Key{Code: tea.KeyDown, Mod: tea.ModAlt}, "\x1b[1;3B"},
		{"ctrl right", tea.Key{Code: tea.KeyRight, Mod: tea.ModCtrl}, "\x1b[1;5C"},
		{"ctrl shift left", tea.Key{Code: tea.KeyLeft, Mod: tea.ModCtrl | tea.ModShift}, "\x1b[1;6D"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(m.encodeKey(tea.KeyPressMsg(tt.key)))
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeKeyReservedCtrlShiftLettersAreNotForwarded(t *testing.T) {
	m := &Model{}
	got := m.encodeKey(tea.KeyPressMsg(tea.Key{Code: 't', Mod: tea.ModCtrl | tea.ModShift}))
	if got != nil {
		t.Fatalf("got %q want nil", got)
	}
}
