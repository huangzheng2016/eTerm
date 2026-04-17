package batchresultview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/ui"
)

func (m *Model) View() tea.View {
	title := ui.TitleStyle.Render("Batch Read-Only Command")
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(
		fmt.Sprintf("hosts:%d  running:%d  ok:%d  failed:%d  %s", len(m.hosts), m.running, m.success, m.failed, doneLabel(m.done)),
	)
	command := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render(m.command)
	hint := ui.DimStyle.Render("j/k select host · click host · wheel list/output · pgup/pgdn · esc close")

	if len(m.hosts) == 0 {
		return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, title, "", status, "", command, "", hint))
	}

	listW := m.width / 3
	if listW < 22 {
		listW = 22
	}
	if listW > m.width-24 {
		listW = m.width / 2
	}
	bodyW := m.width - listW - 2
	if bodyW < 20 {
		bodyW = m.width - 2
		listW = m.width
	}

	var rows []string
	for i, host := range m.hosts {
		line := fmt.Sprintf("%-18s %s", truncate(host.Label, max(8, listW-8)), host.Status)
		style := ui.DimStyle
		if i == m.cursor {
			style = ui.SelectedStyle
		}
		rows = append(rows, style.Render(line))
	}
	listBlock := lipgloss.NewStyle().Width(listW).Render(strings.Join(rows, "\n"))

	lines := strings.Split(m.selectedOutput(), "\n")
	maxLines := m.height - 6
	if maxLines < 3 {
		maxLines = 3
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > len(lines) {
		m.scroll = len(lines)
	}
	end := m.scroll + maxLines
	if end > len(lines) {
		end = len(lines)
	}
	body := strings.Join(lines[m.scroll:end], "\n")
	if strings.TrimSpace(body) == "" {
		body = "(no output yet)"
	}
	outputBlock := lipgloss.NewStyle().
		Width(bodyW).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Render(body)

	main := lipgloss.JoinHorizontal(lipgloss.Top, listBlock, "  ", outputBlock)
	return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, title, status, "", command, "", main, "", hint))
}

func doneLabel(done bool) string {
	if done {
		return "done"
	}
	return "running"
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen == 1 {
		return s[:1]
	}
	return s[:maxLen-1] + "…"
}
