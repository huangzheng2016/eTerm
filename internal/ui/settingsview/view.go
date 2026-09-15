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
	logicalIdx int
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

func shellInputLine(value string, editing bool, input string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	label := "Local terminal shell"
	val := value
	if val == "" {
		val = "(auto)"
	}
	if editing {
		val = input
	}
	if selected {
		return fmt.Sprintf("%s%s  %s", cursor, selectedStyle.Render(labelStyle.Render(label)), selectedStyle.Render(val))
	}
	return fmt.Sprintf("%s%s  %s", cursor, labelStyle.Render(label), keyStyle.Render(val))
}

func tmuxConfigInputLine(value string, editing bool, input string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	label := "tmux config file"
	val := value
	if val == "" {
		val = "(built-in)"
	}
	if editing {
		val = input
	}
	if selected {
		return fmt.Sprintf("%s%s  %s", cursor, selectedStyle.Render(labelStyle.Render(label)), selectedStyle.Render(val))
	}
	return fmt.Sprintf("%s%s  %s", cursor, labelStyle.Render(label), keyStyle.Render(val))
}

func shareHoursInputLine(value string, editing bool, input string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "> "
	}
	label := "Share link max hours"
	val := value
	if editing {
		val = input
	}
	if selected {
		return fmt.Sprintf("%s%s  %s", cursor, selectedStyle.Render(labelStyle.Render(label)), selectedStyle.Render(val))
	}
	return fmt.Sprintf("%s%s  %s", cursor, labelStyle.Render(label), keyStyle.Render(val))
}

func (m *Model) buildScrollLines() []scrollLine {
	var out []scrollLine
	out = append(out, scrollLine{catStyle.Render("  Recording"), -1})
	out = append(out, scrollLine{
		prefToggleLine("Save session transcripts", m.saveSessionTranscript, m.cursor == cursorSaveTranscript),
		cursorSaveTranscript,
	})
	out = append(out, scrollLine{
		prefToggleLine("Record session replay", m.replaySessions, m.cursor == cursorReplaySessions),
		cursorReplaySessions,
	})
	out = append(out, scrollLine{"", -1})
	out = append(out, scrollLine{catStyle.Render("  Interface"), -1})
	out = append(out, scrollLine{
		prefToggleLine("Grid status text", m.gridStatusWords, m.cursor == cursorGridStatus),
		cursorGridStatus,
	})
	out = append(out, scrollLine{"", -1})
	out = append(out, scrollLine{catStyle.Render("  Terminal"), -1})
	out = append(out, scrollLine{
		shellInputLine(m.localTerminalShell, m.state == stateShell, m.shellInput.View(), m.cursor == cursorLocalShell),
		cursorLocalShell,
	})
	out = append(out, scrollLine{
		tmuxConfigInputLine(m.tmuxConfigFile, m.state == stateTmuxConfig, m.tmuxConfigInput.View(), m.cursor == cursorTmuxConfigFile),
		cursorTmuxConfigFile,
	})
	out = append(out, scrollLine{"", -1})
	out = append(out, scrollLine{catStyle.Render("  Sharing"), -1})
	out = append(out, scrollLine{
		shareHoursInputLine(m.shareMaxHours, m.state == stateShareHours, m.shareHoursInput.View(), m.cursor == cursorShareMaxHours),
		cursorShareMaxHours,
	})
	out = append(out, scrollLine{"", -1})
	out = append(out, scrollLine{catStyle.Render("  Security"), -1})
	out = append(out, scrollLine{
		passwordActionLine(m.cursor == cursorPassword),
		cursorPassword,
	})
	return out
}

func (m *Model) totalScrollLines() int {
	return len(m.buildScrollLines())
}

func (m *Model) View() tea.View {
	if m.pwd != nil {
		return tea.NewView(m.pwd.View())
	}

	if m.confirmReset.IsActive() {
		return tea.NewView(centerDialog(m.confirmReset.View(), m.width, m.height))
	}

	var b strings.Builder

	title := headerStyle.Render("Settings")
	hints := hintStyle.Render("space/enter:toggle/edit  C-s:save  C-r:reset  esc:close")
	b.WriteString(title + "  " + hints + "\n")

	if m.state == stateShell {
		b.WriteString(captureStyle.Render("  Enter shell path...  (enter to accept, esc to cancel)") + "\n")
	} else if m.state == stateTmuxConfig {
		b.WriteString(captureStyle.Render("  Enter tmux config file...  (enter to accept, esc to cancel)") + "\n")
	} else if m.state == stateShareHours {
		hint := "  Enter share link max hours (1-168)...  (enter to accept, esc to cancel)"
		if m.shareHoursErr != "" {
			hint = "  " + m.shareHoursErr + "  (enter to accept, esc to cancel)"
		}
		b.WriteString(captureStyle.Render(hint) + "\n")
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

func centerDialog(dialog string, width, height int) string {
	if width <= 0 {
		return dialog
	}
	h := height
	if h <= 0 {
		h = lipgloss.Height(dialog) + 2
	}
	return lipgloss.Place(width, h, lipgloss.Center, lipgloss.Center, dialog)
}

func dialogOrigin(dialog string, width, height int) (int, int) {
	if width <= 0 {
		return 0, 0
	}
	h := height
	if h <= 0 {
		h = lipgloss.Height(dialog) + 2
	}
	return (width - lipgloss.Width(dialog)) / 2, (h - lipgloss.Height(dialog)) / 2
}
