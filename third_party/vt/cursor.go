package vt

import uv "github.com/charmbracelet/ultraviolet"

type CursorStyle int

const (
	CursorBlock CursorStyle = iota
	CursorUnderline
	CursorBar
)

type Cursor struct {
	Pen  uv.Style
	Link uv.Link

	uv.Position

	Style  CursorStyle
	Steady bool
	Hidden bool
}
