package home

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

const doubleClickWindow = 450 * time.Millisecond

func (m Model) Init() tea.Cmd {
	return m.loadHosts()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case hostsLoadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg {
				return types.ErrorMsg{Err: msg.err}
			}
		}
		m.allHosts = msg.hosts
		m.remotePeers = msg.remotePeers
		m.remoteHosts = msg.remoteHosts
		m.allTags = collectAllTags(msg.hosts)
		m.loaded = true
		m.gridStatusWords = readGridStatusWords(m.db)

		switch m.mode {
		case tagView:
			if m.selectedTag != "" {
				// Re-filter with current tag
				var filtered []db.Host
				for _, h := range m.allHosts {
					if hostHasTag(h, m.selectedTag) {
						filtered = append(filtered, h)
					}
				}
				m.populateHostList(filtered)
			} else {
				m.populateTagList()
			}
		default:
			m.populateHostList(msg.hosts)
		}
		m.SetSize(m.width, m.height)
		// Trigger async host status probing
		return m, probeHosts(msg.hosts)

	case probeResultMsg:
		if m.hostStatus == nil {
			m.hostStatus = make(map[uint]HostStatus)
		}
		m.hostStatus[msg.hostID] = msg.status
		return m, readProbe(msg.ch)

	case types.RefreshListMsg:
		return m, m.loadHosts()

	case types.HostDeletedMsg:
		return m, m.loadHosts()

	case types.HostSavedMsg:
		return m, m.loadHosts()

	case tea.MouseClickMsg:
		if m2, c, done := m.handleGridMouse(msg); done {
			return m2, c
		}

	case tea.KeyPressMsg:
		if m2, c, done := m.handleHomeKeyPress(msg); done {
			return m2, c
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}
