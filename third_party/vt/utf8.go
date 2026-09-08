package vt

import (
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handlePrint(r rune) {
	if r >= ansi.SP && r < ansi.DEL {
		if len(e.grapheme) > 0 {
			e.flushGrapheme()
		}
		e.handleGrapheme(string(r), 1)
	} else {
		e.grapheme = append(e.grapheme, r)
	}
}

func (e *Emulator) flushGrapheme() {
	if len(e.grapheme) == 0 {
		return
	}

	method := ansi.GraphemeWidth
	graphemes := string(e.grapheme)
	for len(graphemes) > 0 {
		cluster, width := ansi.FirstGraphemeCluster(graphemes, method)
		e.handleGrapheme(cluster, width)
		graphemes = graphemes[len(cluster):]
	}
	e.grapheme = e.grapheme[:0]
}

func (e *Emulator) handleGrapheme(content string, width int) {
	awm := e.isModeSet(ansi.ModeAutoWrap)
	cell := uv.Cell{
		Content: content,
		Width:   width,
		Style:   e.scr.cursorPen(),
		Link:    e.scr.cursorLink(),
	}

	x, y := e.scr.CursorPosition()
	if e.atPhantom && awm {
		e.scr.SetLineWrapped(y, true)
		e.index()
		_, y = e.scr.CursorPosition()
		x = 0
	}

	if len(content) == 1 { //nolint:nestif
		var charset CharSet
		c := content[0]
		if e.gsingle > 1 && e.gsingle < 4 {
			charset = e.charsets[e.gsingle]
			e.gsingle = 0
		} else if c < 128 {
			charset = e.charsets[e.gl]
		} else {
			charset = e.charsets[e.gr]
		}

		if charset != nil {
			if r, ok := charset[c]; ok {
				cell.Content = r
				cell.Width = 1
			}
		}
	}

	if cell.Width == 1 && len(content) == 1 {
		e.lastChar, _ = utf8.DecodeRuneInString(content)
	}

	e.scr.SetCell(x, y, &cell)

	e.atPhantom = awm && x >= e.scr.Width()-1
	if !e.atPhantom {
		x += cell.Width
	}

	e.scr.setCursor(x, y, false)
}
