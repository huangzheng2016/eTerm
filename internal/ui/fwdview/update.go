package fwdview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"
	"github.com/eterm/eterm/internal/viewkeys"
)

func (m Model) Init() tea.Cmd {
	return m.loadRules()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case forwardsLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg {
				return types.ErrorMsg{Err: msg.err}
			}
		}
		m.rules = msg.rules
		m.loaded = true
		if m.gridCursor >= len(m.rules) {
			m.gridCursor = 0
		}
		return m, nil

	case types.ForwardRuleResultMsg:
		if msg.RuleID == 0 {
			return m, nil
		}
		if m.running == nil {
			m.running = make(map[uint]bool)
		}
		if msg.Err != nil {
			delete(m.running, msg.RuleID)
		} else {
			m.running[msg.RuleID] = msg.Running
		}
		return m, nil

	case types.RefreshListMsg:
		return m, m.loadRules()

	case tea.KeyPressMsg:
		if msg.Key().IsRepeat {
			break
		}
		// Grid navigation
		switch msg.String() {
		case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
			newCur, changed := components.GridMove(msg.String(), m.gridCursor, len(m.rules), m.gridLayout)
			if changed {
				m.gridCursor = newCur
			}
			return m, nil
		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		default:
			s := msg.String()
			switch {
			case viewkeys.MatchAny(s, m.vk.Start):
				if r := m.SelectedRule(); r != nil && r.ID != 0 {
					id := r.ID
					return m, func() tea.Msg { return types.ForwardRuleStartMsg{RuleID: id} }
				}
				return m, nil
			case viewkeys.MatchAny(s, m.vk.Stop):
				if r := m.SelectedRule(); r != nil && r.ID != 0 {
					id := r.ID
					return m, func() tea.Msg { return types.ForwardRuleStopMsg{RuleID: id} }
				}
				return m, nil
			case viewkeys.MatchAny(s, m.vk.New):
				return m, func() tea.Msg {
					return types.NewTabMsg{Type: "fwd-editor", Title: "New Forward"}
				}
			case viewkeys.MatchAny(s, m.vk.Edit):
				if r := m.SelectedRule(); r != nil && r.ID != 0 {
					id := r.ID
					return m, func() tea.Msg {
						return types.NewTabMsg{Type: "fwd-editor", Title: "Edit Forward", Data: id}
					}
				}
				return m, nil
			case viewkeys.MatchAny(s, m.vk.Delete):
				if r := m.SelectedRule(); r != nil && r.ID != 0 {
					id := r.ID
					desc := ruleCardTitle(*r)
					return m, func() tea.Msg {
						return types.ForwardRuleDeleteRequestMsg{ID: id, Desc: desc}
					}
				}
				return m, nil
			}
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			page := 0
			if m.gridLayout.PageSize > 0 {
				page = m.gridCursor / m.gridLayout.PageSize
			}
			idx, ok := components.GridIndexAtMouse(msg.X, msg.Y, len(m.rules), m.gridLayout, page)
			if ok {
				m.gridCursor = idx
			}
			return m, nil
		}
	}

	return m, nil
}
