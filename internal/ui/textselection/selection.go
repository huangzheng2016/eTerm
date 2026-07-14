package textselection

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Point struct {
	Line int
	Col  int
}

type Selection struct {
	Active   bool
	Dragging bool
	Moved    bool
	Anchor   Point
	Caret    Point
}

var selectedStyle = lipgloss.NewStyle().Reverse(true)

func (s *Selection) Begin(line, col int) {
	p := Point{Line: line, Col: max(0, col)}
	*s = Selection{Active: true, Dragging: true, Anchor: p, Caret: p}
}

func (s *Selection) Move(line, col int) {
	if !s.Dragging {
		return
	}
	s.Caret = Point{Line: max(0, line), Col: max(0, col)}
	s.Moved = s.Caret != s.Anchor
}

func (s *Selection) End(line, col int) bool {
	s.Move(line, col)
	s.Dragging = false
	if !s.Moved {
		s.Active = false
	}
	return s.Active
}

func (s Selection) Text(lines []string) string {
	if !s.Active || len(lines) == 0 {
		return ""
	}
	start, end := s.bounds()
	start.Line = min(max(0, start.Line), len(lines)-1)
	end.Line = min(max(0, end.Line), len(lines)-1)
	var out []string
	for line := start.Line; line <= end.Line; line++ {
		runes := []rune(ansi.Strip(lines[line]))
		from, to := 0, len(runes)
		if line == start.Line {
			from = runeAtCell(runes, start.Col)
		}
		if line == end.Line {
			to = min(len(runes), runeAtCell(runes, end.Col)+1)
		}
		if from > to {
			from = to
		}
		out = append(out, strings.TrimRight(string(runes[from:to]), " "))
	}
	return strings.Join(out, "\n")
}

func (s Selection) RenderLine(line string, lineIndex int) string {
	if !s.Active {
		return line
	}
	start, end := s.bounds()
	if lineIndex < start.Line || lineIndex > end.Line {
		return line
	}
	runes := []rune(ansi.Strip(line))
	from, to := 0, len(runes)
	if lineIndex == start.Line {
		from = runeAtCell(runes, start.Col)
	}
	if lineIndex == end.Line {
		to = min(len(runes), runeAtCell(runes, end.Col)+1)
	}
	if from > to {
		from = to
	}
	return string(runes[:from]) + selectedStyle.Render(string(runes[from:to])) + string(runes[to:])
}

func (s Selection) bounds() (Point, Point) {
	a, b := s.Anchor, s.Caret
	if a.Line < b.Line || a.Line == b.Line && a.Col <= b.Col {
		return a, b
	}
	return b, a
}

func runeAtCell(runes []rune, col int) int {
	width := 0
	for i, r := range runes {
		w := ansi.StringWidth(string(r))
		if width+w > col {
			return i
		}
		width += w
	}
	return len(runes)
}
