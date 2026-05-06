package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"gorm.io/gorm"
)

type snippetPickerModel struct {
	snippets []db.Snippet
	cursor   int
	db       *gorm.DB
}

func newSnippetPickerModel(database *gorm.DB) *snippetPickerModel {
	var snippets []db.Snippet
	database.Order("name asc").Find(&snippets)
	return &snippetPickerModel{snippets: snippets, db: database}
}

func (s *snippetPickerModel) View() string {
	title := ui.TitleStyle.Render("Snippets")

	if len(s.snippets) == 0 {
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("No snippets saved. Add them in the Snippets tab (Ctrl+Shift+B).")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", hint, "", "Esc: close")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2).
			Render(content)
	}

	var lines []string
	for i, sn := range s.snippets {
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#aaa"))
		if i == s.cursor {
			prefix = "> "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
		}
		line := style.Render(fmt.Sprintf("%s%-16s %s", prefix, sn.Name, sn.Command))
		lines = append(lines, line)
	}

	list := lipgloss.JoinVertical(lipgloss.Left, lines...)
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render("Enter: paste | Esc: cancel | j/k: navigate")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", list, "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

func (a App) handleSnippetPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	sp := a.snippetPicker
	switch msg.String() {
	case "esc":
		a.snippetPicker = nil
		return a, nil
	case "enter":
		if len(sp.snippets) > 0 && sp.cursor >= 0 && sp.cursor < len(sp.snippets) {
			cmd := sp.snippets[sp.cursor].Command
			a.snippetPicker = nil
			return a, func() tea.Msg { return types.SnippetSelectedMsg{Command: cmd} }
		}
		a.snippetPicker = nil
		return a, nil
	case "j", "down":
		if sp.cursor < len(sp.snippets)-1 {
			sp.cursor++
		}
		return a, nil
	case "k", "up":
		if sp.cursor > 0 {
			sp.cursor--
		}
		return a, nil
	}
	return a, nil
}
