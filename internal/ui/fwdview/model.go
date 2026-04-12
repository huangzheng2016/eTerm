package fwdview

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/ui/components"
	"github.com/eterm/eterm/internal/viewkeys"
)

type Model struct {
	db         *gorm.DB
	width      int
	height     int
	loaded     bool
	rules      []db.PortForward
	running    map[uint]bool
	gridCursor int
	gridLayout components.GridLayout
	vk         viewkeys.FwdKeys
}

func (m *Model) SetViewKeys(vk viewkeys.FwdKeys) { m.vk = vk }

func New(database *gorm.DB, vk viewkeys.FwdKeys) Model {
	return Model{
		db:      database,
		running: make(map[uint]bool),
		vk:      vk,
	}
}

func (m *Model) SetSize(w, h int) {
	if w < 20 {
		w = 80
	}
	m.width = w
	m.height = h
	m.gridLayout = components.ComputeGrid(w, h)
}

func (m Model) SelectedRule() *db.PortForward {
	if m.gridCursor < 0 || m.gridCursor >= len(m.rules) {
		return nil
	}
	return &m.rules[m.gridCursor]
}

func (m Model) loadRules() tea.Cmd {
	return func() tea.Msg {
		var rules []db.PortForward
		err := m.db.Preload("Host").Order("updated_at DESC, id ASC").Find(&rules).Error
		return forwardsLoadedMsg{rules: rules, err: err}
	}
}

type forwardsLoadedMsg struct {
	rules []db.PortForward
	err   error
}

func hostAlias(h db.Host) string {
	if h.Alias != "" {
		return h.Alias
	}
	if h.Username != "" {
		return h.Username + "@" + h.Hostname
	}
	return h.Hostname
}

func ruleCardTitle(r db.PortForward) string {
	switch r.Direction {
	case "dynamic":
		return fmt.Sprintf("D :%d (SOCKS5)", r.LocalPort)
	case "remote":
		return fmt.Sprintf("R :%d→%s:%d", r.RemotePort, r.RemoteHost, r.LocalPort)
	default:
		return fmt.Sprintf("L :%d→%s:%d", r.LocalPort, r.RemoteHost, r.RemotePort)
	}
}

func ruleCardDesc(r db.PortForward, running bool) string {
	st := "○"
	if running {
		st = "●"
	}
	alias := ""
	if r.Host.ID != 0 {
		alias = hostAlias(r.Host)
	}
	return fmt.Sprintf("%s %s", st, alias)
}
