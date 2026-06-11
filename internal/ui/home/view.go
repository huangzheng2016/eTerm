package home

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

var tagBadgeStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230")).
	Padding(0, 1)

func (m Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(components.Loading(m.width, m.height, "Loading..."))
	}

	if m.mode == tagView {
		if m.selectedTag == "" {
			// Show tag picker list
			if len(m.allTags) == 0 {
				return tea.NewView(m.centeredEmptyHint(
					"No tags found. Add tags to hosts via '"+homeBindingLabel(m.keys.EditHost.Help().Key, "e")+"' (edit).",
					m.emptyHint(),
				))
			}
			return tea.NewView(m.tagList.View())
		}
		// Show filtered host list with tag badge
		badge := tagBadgeStyle.Render(fmt.Sprintf(" %s ", m.selectedTag))
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("  bksp:back  " + homeBindingLabel(m.keys.ToggleView.Help().Key, "t") + ":group view")
		header := badge + hint
		hosts := m.gridHosts()
		if len(hosts) == 0 {
			body := components.EmptyState(0, 0,
				"No hosts with this tag.",
				"bksp: tag list · "+homeBindingLabel(m.keys.ToggleView.Help().Key, "t")+": groups · "+homeBindingLabel(m.keys.Search.Help().Key, "/")+": search",
			)
			return tea.NewView(components.Page{
				Width:      m.width,
				Height:     m.height,
				Header:     header,
				Body:       body,
				CenterBody: true,
			}.Render())
		}
		// In search mode, show bubbles list (has search input UI)
		if m.list.FilterState() != 0 {
			return tea.NewView(header + "\n" + m.list.View())
		}
		gridH := m.height - 1 // subtract header line
		if gridH < cardOuterH {
			gridH = cardOuterH
		}
		gl := computeGrid(m.width, gridH)
		return tea.NewView(header + "\n" + renderGrid(hosts, m.gridCursor, gl, m.width, m.hostStatus, m.selectedHosts, m.gridStatusWords))
	}

	// Group view (default)
	hosts := m.gridHosts()
	if len(hosts) == 0 {
		return tea.NewView(m.centeredEmptyHint(
			"No connections. Press '"+homeBindingLabel(m.keys.NewHost.Help().Key, "n")+"' to add one.",
			m.emptyHint(),
		))
	}

	// In search mode, show bubbles list (has search input UI)
	if m.list.FilterState() != 0 {
		return tea.NewView(m.list.View())
	}

	return tea.NewView(renderGrid(hosts, m.gridCursor, m.gridLayout, m.width, m.hostStatus, m.selectedHosts, m.gridStatusWords))
}

// centeredEmptyHint adds an optional muted shortcut line under the primary text, then centers the block.
func (m Model) centeredEmptyHint(primary, hint string) string {
	if hint != "" {
		return components.EmptyState(m.width, m.height, primary, hint)
	}
	return components.EmptyState(m.width, m.height, primary)
}

func (m Model) emptyHint() string {
	return homeBindingLabel(m.keys.ToggleView.Help().Key, "t") + ": tags · " +
		homeBindingLabel(m.keys.NewHost.Help().Key, "n") + ": new host · " +
		homeBindingLabel(viewkeys.HelpLabel(m.helpKeys), "?") + ": all keys"
}

func homeBindingLabel(label, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}
