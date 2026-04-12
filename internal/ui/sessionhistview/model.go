package sessionhistview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/types"
)

type Model struct {
	db        *gorm.DB
	hostID    uint
	hostTitle string
	rows      []db.ConnectionHistory
	sel       int
	scroll    int
	focusList bool
	width     int
	height    int
	loaded    bool
}

func New(database *gorm.DB, hostID uint) *Model {
	return &Model{db: database, hostID: hostID, focusList: true}
}

func (m *Model) Init() tea.Cmd {
	return m.reload()
}

func (m *Model) reload() tea.Cmd {
	return func() tea.Msg {
		var host db.Host
		if err := m.db.First(&host, m.hostID).Error; err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("host: %w", err)}
		}
		title := host.Alias
		if title == "" {
			title = fmt.Sprintf("%s@%s", host.Username, host.Hostname)
		}
		var rows []db.ConnectionHistory
		err := m.db.Where("host_id = ?", m.hostID).Order("connected_at DESC").Find(&rows).Error
		if err != nil {
			return types.ErrorMsg{Err: err}
		}
		return loadedMsg{hostTitle: title, rows: rows}
	}
}

type loadedMsg struct {
	hostTitle string
	rows      []db.ConnectionHistory
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *Model) selectedTranscript() string {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return ""
	}
	return m.rows[m.sel].Transcript
}
