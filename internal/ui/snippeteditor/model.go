package snippeteditor

import (
	"charm.land/bubbles/v2/textinput"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
)

type Model struct {
	inputs    [2]textinput.Model // 0=name, 1=command
	focused   int
	db        *gorm.DB
	width     int
	height    int
	err       string
	snippetID uint // 0 = new
}

func New(database *gorm.DB, snippet *db.Snippet) Model {
	var inputs [2]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[0].Placeholder = "Snippet name"
	inputs[1].Placeholder = "Command (e.g. docker ps -a)"

	m := Model{inputs: inputs, db: database}
	if snippet != nil && snippet.ID > 0 {
		m.snippetID = snippet.ID
		inputs[0].SetValue(snippet.Name)
		inputs[1].SetValue(snippet.Command)
	}
	inputs[0].Focus()
	m.syncInputWidths()
	return m
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.syncInputWidths()
}

func (m *Model) syncInputWidths() {
	iw := 36
	if m.width > 0 && m.width < 50 {
		iw = max(10, m.width-14)
	}
	for i := range m.inputs {
		m.inputs[i].SetWidth(iw)
	}
}
