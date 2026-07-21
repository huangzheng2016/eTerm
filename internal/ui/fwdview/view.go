package fwdview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func (m Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(components.Loading(m.width, m.height, "Loading..."))
	}

	if len(m.rules) == 0 {
		return tea.NewView(components.EmptyState(m.width, m.height,
			"No port-forward rules.",
			"Press '"+viewkeys.HelpLabel(m.vk.New)+"' to add one.",
			fwdHelpLine(m.vk),
		))
	}

	cards := make([]string, len(m.rules))
	start, end := components.GridPageRange(len(m.rules), m.gridCursor, m.gridLayout)
	for i := start; i < end; i++ {
		r := m.rules[i]
		running := m.running[r.ID]
		cards[i] = components.RenderCard(ruleCardTitle(r), ruleCardDesc(r, running), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.rules), m.gridCursor, m.gridLayout)
	return tea.NewView(grid)
}

func fwdHelpLine(vk viewkeys.FwdKeys) string {
	return viewkeys.HelpLabel(vk.New) + ":add · " +
		viewkeys.HelpLabel(vk.Edit) + ":edit · " +
		viewkeys.HelpLabel(vk.Delete) + ":delete · " +
		viewkeys.HelpLabel(vk.Start) + ":toggle · " +
		viewkeys.HelpLabel(vk.Stop) + ":stop"
}
