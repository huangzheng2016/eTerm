package snippeteditor

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab", "down":
			m.inputs[m.focused].Blur()
			m.focused = (m.focused + 1) % 2
			return m, m.inputs[m.focused].Focus()
		case "shift+tab", "up":
			m.inputs[m.focused].Blur()
			m.focused = (m.focused - 1 + 2) % 2
			return m, m.inputs[m.focused].Focus()
		case "ctrl+s":
			return m, m.save()
		case "enter":
			if m.focused == 1 {
				return m, m.save()
			}
			m.inputs[m.focused].Blur()
			m.focused = 1
			return m, m.inputs[1].Focus()
		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}

		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd

	case tea.PasteMsg:
		m.inputs[m.focused] = inputpaste.TextInput(m.inputs[m.focused], msg)
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		rendered, actionY := m.renderForm()
		ox, oy, ow, oh := m.centeredBounds(rendered)
		lx := msg.X - ox
		ly := msg.Y - oy
		if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
			return m, nil
		}
		switch ly {
		case 4:
			m.inputs[m.focused].Blur()
			m.focused = 0
			return m, m.inputs[0].Focus()
		case 5:
			m.inputs[m.focused].Blur()
			m.focused = 1
			return m, m.inputs[1].Focus()
		case actionY:
			if lx < ow/2 {
				return m, m.save()
			}
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}
	}
	return m, nil
}

func (m Model) save() tea.Cmd {
	name := strings.TrimSpace(m.inputs[0].Value())
	if name == "" {
		m.err = "Name is required"
		return nil
	}
	command := strings.TrimSpace(m.inputs[1].Value())
	if command == "" {
		m.err = "Command is required"
		return nil
	}

	snippet := db.Snippet{Name: name, Command: command}
	database := m.db
	sid := m.snippetID

	return func() tea.Msg {
		if sid > 0 {
			snippet.ID = sid
			if err := database.Save(&snippet).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
		} else {
			if err := database.Create(&snippet).Error; err != nil {
				return types.ErrorMsg{Err: err}
			}
		}
		return types.SnippetSavedMsg{Snippet: snippet}
	}
}
