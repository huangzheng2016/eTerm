package sftpview

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	dateLayout      = "2006-01-02 15:04"
	sizeColumnCells = 10
	colGap          = "  "
	ellipsis        = "…"
)

type fileDelegate struct {
	styles list.DefaultItemStyles
}

func newFileDelegate() fileDelegate {
	st := list.NewDefaultItemStyles(true)
	// Default SelectedTitle adds a left border (+1 cell). That makes the row wider than
	// list width and the terminal wraps the tail (e.g. time) to the next line.
	st.SelectedTitle = st.SelectedTitle.UnsetBorderStyle().UnsetBorderForeground().Padding(0, 0, 0, 2)
	st.SelectedDesc = st.SelectedDesc.UnsetBorderStyle().UnsetBorderForeground()
	return fileDelegate{styles: st}
}

func (fileDelegate) Height() int   { return 1 }
func (fileDelegate) Spacing() int { return 0 }

func (fileDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d fileDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	fi, ok := item.(fileItem)
	if !ok || m.Width() <= 0 {
		return
	}

	s := &d.styles
	textwidth := m.Width() - s.NormalTitle.GetPaddingLeft() - s.NormalTitle.GetPaddingRight()
	if textwidth < 12 {
		textwidth = 12
	}

	kind := "[f]"
	if fi.info.IsDir {
		kind = "[d]"
	}
	prefix := kind + " "

	sizeStr := formatSize(fi.info.Size)
	sizeCol := alignRight(sizeStr, sizeColumnCells)
	dateStr := fi.info.ModTime.Format(dateLayout)

	rightBlock := colGap + sizeCol + colGap + dateStr
	rightW := ansi.StringWidth(rightBlock)
	prefixW := ansi.StringWidth(prefix)

	nameMax := textwidth - prefixW - rightW
	if nameMax < 4 {
		nameMax = 4
	}

	name := fi.info.Name
	isSelected := index == m.Index() && m.FilterState() != list.Filtering
	nameShown := fitFileName(name, nameMax, isSelected)
	nameCol := padRightVisual(nameShown, nameMax)

	line := prefix + nameCol + rightBlock
	line = strings.ReplaceAll(line, "\n", " ")
	if ansi.StringWidth(line) > textwidth {
		line = ansi.Truncate(line, textwidth, ellipsis)
	}

	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""

	switch {
	case emptyFilter:
		line = s.DimmedTitle.Render(line)
	case isSelected:
		line = s.SelectedTitle.Render(line)
	default:
		line = s.NormalTitle.Render(line)
	}

	_, _ = fmt.Fprint(w, line)
}

func alignRight(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, ellipsis)
	}
	if sw >= w {
		return s
	}
	return strings.Repeat(" ", w-sw) + s
}

func padRightVisual(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-sw)
}

// fitFileName fits the file name to a fixed cell budget. Unselected: head + ellipsis.
// Selected: ellipsis + tail (so the end of long names stays visible).
func fitFileName(name string, budget int, preferTail bool) string {
	if budget <= 0 {
		return ""
	}
	if ansi.StringWidth(name) <= budget {
		return name
	}
	if !preferTail {
		return ansi.Truncate(name, budget, ellipsis)
	}
	ew := ansi.StringWidth(ellipsis)
	if budget <= ew {
		return ansi.Truncate(ellipsis, budget, "")
	}
	sw := ansi.StringWidth(name)
	tailBudget := budget - ew
	skip := sw - tailBudget
	if skip <= 0 {
		return ansi.Truncate(name, budget, ellipsis)
	}
	return ansi.TruncateLeft(name, skip, ellipsis)
}
