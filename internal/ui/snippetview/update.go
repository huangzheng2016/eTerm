package snippetview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"
)

func (m *Model) Init() tea.Cmd {
	return m.loadSnippets()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case snippetsLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.snippets = msg.snippets
		m.loaded = true
		if m.gridCursor >= len(m.snippets) {
			m.gridCursor = 0
		}
		return m, nil

	case types.RefreshListMsg:
		return m, m.loadSnippets()

	case tea.KeyPressMsg:
		if msg.Key().IsRepeat {
			break
		}
		switch msg.String() {
		case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
			newCur, changed := components.GridMove(msg.String(), m.gridCursor, len(m.snippets), m.gridLayout)
			if changed {
				m.gridCursor = newCur
			}
			return m, nil
		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		case "n":
			return m, func() tea.Msg {
				return types.NewTabMsg{Type: "snippet-editor", Title: "New Snippet"}
			}
		case "e":
			if s := m.SelectedSnippet(); s != nil && s.ID != 0 {
				id := s.ID
				return m, func() tea.Msg {
					return types.NewTabMsg{Type: "snippet-editor", Title: "Edit Snippet", Data: id}
				}
			}
			return m, nil
		case "d":
			if s := m.SelectedSnippet(); s != nil && s.ID != 0 {
				id := s.ID
				name := s.Name
				return m, func() tea.Msg {
					return types.SnippetDeleteRequestMsg{ID: id, Name: name}
				}
			}
			return m, nil
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			page := 0
			if m.gridLayout.PageSize > 0 {
				page = m.gridCursor / m.gridLayout.PageSize
			}
			idx, ok := components.GridIndexAtMouse(msg.X, msg.Y, len(m.snippets), m.gridLayout, page)
			if ok {
				m.gridCursor = idx
			}
			return m, nil
		}
	}

	return m, nil
}
