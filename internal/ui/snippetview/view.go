package snippetview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
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
		return tea.NewView(components.Loading(m.width, m.height, "Loading..."))
	}

	if len(m.snippets) == 0 {
		return tea.NewView(components.EmptyState(m.width, m.height,
			"No snippets.",
			"Press '"+viewkeys.HelpLabel(m.vk.New)+"' to add one.",
			snippetHelpLine(m.vk),
		))
	}

	cards := make([]string, len(m.snippets))
	for i, s := range m.snippets {
		cards[i] = components.RenderCard(snippetCardTitle(s.Name), snippetCardDesc(s.Command), i == m.gridCursor, m.gridLayout.CardW)
	}
	grid := components.RenderGridRows(cards, len(m.snippets), m.gridCursor, m.gridLayout)
	return tea.NewView(grid)
}

func snippetHelpLine(vk viewkeys.SnippetKeys) string {
	return viewkeys.HelpLabel(vk.New) + ":add · " +
		viewkeys.HelpLabel(vk.Edit) + ":edit · " +
		viewkeys.HelpLabel(vk.Delete) + ":delete"
}
