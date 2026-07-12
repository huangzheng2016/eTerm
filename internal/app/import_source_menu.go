package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type importSourceItem int

const (
	importSourceSSHConfig importSourceItem = iota
	importSourceTermius
)

type importSourceMenuModel struct {
	cursor importSourceItem
}

func newImportSourceMenu() *importSourceMenuModel {
	return &importSourceMenuModel{}
}

func (m *importSourceMenuModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < importSourceTermius {
			m.cursor++
		}
	case "enter":
		switch m.cursor {
		case importSourceTermius:
			return true, func() tea.Msg { return termiusLoadMsg{} }
		case importSourceSSHConfig:
			return true, func() tea.Msg { return sshConfigLoadMsg{} }
		}
	case "s":
		return true, func() tea.Msg { return sshConfigLoadMsg{} }
	case "t":
		return true, func() tea.Msg { return termiusLoadMsg{} }
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *importSourceMenuModel) View() string {
	items := []struct {
		label string
		key   string
	}{
		{"  .ssh/config   ", "s"},
		{"  Termius       ", "t"},
	}
	var rows string
	for i, item := range items {
		cursor := "  "
		style := ui.DimStyle
		if importSourceItem(i) == m.cursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		row := fmt.Sprintf("%s%s %s", cursor, style.Render(item.label), ui.DimStyle.Render("["+item.key+"]"))
		rows += row + "\n"
	}
	title := ui.TitleStyle.Render("Import from")
	hint1 := ui.DimStyle.Render("↑↓ navigate · enter select")
	hint2 := ui.DimStyle.Render("esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows, hint1, hint2)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}
