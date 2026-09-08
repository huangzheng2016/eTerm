package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/ordered"
)

type Screen struct {
	cb         *Callbacks
	buf        *uv.RenderBuffer
	cur, saved Cursor
	scroll     uv.Rectangle
	scrollback *Scrollback
	wrapped    []bool
}

func NewScreen(w, h int) *Screen {
	s := Screen{
		buf:        uv.NewRenderBuffer(w, h),
		scrollback: NewScrollback(DefaultScrollbackSize),
		wrapped:    make([]bool, h),
	}
	s.scroll = s.buf.Bounds()
	return &s
}

func (s *Screen) Reset() {
	s.buf.Clear()
	clear(s.wrapped)
	s.cur = Cursor{}
	s.saved = Cursor{}
	s.scroll = s.buf.Bounds()
	s.buf.Touched = nil
}

func (s *Screen) Bounds() uv.Rectangle {
	return s.buf.Bounds()
}

func (s *Screen) Touched() []*uv.LineData {
	return s.buf.Touched
}

func (s *Screen) ClearTouched() {
	s.buf.Touched = nil
}

func (s *Screen) CellAt(x int, y int) *uv.Cell {
	return s.buf.CellAt(x, y)
}

func (s *Screen) SetCell(x, y int, c *uv.Cell) {
	s.buf.SetCell(x, y, c)
}

func (s *Screen) SetLineWrapped(y int, wrapped bool) {
	if y >= 0 && y < len(s.wrapped) {
		s.wrapped[y] = wrapped
	}
}

func (s *Screen) LineWrapped(y int) bool {
	return y >= 0 && y < len(s.wrapped) && s.wrapped[y]
}

func (s *Screen) Height() int {
	return s.buf.Height()
}

func (s *Screen) Resize(width int, height int) {
	oldWrapped := s.wrapped
	if s.buf == nil {
		s.buf = uv.NewRenderBuffer(width, height)
	} else {
		s.buf.Resize(width, height)
		s.buf.Touched = nil
	}
	s.scroll = s.buf.Bounds()
	s.wrapped = make([]bool, height)
	copy(s.wrapped, oldWrapped)
}

func (s *Screen) Width() int {
	return s.buf.Width()
}

func (s *Screen) Clear() {
	s.ClearArea(s.Bounds())
}

func (s *Screen) ClearWithScrollback() {
	if s.scrollback != nil {
		for y := 0; y < s.buf.Height(); y++ {
			line := s.buf.Line(y)
			if line != nil && !s.isLineEmpty(line) {
				s.scrollback.PushWrapped(line, s.LineWrapped(y))
			}
		}
	}
	s.Clear()
}

func (s *Screen) isLineEmpty(line uv.Line) bool {
	for _, cell := range line {
		if cell.Content != "" && cell.Content != " " {
			return false
		}
	}
	return true
}

func (s *Screen) ClearArea(area uv.Rectangle) {
	s.buf.ClearArea(area)
	if area.Min.X == 0 && area.Max.X == s.Width() {
		for y := max(0, area.Min.Y); y < min(len(s.wrapped), area.Max.Y); y++ {
			s.wrapped[y] = false
		}
	}
	s.touchArea(area)
}

func (s *Screen) Fill(c *uv.Cell) {
	s.FillArea(c, s.Bounds())
}

func (s *Screen) FillArea(c *uv.Cell, area uv.Rectangle) {
	s.buf.FillArea(c, area)
	s.touchArea(area)
}

func (s *Screen) setHorizontalMargins(left, right int) {
	s.scroll.Min.X = left
	s.scroll.Max.X = right
}

func (s *Screen) setVerticalMargins(top, bottom int) {
	s.scroll.Min.Y = top
	s.scroll.Max.Y = bottom
}

func (s *Screen) setCursorX(x int, margins bool) {
	s.setCursor(x, s.cur.Y, margins)
}

func (s *Screen) setCursor(x, y int, margins bool) {
	old := s.cur.Position
	if !margins {
		y = ordered.Clamp(y, 0, s.buf.Height()-1)
		x = ordered.Clamp(x, 0, s.buf.Width()-1)
	} else {
		y = ordered.Clamp(s.scroll.Min.Y+y, s.scroll.Min.Y, s.scroll.Max.Y-1)
		x = ordered.Clamp(s.scroll.Min.X+x, s.scroll.Min.X, s.scroll.Max.X-1)
	}
	s.cur.X, s.cur.Y = x, y

	if s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
		s.cb.CursorPosition(old, uv.Pos(x, y))
	}
}

func (s *Screen) moveCursor(dx, dy int) {
	scroll := s.scroll
	old := s.cur.Position
	if old.X < scroll.Min.X {
		scroll.Min.X = 0
	}
	if old.X >= scroll.Max.X {
		scroll.Max.X = s.buf.Width()
	}

	pt := uv.Pos(s.cur.X+dx, s.cur.Y+dy)

	var x, y int
	if old.In(scroll) {
		y = ordered.Clamp(pt.Y, scroll.Min.Y, scroll.Max.Y-1)
		x = ordered.Clamp(pt.X, scroll.Min.X, scroll.Max.X-1)
	} else {
		y = ordered.Clamp(pt.Y, 0, s.buf.Height()-1)
		x = ordered.Clamp(pt.X, 0, s.buf.Width()-1)
	}

	s.cur.X, s.cur.Y = x, y

	if s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
		s.cb.CursorPosition(old, uv.Pos(x, y))
	}
}

