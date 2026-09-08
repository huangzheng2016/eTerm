package vt

import (
	"io"

	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handleMode(params ansi.Params, set, isAnsi bool) {
	for _, p := range params {
		param := p.Param(-1)
		if param == -1 {
			continue
		}

		var mode ansi.Mode = ansi.DECMode(param)
		if isAnsi {
			mode = ansi.ANSIMode(param)
		}

		setting := e.modes[mode]
		if setting == ansi.ModePermanentlyReset || setting == ansi.ModePermanentlySet {
			continue
		}

		setting = ansi.ModeReset
		if set {
			setting = ansi.ModeSet
		}

		e.setMode(mode, setting)
	}
}

func (e *Emulator) setAltScreenMode(on bool) {
	if (on && e.scr == &e.scrs[1]) || (!on && e.scr == &e.scrs[0]) {
		return
	}
	if on {
		e.scr = &e.scrs[1]
		e.scrs[1].cur = e.scrs[0].cur
		e.scr.Clear()
		e.scr.ClearTouched()
		e.setCursor(0, 0)
	} else {
		e.scr = &e.scrs[0]
	}
	if e.cb.AltScreen != nil {
		e.cb.AltScreen(on)
	}
	if e.cb.CursorVisibility != nil {
		e.cb.CursorVisibility(!e.scr.cur.Hidden)
	}
}

func (e *Emulator) saveCursor() {
	e.scr.SaveCursor()
}

func (e *Emulator) restoreCursor() {
	e.scr.RestoreCursor()
}

func (e *Emulator) setMode(mode ansi.Mode, setting ansi.ModeSetting) {
	e.logf("setting mode %T(%v) to %v", mode, mode, setting)
	e.modes[mode] = setting
	switch mode {
	case ansi.ModeTextCursorEnable:
		e.scr.setCursorHidden(!setting.IsSet())
	case ansi.ModeAltScreen:
		e.setAltScreenMode(setting.IsSet())
	case ansi.ModeSaveCursor:
		if setting.IsSet() {
			e.saveCursor()
		} else {
			e.restoreCursor()
		}
	case ansi.ModeAltScreenSaveCursor:
		if setting.IsSet() {
			e.saveCursor()
		}
		e.setAltScreenMode(setting.IsSet())
	case ansi.ModeInBandResize:
		if setting.IsSet() {
			_, _ = io.WriteString(e.pw, ansi.InBandResize(e.Height(), e.Width(), 0, 0))
		}
	}
	if setting.IsSet() {
		if e.cb.EnableMode != nil {
			e.cb.EnableMode(mode)
		}
	} else if setting.IsReset() {
		if e.cb.DisableMode != nil {
			e.cb.DisableMode(mode)
		}
	}
}

func (e *Emulator) isModeSet(mode ansi.Mode) bool {
	m, ok := e.modes[mode]
	return ok && m.IsSet()
}
