package editor

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

type editorDataLoadedMsg struct {
	keys  []db.SSHKey
	hosts []db.Host
	err   error
}

type fieldSpan struct {
	field  int
	startY int
	endY   int
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
	fields := m.activeFields()
	idx := m.focused
	if m.advancedActive {
		idx = m.advancedFocused
	}
	if idx >= 0 && idx < len(fields) {
		return fields[idx]
	}
	return -1
}

func (m *Model) blurCurrent() {
	field := m.currentField()
	if fieldUsesTextarea(field) {
		switch field {
		case remoteCommandField:
			m.remoteCommand.Blur()
		case extraOptionsField:
			m.extraOptions.Blur()
		}
		return
	}
	idx := inputIndexForField(field)
	if idx >= 0 {
		m.inputs[idx].Blur()
	}
}

func (m *Model) focusCurrent() tea.Cmd {
	field := m.currentField()
	if fieldUsesTextarea(field) {
		switch field {
		case remoteCommandField:
			return m.remoteCommand.Focus()
		case extraOptionsField:
			return m.extraOptions.Focus()
		}
	}
	idx := inputIndexForField(field)
	if idx >= 0 {
		return m.inputs[idx].Focus()
	}
	return nil
}

func (m *Model) clampFocus() {
	fields := m.activeFields()
	if len(fields) == 0 {
		return
	}
	if m.advancedActive {
		if m.advancedFocused < 0 {
			m.advancedFocused = 0
		}
		if m.advancedFocused >= len(fields) {
			m.advancedFocused = len(fields) - 1
		}
		return
	}
	if m.focused < 0 {
		m.focused = 0
	}
	if m.focused >= len(fields) {
		m.focused = len(fields) - 1
	}
}

func (m *Model) activeFocus() *int {
	if m.advancedActive {
		return &m.advancedFocused
	}
	return &m.focused
}

func (m *Model) moveFocus(delta int) tea.Cmd {
	fields := m.activeFields()
	if len(fields) == 0 {
		return nil
	}
	m.blurCurrent()
	idx := m.activeFocus()
	*idx = (*idx + delta + len(fields)) % len(fields)
	return m.focusCurrent()
}

func (m *Model) openAdvanced() tea.Cmd {
	m.advancedActive = true
	m.advancedFocused = 0
	m.err = ""
	return m.focusCurrent()
}

func (m *Model) closeAdvanced() tea.Cmd {
	m.advancedActive = false
	m.advancedFocused = 0
	return m.focusCurrent()
}

func (m *Model) cycleSelector(field, dir int) bool {
	switch field {
	case authMethodField:
		m.authIdx = (m.authIdx + dir + len(authOptions)) % len(authOptions)
		m.clampFocus()
		return true
	case keyIDField:
		if len(m.keyOptions) == 0 {
			return true
		}
		m.keyIdx = (m.keyIdx + dir + len(m.keyOptions)) % len(m.keyOptions)
		return true
	case jumpHostField:
		n := len(m.jumpHostOptions)
		if n == 0 {
			m.jumpIdx = -1
			return true
		}
		if dir < 0 {
			if m.jumpIdx < 0 {
				m.jumpIdx = n - 1
			} else {
				m.jumpIdx--
			}
		} else {
			if m.jumpIdx < 0 {
				m.jumpIdx = 0
			} else if m.jumpIdx < n-1 {
				m.jumpIdx++
			} else {
				m.jumpIdx = -1
			}
		}
		return true
	case proxyTypeField:
		m.proxyTypeIdx = (m.proxyTypeIdx + dir + len(proxyOptions)) % len(proxyOptions)
		m.clampFocus()
		return true
	case gssapiSourceField:
		m.gssapiSourceIdx = (m.gssapiSourceIdx + dir + len(gssapiSourceOptions)) % len(gssapiSourceOptions)
		m.clampFocus()
		return true
	case forwardAgentField:
		m.forwardAgentIdx = (m.forwardAgentIdx + dir + len(boolOptions)) % len(boolOptions)
		return true
	default:
		return false
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
		if m.advancedActive {
			return m.handleAdvancedKey(msg)
		}
		return m.handleMainKey(msg)

	case tea.PasteMsg:
		return m.pasteCurrentField(msg)

	case tea.MouseClickMsg:
		if m.advancedActive {
			return m.handleAdvancedMouse(msg)
		}
		return m.handleMainMouse(msg)
	}

	return m, nil
}

func (m Model) pasteCurrentField(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	field := m.currentField()
	if fieldUsesTextarea(field) {
		switch field {
		case remoteCommandField:
			m.remoteCommand = inputpaste.TextArea(m.remoteCommand, msg)
		case extraOptionsField:
			m.extraOptions = inputpaste.TextArea(m.extraOptions, msg)
		}
		return m, nil
	}
	idx := inputIndexForField(field)
	if idx >= 0 {
		m.inputs[idx] = inputpaste.TextInput(m.inputs[idx], msg)
	}
	return m, nil
}

