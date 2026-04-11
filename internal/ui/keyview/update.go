package keyview

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keys"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"
)

type keysLoadedMsg struct {
	keys []db.SSHKey
	err  error
}

type keyCreatedMsg struct {
	key *db.SSHKey
	err error
}

type keyDeletedMsg struct {
	err error
}

type keyImportedMsg struct {
	key *db.SSHKey
	err error
}

func isCtrlEnter(msg tea.KeyPressMsg) bool {
	if msg.String() == "ctrl+enter" {
		return true
	}
	k := msg.Key()
	return k.Code == tea.KeyEnter && k.Mod.Contains(tea.ModCtrl)
}

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
			case "esc":
				return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
			case "n":
				m.mode = modeGenerate
				m.step = 0
				m.nameInput.SetValue("")
				cmd := m.nameInput.Focus()
				return m, tea.Batch(cmd, textinput.Blink)
			case "i":
				m.mode = modeImport
				m.step = 0
				m.nameInput.SetValue("")
				cmd := m.nameInput.Focus()
				return m, tea.Batch(cmd, textinput.Blink)
			case "e":
				if k := m.SelectedKey(); k != nil {
					database := m.db
					mk := m.masterKey
					keyID := k.ID
					return m, func() tea.Msg {
						err := keys.ExportKey(database, mk, keyID, "exported_key.pem")
						if err != nil {
							return types.ErrorMsg{Err: err}
						}
						return types.SuccessMsg{Message: "Key exported to exported_key.pem"}
					}
				}
			case "d":
				if k := m.SelectedKey(); k != nil {
					database := m.db
					id := k.ID
					return m, func() tea.Msg {
						err := keys.DeleteKey(database, id)
						return keyDeletedMsg{err: err}
					}
				}
			case "c":
				if k := m.SelectedKey(); k != nil {
					return m, tea.SetClipboard(k.PublicKeyData)
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
					m.keyPaste.SetValue("")
					m.syncKeyPasteSize()
					cmd := m.keyPaste.Focus()
					return m, cmd
				}
				var cmd tea.Cmd
				m.nameInput, cmd = m.nameInput.Update(msg)
				return m, cmd
			}
			if m.step == 1 {
				if isCtrlEnter(msg) {
					raw := m.keyPaste.Value()
					if raw == "" {
						return m, nil
					}
					name := m.nameInput.Value()
					database := m.db
					masterKey := m.masterKey
					return m, func() tea.Msg {
						key, err := keys.ImportKeyFromUserInput(database, masterKey, name, raw, "database")
						return keyImportedMsg{key: key, err: err}
					}
				}
				var cmd tea.Cmd
				m.keyPaste, cmd = m.keyPaste.Update(msg)
				return m, cmd
			}
			return m, nil
		}

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
