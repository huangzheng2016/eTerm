package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/eterm/eterm/internal/ui"
)

func (t TabsModel) View() string {
	if len(t.items) == 0 {
		return ""
	}

	widths := tabWidths(t.items, t.activeIdx)

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

	budget := avail - tabBarPadLeft*2
	hasLeft := t.scrollIdx > 0
	if hasLeft {
		budget -= arrowWidth
	}

	// Determine which tabs fit
	var visible []int
	used := 0
	for i := t.scrollIdx; i < len(t.items); i++ {
		need := widths[i]
		if len(visible) > 0 {
			need += tabGap
		}
		// Check if we need a right arrow
		if used+need > budget {
			break
		}
		// If adding this tab leaves no room for right arrow and there are more tabs
		if i < len(t.items)-1 && used+need+arrowWidth > budget {
			break
		}
		visible = append(visible, i)
		used += need
	}
	hasRight := len(visible) > 0 && visible[len(visible)-1] < len(t.items)-1

	// Build the row
	var parts []string
	if hasLeft {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowLeft))
	}
	for j, idx := range visible {
		item := t.items[idx]
		if idx == t.activeIdx {
			parts = append(parts, ui.ActiveTabStyle.Render(item.Title))
		} else {
			parts = append(parts, ui.InactiveTabStyle.Render(item.Title))
		}
		if j < len(visible)-1 {
			parts = append(parts, " ")
		}
	}
	if hasRight {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowRight))
	}

	row := strings.Join(parts, "")
	padded := lipgloss.NewStyle().Padding(0, tabBarPadLeft, 0, tabBarPadLeft).MaxWidth(avail).Render(row)
	return strings.TrimRight(padded, "\n")
}
