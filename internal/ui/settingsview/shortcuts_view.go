package settingsview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *ShortcutsModel) buildScrollLines() []scrollLine {
	var out []scrollLine

	lastCat := ""
	group := 0
	for i, e := range m.entries {
		if e.Category != lastCat {
			lastCat = e.Category
			group++
			if i > 0 {
				out = append(out, scrollLine{"", -1})
			}
			out = append(out, scrollLine{catStyle.Render(fmt.Sprintf("  [%d] %s", group, e.Category)), -1})
		}

		cursor := "  "
		lbl := labelStyle.Render(e.Label)
		keys := keyStyle.Render(formatKeys(e.Keys))

		if i == m.cursor {
			cursor = "> "
			lbl = selectedStyle.Render(labelStyle.Render(e.Label))
			if m.state == stateCapture || m.state == stateAppend {
				keys = captureStyle.Render("...")
			} else {
				keys = selectedStyle.Render(formatKeys(e.Keys))
			}
		}

		out = append(out, scrollLine{
			fmt.Sprintf("%s%s  %s", cursor, lbl, keys),
			i,
		})
	}
	return out
}

func (m *ShortcutsModel) totalScrollLines() int {
	return len(m.buildScrollLines())
}

func (m *ShortcutsModel) View() tea.View {
	if m.confirmReset.IsActive() {
		return tea.NewView(centerDialog(m.confirmReset.View(), m.width, m.height))
	}

	var b strings.Builder

	title := headerStyle.Render("Shortcuts")
	hints := hintStyle.Render("enter:set key  +:add  bksp:clear  [/] or 1-7:group  C-s:save  C-r:reset  wheel:scroll  esc:close")
	b.WriteString(title + "  " + hints + "\n")

	if m.state == stateCapture {
		b.WriteString(captureStyle.Render("  Press a key to bind...  (esc to cancel)") + "\n")
	} else if m.state == stateAppend {
		b.WriteString(captureStyle.Render("  Press a key to add...  (esc to cancel)") + "\n")
	} else {
		b.WriteString("\n")
	}

	lines := m.buildScrollLines()

	cursorLine := 0
	for li, sl := range lines {
		if sl.logicalIdx == m.cursor {
			cursorLine = li
			break
		}
	}

	vis := visibleRowsFor(m.height)
	if m.scroll > cursorLine {
		m.scroll = cursorLine
	}
	if cursorLine >= m.scroll+vis {
		m.scroll = cursorLine - vis + 1
	}
	end := m.scroll + vis
	if end > len(lines) {
		end = len(lines)
	}

	for i := m.scroll; i < end; i++ {
		b.WriteString(lines[i].text + "\n")
	}

	if m.modified {
		b.WriteString("\n" + captureStyle.Render("  * unsaved changes"))
	}

	return tea.NewView(b.String())
}
