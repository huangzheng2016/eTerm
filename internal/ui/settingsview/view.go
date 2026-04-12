package settingsview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	catStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	labelStyle    = lipgloss.NewStyle().Width(22)
	keyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("230"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))
	captureStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888"))
)

type scrollLine struct {
	text       string
	logicalIdx int // -1 category/spacer; else matches m.cursor when selected
}

func prefToggleLine(label string, on bool, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	var lbl string
	var val string
	if selected {
		lbl = selectedStyle.Render(labelStyle.Render(label))
		if on {
			val = selectedStyle.Render("on")
		} else {
			val = selectedStyle.Render("off")
		}
	} else {
		lbl = labelStyle.Render(label)
		if on {
			val = keyStyle.Render("on")
		} else {
			val = dimStyle.Render("off")
		}
	}
	return fmt.Sprintf("%s%s  %s", cursor, lbl, val)
}

func passwordActionLine(selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	label := "Change master password"
	hint := "enter"
	if selected {
		return fmt.Sprintf("%s%s  %s", cursor, selectedStyle.Render(labelStyle.Render(label)), selectedStyle.Render(hint))
	}
	return fmt.Sprintf("%s%s  %s", cursor, labelStyle.Render(label), dimStyle.Render(hint))
}

func (m *Model) buildScrollLines() []scrollLine {
	var out []scrollLine
	out = append(out, scrollLine{catStyle.Render("  General"), -1})
	out = append(out, scrollLine{
		prefToggleLine("Save session transcripts", m.saveSessionTranscript, m.cursor == 0),
		0,
	})
	out = append(out, scrollLine{
		prefToggleLine("Grid status text", m.gridStatusWords, m.cursor == 1),
		1,
	})
	out = append(out, scrollLine{"", -1})
	out = append(out, scrollLine{catStyle.Render("  Security"), -1})
	out = append(out, scrollLine{
		passwordActionLine(m.cursor == 2),
		2,
	})
	out = append(out, scrollLine{"", -1})

	lastCat := ""
	for i, e := range m.entries {
		if e.Category != lastCat {
			lastCat = e.Category
			if i > 0 {
				out = append(out, scrollLine{"", -1})
			}
			out = append(out, scrollLine{catStyle.Render("  " + e.Category), -1})
		}

		logical := 3 + i
		cursor := "  "
		lbl := labelStyle.Render(e.Label)
		keys := keyStyle.Render(formatKeys(e.Keys))

		if logical == m.cursor {
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
			logical,
		})
	}
	return out
}

func (m *Model) totalScrollLines() int {
	return len(m.buildScrollLines())
}

func (m *Model) View() tea.View {
	if m.pwd != nil {
		return tea.NewView(m.pwd.View())
	}

	var b strings.Builder

	title := headerStyle.Render("Settings")
	hints := hintStyle.Render("space/enter:toggle pref  enter:set key  +:add  bksp:clear  C-s:save  C-r:reset  wheel:scroll  esc:close")
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

	vis := m.visibleRows()
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
