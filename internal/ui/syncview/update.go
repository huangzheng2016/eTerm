package syncview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case types.SyncTestResultMsg:
		m.testing = false
		if msg.OK {
			m.err = "Connection OK"
		} else {
			m.err = fmt.Sprintf("Test failed: %v", msg.Err)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.PasteMsg:
		f := m.currentField()
		idx := m.inputIdxForField(f)
		if idx >= 0 {
			m.inputs[idx] = inputpaste.TextInput(m.inputs[idx], msg)
		}
		return m, nil
	}
	f := m.currentField()
	idx := m.inputIdxForField(f)
	if idx >= 0 {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}
	return m, nil
}
