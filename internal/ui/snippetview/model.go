package snippetview

import (
	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type Model struct {
	db         *gorm.DB
	width      int
	height     int
	loaded     bool
	snippets   []db.Snippet
	gridCursor int
	gridLayout components.GridLayout
	vk         viewkeys.SnippetKeys
}

func (m *Model) SetViewKeys(vk viewkeys.SnippetKeys) { m.vk = vk }

func New(database *gorm.DB, vk viewkeys.SnippetKeys) *Model {
	return &Model{db: database, vk: vk}
}

func (m *Model) SetSize(w, h int) {
	if w < 20 {
		w = 80
	}
	m.width = w
	m.height = h
	m.gridLayout = components.ComputeGrid(w, h)
}

func (m Model) SelectedSnippet() *db.Snippet {
	if m.gridCursor < 0 || m.gridCursor >= len(m.snippets) {
		return nil
	}
	return &m.snippets[m.gridCursor]
}

func (m *Model) loadSnippets() tea.Cmd {
	return func() tea.Msg {
		var snippets []db.Snippet
		err := m.db.Order("updated_at DESC, name ASC").Find(&snippets).Error
		return snippetsLoadedMsg{snippets: snippets, err: err}
	}
}

type snippetsLoadedMsg struct {
	snippets []db.Snippet
	err      error
}
