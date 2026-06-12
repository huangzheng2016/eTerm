package sshview

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// encodeKey maps a Bubble Tea key event to bytes for the remote PTY (xterm-256color).
//
// Application cursor keys (ESC O A vs ESC [ A) are selected via IsAltScreen()
// as a heuristic for DECCKM — full-screen programs (vim, less) use alt screen.
func (m *Model) encodeKey(msg tea.KeyPressMsg) []byte {
	k := msg.Key()
	mod := k.Mod
	ctrl := mod.Contains(tea.ModCtrl)
	alt := mod.Contains(tea.ModAlt)
	meta := mod.Contains(tea.ModMeta)
	shift := mod.Contains(tea.ModShift)

	// Ctrl+Shift+letter is reserved by the App layer — never send to PTY.
	if ctrl && shift {
		ch := k.Code
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
			return nil
		}
	}

	// Alt/Meta + printable: ESC prefix
	if (alt || meta) && k.Text != "" && !ctrl {
		return append([]byte{0x1b}, []byte(k.Text)...)
	}

	ack := m.appCursorKeys

	// Ctrl + letter → control character
	if ctrl && k.Code >= 'a' && k.Code <= 'z' {
		return []byte{byte(k.Code - 'a' + 1)}
	}
	if ctrl && k.Code >= 'A' && k.Code <= 'Z' {
		return []byte{byte(k.Code - 'A' + 1)}
	}
	if ctrl {
		switch k.Code {
		case '[':
			return []byte{0x1b}
		case '\\':
			return []byte{0x1c}
		case ']':
			return []byte{0x1d}
		case '^':
			return []byte{0x1e}
		case '_':
			return []byte{0x1f}
		case ' ':
			return []byte{0}
		}
	}

	// Special keys by Code
	switch k.Code {
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		if shift {
			return []byte("\x1b[Z")
		}
		return []byte{'\t'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeyUp:
		return cursorKey(ack, 'A')
	case tea.KeyDown:
		return cursorKey(ack, 'B')
	case tea.KeyRight:
		return cursorKey(ack, 'C')
	case tea.KeyLeft:
		return cursorKey(ack, 'D')
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyF1:
		return []byte("\x1bOP")
	case tea.KeyF2:
		return []byte("\x1bOQ")
	case tea.KeyF3:
		return []byte("\x1bOR")
	case tea.KeyF4:
		return []byte("\x1bOS")
	case tea.KeyF5:
		return []byte("\x1b[15~")
	case tea.KeyF6:
		return []byte("\x1b[17~")
	case tea.KeyF7:
		return []byte("\x1b[18~")
	case tea.KeyF8:
		return []byte("\x1b[19~")
	case tea.KeyF9:
		return []byte("\x1b[20~")
	case tea.KeyF10:
		return []byte("\x1b[21~")
	case tea.KeyF11:
		return []byte("\x1b[23~")
	case tea.KeyF12:
		return []byte("\x1b[24~")
	}

	// Keystroke name fallback (some terminals set Keystroke but not Code)
	switch k.Keystroke() {
	case "enter", "shift+enter":
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "shift+tab":
		return []byte("\x1b[Z")
	case "backspace":
		return []byte{0x7f}
	case "escape":
		return []byte{0x1b}
	case "space":
		return []byte{' '}
	case "up":
		return cursorKey(ack, 'A')
	case "down":
		return cursorKey(ack, 'B')
	case "right":
		return cursorKey(ack, 'C')
	case "left":
		return cursorKey(ack, 'D')
	}

	// Printable text (covers normal typing, Shift+key producing symbols, etc.)
	if k.Text != "" {
		return []byte(k.Text)
	}

	// Fallback: try Code / ShiftedCode / Keystroke / String()
	if !ctrl && !alt && !meta {
		ch := k.Code
		if shift && k.ShiftedCode != 0 {
			ch = k.ShiftedCode
		}
		if ch != 0 && unicode.IsPrint(ch) {
			var buf [utf8.UTFMax]byte
			n := utf8.EncodeRune(buf[:], ch)
			return buf[:n]
		}
		if ks := k.Keystroke(); len(ks) == 1 {
			r, _ := utf8.DecodeRuneInString(ks)
			if unicode.IsPrint(r) {
				return []byte(ks)
			}
		}
		if s := msg.String(); len(s) == 1 {
			r, _ := utf8.DecodeRuneInString(s)
			if unicode.IsPrint(r) {
				return []byte(s)
			}
		}
	}

	return nil
}

func cursorKey(appMode bool, ch byte) []byte {
	if appMode {
		return []byte{0x1b, 'O', ch}
	}
	return []byte{0x1b, '[', ch}
}
