package sessionhistview

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func (m *Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(components.Loading(m.width, m.height, "Loading session history..."))
	}
	if len(m.rows) == 0 {
		return tea.NewView(components.EmptyState(m.width, m.height, "No saved sessions for this host yet."))
	}

	listW, transW, stacked := m.layoutWidths()

	var listLines []string
	listStart, listEnd := m.listPageRange()
	for i := listStart; i < listEnd; i++ {
		r := m.rows[i]
		line := formatRowMeta(r)
		if len(line) > listW-2 {
			line = truncateUTF8BytesEllipsis(line, max(0, listW-4))
		}
		st := ui.DimStyle
		if i == m.sel && m.focusList {
			st = ui.SelectedStyle
		}
		listLines = append(listLines, st.Render(line))
	}
	listBlock := strings.Join(listLines, "\n")
	listStyled := lipgloss.NewStyle().Width(listW).Render(listBlock)

	body := m.selectedDisplayTranscript()
	lines := strings.Split(body, "\n")
	maxLines := m.transcriptPageSize()
	scroll := m.scroll
	if scroll > len(lines) {
		scroll = len(lines)
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + maxLines
	if end > len(lines) {
		end = len(lines)
	}
	var chunk []string
	if scroll < len(lines) {
		chunk = lines[scroll:end]
	}
	for i := range chunk {
		chunk[i] = m.selection.RenderLine(chunk[i], scroll+i)
	}
	transBody := strings.Join(chunk, "\n")
	transBox := lipgloss.NewStyle().Width(transW)
	if !m.focusList {
		transBox = transBox.Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#7D56F4"))
	}
	transStyled := transBox.Render(transBody)

	title := ui.TitleStyle.Render("Sessions: " + m.hostTitle)
	emptyHint := viewkeys.HelpLabel(m.showEmptyKeys) + " show empty"
	if m.showEmpty {
		emptyHint = viewkeys.HelpLabel(m.showEmptyKeys) + " hide empty"
	}
	hint := ui.DimStyle.Render("tab focus · j/k scroll · pgup/pgdn page · mouse wheel · c copy transcript · " + emptyHint + " · esc close")

	var main string
	if stacked {
		main = lipgloss.JoinVertical(lipgloss.Left, title, "", listStyled, "", transStyled, "", hint)
	} else {
		row := lipgloss.JoinHorizontal(lipgloss.Top, listStyled, "  ", transStyled)
		main = lipgloss.JoinVertical(lipgloss.Left, title, "", row, "", hint)
	}
	return tea.NewView(main)
}
