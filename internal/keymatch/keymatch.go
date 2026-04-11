// Package keymatch matches host-list shortcuts without relying on charm bubbles
// key.Matches, which compares msg.String(): for many keys String() returns Key.Text,
// so Enter can be "\r" instead of "enter". Prefer Keystroke() and ultraviolet.MatchString.
package keymatch

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	tea "charm.land/bubbletea/v2"
)

func teaKey(k tea.Key) uv.Key {
	return uv.Key(k)
}

// noHostileChord is true when Ctrl/Alt/Meta are not held (plain Enter / line break).
func noHostileChord(m tea.KeyMod) bool {
	return !m.Contains(tea.ModCtrl) && !m.Contains(tea.ModAlt) && !m.Contains(tea.ModMeta)
}

// MatchConnect reports whether msg should trigger SSH connect on the host list.
func MatchConnect(msg tea.KeyPressMsg) bool {
	k := msg.Key()
	uk := teaKey(k)

	if uk.MatchString("enter") || uk.MatchString("shift+enter") {
		return true
	}

	switch k.Keystroke() {
	case "enter", "shift+enter":
		return true
	}

	if k.Code == tea.KeyEnter || k.Code == tea.KeyKpEnter {
		return noHostileChord(k.Mod)
	}

	t := k.Text
	if t != "" {
		if strings.HasPrefix(t, "\r\n") || strings.Contains(t, "\r\n") {
			return noHostileChord(k.Mod)
		}
		switch t {
		case "\r", "\n":
			return noHostileChord(k.Mod)
		default:
			if strings.HasPrefix(t, "\r") {
				return noHostileChord(k.Mod)
			}
		}
	}

	// BaseCode (Kitty / enhanced keyboards): physical key is Enter even if Code differs.
	if k.BaseCode == tea.KeyEnter || k.BaseCode == tea.KeyKpEnter {
		return noHostileChord(k.Mod)
	}

	// Rune 13 / 10 as last resort when Code constant mismatches.
	if (k.Code == '\r' || k.Code == '\n') && noHostileChord(k.Mod) {
		return true
	}

	return false
}

// MatchSFTP reports whether msg should open SFTP on the host list.
// Deliberately does not treat ctrl+s (terminal XOFF / flow control) as SFTP.
func MatchSFTP(msg tea.KeyPressMsg) bool {
	k := msg.Key()
	uk := teaKey(k)
	ks := k.Keystroke()

	if ks == "ctrl+s" {
		return false
	}

	if uk.MatchString("ctrl+f") || ks == "ctrl+f" {
		return true
	}

	if k.Code == 'f' && k.Mod.Contains(tea.ModCtrl) {
		return true
	}

	if ks == "s" || (uk.MatchString("s") && !k.Mod.Contains(tea.ModCtrl)) {
		return true
	}

	if k.Code == 's' && !k.Mod.Contains(tea.ModCtrl) {
		return true
	}

	return false
}

// plainUnmodifiedKey matches a single-key list shortcut without any modifier (Ctrl/Alt/Meta/Shift).
// name is the ultraviolet keystroke token (e.g. "n", "e", "/", "c").
func plainUnmodifiedKey(msg tea.KeyPressMsg, code rune, name string) bool {
	k := msg.Key()
	if k.Mod.Contains(tea.ModCtrl) || k.Mod.Contains(tea.ModAlt) || k.Mod.Contains(tea.ModMeta) || k.Mod.Contains(tea.ModShift) {
		return false
	}
	uk := teaKey(k)
	if uk.MatchString(name) {
		return true
	}
	ks := k.Keystroke()
	if ks == name {
		return true
	}
	if k.Code == code {
		return true
	}
	if k.BaseCode == code {
		return true
	}
	return false
}

// MatchNewHost is the "n" shortcut (new host editor tab).
func MatchNewHost(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, 'n', "n")
}

// MatchEdit is the "e" shortcut (edit selected host).
func MatchEdit(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, 'e', "e")
}

// MatchDelete is the "d" shortcut (delete host).
func MatchDelete(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, 'd', "d")
}

// MatchCopy is the "c" shortcut (copy ssh command line).
func MatchCopy(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, 'c', "c")
}

// MatchSearch is the "/" shortcut (start list filter). Callers may ignore it if the list consumes "/".
func MatchSearch(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, '/', "/")
}
