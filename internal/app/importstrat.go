package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui"
)

type importStratItem int

const (
	stratSkip importStratItem = iota
	stratOverwrite
)

type importStratMenuModel struct {
	conflicts int
	cursor    importStratItem
}

func newImportStratMenu(conflicts int) *importStratMenuModel {
	return &importStratMenuModel{conflicts: conflicts, cursor: stratSkip}
}

func (m *importStratMenuModel) Update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < stratOverwrite {
			m.cursor++
		}
	case "enter":
		switch m.cursor {
		case stratSkip:
			return true, func() tea.Msg { return types.ImportSSHConfigRunMsg{Strategy: "skip"} }
		case stratOverwrite:
			return true, func() tea.Msg { return types.ImportSSHConfigRunMsg{Strategy: "overwrite"} }
		}
	case "1":
		return true, func() tea.Msg { return types.ImportSSHConfigRunMsg{Strategy: "skip"} }
	case "2":
		return true, func() tea.Msg { return types.ImportSSHConfigRunMsg{Strategy: "overwrite"} }
	case "esc", "escape", "q":
		return true, nil
	}
	return false, nil
}

func (m *importStratMenuModel) View() string {
	title := ui.TitleStyle.Render("SSH config import")
	msg := fmt.Sprintf("%d host entries already exist in eTerm.", m.conflicts)
	items := []struct {
		label string
		key   string
		s     importStratItem
	}{
		{"  Skip duplicates (safe)     ", "1", stratSkip},
		{"  Overwrite matching hosts    ", "2", stratOverwrite},
	}
	var rows string
	for _, it := range items {
		cur := "  "
		st := ui.DimStyle
		if it.s == m.cursor {
			cur = "▸ "
			st = ui.SelectedStyle
		}
		rows += fmt.Sprintf("%s%s %s\n", cur, st.Render(it.label), ui.DimStyle.Render("["+it.key+"]"))
	}
	hint := ui.DimStyle.Render("↑↓ · 1/2 · enter · esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", msg, "", rows, hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(46).
		Render(content)
}
