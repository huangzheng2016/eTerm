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

// matchCtrlShiftAnyOf extracts Ctrl+Shift letters from a key.Binding and matches any of them.
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

// matchCtrlShiftFromKeys extracts Ctrl+Shift letters from a key string slice and matches any.
func matchCtrlShiftFromKeys(msg tea.KeyPressMsg, keys []string) bool {
	for _, k := range keys {
		if len(k) > len("ctrl+shift+") && strings.HasPrefix(k, "ctrl+shift+") {
			letter := []rune(strings.TrimPrefix(k, "ctrl+shift+"))
			if len(letter) == 1 && matchCtrlShift(msg, letter[0]) {
				return true
			}
		}
	}
	return false
}

// matchAppNextTab matches next-tab chords even when key.Matches misses (terminal string quirks).
func matchAppNextTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.NextTab) {
		return true
	}
	k := msg.Key()
	// Alt+n as reliable alternative (works in all terminals)
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
	// Ctrl+] as alternative next-tab (works in most terminals)
	if k.Code == ']' {
		return true
	}
	// Ctrl+Right
	if k.Code == tea.KeyRight {
		return true
	}
	return false
}

// matchAppPrevTab matches previous-tab chords; Ctrl+Shift+Tab is matched structurally so it
// always switches tabs and is never forwarded to an SSH PTY.
func matchAppPrevTab(msg tea.KeyPressMsg, km KeyMap) bool {
	if key.Matches(msg, km.PrevTab) {
		return true
	}
	k := msg.Key()
	// Alt+p as reliable alternative (works in all terminals)
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
	// Ctrl+Left
	if k.Code == tea.KeyLeft {
		return true
	}
	// Ctrl+[ is ESC (0x1b), skip it
	return false
}

// matchAltNumber checks for Alt+1..9 or Ctrl+1..9 to jump to a specific tab.
func matchAltNumber(msg tea.KeyPressMsg) (int, bool) {
	k := msg.Key()
	if k.Code >= '1' && k.Code <= '9' {
		if k.Mod.Contains(tea.ModAlt) || k.Mod.Contains(tea.ModCtrl) {
			return int(k.Code - '1'), true
		}
	}
	// Fallback: parse the string representation for terminals that encode Alt as ESC prefix.
	s := msg.String()
	if len(s) >= 3 {
		// e.g. "alt+1", "ctrl+1"
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
