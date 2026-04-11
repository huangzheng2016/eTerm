package keyview

import (
	"fmt"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keys"
	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/ui/components"
)

type inputMode int

const (
	modeNone     inputMode = 0
	modeGenerate inputMode = 1
	modeImport   inputMode = 2
)

type keyItem struct {
	key db.SSHKey
}

func (i keyItem) FilterValue() string {
	return i.key.Name
}

func (i keyItem) Title() string {
	return i.key.Name
}

func (i keyItem) Description() string {
	return fmt.Sprintf("%s | %s | %s", i.key.Type, i.key.StorageMode, i.key.Fingerprint)
}

type Model struct {
	db          *gorm.DB
	masterKey   *security.MasterKeyManager
	width       int
	height      int
	loaded      bool
	mode        inputMode
	nameInput   textinput.Model
	keyPaste    textarea.Model
	typeOptions []string
	typeIdx     int
	step        int
	sshKeys     []db.SSHKey
	gridCursor  int
	gridLayout  components.GridLayout
}

func New(database *gorm.DB, masterKey *security.MasterKeyManager) Model {
	ni := textinput.New()
	ni.Placeholder = "Key name"

	kp := textarea.New()
	kp.ShowLineNumbers = false
	kp.Placeholder = "Paste private key (PEM), or one line: /path or ~/path"

	return Model{
		db:          database,
		masterKey:   masterKey,
		nameInput:   ni,
		keyPaste:    kp,
		typeOptions: []string{"ed25519", "rsa"},
		typeIdx:     0,
		step:        0,
		mode:        modeNone,
	}
}

func (m *Model) SetSize(w, h int) {
	if w < 20 {
		w = 80
	}
	m.width = w
	m.height = h
	m.gridLayout = components.ComputeGrid(w, h)
	m.syncKeyPasteSize()
	m.nameInput.SetWidth(m.overlayFieldWidth())
}

func (m *Model) syncKeyPasteSize() {
	tw := m.overlayFieldWidth()
	m.keyPaste.SetWidth(tw)
	m.keyPaste.SetHeight(8)
}

func (m *Model) overlayFieldWidth() int {
	tw := m.width - 10
	if tw < 24 {
		tw = 24
	}
	if tw > 72 {
		tw = 72
	}
	return tw
}

func (m Model) SelectedKey() *db.SSHKey {
	if m.gridCursor < 0 || m.gridCursor >= len(m.sshKeys) {
		return nil
	}
	return &m.sshKeys[m.gridCursor]
}

func (m Model) loadKeys() tea.Cmd {
	return func() tea.Msg {
		keysList, err := keys.ListKeys(m.db)
		return keysLoadedMsg{keys: keysList, err: err}
	}
}

func (m *Model) resetMode() {
	m.mode = modeNone
	m.step = 0
	m.typeIdx = 0
	m.nameInput.SetValue("")
	m.nameInput.Blur()
	m.keyPaste.SetValue("")
	m.keyPaste.Blur()
}
