package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

func (t TabsModel) View() string {
	if len(t.items) == 0 {
		return ""
	}

	avail := t.width
	if avail <= 0 {
		// No width constraint — render all tabs
		var tabs []string
		for i, item := range t.items {
			if i == t.activeIdx {
				tabs = append(tabs, ui.ActiveTabStyle.Render(item.Title))
			} else {
				tabs = append(tabs, ui.InactiveTabStyle.Render(item.Title))
			}
		}
		row := strings.Join(tabs, " ")
		return strings.TrimRight(lipgloss.NewStyle().Padding(0, 2, 0, 2).Render(row), "\n")
	}

	layout := t.layout()

	// Build the row
	var parts []string
	if layout.hasLeft {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowLeft))
	}
	for j, idx := range layout.visible {
		item := t.items[idx]
		if idx == t.activeIdx {
			parts = append(parts, ui.ActiveTabStyle.Render(item.Title))
		} else {
			parts = append(parts, ui.InactiveTabStyle.Render(item.Title))
		}
		if j < len(layout.visible)-1 {
			parts = append(parts, " ")
		}
	}
	if layout.hasRight {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowRight))
	}

	row := strings.Join(parts, "")
	padded := lipgloss.NewStyle().Padding(0, tabBarPadLeft, 0, tabBarPadLeft).MaxWidth(avail).Render(row)
	return strings.TrimRight(padded, "\n")
}
