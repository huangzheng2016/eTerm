package editor

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/types"
)

type editorDataLoadedMsg struct {
	keys  []db.SSHKey
	hosts []db.Host
	err   error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadEditorData(), textinput.Blink)
}

func (m Model) loadEditorData() tea.Cmd {
	return func() tea.Msg {
		var keys []db.SSHKey
		var hosts []db.Host
		errK := m.db.Order("name").Find(&keys).Error
		errH := m.db.Order("alias").Find(&hosts).Error
		err := errK
		if errH != nil {
			err = errH
		}
		return editorDataLoadedMsg{keys: keys, hosts: hosts, err: err}
	}
}

func (m Model) currentField() int {
	vf := m.visibleFields()
	if m.focused >= 0 && m.focused < len(vf) {
		return vf[m.focused]
	}
	return -1
}

func (m *Model) blurCurrent() {
	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		m.inputs[idx].Blur()
	}
}

func (m *Model) focusCurrent() tea.Cmd {
	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}

func (m *Model) clampFocus() {
	vf := m.visibleFields()
	if len(vf) == 0 {
		return
	}
	if m.focused >= len(vf) {
		m.focused = len(vf) - 1
	}
	if m.focused < 0 {
		m.focused = 0
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editorDataLoadedMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.keyOptions = msg.keys
			if m.host != nil && m.host.KeyID != nil {
				for i, k := range m.keyOptions {
					if k.ID == *m.host.KeyID {
						m.keyIdx = i
						break
					}
				}
			}
			var opts []db.Host
			for _, h := range msg.hosts {
				if m.host != nil && h.ID > 0 && h.ID == m.host.ID {
					continue
				}
				opts = append(opts, h)
			}
			m.jumpHostOptions = opts
			m.jumpIdx = -1
			if m.host != nil && m.host.JumpHostID != nil {
				for i, jh := range m.jumpHostOptions {
					if jh.ID == *m.host.JumpHostID {
						m.jumpIdx = i
						break
					}
				}
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncInputWidths()
		return m, nil

	case tea.KeyPressMsg:
		vf := m.visibleFields()
		field := m.currentField()

		switch msg.String() {
		case "tab", "down":
			m.blurCurrent()
			m.focused = (m.focused + 1) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "shift+tab", "up":
			m.blurCurrent()
			m.focused = (m.focused - 1 + len(vf)) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "left":
			if field == authMethodField {
				m.authIdx = (m.authIdx - 1 + len(authOptions)) % len(authOptions)
				return m, nil
			}
			if field == keyIDField && len(m.keyOptions) > 0 {
				m.keyIdx = (m.keyIdx - 1 + len(m.keyOptions)) % len(m.keyOptions)
				return m, nil
			}
			if field == jumpHostField {
				n := len(m.jumpHostOptions)
				if n == 0 {
					m.jumpIdx = -1
					return m, nil
				}
				if m.jumpIdx < 0 {
					m.jumpIdx = n - 1
				} else {
					m.jumpIdx--
				}
				return m, nil
			}
			if field == proxyTypeField {
				m.proxyTypeIdx = (m.proxyTypeIdx - 1 + len(proxyOptions)) % len(proxyOptions)
				m.clampFocus()
				return m, nil
			}
			if field == gssapiSourceField {
				m.gssapiSourceIdx = (m.gssapiSourceIdx - 1 + len(gssapiSourceOptions)) % len(gssapiSourceOptions)
				m.clampFocus()
				return m, nil
			}

		case "right":
			if field == authMethodField {
				m.authIdx = (m.authIdx + 1) % len(authOptions)
				return m, nil
			}
			if field == keyIDField && len(m.keyOptions) > 0 {
				m.keyIdx = (m.keyIdx + 1) % len(m.keyOptions)
				return m, nil
			}
			if field == jumpHostField {
				n := len(m.jumpHostOptions)
				if n == 0 {
					m.jumpIdx = -1
					return m, nil
				}
				if m.jumpIdx < 0 {
					m.jumpIdx = 0
				} else if m.jumpIdx < n-1 {
					m.jumpIdx++
				} else {
					m.jumpIdx = -1
				}
				return m, nil
			}
			if field == proxyTypeField {
				m.proxyTypeIdx = (m.proxyTypeIdx + 1) % len(proxyOptions)
				m.clampFocus()
				return m, nil
			}
			if field == gssapiSourceField {
				m.gssapiSourceIdx = (m.gssapiSourceIdx + 1) % len(gssapiSourceOptions)
				m.clampFocus()
				return m, nil
			}

		case "enter":
			if m.focused == len(vf)-1 {
				return m, m.save()
			}
			m.blurCurrent()
			m.focused = (m.focused + 1) % len(vf)
			cmd := m.focusCurrent()
			return m, cmd

		case "ctrl+s":
			return m, m.save()

		case "esc":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		}
	}

	field := m.currentField()
	idx := inputIndexForField(field)
	if idx >= 0 {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}

	return m, nil
}
