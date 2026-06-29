package tmuxmenu

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type Model struct {
	sessions []types.TmuxSession
	cursor   int
}

func New(sessions []types.TmuxSession) *Model {
	return &Model{sessions: sessions}
}

func (m *Model) SetSessions(s []types.TmuxSession) {
	m.sessions = s
	if m.cursor > len(s) {
		m.cursor = 0
	}
}

func (m *Model) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	max := len(m.sessions)
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < max {
			m.cursor++
		}
	case "enter":
		if m.cursor == 0 {
			return true, func() tea.Msg { return types.TmuxOpenMsg{New: true} }
		}
		name := m.sessions[m.cursor-1].Name
		return true, func() tea.Msg { return types.TmuxOpenMsg{Name: name} }
	case "r":
		if m.cursor > 0 {
			name := m.sessions[m.cursor-1].Name
			return false, func() tea.Msg { return types.TmuxRenameRequestMsg{Name: name} }
		}
	case "d", "delete":
		if m.cursor > 0 {
			name := m.sessions[m.cursor-1].Name
			return false, func() tea.Msg { return types.TmuxKillRequestMsg{Name: name} }
		}
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *Model) View() string {
	rows := []string{ui.TitleStyle.Render("tmux"), ""}
	rows = append(rows, m.row(0, "+ New session", "start a local tmux session"))
	for i, s := range m.sessions {
		rows = append(rows, m.row(i+1, s.Name, tmuxDesc(s)))
	}
	rows = append(rows, "", ui.DimStyle.Render("up/down navigate · enter open · r rename · d kill · esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(56).
		Render(strings.Join(rows, "\n"))
}

func tmuxDesc(s types.TmuxSession) string {
	desc := time.Unix(s.CreatedUnix, 0).Format("15:04:05")
	if s.CreatedUnix == 0 {
		desc = ""
	}
	if s.Attached {
		if desc != "" {
			desc += " "
		}
		desc += "attached"
	}
	if desc == "" {
		return ""
	}
	return fmt.Sprintf("%s", desc)
}

func (m *Model) row(idx int, title, desc string) string {
	cursor := "  "
	style := ui.DimStyle
	if idx == m.cursor {
		cursor = "> "
		style = ui.SelectedStyle
	}
	return strings.TrimRight(cursor+style.Render(title)+" "+ui.DimStyle.Render(desc), " ")
}
