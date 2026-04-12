package home

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var tagBadgeStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230")).
	Padding(0, 1)

var tagEmptyHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))

func (m Model) View() tea.View {
	if !m.loaded {
		return tea.NewView("Loading...")
	}

	if m.mode == tagView {
		if m.selectedTag == "" {
			// Show tag picker list
			if len(m.allTags) == 0 {
				return tea.NewView(m.centeredEmptyHint(
					"No tags found. Add tags to hosts via 'e' (edit).",
					"t: groups · n: new host · ?: all keys",
				))
			}
			return tea.NewView(m.tagList.View())
		}
		// Show filtered host list with tag badge
		badge := tagBadgeStyle.Render(fmt.Sprintf(" %s ", m.selectedTag))
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("  bksp:back  t:group view")
		header := badge + hint
		hosts := m.gridHosts()
		if len(hosts) == 0 {
			hintText := "bksp: tag list · t: groups · /: search"
			var hintStr string
			if m.width > 0 {
				hintStr = tagEmptyHintStyle.Width(m.width).Render(hintText)
			} else {
				hintStr = tagEmptyHintStyle.Render(hintText)
			}
			block := lipgloss.JoinVertical(lipgloss.Center,
				"No hosts with this tag.",
				"",
				hintStr,
			)
			body := block
			if m.width > 0 && m.height > 1 {
				subH := m.height - 1
				if subH < 1 {
					subH = 1
				}
				body = lipgloss.Place(m.width, subH, lipgloss.Center, lipgloss.Center, block)
			}
			return tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body))
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
			"No connections. Press 'n' to add one.",
			"t: tags · n: new host · ?: all keys",
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
	var block string
	if hint != "" {
		var hintStr string
		if m.width > 0 {
			hintStr = tagEmptyHintStyle.Width(m.width).Render(hint)
		} else {
			hintStr = tagEmptyHintStyle.Render(hint)
		}
		block = lipgloss.JoinVertical(lipgloss.Center, primary, "", hintStr)
	} else {
		block = primary
	}
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
	}
	return block
}
