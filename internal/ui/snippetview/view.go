package snippetview

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

var (
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	emptyHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

func snippetCardTitle(name string) string {
	return name
}

func snippetCardDesc(command string) string {
	if len(command) > 40 {
		return command[:37] + "..."
	}
	return command
}

func (m *Model) View() tea.View {
	if !m.loaded {
		return tea.NewView("Loading...")
	}

	help := helpStyle.Render("n:add · e:edit · d:delete")

	if len(m.snippets) == 0 {
		block := lipgloss.JoinVertical(lipgloss.Center,
			"No snippets.",
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

	cards := make([]string, len(m.snippets))
	for i, s := range m.snippets {
		cards[i] = components.RenderCard(snippetCardTitle(s.Name), snippetCardDesc(s.Command), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.snippets), m.gridCursor, m.gridLayout)
	return tea.NewView(grid)
}
