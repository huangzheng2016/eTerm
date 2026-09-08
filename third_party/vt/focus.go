package vt

import (
	"io"

	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) Focus() {
	e.focus(true)
}

func (e *Emulator) Blur() {
	e.focus(false)
}

func (e *Emulator) focus(focus bool) {
	if mode, ok := e.modes[ansi.ModeFocusEvent]; ok && mode.IsSet() {
		if focus {
			_, _ = io.WriteString(e.pw, ansi.Focus)
		} else {
			_, _ = io.WriteString(e.pw, ansi.Blur)
		}
	}
}
