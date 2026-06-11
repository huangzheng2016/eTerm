package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type escMenuItem int

const (
	escMenuQuit escMenuItem = iota
	escMenuSettings
	escMenuImport
	escMenuSync
)

type escMenuModel struct {
	cursor escMenuItem
}

func newEscMenu() *escMenuModel {
	return &escMenuModel{cursor: escMenuQuit}
}

func (m *escMenuModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < escMenuSync {
			m.cursor++
		}
	case "enter":
		switch m.cursor {
		case escMenuQuit:
			return true, func() tea.Msg { return types.QuitRequestMsg{} }
		case escMenuSettings:
			return true, func() tea.Msg { return types.OpenSettingsMsg{} }
		case escMenuImport:
			return true, func() tea.Msg { return types.OpenImportSourceMenuMsg{} }
		case escMenuSync:
			return true, func() tea.Msg { return types.OpenSyncMsg{} }
		}
	case "esc", "escape":
		return true, nil
	case "q":
		return true, func() tea.Msg { return types.QuitRequestMsg{} }
	case "s":
		return true, func() tea.Msg { return types.OpenSettingsMsg{} }
	case "i":
		return true, func() tea.Msg { return types.OpenImportSourceMenuMsg{} }
	case "y":
		return true, func() tea.Msg { return types.OpenSyncMsg{} }
	}
	return false, nil
}

func (m *escMenuModel) View() string {
	items := []struct {
		label string
		key   string
	}{
		{"  Quit          ", "q"},
		{"  Settings      ", "s"},
		{"  Import        ", "i"},
		{"  Sync          ", "y"},
	}

	var rows string
	for i, item := range items {
		cursor := "  "
		style := ui.DimStyle
		if escMenuItem(i) == m.cursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		row := fmt.Sprintf("%s%s %s", cursor, style.Render(item.label), ui.DimStyle.Render("["+item.key+"]"))
		rows += row + "\n"
	}

	title := ui.TitleStyle.Render("eTerm")
	hint1 := ui.DimStyle.Render("↑↓ navigate · enter select")
	hint2 := ui.DimStyle.Render("esc close")

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		rows,
		hint1,
		hint2,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}
