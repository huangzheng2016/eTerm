package sessionhistview

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

type Model struct {
	db            *gorm.DB
	hostID        uint
	hostTitle     string
	rows          []db.ConnectionHistory
	sel           int
	scroll        int
	focusList     bool
	width         int
	height        int
	loaded        bool
	showEmpty     bool
	showEmptyKeys []string
	selection     textselection.Selection
	contentSeq    int
}

const contentDebounceDelay = 150 * time.Millisecond

type contentTickMsg struct{ seq int }

type contentLoadedMsg struct {
	id         uint
	transcript string
	ansi       string
	err        error
}

func (m *Model) scheduleContentLoad() tea.Cmd {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return nil
	}
	if !m.rows[m.sel].NeedsContentLoad() {
		return nil
	}
	m.contentSeq++
	seq := m.contentSeq
	return tea.Tick(contentDebounceDelay, func(time.Time) tea.Msg { return contentTickMsg{seq: seq} })
}

func New(database *gorm.DB, hostID uint) *Model {
	return &Model{db: database, hostID: hostID, focusList: true, showEmptyKeys: []string{"h"}}
}

func (m *Model) SetShowEmptyKeys(keys []string) { m.showEmptyKeys = keys }

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
		q := m.db.Where("host_id = ?", m.hostID).Select(db.HistoryMetaColumns)
		if !m.showEmpty {
			q = q.Where(db.HistoryNonEmptyFilter)
		}
		err := q.Order("connected_at DESC").Limit(db.HistoryListLimit).Find(&rows).Error
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

func (m *Model) selectedDisplayTranscript() string {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return ""
	}
	if m.rows[m.sel].ANSITranscript != "" {
		return m.rows[m.sel].ANSITranscript
	}
	return m.rows[m.sel].Transcript
}

func (m *Model) transcriptPageSize() int {
	return max(3, m.height-2)
}

func (m *Model) listPageRange() (int, int) {
	pageSize := max(1, m.height-4)
	start := m.sel / pageSize * pageSize
	return start, min(len(m.rows), start+pageSize)
}
