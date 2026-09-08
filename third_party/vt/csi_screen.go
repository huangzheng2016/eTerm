package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
)

func (e *Emulator) eraseCharacter(n int) {
	if n <= 0 {
		n = 1
	}
	x, y := e.scr.CursorPosition()
	rect := uv.Rect(x, y, n, 1)
	e.scr.FillArea(e.scr.blankCell(), rect)
	e.atPhantom = false
}
