package app

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func matchCtrlShift(msg tea.KeyPressMsg, letter rune) bool {
	k := msg.Key()
	if !k.Mod.Contains(tea.ModCtrl) || !k.Mod.Contains(tea.ModShift) {
		return false
	}
	lower := unicode.ToLower(letter)
	upper := unicode.ToUpper(letter)
	return k.Code == lower || k.Code == upper || k.ShiftedCode == lower || k.ShiftedCode == upper
}

func matchCtrlShiftAnyOf(msg tea.KeyPressMsg, binding key.Binding) bool {
	for _, k := range binding.Keys() {
		if len(k) > len("ctrl+shift+") && strings.HasPrefix(k, "ctrl+shift+") {
			letter := []rune(strings.TrimPrefix(k, "ctrl+shift+"))
			if len(letter) == 1 && matchCtrlShift(msg, letter[0]) {
				return true
			}
		}
	}
	return false
}

func matchAppNextTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.NextTab) {
		return true
	}
	k := msg.Key()
	if k.Mod.Contains(tea.ModAlt) && k.Code == 'n' {
		return true
	}
	if !k.Mod.Contains(tea.ModCtrl) {
		return false
	}
	if k.Code == tea.KeyTab && !k.Mod.Contains(tea.ModShift) {
		return true
	}
	if k.Code == tea.KeyPgDown {
		return true
	}
	if k.Code == ']' {
		return true
	}
	if k.Code == tea.KeyRight {
		return true
	}
	return false
}

func matchAppPrevTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.PrevTab) {
		return true
	}
	k := msg.Key()
	if k.Mod.Contains(tea.ModAlt) && k.Code == 'p' {
		return true
	}
	if !k.Mod.Contains(tea.ModCtrl) {
		return false
	}
	if k.Code == tea.KeyTab && k.Mod.Contains(tea.ModShift) {
		return true
	}
	if k.Code == tea.KeyPgUp {
		return true
	}
	if k.Code == tea.KeyLeft {
		return true
	}
	return false
}

func matchAltNumber(msg tea.KeyPressMsg) (int, bool) {
	k := msg.Key()
	if k.Code >= '1' && k.Code <= '9' {
		if k.Mod.Contains(tea.ModAlt) || k.Mod.Contains(tea.ModCtrl) {
			return int(k.Code - '1'), true
		}
	}
	s := msg.String()
	if len(s) >= 3 {
		for _, prefix := range []string{"alt+", "ctrl+"} {
			if len(s) == len(prefix)+1 {
				ch := s[len(prefix)]
				if s[:len(prefix)] == prefix && ch >= '1' && ch <= '9' {
					return int(ch - '1'), true
				}
			}
		}
	}
	return 0, false
}
