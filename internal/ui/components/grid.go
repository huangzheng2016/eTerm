package components

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Grid card defaults.
const (
	CardMinOuterW = 30 // minimum total card width (including border)
	CardOuterH    = 4  // 2 content lines + 2 border lines
	GridGap       = 1  // horizontal gap between cards
)

// Card border styles shared across all grid views.
var (
	ActiveCardBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#7D56F4"))
	InactiveCardBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#555"))
	CardTitleBase      = lipgloss.NewStyle().Bold(true)
	CardDescBase       = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
	PageIndicatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
	PageNumStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
)

// GridLayout holds computed grid dimensions.
type GridLayout struct {
	Cols     int
	Rows     int
	PageSize int
	CardW    int // total card width (includes border)
	CardH    int
}

// PLACEHOLDER_FUNCS

// ComputeGrid calculates grid dimensions for the given terminal size.
func ComputeGrid(width, height int) GridLayout {
	return ComputeGridWithCardHeight(width, height, CardOuterH)
}

func ComputeGridWithCardHeight(width, height, cardH int) GridLayout {
	cols := (width + GridGap) / (CardMinOuterW + GridGap)
	if cols < 1 {
		cols = 1
	}
	cardW := (width - (cols-1)*GridGap) / cols
	if cardW < CardMinOuterW {
		cardW = CardMinOuterW
	}
	rows := height / cardH
	if rows < 1 {
		rows = 1
	}
	return GridLayout{Cols: cols, Rows: rows, PageSize: cols * rows, CardW: cardW, CardH: cardH}
}

// GridPage returns the page number for a given cursor and pageSize.
func GridPage(cursor, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return cursor / pageSize
}

func GridPageRange(total, cursor int, gl GridLayout) (int, int) {
	start := GridPage(cursor, gl.PageSize) * gl.PageSize
	return start, min(total, start+gl.PageSize)
}

// GridMove computes the new cursor after a direction key press.
func GridMove(dir string, cursor, total int, gl GridLayout) (int, bool) {
	if total == 0 {
		return 0, false
	}
	cols := gl.Cols
	switch dir {
	case "up":
		if cursor-cols >= 0 {
			return cursor - cols, true
		}
		return cursor, false
	case "down":
		if cursor+cols < total {
			return cursor + cols, true
		}
		return cursor, false
	case "left":
		if cursor > 0 {
			return cursor - 1, true
		}
		return cursor, false
	case "right":
		if cursor < total-1 {
			return cursor + 1, true
		}
		return cursor, false
	case "pgup":
		n := cursor - gl.PageSize
		if n < 0 {
			n = 0
		}
		return n, n != cursor
	case "pgdown":
		n := cursor + gl.PageSize
		if n >= total {
			n = total - 1
		}
		return n, n != cursor
	case "home":
		return 0, cursor != 0
	case "end":
		return total - 1, cursor != total-1
	}
	return cursor, false
}

// GridIndexAtMouse returns the item index for a click at (x, y) within the grid area.
func GridIndexAtMouse(x, y, total int, gl GridLayout, page int) (int, bool) {
	if x < 0 || y < 0 {
		return 0, false
	}
	col := x / (gl.CardW + GridGap)
	cardH := gl.CardH
	if cardH == 0 {
		cardH = CardOuterH
	}
	row := y / cardH
	if col >= gl.Cols || row >= gl.Rows {
		return 0, false
	}
	idx := page*gl.PageSize + row*gl.Cols + col
	if idx >= total {
		return 0, false
	}
	return idx, true
}

// truncateToWidth cuts s to fit within maxW visible cells, appending "…" if truncated.
func truncateToWidth(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w <= maxW {
		return s
	}
	// Trim rune by rune until it fits
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= maxW {
			return candidate
		}
	}
	return ""
}

// RenderCard renders a two-line card with the given title, description, and active state.
func RenderCard(title, desc string, active bool, cardW int) string {
	innerW := cardW - 2
	if innerW < 1 {
		innerW = 1
	}
	// Truncate text before rendering to prevent line wrapping inside the card.
	title = truncateToWidth(title, innerW)
	desc = truncateToWidth(desc, innerW)
	t := CardTitleBase.Width(innerW).Render(title)
	d := CardDescBase.Width(innerW).Render(desc)
	content := t + "\n" + d
	if active {
		return ActiveCardBorder.Width(cardW).Height(2).Render(content)
	}
	return InactiveCardBorder.Width(cardW).Height(2).Render(content)
}

func RenderThreeLineCard(title, second, third string, active bool, cardW int) string {
	innerW := max(1, cardW-2)
	title = truncateToWidth(title, innerW)
	second = truncateToWidth(second, innerW)
	third = truncateToWidth(third, innerW)
	content := CardTitleBase.Width(innerW).Render(title) + "\n" +
		CardDescBase.Width(innerW).Render(second) + "\n" +
		CardDescBase.Width(innerW).Render(third)
	if active {
		return ActiveCardBorder.Width(cardW).Height(3).Render(content)
	}
	return InactiveCardBorder.Width(cardW).Height(3).Render(content)
}

// EmptyCard returns a blank placeholder the same size as a card.
func EmptyCard(cardW, cardH int) string {
	line := strings.Repeat(" ", cardW)
	lines := make([]string, cardH)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// RenderGridRows renders a grid page from pre-rendered card strings.
// cards is the full list of rendered card strings; cursor is the selected index.
func RenderGridRows(cards []string, total, cursor int, gl GridLayout) string {
	if total == 0 || gl.PageSize == 0 {
		return ""
	}
	page := GridPage(cursor, gl.PageSize)
	cardH := gl.CardH
	if cardH == 0 {
		cardH = CardOuterH
	}
	start := page * gl.PageSize
	if start >= total {
		start = 0
	}

	var rowStrings []string
	for r := 0; r < gl.Rows; r++ {
		var row []string
		for c := 0; c < gl.Cols; c++ {
			idx := start + r*gl.Cols + c
			if idx < total {
				row = append(row, cards[idx])
			} else {
				row = append(row, EmptyCard(gl.CardW, cardH))
			}
		}
		joined := lipgloss.JoinHorizontal(lipgloss.Top, Intersperse(row, strings.Repeat(" ", GridGap))...)
		rowStrings = append(rowStrings, joined)
	}

	grid := lipgloss.JoinVertical(lipgloss.Left, rowStrings...)

	totalPages := (total + gl.PageSize - 1) / gl.PageSize
	// Always show page indicator
	var parts []string
	if page > 0 {
		parts = append(parts, PageIndicatorStyle.Render("◀ "))
	} else {
		parts = append(parts, PageIndicatorStyle.Render("  "))
	}
	parts = append(parts, PageNumStyle.Render(fmt.Sprintf("%d", page+1)))
	parts = append(parts, PageIndicatorStyle.Render(" / "))
	parts = append(parts, PageIndicatorStyle.Render(fmt.Sprintf("%d", totalPages)))
	if page < totalPages-1 {
		parts = append(parts, PageIndicatorStyle.Render(" ▶"))
	}
	indicator := strings.Join(parts, "")
	grid += "\n" + indicator
	return grid
}

// Intersperse inserts sep between each element.
func Intersperse(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, s := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, s)
	}
	return out
}