func (s *Screen) Cursor() Cursor {
	return s.cur
}

func (s *Screen) CursorPosition() (x, y int) {
	return s.cur.X, s.cur.Y
}

func (s *Screen) ScrollRegion() uv.Rectangle {
	return s.scroll
}

func (s *Screen) SaveCursor() {
	s.saved = s.cur
}

func (s *Screen) RestoreCursor() {
	old := s.cur.Position
	s.cur = s.saved

	if s.cb.CursorPosition != nil && (old.X != s.cur.X || old.Y != s.cur.Y) {
		s.cb.CursorPosition(old, s.cur.Position)
	}
}

func (s *Screen) setCursorHidden(hidden bool) {
	changed := s.cur.Hidden != hidden
	s.cur.Hidden = hidden
	if changed && s.cb.CursorVisibility != nil {
		s.cb.CursorVisibility(!hidden)
	}
}

func (s *Screen) setCursorStyle(style CursorStyle, blink bool) {
	changed := s.cur.Style != style || s.cur.Steady != !blink
	s.cur.Style = style
	s.cur.Steady = !blink
	if changed && s.cb.CursorStyle != nil {
		s.cb.CursorStyle(style, !blink)
	}
}

func (s *Screen) cursorPen() uv.Style {
	return s.cur.Pen
}

func (s *Screen) cursorLink() uv.Link {
	return s.cur.Link
}

func (s *Screen) ShowCursor() {
	s.setCursorHidden(false)
}

func (s *Screen) HideCursor() {
	s.setCursorHidden(true)
}

func (s *Screen) InsertCell(n int) {
	if n <= 0 {
		return
	}

	x, y := s.cur.X, s.cur.Y
	s.buf.InsertCellArea(x, y, n, s.blankCell(), s.scroll)
}

func (s *Screen) DeleteCell(n int) {
	if n <= 0 {
		return
	}

	x, y := s.cur.X, s.cur.Y
	s.buf.DeleteCellArea(x, y, n, s.blankCell(), s.scroll)
}

func (s *Screen) ScrollUp(n int) {
	x, y := s.CursorPosition()
	s.setCursor(s.cur.X, 0, true)
	s.DeleteLine(n)
	s.setCursor(x, y, false)
}

func (s *Screen) ScrollDown(n int) {
	x, y := s.CursorPosition()
	s.setCursor(s.cur.X, 0, true)
	s.InsertLine(n)
	s.setCursor(x, y, false)
}

func (s *Screen) InsertLine(n int) bool {
	if n <= 0 {
		return false
	}

	x, y := s.cur.X, s.cur.Y

	if y < s.scroll.Min.Y || y >= s.scroll.Max.Y ||
		x < s.scroll.Min.X || x >= s.scroll.Max.X {
		return false
	}

	lines := min(n, s.scroll.Max.Y-y)
	s.buf.InsertLineArea(y, n, s.blankCell(), s.scroll)
	copy(s.wrapped[y+lines:s.scroll.Max.Y], s.wrapped[y:s.scroll.Max.Y-lines])
	clear(s.wrapped[y : y+lines])

	return true
}

func (s *Screen) DeleteLine(n int) bool {
	if n <= 0 {
		return false
	}

	scroll := s.scroll
	x, y := s.cur.X, s.cur.Y

	if y < scroll.Min.Y || y >= scroll.Max.Y ||
		x < scroll.Min.X || x >= scroll.Max.X {
		return false
	}

	if s.scrollback != nil && y == scroll.Min.Y &&
		scroll.Min.X == 0 && scroll.Max.X == s.buf.Width() {
		linesToSave := min(n, scroll.Max.Y-y)
		for i := range linesToSave {
			s.scrollback.PushWrapped(s.buf.Line(y+i), s.LineWrapped(y+i))
		}
	}

	s.buf.DeleteLineArea(y, n, s.blankCell(), scroll)
	lines := min(n, scroll.Max.Y-y)
	copy(s.wrapped[y:scroll.Max.Y-lines], s.wrapped[y+lines:scroll.Max.Y])
	clear(s.wrapped[scroll.Max.Y-lines : scroll.Max.Y])

	return true
}

func (s *Screen) blankCell() *uv.Cell {
	if s.cur.Pen.Bg == nil {
		return nil
	}

	c := uv.EmptyCell
	c.Style.Bg = s.cur.Pen.Bg
	return &c
}

func (s *Screen) touchArea(area uv.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		s.buf.TouchLine(area.Min.X, y, area.Max.X-area.Min.X)
	}
}

func (s *Screen) Scrollback() *Scrollback {
	return s.scrollback
}

func (s *Screen) SetScrollback(sb *Scrollback) {
	s.scrollback = sb
}

func (s *Screen) SetScrollbackSize(maxLines int) {
	if s.scrollback == nil {
		s.scrollback = NewScrollback(maxLines)
	} else {
		s.scrollback.SetMaxLines(maxLines)
	}
}
