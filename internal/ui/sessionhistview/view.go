package sessionhistview

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
)

func (m *Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(ui.DimStyle.Render("Loading session history…"))
	}
	if len(m.rows) == 0 {
		return tea.NewView(ui.DimStyle.Render("No saved sessions for this host yet."))
	}

	listW := m.width / 3
	if listW < 24 {
		listW = 24
	}
	if listW > m.width-20 {
		listW = m.width / 2
	}
	transW := m.width - listW - 2
	if transW < 20 {
		transW = m.width - 2
		listW = m.width
	}

	var listLines []string
	for i, r := range m.rows {
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

	body := m.selectedTranscript()
	lines := strings.Split(body, "\n")
	maxLines := m.height - 2
	if maxLines < 3 {
		maxLines = 3
	}
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
	transBody := strings.Join(chunk, "\n")
	if strings.TrimSpace(transBody) == "" {
		transBody = "(no transcript saved for this session)"
	}
	transBox := lipgloss.NewStyle().Width(transW)
	if !m.focusList {
		transBox = transBox.Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#7D56F4"))
	}
	transStyled := transBox.Render(transBody)

	title := ui.TitleStyle.Render("Sessions: " + m.hostTitle)
	hint := ui.DimStyle.Render("tab focus · j/k · pgup/pgdn transcript · esc close")

	var main string
	if listW >= m.width-2 {
		main = lipgloss.JoinVertical(lipgloss.Left, title, "", listStyled, "", transStyled, "", hint)
	} else {
		row := lipgloss.JoinHorizontal(lipgloss.Top, listStyled, "  ", transStyled)
		main = lipgloss.JoinVertical(lipgloss.Left, title, "", row, "", hint)
	}
	return tea.NewView(main)
}
