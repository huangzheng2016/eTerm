package settingsview

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
)

const (
	cursorSaveTranscript = iota
	cursorReplaySessions
	cursorGridStatus
	cursorLocalShell
	cursorTmuxConfigFile
	cursorShareMaxHours
	cursorPassword
)

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) openPasswordOverlay() (tea.Model, tea.Cmd) {
	m.pwd = newPasswordOverlay(m.noPasswordMode, m.width, m.height)
	return m, m.pwd.Init()
}

func (m *Model) resetToFactory() {
	m.saveSessionTranscript = true
	m.replaySessions = true
	m.gridStatusWords = false
	m.localTerminalShell = ""
	m.tmuxConfigFile = ""
	m.shareMaxHours = "4"
	m.modified = true
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case types.SettingsSavedMsg:
		if msg.Err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.Err} }
		}
		m.modified = false
		return m, func() tea.Msg { return types.RefreshListMsg{} }

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		if m.pwd != nil {
			m.pwd.SetSize(msg.Width, msg.Height)
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.pwd != nil || m.confirmReset.IsActive() {
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
		if m.pwd != nil {
			return m, nil
		}
		if m.confirmReset.IsActive() {
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
		switch li {
		case cursorPassword:
			return m.openPasswordOverlay()
		case cursorLocalShell:
			return m.startShellEdit()
		case cursorTmuxConfigFile:
			return m.startTmuxConfigEdit()
		case cursorShareMaxHours:
			return m.startShareHoursEdit()
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
		if m.confirmReset.IsActive() {
			m.confirmReset, _ = m.confirmReset.Update(msg)
			if !m.confirmReset.IsActive() && m.confirmReset.Result() {
				m.resetToFactory()
			}
			return m, nil
		}
		if m.state == stateShell {
			return m.handleShellEdit(msg)
		}
		if m.state == stateTmuxConfig {
			return m.handleTmuxConfigEdit(msg)
		}
		if m.state == stateShareHours {
			return m.handleShareHoursEdit(msg)
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
		if m.cursor < cursorPassword {
			m.cursor++
		}
	case " ", "enter":
		switch m.cursor {
		case cursorSaveTranscript:
			m.saveSessionTranscript = !m.saveSessionTranscript
			m.modified = true
		case cursorReplaySessions:
			m.replaySessions = !m.replaySessions
			m.modified = true
		case cursorGridStatus:
			m.gridStatusWords = !m.gridStatusWords
			m.modified = true
		case cursorPassword:
			return m.openPasswordOverlay()
		}
		if msg.String() == "enter" {
			switch m.cursor {
			case cursorLocalShell:
				return m.startShellEdit()
			case cursorTmuxConfigFile:
				return m.startTmuxConfigEdit()
			case cursorShareMaxHours:
				return m.startShareHoursEdit()
			}
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

func (m *Model) startShareHoursEdit() (tea.Model, tea.Cmd) {
	m.shareHoursInput.SetValue(m.shareMaxHours)
	m.shareHoursInput.SetWidth(max(20, m.width-30))
	m.shareHoursErr = ""
	m.state = stateShareHours
	return m, m.shareHoursInput.Focus()
}

func (m *Model) handleShareHoursEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.state = stateNormal
		m.shareHoursErr = ""
		m.shareHoursInput.Blur()
		return m, nil
	case "enter":
		n, err := strconv.Atoi(strings.TrimSpace(m.shareHoursInput.Value()))
		if err != nil || n < 1 || n > 168 {
			m.shareHoursErr = "must be a number between 1 and 168"
			return m, nil
		}
		m.shareMaxHours = strconv.Itoa(n)
		m.shareHoursErr = ""
		m.modified = true
		m.state = stateNormal
		m.shareHoursInput.Blur()
		return m, nil
	}
	m.shareHoursErr = ""
	var cmd tea.Cmd
	m.shareHoursInput, cmd = m.shareHoursInput.Update(msg)
	return m, cmd
}

func (m *Model) save() tea.Cmd {
	database := m.db
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
	shareMaxHours := strings.TrimSpace(m.shareMaxHours)
	return func() tea.Msg {
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
		if err := db.SetSetting(database, "share_max_hours", shareMaxHours); err != nil {
			return types.SettingsSavedMsg{Err: err}
		}
		return types.SettingsSavedMsg{}
	}
}
