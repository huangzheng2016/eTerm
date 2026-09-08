package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handleControl(r byte) {
	e.flushGrapheme()
	if !e.handleCc(r) {
		e.logf("unhandled sequence: ControlCode %q", r)
	}
}

func (e *Emulator) linefeed() {
	_, y := e.scr.CursorPosition()
	e.scr.SetLineWrapped(y, false)
	e.index()
	if e.isModeSet(ansi.ModeLineFeedNewLine) {
		e.carriageReturn()
	}
}

func (e *Emulator) index() {
	x, y := e.scr.CursorPosition()
	scroll := e.scr.ScrollRegion()
	if y == scroll.Max.Y-1 && x >= scroll.Min.X && x < scroll.Max.X {
		e.scr.ScrollUp(1)
	} else if y < scroll.Max.Y-1 || !uv.Pos(x, y).In(scroll) {
		e.scr.moveCursor(0, 1)
	}
	e.atPhantom = false
}

func (e *Emulator) horizontalTabSet() {
	x, _ := e.scr.CursorPosition()
	e.tabstops.Set(x)
}

func (e *Emulator) reverseIndex() {
	x, y := e.scr.CursorPosition()
	scroll := e.scr.ScrollRegion()
	if y == scroll.Min.Y && x >= scroll.Min.X && x < scroll.Max.X {
		e.scr.ScrollDown(1)
	} else {
		e.scr.moveCursor(0, -1)
	}
}

func (e *Emulator) backspace() {
	e.moveCursor(-1, 0)
}
