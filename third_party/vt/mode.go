package vt

import "github.com/charmbracelet/x/ansi"

func (e *Emulator) resetModes() {
	e.modes = ansi.Modes{
		ansi.ModeCursorKeys:          ansi.ModeReset,
		ansi.ModeOrigin:              ansi.ModeReset,
		ansi.ModeAutoWrap:            ansi.ModeSet,
		ansi.ModeMouseX10:            ansi.ModeReset,
		ansi.ModeLineFeedNewLine:     ansi.ModeReset,
		ansi.ModeTextCursorEnable:    ansi.ModeSet,
		ansi.ModeNumericKeypad:       ansi.ModeReset,
		ansi.ModeLeftRightMargin:     ansi.ModeReset,
		ansi.ModeMouseNormal:         ansi.ModeReset,
		ansi.ModeMouseHighlight:      ansi.ModeReset,
		ansi.ModeMouseButtonEvent:    ansi.ModeReset,
		ansi.ModeMouseAnyEvent:       ansi.ModeReset,
		ansi.ModeFocusEvent:          ansi.ModeReset,
		ansi.ModeMouseExtSgr:         ansi.ModeReset,
		ansi.ModeAltScreen:           ansi.ModeReset,
		ansi.ModeSaveCursor:          ansi.ModeReset,
		ansi.ModeAltScreenSaveCursor: ansi.ModeReset,
		ansi.ModeBracketedPaste:      ansi.ModeReset,
	}

	for mode, setting := range e.modes {
		e.setMode(mode, setting)
	}
}
