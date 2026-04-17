package editor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const formContentOffsetY = 2

var (
	labelStyle = lipgloss.NewStyle().
			Width(14).
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	focusedLabelStyle = lipgloss.NewStyle().
				Width(14).
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	selectorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	selectorFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	arrowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999"))

	arrowFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999"))

	actionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#111")).
			Background(lipgloss.Color("#7D56F4")).
			Bold(true).
			Padding(0, 1)

	actionAltStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ddd")).
			Background(lipgloss.Color("#444")).
			Bold(true).
			Padding(0, 1)

	formStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3).
			Width(60)

	overlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3).
			Width(72)
)

func (m Model) View() tea.View {
	var box string
	if m.advancedActive {
		box, _, _ = m.renderAdvancedOverlay()
	} else {
		box, _, _ = m.renderMainForm()
	}
	if m.width > 0 && m.height > 0 {
		box = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return tea.NewView(box)
}

func (m Model) renderMainForm() (string, []fieldSpan, int) {
	title := "New Host"
	if m.host != nil && m.host.ID > 0 {
		title = "Edit Host"
	}
	contentRows := []string{titleStyle.Render(title), ""}
	fieldRows, spans := m.renderFields(m.mainVisibleFields(), m.focused, len(contentRows))
	contentRows = append(contentRows, fieldRows...)
	contentRows = append(contentRows, "")
	actionY := len(contentRows) + formContentOffsetY
	contentRows = append(contentRows, m.renderActionRow("Save", "Cancel"))
	if m.err != "" {
		contentRows = append(contentRows, errorStyle.Render("! "+m.err))
	}
	contentRows = append(contentRows, footerStyle.Render("Tab next | A advanced | Ctrl+S save | Esc cancel | click fields/buttons"))
	return formStyle.Render(lipgloss.JoinVertical(lipgloss.Left, contentRows...)), spans, actionY
}

func (m Model) renderAdvancedOverlay() (string, []fieldSpan, int) {
	contentRows := []string{
		titleStyle.Render("Advanced SSH"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Proxy, agent forwarding, remote command, raw options"),
		"",
	}
	fieldRows, spans := m.renderFields(m.advancedVisibleFields(), m.advancedFocused, len(contentRows))
	contentRows = append(contentRows, fieldRows...)
	contentRows = append(contentRows, "")
	actionY := len(contentRows) + formContentOffsetY
	contentRows = append(contentRows, m.renderActionRow("Back", "Save"))
	if m.err != "" {
		contentRows = append(contentRows, errorStyle.Render("! "+m.err))
	}
	contentRows = append(contentRows, footerStyle.Render("Tab next | Ctrl+S save | Esc back | click outside closes"))
	return overlayStyle.Render(lipgloss.JoinVertical(lipgloss.Left, contentRows...)), spans, actionY
}

func (m Model) renderFields(fields []int, focusedIndex, startRow int) ([]string, []fieldSpan) {
	rows := make([]string, 0, len(fields)+4)
	spans := make([]fieldSpan, 0, len(fields))
	currentRow := startRow
	for i, field := range fields {
		lines := m.renderFieldLines(field, i == focusedIndex)
		rows = append(rows, lines...)
		spans = append(spans, fieldSpan{
			field:  field,
			startY: currentRow + formContentOffsetY,
			endY:   currentRow + len(lines) - 1 + formContentOffsetY,
		})
		currentRow += len(lines)
	}
	return rows, spans
}

func (m Model) renderFieldLines(field int, focused bool) []string {
	ls := labelStyle
	if focused {
		ls = focusedLabelStyle
	}
	label := ls.Render(fieldLabels[field])

	switch field {
	case authMethodField:
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(authOptions[m.authIdx], focused))}
	case proxyTypeField:
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(proxyDisplay[m.proxyTypeIdx], focused))}
	case gssapiSourceField:
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(gssapiSourceDisplay[m.gssapiSourceIdx], focused))}
	case forwardAgentField:
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(boolDisplay[m.forwardAgentIdx], focused))}
	case keyIDField:
		name := "(none)"
		if m.keyIdx >= 0 && m.keyIdx < len(m.keyOptions) {
			name = m.keyOptions[m.keyIdx].Name
		}
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(name, focused))}
	case jumpHostField:
		name := "(none)"
		if m.jumpIdx >= 0 && m.jumpIdx < len(m.jumpHostOptions) {
			h := m.jumpHostOptions[m.jumpIdx]
			name = strings.TrimSpace(h.Alias)
			if name == "" {
				name = h.Hostname
			}
		}
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector(name, focused))}
	case advancedField:
		return []string{fmt.Sprintf("%s %s", label, m.renderSelector("Edit ("+m.advancedSummary()+")", focused))}
	case remoteCommandField:
		return append([]string{label}, strings.Split(strings.TrimRight(m.remoteCommand.View(), "\n"), "\n")...)
	case extraOptionsField:
		return append([]string{label}, strings.Split(strings.TrimRight(m.extraOptions.View(), "\n"), "\n")...)
	default:
		idx := inputIndexForField(field)
		if idx >= 0 {
			return []string{fmt.Sprintf("%s %s", label, m.inputs[idx].View())}
		}
		return []string{label}
	}
}

func (m Model) renderActionRow(left, right string) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		actionStyle.Render(left),
		"  ",
		actionAltStyle.Render(right),
	)
}

func (m Model) renderSelector(text string, focused bool) string {
	as := arrowStyle
	ss := selectorStyle
	if focused {
		as = arrowFocusedStyle
		ss = selectorFocusedStyle
	}
	return fmt.Sprintf("%s %s %s", as.Render("\u25c0"), ss.Render(text), as.Render("\u25b6"))
}
