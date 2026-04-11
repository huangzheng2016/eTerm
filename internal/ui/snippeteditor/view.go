package snippeteditor

import (
	"fmt"

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
)

func (m Model) View() tea.View {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Render("Snippet")

	labels := []string{"Name", "Command"}
	var rows []string
	rows = append(rows, title)
	rows = append(rows, "")
	for i := 0; i < 2; i++ {
		rows = append(rows, labelStyle.Render(labels[i])+m.inputs[i].View())
	}

	if m.err != "" {
		rows = append(rows, errorStyle.Render(fmt.Sprintf("⚠ %s", m.err)))
	}
	rows = append(rows, footerStyle.Render("Tab/↓:next | Ctrl+S:save | Esc:cancel"))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := formStyle.Render(content)

	if m.width > 0 && m.height > 0 {
		return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box))
	}
	return tea.NewView(box)
}
