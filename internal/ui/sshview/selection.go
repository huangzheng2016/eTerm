package sshview

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// selPoint is a position in the absolute-line coordinate space (see visibleAbsLine):
// line indexes scrollback rows [0, sbLen) then live screen rows [sbLen, sbLen+h).
type selPoint struct {
	line, col int
}

type selection struct {
	active   bool
	dragging bool
	anchor   selPoint
	caret    selPoint
}

// visibleAbsLine maps a content-relative row (0-based, within the SSH body) to an
// absolute line. It mirrors the window split in renderScrollback so highlighting and
// hit-testing agree.
func (m *Model) visibleAbsLine(yVis int) int {
	sbLen := m.emu.ScrollbackLen()
	h := m.emu.Height()
	if m.scrollOffset == 0 {
		// Live view; bottomPad pushes screen content up by bottomPad rows.
		return sbLen + m.bottomPad + yVis
	}
	offset := m.scrollOffset
	if offset > sbLen {
		offset = sbLen
	}
	sbLines := offset
	if h-sbLines < 0 {
		sbLines = h
	}
	if yVis < sbLines {
		return (sbLen - offset) + yVis
	}
	return sbLen + (yVis - sbLines)
}

func normSel(s selection) (start, end selPoint) {
	a, c := s.anchor, s.caret
	if a.line < c.line || (a.line == c.line && a.col <= c.col) {
		return a, c
	}
	return c, a
}

func inSelection(start, end selPoint, line, col int) bool {
	if line < start.line || line > end.line {
		return false
	}
	if line == start.line && col < start.col {
		return false
	}
	if line == end.line && col > end.col {
		return false
	}
	return true
}

func (m *Model) cellAtAbs(line, col int) *uv.Cell {
	sbLen := m.emu.ScrollbackLen()
	if line < sbLen {
		return m.emu.ScrollbackCellAt(col, line)
	}
	return m.emu.CellAt(col, line-sbLen)
}

// selectedText extracts the highlighted text: full rows for interior lines, partial
// for the first/last. Wide-char placeholders (Width==0) are skipped and trailing
// spaces trimmed per line.
func (m *Model) selectedText() string {
	if !m.sel.active {
		return ""
	}
	start, end := normSel(m.sel)
	w := m.emu.Width()
	var b strings.Builder
	for line := start.line; line <= end.line; line++ {
		c0, c1 := 0, w-1
		if line == start.line {
			c0 = start.col
		}
		if line == end.line {
			c1 = end.col
		}
		if c0 < 0 {
			c0 = 0
		}
		if c1 > w-1 {
			c1 = w - 1
		}
		var row strings.Builder
		for col := c0; col <= c1; col++ {
			cell := m.cellAtAbs(line, col)
			if cell == nil || cell.Width == 0 {
				if cell == nil {
					row.WriteByte(' ')
				}
				continue
			}
			if cell.Content == "" {
				row.WriteByte(' ')
				continue
			}
			row.WriteString(cell.Content)
		}
		b.WriteString(strings.TrimRight(row.String(), " "))
		if line < end.line {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