func (m Model) handleMainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	field := m.currentField()
	isTextarea := fieldUsesTextarea(field)
	fields := m.mainVisibleFields()

	switch msg.String() {
	case "tab":
		return m, m.moveFocus(1)
	case "shift+tab":
		return m, m.moveFocus(-1)
	case "down":
		if isTextarea {
			break
		}
		return m, m.moveFocus(1)
	case "up":
		if isTextarea {
			break
		}
		return m, m.moveFocus(-1)
	case "left":
		if m.cycleSelector(field, -1) {
			return m, nil
		}
	case "right":
		if m.cycleSelector(field, 1) {
			return m, nil
		}
	case "a":
		return m, m.openAdvanced()
	case "enter":
		if field == advancedField {
			return m, m.openAdvanced()
		}
		if isTextarea {
			break
		}
		if m.focused >= len(fields)-1 {
			return m, nil
		}
		return m, m.moveFocus(1)
	case "ctrl+s":
		return m, m.save()
	case "esc":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}

	return m.updateCurrentField(msg)
}

func (m Model) handleAdvancedKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	field := m.currentField()
	isTextarea := fieldUsesTextarea(field)
	fields := m.advancedVisibleFields()

	switch msg.String() {
	case "tab":
		return m, m.moveFocus(1)
	case "shift+tab":
		return m, m.moveFocus(-1)
	case "down":
		if isTextarea {
			break
		}
		return m, m.moveFocus(1)
	case "up":
		if isTextarea {
			break
		}
		return m, m.moveFocus(-1)
	case "left":
		if m.cycleSelector(field, -1) {
			return m, nil
		}
	case "right":
		if m.cycleSelector(field, 1) {
			return m, nil
		}
	case "ctrl+s":
		return m, m.save()
	case "esc":
		return m, m.closeAdvanced()
	case "enter":
		if isTextarea {
			break
		}
		if m.advancedFocused >= len(fields)-1 {
			return m, nil
		}
		return m, m.moveFocus(1)
	}

	return m.updateCurrentField(msg)
}

func (m Model) updateCurrentField(msg tea.Msg) (tea.Model, tea.Cmd) {
	field := m.currentField()
	if fieldUsesTextarea(field) {
		var cmd tea.Cmd
		switch field {
		case remoteCommandField:
			m.remoteCommand, cmd = m.remoteCommand.Update(msg)
			return m, cmd
		case extraOptionsField:
			m.extraOptions, cmd = m.extraOptions.Update(msg)
			return m, cmd
		}
	}
	idx := inputIndexForField(field)
	if idx >= 0 {
		var cmd tea.Cmd
		m.inputs[idx], cmd = m.inputs[idx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) centeredBounds(rendered string) (ox, oy, ow, oh int) {
	lines := strings.Split(rendered, "\n")
	oh = len(lines)
	for _, l := range lines {
		if w := lipgloss.Width(l); w > ow {
			ow = w
		}
	}
	layoutW := m.width
	if layoutW <= 0 {
		layoutW = 80
	}
	layoutH := m.height
	if layoutH <= 0 {
		layoutH = 24
	}
	ox = (layoutW - ow) / 2
	oy = (layoutH - oh) / 2
	return
}

func fieldIndex(fields []int, field int) int {
	for i, f := range fields {
		if f == field {
			return i
		}
	}
	return -1
}

func (m Model) handleMainMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	rendered, spans, actionY := m.renderMainForm()
	ox, oy, ow, oh := m.centeredBounds(rendered)
	lx := msg.X - ox
	ly := msg.Y - oy
	if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
		return m, nil
	}
	if ly == actionY {
		if lx < ow/2 {
			return m, m.save()
		}
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}
	fields := m.mainVisibleFields()
	for _, span := range spans {
		if ly < span.startY || ly > span.endY {
			continue
		}
		if idx := fieldIndex(fields, span.field); idx >= 0 {
			m.blurCurrent()
			m.focused = idx
		}
		if span.field == advancedField {
			return m, m.openAdvanced()
		}
		if fieldUsesSelector(span.field) {
			dir := 1
			if lx < ow/2 {
				dir = -1
			}
			m.cycleSelector(span.field, dir)
			return m, nil
		}
		return m, m.focusCurrent()
	}
	return m, nil
}

func (m Model) handleAdvancedMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	rendered, spans, actionY := m.renderAdvancedOverlay()
	ox, oy, ow, oh := m.centeredBounds(rendered)
	lx := msg.X - ox
	ly := msg.Y - oy
	if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
		return m, m.closeAdvanced()
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if ly == actionY {
		if lx < ow/2 {
			return m, m.closeAdvanced()
		}
		return m, m.save()
	}
	fields := m.advancedVisibleFields()
	for _, span := range spans {
		if ly < span.startY || ly > span.endY {
			continue
		}
		if idx := fieldIndex(fields, span.field); idx >= 0 {
			m.blurCurrent()
			m.advancedFocused = idx
		}
		if fieldUsesSelector(span.field) {
			dir := 1
			if lx < ow/2 {
				dir = -1
			}
			m.cycleSelector(span.field, dir)
			return m, nil
		}
		return m, m.focusCurrent()
	}
	return m, nil
}
