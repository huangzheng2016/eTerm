package fwdeditor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	formStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 3).
			Width(50)
	labelStyle = lipgloss.NewStyle().
			Width(14).
			Foreground(lipgloss.Color("#7D56F4"))
	selectorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00cc00"))
	focusedSelectorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00ff00")).
				Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff0000"))
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262"))
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
)

func (m Model) renderSelector(value string, focused bool) string {
	if focused {
		return focusedSelectorStyle.Render(fmt.Sprintf("◀ %s ▶", value))
	}
	return selectorStyle.Render(fmt.Sprintf("◀ %s ▶", value))
}

func (m Model) View() tea.View {
	box, _, _ := m.renderForm()
	if m.width > 0 && m.height > 0 {
		return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box))
	}
	return tea.NewView(box)
}

func (m Model) renderForm() (string, []int, int) {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Port Forward Rule")

	vf := m.visibleFields()
	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")
	fieldYs := make([]int, 0, len(vf))

	for i, field := range vf {
		fieldYs = append(fieldYs, len(rows)+2)
		focused := i == m.focused
		label := labelStyle.Render(fieldLabels[field])
		var value string
		switch field {
		case hostField:
			name := "(no hosts)"
			if m.hostIdx >= 0 && m.hostIdx < len(m.hostOptions) {
				name = hostLabel(m.hostOptions[m.hostIdx])
			}
			value = m.renderSelector(name, focused)
		case directionField:
			value = m.renderSelector(directionDisplay[m.directionIdx], focused)
		default:
			idx := inputIndexForField(field)
			if idx >= 0 {
				value = m.inputs[idx].View()
			}
		}
		rows = append(rows, label+value)
	}
	rows = append(rows, "")
	actionY := len(rows) + 2
	rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, actionStyle.Render("Save"), "  ", actionAltStyle.Render("Cancel")))

	if m.err != "" {
		rows = append(rows, errorStyle.Render(fmt.Sprintf("⚠ %s", m.err)))
	}

	rows = append(rows, footerStyle.Render("Tab/↓ next | Shift+Tab/↑ prev | ←→ select | Ctrl+S save | Esc cancel | click fields/buttons"))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return formStyle.Render(content), fieldYs, actionY
}

func (m Model) centeredBounds(rendered string) (ox, oy, ow, oh int) {
	lines := strings.Split(rendered, "\n")
	oh = len(lines)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > ow {
			ow = w
		}
	}
	layoutW := m.width
	if layoutW <= 0 {
		layoutW = 80
	}
	layoutH := m.height
	if layoutH <= 0 {
		layoutH = 24
	}
	ox = (layoutW - ow) / 2
	oy = (layoutH - oh) / 2
	return
}
