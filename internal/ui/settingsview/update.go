package settingsview

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

const (
	cursorSaveTranscript = 0
	cursorGridStatus     = 1
	cursorLocalShell     = 2
	cursorTmuxConfigFile = 3
	cursorReplaySessions = 4
	cursorPassword       = 5
	bindingCursorBase    = 6
)

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) maxCursor() int {
	if len(m.entries) == 0 {
		return cursorPassword
	}
	return bindingCursorBase - 1 + len(m.entries)
}

func (m *Model) openPasswordOverlay() (tea.Model, tea.Cmd) {
	m.pwd = newPasswordOverlay(m.noPasswordMode, m.width, m.height)
	return m, m.pwd.Init()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if m.pwd != nil {
			m.pwd.SetSize(msg.Width, msg.Height)
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.pwd != nil {
			return m, nil
		}
		if m.state != stateNormal {
			return m, nil
		}
		n := m.totalScrollLines()
		vis := m.visibleRows()
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
		if m.pwd != nil {
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
		if li == cursorPassword {
			return m.openPasswordOverlay()
		}
		if li == cursorLocalShell {
			return m.startShellEdit()
		}
		if li == cursorTmuxConfigFile {
			return m.startTmuxConfigEdit()
		}
		return m, nil

	case tea.PasteMsg:
		if m.state == stateShell {
			m.shellInput = inputpaste.TextInput(m.shellInput, msg)
		}
		if m.state == stateTmuxConfig {
			m.tmuxConfigInput = inputpaste.TextInput(m.tmuxConfigInput, msg)
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.pwd != nil {
			next, cmd := m.pwd.Update(msg)
			m.pwd = next
			return m, cmd
		}
		if m.state == stateShell {
			return m.handleShellEdit(msg)
		}
		if m.state == stateTmuxConfig {
			return m.handleTmuxConfigEdit(msg)
		}
		if m.state == stateCapture || m.state == stateAppend {
			if m.cursor < bindingCursorBase {
				m.state = stateNormal
				return m, nil
			}
			return m.handleCapture(msg)
		}
		return m.handleNormal(msg)
	}
	return m, nil
}

func (m *Model) handleNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.maxCursor() {
			m.cursor++
		}
	case " ":
		if m.cursor == cursorReplaySessions {
			m.replaySessions = !m.replaySessions
			m.modified = true
			return m, nil
		}
		if m.cursor < cursorLocalShell {
			if m.cursor == cursorSaveTranscript {
				m.saveSessionTranscript = !m.saveSessionTranscript
			} else {
				m.gridStatusWords = !m.gridStatusWords
			}
			m.modified = true
			return m, nil
		}
		if m.cursor == cursorPassword {
			return m.openPasswordOverlay()
		}
	case "enter":
		if m.cursor == cursorReplaySessions {
			m.replaySessions = !m.replaySessions
			m.modified = true
			return m, nil
		}
		if m.cursor < cursorLocalShell {
			if m.cursor == cursorSaveTranscript {
				m.saveSessionTranscript = !m.saveSessionTranscript
			} else {
				m.gridStatusWords = !m.gridStatusWords
			}
			m.modified = true
			return m, nil
		}
		if m.cursor == cursorLocalShell {
			return m.startShellEdit()
		}
		if m.cursor == cursorTmuxConfigFile {
			return m.startTmuxConfigEdit()
		}
		if m.cursor == cursorPassword {
			return m.openPasswordOverlay()
		}
		m.state = stateCapture
	case "+", "=":
		if m.cursor >= bindingCursorBase {
			m.state = stateAppend
		}
	case "backspace", "delete":
		if m.cursor < bindingCursorBase {
			return m, nil
		}
		idx := m.cursor - bindingCursorBase
		if len(m.entries[idx].Keys) > 0 {
			m.entries[idx].Keys = nil
			m.modified = true
		}
	case "ctrl+s":
		return m, m.save()
	case "ctrl+r":
		m.entries = buildEntries(m.defaultsJSON)
		m.saveSessionTranscript = true
		m.replaySessions = true
		m.gridStatusWords = false
		m.localTerminalShell = ""
		m.tmuxConfigFile = ""
		m.modified = true
	case "esc":
		return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
	}
	return m, nil
}

func (m *Model) handleCapture(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	idx := m.cursor - bindingCursorBase
	if idx < 0 || idx >= len(m.entries) {
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
		for _, k := range m.entries[idx].Keys {
			if k == ks {
				found = true
				break
			}
		}
		if !found {
			m.entries[idx].Keys = append(m.entries[idx].Keys, ks)
		}
	} else {
		m.entries[idx].Keys = []string{ks}
	}
	m.modified = true
	m.state = stateNormal

	return m, nil
}

func (m *Model) startShellEdit() (tea.Model, tea.Cmd) {
	m.shellInput.SetValue(m.localTerminalShell)
	m.shellInput.SetWidth(max(20, m.width-30))
	m.state = stateShell
	return m, m.shellInput.Focus()
}

func (m *Model) handleShellEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.state = stateNormal
		m.shellInput.Blur()
		return m, nil
	case "enter":
		m.localTerminalShell = strings.TrimSpace(m.shellInput.Value())
		m.modified = true
		m.state = stateNormal
		m.shellInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.shellInput, cmd = m.shellInput.Update(msg)
	return m, cmd
}

func (m *Model) startTmuxConfigEdit() (tea.Model, tea.Cmd) {
	m.tmuxConfigInput.SetValue(m.tmuxConfigFile)
	m.tmuxConfigInput.SetWidth(max(20, m.width-30))
	m.state = stateTmuxConfig
	return m, m.tmuxConfigInput.Focus()
}

func (m *Model) handleTmuxConfigEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.state = stateNormal
		m.tmuxConfigInput.Blur()
		return m, nil
	case "enter":
		m.tmuxConfigFile = strings.TrimSpace(m.tmuxConfigInput.Value())
		m.modified = true
		m.state = stateNormal
		m.tmuxConfigInput.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.tmuxConfigInput, cmd = m.tmuxConfigInput.Update(msg)
	return m, cmd
}

func (m *Model) save() tea.Cmd {
	database := m.db
	configData := m.ConfigJSON()
	saveTr := "true"
	if !m.saveSessionTranscript {
		saveTr = "false"
	}
	gridW := "false"
	if m.gridStatusWords {
		gridW = "true"
	}
	localShell := strings.TrimSpace(m.localTerminalShell)
	captureMode := "transcript"
	if m.replaySessions {
		captureMode = "replay"
	}
	tmuxConfigFile := strings.TrimSpace(m.tmuxConfigFile)
	return func() tea.Msg {
		if err := db.SetSetting(database, "keybindings", string(configData)); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		if err := db.SetSetting(database, "save_session_transcript", saveTr); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		if err := db.SetSetting(database, "session_capture_mode", captureMode); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		if err := db.SetSetting(database, "grid_status_words", gridW); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		if err := db.SetSetting(database, localterm.SettingShell, localShell); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		if err := db.SetSetting(database, "tmux_config_file", tmuxConfigFile); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		return types.SettingsSavedMsg{}
	}
}
