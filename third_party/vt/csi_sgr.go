package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handleSgr(params ansi.Params) {
	uv.ReadStyle(params, &e.scr.cur.Pen)
}
