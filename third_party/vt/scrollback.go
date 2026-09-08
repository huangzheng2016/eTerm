package vt

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

const DefaultScrollbackSize = 10000

type Scrollback struct {
	lines    []uv.Line
	wrapped  []bool
	maxLines int
}

func NewScrollback(maxLines int) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	return &Scrollback{
		lines:    make([]uv.Line, 0, min(maxLines, 1000)),
		maxLines: maxLines,
	}
}

func (s *Scrollback) Push(line uv.Line) {
	s.PushWrapped(line, false)
}

func (s *Scrollback) PushWrapped(line uv.Line, wrapped bool) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	lastNonEmpty := -1
	for i := len(line) - 1; i >= 0; i-- {
		c := &line[i]
		if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
			lastNonEmpty = i
			break
		}
	}

	cloned := slices.Clone(line[:lastNonEmpty+1])

	if len(s.lines) >= s.maxLines {
		s.lines = slices.Delete(s.lines, 0, 1)
		s.wrapped = slices.Delete(s.wrapped, 0, 1)
	}
	s.lines = append(s.lines, cloned)
	s.wrapped = append(s.wrapped, wrapped)
}

func (s *Scrollback) PushN(buf *uv.RenderBuffer, y, n int) {
	if s == nil || buf == nil || n <= 0 {
		return
	}

	for i := range min(n, buf.Height()-y) {
		if line := buf.Line(y + i); line != nil {
			s.Push(line)
		}
	}
}

func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return len(s.lines)
}

func (s *Scrollback) MaxLines() int {
	if s == nil {
		return 0
	}
	return s.maxLines
}

func (s *Scrollback) SetMaxLines(maxLines int) {
	if s == nil || maxLines <= 0 {
		return
	}

	s.maxLines = maxLines
	if len(s.lines) > maxLines {
		s.lines = s.lines[len(s.lines)-maxLines:]
		s.wrapped = s.wrapped[len(s.wrapped)-maxLines:]
	}
}

func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= len(s.lines) {
		return nil
	}
	return s.lines[index]
}

func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	return s.lines
}

func (s *Scrollback) LineWrapped(index int) bool {
	return s != nil && index >= 0 && index < len(s.wrapped) && s.wrapped[index]
}

func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	s.lines = s.lines[:0]
	s.wrapped = s.wrapped[:0]
}

func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	line := s.Line(y)
	if line == nil || x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}
