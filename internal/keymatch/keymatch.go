// Package keymatch matches host-list shortcuts without relying on charm bubbles
// key.Matches, which compares msg.String(): for many keys String() returns Key.Text,
// so Enter can be "\r" instead of "enter". Prefer Keystroke() and ultraviolet.MatchString.
package keymatch

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	tea "charm.land/bubbletea/v2"
)

// Config holds configurable key targets for host-list shortcuts.
type Config struct {
	ConnectKeys []string // e.g. ["enter"]
	SFTPKeys    []string // e.g. ["ctrl+f", "s"]
	NewHostKey  rune
	NewHostName string
	EditKey     rune
	EditName    string
	DeleteKey   rune
	DeleteName  string
	CopyKey     rune
	CopyName    string
	SearchKey   rune
	SearchName  string
}

// DefaultConfig returns the default keymatch configuration.
func DefaultConfig() Config {
	return Config{
		ConnectKeys: []string{"enter"},
		SFTPKeys:    []string{"ctrl+f", "s"},
		NewHostKey:  'n',
		NewHostName: "n",
		EditKey:     'e',
		EditName:    "e",
		DeleteKey:   'd',
		DeleteName:  "d",
		CopyKey:     'c',
		CopyName:    "c",
		SearchKey:   '/',
		SearchName:  "/",
	}
}

func teaKey(k tea.Key) uv.Key {
	return uv.Key(k)
}

// noHostileChord is true when Ctrl/Alt/Meta are not held (plain Enter / line break).
func noHostileChord(m tea.KeyMod) bool {
	return !m.Contains(tea.ModCtrl) && !m.Contains(tea.ModAlt) && !m.Contains(tea.ModMeta)
}

// MatchConnect reports whether msg should trigger SSH connect on the host list.
func (c Config) MatchConnect(msg tea.KeyPressMsg) bool {
	k := msg.Key()
	uk := teaKey(k)

	for _, target := range c.ConnectKeys {
		if uk.MatchString(target) || uk.MatchString("shift+"+target) {
			return true
		}
		ks := k.Keystroke()
		if ks == target || ks == "shift+"+target {
			return true
		}
	}

	// Enter-specific fallbacks for terminal compatibility
	if containsKey(c.ConnectKeys, "enter") {
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
		if k.BaseCode == tea.KeyEnter || k.BaseCode == tea.KeyKpEnter {
			return noHostileChord(k.Mod)
		}
		if (k.Code == '\r' || k.Code == '\n') && noHostileChord(k.Mod) {
			return true
		}
	}

	return false
}

// MatchSFTP reports whether msg should open SFTP on the host list.
func (c Config) MatchSFTP(msg tea.KeyPressMsg) bool {
	k := msg.Key()
	uk := teaKey(k)
	ks := k.Keystroke()

	// ctrl+s is terminal XOFF, never match it
	if ks == "ctrl+s" {
		return false
	}

	for _, target := range c.SFTPKeys {
		if uk.MatchString(target) || ks == target {
			return true
		}
		// Check modifier-aware matching
		if strings.HasPrefix(target, "ctrl+") {
			letter := strings.TrimPrefix(target, "ctrl+")
			if len(letter) == 1 && k.Code == rune(letter[0]) && k.Mod.Contains(tea.ModCtrl) {
				return true
			}
		} else if len(target) == 1 {
			r := rune(target[0])
			if k.Code == r && !k.Mod.Contains(tea.ModCtrl) {
				return true
			}
		}
	}

	return false
}

// plainUnmodifiedKey matches a single-key list shortcut without any modifier (Ctrl/Alt/Meta/Shift).
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

func (c Config) MatchNewHost(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, c.NewHostKey, c.NewHostName)
}

func (c Config) MatchEdit(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, c.EditKey, c.EditName)
}

func (c Config) MatchDelete(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, c.DeleteKey, c.DeleteName)
}

func (c Config) MatchCopy(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, c.CopyKey, c.CopyName)
}

func (c Config) MatchSearch(msg tea.KeyPressMsg) bool {
	return plainUnmodifiedKey(msg, c.SearchKey, c.SearchName)
}

func containsKey(keys []string, target string) bool {
	for _, k := range keys {
		if k == target {
			return true
		}
	}
	return false
}

// Package-level convenience functions using DefaultConfig for backward compatibility.

func MatchConnect(msg tea.KeyPressMsg) bool  { return DefaultConfig().MatchConnect(msg) }
func MatchSFTP(msg tea.KeyPressMsg) bool     { return DefaultConfig().MatchSFTP(msg) }
func MatchNewHost(msg tea.KeyPressMsg) bool   { return DefaultConfig().MatchNewHost(msg) }
func MatchEdit(msg tea.KeyPressMsg) bool      { return DefaultConfig().MatchEdit(msg) }
func MatchDelete(msg tea.KeyPressMsg) bool    { return DefaultConfig().MatchDelete(msg) }
func MatchCopy(msg tea.KeyPressMsg) bool      { return DefaultConfig().MatchCopy(msg) }
func MatchSearch(msg tea.KeyPressMsg) bool    { return DefaultConfig().MatchSearch(msg) }
