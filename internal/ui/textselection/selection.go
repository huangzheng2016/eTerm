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

// Line break kinds for TextJoined, indexed by content line (line 0 is always
// BreakNewline): how a line connects to the previous one.
const (
	BreakNewline   = byte(iota) // real break: join with "\n"
	BreakJoin                   // soft-wrap continuation: join with ""
	BreakJoinSpace              // soft-wrap continuation: join with " "
)

// LineBreak describes how a content line connects to the previous one.
// Skip is the number of leading cells the wrapper inserted on a continuation
// line (e.g. a document margin or list hanging indent); they are dropped
// when the line is joined into a selection copy.
type LineBreak struct {
	Kind byte
	Skip int
}

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
	return s.TextJoined(lines, nil)
}

// TextJoined is Text with per-line break kinds (see the Break* constants):
// soft-wrap continuations join without a newline, matching how the terminal
// view copies wrapped lines. Nil breaks means every line is a real break.
func (s Selection) TextJoined(lines []string, breaks []LineBreak) string {
	if !s.Active || len(lines) == 0 {
		return ""
	}
	start, end := s.bounds()
	start.Line = min(max(0, start.Line), len(lines)-1)
	end.Line = min(max(0, end.Line), len(lines)-1)
	var out strings.Builder
	for line := start.Line; line <= end.Line; line++ {
		if line > start.Line {
			br := LineBreak{Kind: BreakNewline}
			if line < len(breaks) {
				br = breaks[line]
			}
			switch br.Kind {
			case BreakNewline:
				out.WriteByte('\n')
			case BreakJoinSpace:
				out.WriteByte(' ')
			}
		}
		runes := []rune(ansi.Strip(lines[line]))
		from, to := 0, len(runes)
		if line > start.Line && line < len(breaks) && breaks[line].Kind != BreakNewline {
			from = runeAtCell(runes, breaks[line].Skip)
		}
		if line == start.Line {
			from = runeAtCell(runes, start.Col)
		}
		if line == end.Line {
			to = min(len(runes), runeAtCell(runes, end.Col)+1)
		}
		if from > to {
			from = to
		}
		out.WriteString(strings.TrimRight(string(runes[from:to]), " "))
	}
	return out.String()
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

// AutoScroll tracks edge-drag auto-scrolling for a drag selection: while the
// pointer sits in the top or bottom edge band, the owning view scrolls on a
// timer and extends the caret.
type AutoScroll struct {
	Dir    int // -1 toward the top, +1 toward the bottom, 0 off
	Queued bool
}

// EdgeBand is the height in rows of the top/bottom band that starts
// auto-scrolling during a drag selection.
func EdgeBand(height int) int { return max(2, height/20) }

// Update sets Dir from the pointer row y inside a height-row area and reports
// whether auto-scrolling should be ticking.
func (a *AutoScroll) Update(y, height int) bool {
	if height <= 0 {
		a.Dir = 0
		return false
	}
	band := EdgeBand(height)
	switch {
	case y < band:
		a.Dir = -1
	case y >= height-band:
		a.Dir = 1
	default:
		a.Dir = 0
	}
	return a.Dir != 0
}

// Stop cancels auto-scrolling on drag end or selection clear.
func (a *AutoScroll) Stop() {
	a.Dir = 0
	a.Queued = false
}
