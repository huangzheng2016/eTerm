package keyview

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/keys"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/inputpaste"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func (m Model) Init() tea.Cmd {
	return m.loadKeys()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case keysLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.sshKeys = msg.keys
		m.loaded = true
		if m.gridCursor >= len(m.sshKeys) {
			m.gridCursor = 0
		}
		return m, nil

	case keyCreatedMsg:
		if msg.err != nil {
			m.resetMode()
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.resetMode()
		return m, tea.Batch(m.loadKeys(), func() tea.Msg {
			return types.SuccessMsg{Message: "Key generated successfully"}
		})

	case keyImportedMsg:
		if msg.err != nil {
			m.resetMode()
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.resetMode()
		return m, tea.Batch(m.loadKeys(), func() tea.Msg {
			return types.SuccessMsg{Message: "Key imported successfully"}
		})

	case keyDeletedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		return m, m.loadKeys()

	case keyUpdatedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.resetMode()
		return m, tea.Batch(m.loadKeys(), func() tea.Msg { return types.SuccessMsg{Message: "Key updated"} })

	case types.RefreshListMsg:
		return m, m.loadKeys()

	case tea.KeyPressMsg:
		switch m.mode {
		case modeNone:
			// Grid navigation
			switch msg.String() {
			case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
				newCur, changed := components.GridMove(msg.String(), m.gridCursor, len(m.sshKeys), m.gridLayout)
				if changed {
					m.gridCursor = newCur
				}
				return m, nil
			case "enter":
				if k := m.SelectedKey(); k != nil {
					m.activeKeyID = k.ID
					m.mode = modeDetail
				}
				return m, nil
			default:
				switch {
				case viewkeys.MatchKey(msg, m.vk.New):
					m.mode = modeGenerate
					m.step = 0
					m.nameInput.SetValue("")
					cmd := m.nameInput.Focus()
					return m, tea.Batch(cmd, textinput.Blink)
				case viewkeys.MatchKey(msg, m.vk.Import):
					m.mode = modeImport
					m.step = 0
					m.nameInput.SetValue("")
					m.certPathInput.SetValue("")
					cmd := m.nameInput.Focus()
					return m, tea.Batch(cmd, textinput.Blink)
				case viewkeys.MatchKey(msg, m.vk.Edit):
					return m.startEdit()
				case viewkeys.MatchKey(msg, m.vk.Delete):
					if k := m.SelectedKey(); k != nil {
						m.pendingDeleteID = k.ID
						m.pendingDeleteName = k.Name
						m.mode = modeDelete
						return m, nil
					}
				case viewkeys.MatchKey(msg, m.vk.Copy):
					if k := m.SelectedKey(); k != nil {
						return m, tea.SetClipboard(k.PublicKeyData)
					}
				}
			}

		case modeGenerate:
			switch msg.String() {
			case "esc":
				m.resetMode()
				return m, nil
			case "enter":
				if m.step == 0 {
					if m.nameInput.Value() == "" {
						return m, nil
					}
					m.step = 1
					m.nameInput.Blur()
					return m, nil
				}
				if m.step == 1 {
					name := m.nameInput.Value()
					keyType := m.typeOptions[m.typeIdx]
					database := m.db
					masterKey := m.masterKey
					bits := 0
					if keyType == "rsa" {
						bits = 4096
					}
					return m, func() tea.Msg {
						key, err := keys.CreateKey(database, masterKey, name, keyType, bits, "", "database")
						return keyCreatedMsg{key: key, err: err}
					}
				}
			case "left":
				if m.step == 1 {
					m.typeIdx--
					if m.typeIdx < 0 {
						m.typeIdx = len(m.typeOptions) - 1
					}
					return m, nil
				}
			case "right":
				if m.step == 1 {
					m.typeIdx = (m.typeIdx + 1) % len(m.typeOptions)
					return m, nil
				}
			}
			if m.step == 0 {
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
			return m, nil

		case modeImport:
			switch msg.String() {
			case "esc":
				m.resetMode()
				return m, nil
			}
			if m.step == 0 {
				switch msg.String() {
				case "enter":
					if m.nameInput.Value() == "" {
						return m, nil
					}
					m.step = 1
					m.nameInput.Blur()
					m.certPathInput.SetValue("")
					cmd := m.certPathInput.Focus()
					return m, cmd
				}
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
			if m.step == 1 {
				switch msg.String() {
				case "enter":
					m.step = 2
					m.certPathInput.Blur()
					m.keyPaste.SetValue("")
					m.syncKeyPasteSize()
					cmd := m.keyPaste.Focus()
					return m, cmd
				}
				var cmd tea.Cmd
				m.certPathInput, cmd = m.certPathInput.Update(msg)
				return m, cmd
			}
			if m.step == 2 {
				if isCtrlEnter(msg) {
					raw := m.keyPaste.Value()
					if raw == "" {
						return m, nil
					}
					name := m.nameInput.Value()
					certPath := m.certPathInput.Value()
					database := m.db
					masterKey := m.masterKey
					return m, func() tea.Msg {
						key, err := keys.ImportKeyFromUserInput(database, masterKey, name, raw, certPath, "database")
						return keyImportedMsg{key: key, err: err}
					}
				}
				var cmd tea.Cmd
				m.keyPaste, cmd = m.keyPaste.Update(msg)
				return m, cmd
			}
			return m, nil

		case modeDelete:
			switch msg.String() {
			case "y":
				database := m.db
				id := m.pendingDeleteID
				m.resetMode()
				return m, func() tea.Msg {
					err := keys.DeleteKey(database, id)
					return keyDeletedMsg{err: err}
				}
			case "n", "esc":
				m.resetMode()
				return m, nil
			}

		case modeDetail:
			switch msg.String() {
			case "esc":
				m.resetMode()
				return m, nil
			case "c":
				if k := m.keyByID(m.activeKeyID); k != nil {
					return m, tea.SetClipboard(k.PublicKeyData)
				}
			case "e":
				return m.startEdit()
			}

		case modeEdit:
			switch msg.String() {
			case "esc":
				m.resetMode()
				return m, nil
			case "enter":
				if m.step == 0 {
					if m.nameInput.Value() == "" {
						return m, nil
					}
					m.step = 1
					m.nameInput.Blur()
					return m, m.certPathInput.Focus()
				}
				database, id := m.db, m.activeKeyID
				name, certPath := m.nameInput.Value(), m.certPathInput.Value()
				return m, func() tea.Msg {
					err := database.Model(&db.SSHKey{}).Where("id = ?", id).Updates(map[string]interface{}{"name": name, "certificate_path": certPath}).Error
					return keyUpdatedMsg{err: err}
				}
			}
			var cmd tea.Cmd
			if m.step == 0 {
				m.nameInput, cmd = m.nameInput.Update(msg)
			} else {
				m.certPathInput, cmd = m.certPathInput.Update(msg)
			}
			return m, cmd
		}

	case tea.PasteMsg:
		switch m.mode {
		case modeGenerate:
			if m.step == 0 {
				m.nameInput = inputpaste.TextInput(m.nameInput, msg)
			}
		case modeImport:
			switch m.step {
			case 0:
				m.nameInput = inputpaste.TextInput(m.nameInput, msg)
			case 1:
				m.certPathInput = inputpaste.TextInput(m.certPathInput, msg)
			case 2:
				m.keyPaste = inputpaste.TextArea(m.keyPaste, msg)
			}
		case modeEdit:
			if m.step == 0 {
				m.nameInput = inputpaste.TextInput(m.nameInput, msg)
			} else {
				m.certPathInput = inputpaste.TextInput(m.certPathInput, msg)
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.mode == modeNone && msg.Button == tea.MouseLeft {
			page := 0
			if m.gridLayout.PageSize > 0 {
				page = m.gridCursor / m.gridLayout.PageSize
			}
			idx, ok := components.GridIndexAtMouse(msg.X, msg.Y, len(m.sshKeys), m.gridLayout, page)
			if ok {
				m.gridCursor = idx
			}
			return m, nil
		}
	}

	return m, nil
}

type keyUpdatedMsg struct{ err error }

func (m Model) keyByID(id uint) *db.SSHKey {
	for i := range m.sshKeys {
		if m.sshKeys[i].ID == id {
			return &m.sshKeys[i]
		}
	}
	return nil
}

func (m Model) startEdit() (tea.Model, tea.Cmd) {
	k := m.SelectedKey()
	if m.mode == modeDetail {
		k = m.keyByID(m.activeKeyID)
	}
	if k == nil {
		return m, nil
	}
	m.activeKeyID = k.ID
	m.nameInput.SetValue(k.Name)
	m.certPathInput.SetValue(k.CertificatePath)
	m.step = 0
	m.mode = modeEdit
	return m, m.nameInput.Focus()
}
