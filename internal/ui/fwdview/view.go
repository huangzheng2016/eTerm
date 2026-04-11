package fwdview

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/ui/components"
)

var (
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	emptyHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

func (m Model) View() tea.View {
	if !m.loaded {
		return tea.NewView("Loading...")
	}

	help := helpStyle.Render("n:add · e:edit · d:delete · enter:start · x:stop")

	if len(m.rules) == 0 {
		block := lipgloss.JoinVertical(lipgloss.Center,
			"No port-forward rules.",
			"",
			emptyHintStyle.Render("Press 'n' to add one."),
			"",
			help,
		)
		if m.width > 0 && m.height > 0 {
			return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block))
		}
		return tea.NewView(block)
	}

	cards := make([]string, len(m.rules))
	for i, r := range m.rules {
		running := m.running[r.ID]
		cards[i] = components.RenderCard(ruleCardTitle(r), ruleCardDesc(r, running), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.rules), m.gridCursor, m.gridLayout)
	return tea.NewView(grid)
}
