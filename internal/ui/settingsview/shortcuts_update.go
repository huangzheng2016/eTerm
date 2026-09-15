package settingsview

import (
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m *ShortcutsModel) Init() tea.Cmd {
	return nil
}

func (m *ShortcutsModel) maxCursor() int {
	return len(m.entries) - 1
}

func (m *ShortcutsModel) resetToFactory() {
	m.entries = buildEntries(m.defaultsJSON)
	m.modified = true
}

func (m *ShortcutsModel) jumpGroup(delta int) {
	starts := m.groupStarts()
	if len(starts) == 0 {
		return
	}
	g := m.currentGroup(starts)
	if delta < 0 {
		if m.cursor == starts[g] && g > 0 {
			g--
		}
	} else if g < len(starts)-1 {
		g++
	}
	m.cursor = starts[g]
}

func (m *ShortcutsModel) jumpGroupIndex(n int) {
	starts := m.groupStarts()
	if n >= 0 && n < len(starts) {
		m.cursor = starts[n]
	}
}

func (m *ShortcutsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case types.SettingsSavedMsg:
		if msg.Err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		m.modified = false
		return m, tea.Batch(
			func() tea.Msg { return types.KeyBindingsChangedMsg{} },
			func() tea.Msg { return types.RefreshListMsg{} },
		)

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.MouseWheelMsg:
		if m.confirmReset.IsActive() {
			return m, nil
		}
		if m.state != stateNormal {
			return m, nil
		}
		n := m.totalScrollLines()
		vis := visibleRowsFor(m.height)
		maxScr := n - vis
		if maxScr < 0 {
			maxScr = 0
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.scroll > 0 {
				m.scroll--
			}
		case tea.MouseWheelDown:
			if m.scroll < maxScr {
				m.scroll++
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.confirmReset.IsActive() {
			if msg.Button == tea.MouseLeft {
				ox, oy := dialogOrigin(m.confirmReset.View(), m.width, m.height)
				mm := msg.Mouse()
				mm.X -= ox
				mm.Y -= oy
				msg = tea.MouseClickMsg(mm)
			}
			m.confirmReset, _ = m.confirmReset.Update(msg)
			if !m.confirmReset.IsActive() && m.confirmReset.Result() {
				m.resetToFactory()
			}
			return m, nil
		}
		if m.state != stateNormal {
			return m, nil
		}
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if msg.Y < 2 {
			return m, nil
		}
		lineIdx := m.scroll + (msg.Y - 2)
		lines := m.buildScrollLines()
		if lineIdx < 0 || lineIdx >= len(lines) {
			return m, nil
		}
		li := lines[lineIdx].logicalIdx
		if li < 0 {
			return m, nil
		}
		m.cursor = li
		return m, nil

	case tea.KeyPressMsg:
		if m.confirmReset.IsActive() {
			m.confirmReset, _ = m.confirmReset.Update(msg)
			if !m.confirmReset.IsActive() && m.confirmReset.Result() {
				m.resetToFactory()
			}
			return m, nil
		}
		if m.state == stateCapture || m.state == stateAppend {
			return m.handleCapture(msg)
		}
		return m.handleNormal(msg)
	}
	return m, nil
}

func (m *ShortcutsModel) handleNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.maxCursor() {
			m.cursor++
		}
	case "[":
		m.jumpGroup(-1)
	case "]":
		m.jumpGroup(1)
	case "1", "2", "3", "4", "5", "6", "7":
		m.jumpGroupIndex(int(msg.String()[0] - '1'))
	case "enter":
		m.state = stateCapture
	case "+", "=":
		m.state = stateAppend
	case "backspace", "delete":
		if len(m.entries[m.cursor].Keys) > 0 {
			m.entries[m.cursor].Keys = nil
			m.modified = true
		}
	case "ctrl+s":
		return m, m.save()
	case "ctrl+r":
		m.confirmReset = m.confirmReset.Show()
	case "esc":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}
	return m, nil
}

func (m *ShortcutsModel) handleCapture(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		m.state = stateNormal
		return m, nil
	}
	ks := keyString(msg)

	if ks == "esc" || ks == "escape" {
		m.state = stateNormal
		return m, nil
	}

	if m.state == stateAppend {
		found := false
		for _, k := range m.entries[m.cursor].Keys {
			if k == ks {
				found = true
				break
			}
		}
		if !found {
			m.entries[m.cursor].Keys = append(m.entries[m.cursor].Keys, ks)
		}
	} else {
		m.entries[m.cursor].Keys = []string{ks}
	}
	m.modified = true
	m.state = stateNormal

	return m, nil
}

func (m *ShortcutsModel) save() tea.Cmd {
	database := m.db
	configData := m.ConfigJSON()
	return func() tea.Msg {
		if err := db.SetSetting(database, "keybindings", string(configData)); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		return types.SettingsSavedMsg{}
	}
}
