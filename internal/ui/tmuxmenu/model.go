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
	page     int
	loading  bool
	err      string
}

const pageSize = 8

func New(sessions []types.TmuxSession) *Model {
	return &Model{sessions: sessions}
}

func (m *Model) SetSessions(s []types.TmuxSession) {
	m.sessions = s
	m.loading = false
	m.err = ""
	m.clamp()
}

func (m *Model) SetLoading(loading bool) {
	m.loading = loading
	if loading {
		m.err = ""
	}
}

func (m *Model) SetError(err string) {
	m.err = strings.TrimSpace(err)
	m.loading = false
}

func (m *Model) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	max := len(m.sessions)
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.syncPage()
		}
	case "down", "j":
		if m.cursor < max {
			m.cursor++
			m.syncPage()
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
	if msg.Text == "R" {
		m.SetLoading(true)
		return false, func() tea.Msg { return types.TmuxMenuMsg{} }
	}
	return false, nil
}

func (m *Model) View() string {
	rows := []string{ui.TitleStyle.Render("tmux"), ""}
	rows = append(rows, m.row(0, "+ New session", "start a local tmux session"))
	if m.loading {
		rows = append(rows, ui.DimStyle.Render("Loading tmux sessions..."))
	} else if m.err != "" {
		rows = append(rows, ui.DimStyle.Render(m.err))
	} else if len(m.sessions) == 0 {
		rows = append(rows, ui.DimStyle.Render("No tmux sessions"))
	} else {
		start := m.page * pageSize
		end := start + pageSize
		if end > len(m.sessions) {
			end = len(m.sessions)
		}
		for i, s := range m.sessions[start:end] {
			rows = append(rows, m.row(start+i+1, s.Name, tmuxDesc(s)))
		}
		if len(m.sessions) > pageSize {
			rows = append(rows, "", ui.DimStyle.Render(fmt.Sprintf("page %d/%d", m.page+1, (len(m.sessions)+pageSize-1)/pageSize)))
		}
	}
	rows = append(rows, "", ui.DimStyle.Render("up/down navigate · enter open · r rename · d kill · R refresh · esc close"))
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

func (m *Model) clamp() {
	if m.cursor > len(m.sessions) {
		m.cursor = len(m.sessions)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.syncPage()
}

func (m *Model) syncPage() {
	if m.cursor == 0 {
		m.page = 0
		return
	}
	sessionIdx := m.cursor - 1
	if sessionIdx < m.page*pageSize {
		m.page = sessionIdx / pageSize
	}
	if sessionIdx >= (m.page+1)*pageSize {
		m.page = sessionIdx / pageSize
	}
}
