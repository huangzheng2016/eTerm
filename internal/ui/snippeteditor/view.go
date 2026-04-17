package snippeteditor

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
			Width(10).
			Foreground(lipgloss.Color("#7D56F4"))
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

func (m Model) View() tea.View {
	box, _ := m.renderForm()
	if m.width > 0 && m.height > 0 {
		box = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return tea.NewView(box)
}

func (m Model) renderForm() (string, int) {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Snippet")

	labels := []string{"Name", "Command"}
	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")
	for i := 0; i < 2; i++ {
		rows = append(rows, labelStyle.Render(labels[i])+m.inputs[i].View())
	}
	rows = append(rows, "")
	actionY := len(rows) + 2
	rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, actionStyle.Render("Save"), "  ", actionAltStyle.Render("Cancel")))

	if m.err != "" {
		rows = append(rows, errorStyle.Render(fmt.Sprintf("⚠ %s", m.err)))
	}
	rows = append(rows, footerStyle.Render("Tab/↓ next | Ctrl+S save | Esc cancel | click fields/buttons"))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return formStyle.Render(content), actionY
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
