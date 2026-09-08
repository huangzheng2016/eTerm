package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) nextTab(n int) {
	x, y := e.scr.CursorPosition()
	scroll := e.scr.ScrollRegion()
	for range n {
		ts := e.tabstops.Next(x)
		if ts < x {
			break
		}
		x = ts
	}

	if x >= scroll.Max.X {
		x = min(scroll.Max.X-1, e.Width()-1)
	}

	e.scr.setCursor(x, y, false)
}

func (e *Emulator) prevTab(n int) {
	x, _ := e.scr.CursorPosition()
	leftmargin := 0
	scroll := e.scr.ScrollRegion()
	if e.isModeSet(ansi.DECOM) {
		leftmargin = scroll.Min.X
	}

	for range n {
		ts := e.tabstops.Prev(x)
		if ts > x {
			break
		}
		x = ts
	}

	if x < leftmargin {
		x = leftmargin
	}

	e.scr.setCursorX(x, false)
}

func (e *Emulator) moveCursor(dx, dy int) {
	e.scr.moveCursor(dx, dy)
	e.atPhantom = false
}

func (e *Emulator) setCursor(x, y int) {
	e.scr.setCursor(x, y, false)
	e.atPhantom = false
}

func (e *Emulator) setCursorPosition(x, y int) {
	mode, ok := e.modes[ansi.DECOM]
	margins := ok && mode.IsSet()
	e.scr.setCursor(x, y, margins)
	e.atPhantom = false
}

func (e *Emulator) carriageReturn() {
	mode, ok := e.modes[ansi.DECOM]
	margins := ok && mode.IsSet()
	x, y := e.scr.CursorPosition()
	if margins {
		e.scr.setCursor(0, y, true)
	} else if region := e.scr.ScrollRegion(); uv.Pos(x, y).In(region) {
		e.scr.setCursor(region.Min.X, y, false)
	} else {
		e.scr.setCursor(0, y, false)
	}
	e.atPhantom = false
}

func (e *Emulator) repeatPreviousCharacter(n int) {
	if e.lastChar == 0 {
		return
	}
	for range n {
		e.handlePrint(e.lastChar)
	}
}
