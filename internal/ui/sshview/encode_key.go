package sshview

import (
	"fmt"
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
		if seq, ok := modifiedCSIU(13, mod); ok {
			return seq
		}
		return []byte{'\r'}
	case tea.KeyTab:
		if mod == tea.ModShift {
			return []byte("\x1b[Z")
		}
		if seq, ok := modifiedCSIU(9, mod); ok {
			return seq
		}
		return []byte{'\t'}
	case tea.KeyBackspace:
		if seq, ok := modifiedCSIU(127, mod); ok {
			return seq
		}
		return []byte{0x7f}
	case tea.KeyEscape:
		if seq, ok := modifiedCSIU(27, mod); ok {
			return seq
		}
		return []byte{0x1b}
	case tea.KeyUp:
		if seq, ok := modifiedCursorKey('A', mod); ok {
			return seq
		}
		return cursorKey(ack, 'A')
	case tea.KeyDown:
		if seq, ok := modifiedCursorKey('B', mod); ok {
			return seq
		}
		return cursorKey(ack, 'B')
	case tea.KeyRight:
		if seq, ok := modifiedCursorKey('C', mod); ok {
			return seq
		}
		return cursorKey(ack, 'C')
	case tea.KeyLeft:
		if seq, ok := modifiedCursorKey('D', mod); ok {
			return seq
		}
		return cursorKey(ack, 'D')
	case tea.KeyInsert:
		if seq, ok := modifiedTildeKey(2, mod); ok {
			return seq
		}
		return []byte("\x1b[2~")
	case tea.KeyDelete:
		if seq, ok := modifiedTildeKey(3, mod); ok {
			return seq
		}
		return []byte("\x1b[3~")
	case tea.KeyHome:
		if seq, ok := modifiedCursorKey('H', mod); ok {
			return seq
		}
		return []byte("\x1b[H")
	case tea.KeyEnd:
		if seq, ok := modifiedCursorKey('F', mod); ok {
			return seq
		}
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		if seq, ok := modifiedTildeKey(5, mod); ok {
			return seq
		}
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		if seq, ok := modifiedTildeKey(6, mod); ok {
			return seq
		}
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

func modifierParam(mod tea.KeyMod) (int, bool) {
	mod &= tea.ModShift | tea.ModAlt | tea.ModCtrl | tea.ModMeta
	if mod == 0 {
		return 0, false
	}
	return int(mod) + 1, true
}

func modifiedCSIU(code int, mod tea.KeyMod) ([]byte, bool) {
	p, ok := modifierParam(mod)
	if !ok {
		return nil, false
	}
	return []byte(fmt.Sprintf("\x1b[%d;%du", code, p)), true
}

func modifiedCursorKey(ch byte, mod tea.KeyMod) ([]byte, bool) {
	p, ok := modifierParam(mod)
	if !ok {
		return nil, false
	}
	return []byte(fmt.Sprintf("\x1b[1;%d%c", p, ch)), true
}

func modifiedTildeKey(code int, mod tea.KeyMod) ([]byte, bool) {
	p, ok := modifierParam(mod)
	if !ok {
		return nil, false
	}
	return []byte(fmt.Sprintf("\x1b[%d;%d~", code, p)), true
}
