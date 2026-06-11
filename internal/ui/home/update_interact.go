package home

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

// handleGridMouse returns done=true when Update should return immediately.
func (m Model) handleGridMouse(msg tea.MouseClickMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() != list.Unfiltered {
		return m, nil, false
	}
	if msg.Button != tea.MouseLeft && msg.Button != tea.MouseRight {
		return m, nil, false
	}
	hosts := m.gridHosts()
	gl := m.gridLayout
	y := msg.Y
	if m.mode == tagView && m.selectedTag != "" {
		if y == 0 {
			return m, nil, false
		}
		y--
		gridH := m.height - 1
		if gridH < cardOuterH {
			gridH = cardOuterH
		}
		gl = computeGrid(m.width, gridH)
	}
	page := 0
	if gl.PageSize > 0 {
		page = m.gridCursor / gl.PageSize
	}
	globalIdx, ok := gridIndexAtMouse(msg.X, y, len(hosts), gl, page)
	if !ok {
		return m, nil, false
	}
	m.gridCursor = globalIdx

	switch msg.Button {
	case tea.MouseRight:
		if h := m.SelectedHost(); h != nil {
			id := h.ID
			return m, func() tea.Msg {
				return types.SFTPOpenMsg{HostID: id}
			}, true
		}
		return m, nil, true

	case tea.MouseLeft:
		now := time.Now()
		if globalIdx == m.lastClickIdx && now.Sub(m.lastClickAt) < doubleClickWindow {
			m.lastClickAt = time.Time{}
			m.lastClickIdx = -1
			if h := m.SelectedHost(); h != nil {
				return m, func() tea.Msg {
					return types.SSHConnectMsg{HostID: h.ID}
				}, true
			}
			return m, nil, true
		}
		m.lastClickAt = now
		m.lastClickIdx = globalIdx
		return m, nil, true
	}
	return m, nil, false
}

// handleHomeKeyPress returns done=true when Update should return immediately.
func (m Model) handleHomeKeyPress(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	logKeyPress("KeyPress", fmt.Sprint(m.list.FilterState()), len(m.list.Items()), msg)

	if m.mode == tagView && m.selectedTag == "" {
		if m.tagList.FilterState() == list.Filtering {
			return m, nil, false
		}
		switch {
		case key.Matches(msg, m.keys.ToggleView):
			m.mode = groupView
			m.populateHostList(m.allHosts)
			m.SetSize(m.width, m.height)
			return m, nil, true
		case m.kmCfg.MatchConnect(msg) || key.Matches(msg, m.keys.SSHConnect):
			if item := m.tagList.SelectedItem(); item != nil {
				if ti, ok := item.(tagItem); ok {
					m.selectedTag = ti.name
					var filtered []db.Host
					for _, h := range m.allHosts {
						if hostHasTag(h, m.selectedTag) {
							filtered = append(filtered, h)
						}
					}
					m.populateHostList(filtered)
					m.SetSize(m.width, m.height)
					return m, nil, true
				}
			}
			return m, nil, true
		case msg.String() == "esc" || msg.String() == "escape" || msg.String() == "backspace":
			m.mode = groupView
			m.populateHostList(m.allHosts)
			m.SetSize(m.width, m.height)
			return m, nil, true
		}
		var cmd tea.Cmd
		m.tagList, cmd = m.tagList.Update(msg)
		return m, cmd, true
	}

	if m.mode == tagView && m.selectedTag != "" {
		if m.list.FilterState() != list.Filtering {
			switch {
			case key.Matches(msg, m.keys.ToggleView):
				m.mode = groupView
				m.selectedTag = ""
				m.populateHostList(m.allHosts)
				m.SetSize(m.width, m.height)
				return m, nil, true
			case msg.String() == "backspace" || msg.String() == "esc" || msg.String() == "escape":
				m.selectedTag = ""
				m.populateTagList()
				m.SetSize(m.width, m.height)
				return m, nil, true
			}
		}
	}

	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}

	switch msg.String() {
	case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
		dir := msg.String()
		hosts := m.gridHosts()
		gl := m.gridLayout
		if m.mode == tagView && m.selectedTag != "" {
			gridH := m.height - 1
			if gridH < cardOuterH {
				gridH = cardOuterH
			}
			gl = computeGrid(m.width, gridH)
		}
		newCur, changed := gridMove(dir, m.gridCursor, len(hosts), gl)
		if changed {
			m.gridCursor = newCur
		}
		return m, nil, true
	}

	if msg.Key().IsRepeat {
		if m.kmCfg.MatchConnect(msg) || m.kmCfg.MatchSFTP(msg) ||
			key.Matches(msg, m.keys.SSHConnect) || key.Matches(msg, m.keys.SFTPOpen) {
			logKeyRepeatSuppressed(msg)
			return m, nil, false
		}
	}

	switch {
	case key.Matches(msg, m.keys.ToggleView):
		m.mode = tagView
		m.selectedTag = ""
		m.populateTagList()
		m.SetSize(m.width, m.height)
		return m, nil, true

	case m.kmCfg.MatchConnect(msg) || key.Matches(msg, m.keys.SSHConnect):
		logKeyDispatch("SSHConnect")
		if h := m.SelectedHost(); h != nil {
			return m, func() tea.Msg {
				return types.SSHConnectMsg{HostID: h.ID}
			}, true
		}
		return m, func() tea.Msg {
			return types.ErrorMsg{Err: fmt.Errorf("enter: no host (items=%d loaded=%v)", len(m.list.Items()), m.loaded)}
		}, true

	case m.kmCfg.MatchNewHost(msg) || key.Matches(msg, m.keys.NewHost):
		logKeyDispatch("NewHostTab")
		return m, func() tea.Msg {
			return types.NewTabMsg{Type: "editor", Title: "New Host"}
		}, true

	case m.kmCfg.MatchEdit(msg) || key.Matches(msg, m.keys.EditHost):
		logKeyDispatch("EditHostTab")
		if h := m.SelectedHost(); h != nil {
			host := *h
			title := host.Alias
			if title == "" {
				title = host.Hostname
			}
			return m, func() tea.Msg {
				return types.NewTabMsg{Type: "editor", Title: "Edit: " + title, Data: host}
			}, true
		}
		return m, nil, true

	case m.kmCfg.MatchDelete(msg) || key.Matches(msg, m.keys.DeleteHost):
		logKeyDispatch("DeleteHost")
		if h := m.SelectedHost(); h != nil {
			id := h.ID
			alias := h.Alias
			if alias == "" {
				alias = fmt.Sprintf("%s@%s", h.Username, h.Hostname)
			}
			return m, func() tea.Msg {
				return types.HostDeleteRequestMsg{ID: id, Alias: alias}
			}, true
		}
		return m, nil, true

	case m.kmCfg.MatchSFTP(msg) || key.Matches(msg, m.keys.SFTPOpen):
		logKeyDispatch("SFTPOpen")
		if h := m.SelectedHost(); h != nil {
			return m, func() tea.Msg {
				return types.SFTPOpenMsg{HostID: h.ID}
			}, true
		}
		return m, func() tea.Msg {
			return types.ErrorMsg{Err: fmt.Errorf("sftp: no host (items=%d)", len(m.list.Items()))}
		}, true

	case m.kmCfg.MatchCopy(msg) || key.Matches(msg, m.keys.CopySSH):
		logKeyDispatch("CopySSHCmd")
		if h := m.SelectedHost(); h != nil {
			cmd := "ssh "
			if h.ForwardAgent {
				cmd += "-A "
			}
			cmd += fmt.Sprintf("%s@%s -p %d", h.Username, h.Hostname, h.Port)
			return m, tea.SetClipboard(cmd), true
		}
		return m, nil, true

	case key.Matches(msg, m.keys.CloneHost):
		logKeyDispatch("CloneHost")
		if h := m.SelectedHost(); h != nil {
			id := h.ID
			return m, func() tea.Msg { return types.HostCloneMsg{HostID: id} }, true
		}
		return m, nil, true

	case viewkeys.MatchKey(msg, m.showHiddenKeys):
		logKeyDispatch("ToggleHidden")
		m.showHidden = !m.showHidden
		switch m.mode {
		case tagView:
			if m.selectedTag != "" {
				var filtered []db.Host
				for _, h := range m.allHosts {
					if hostHasTag(h, m.selectedTag) {
						filtered = append(filtered, h)
					}
				}
				m.populateHostList(filtered)
			}
		default:
			m.populateHostList(m.allHosts)
		}
		m.SetSize(m.width, m.height)
		toast := "Hidden hosts visible"
		if !m.showHidden {
			toast = "Hidden hosts hidden"
		}
		return m, func() tea.Msg { return types.SuccessMsg{Message: toast} }, true

	case viewkeys.MatchKey(msg, m.hideHostKeys):
		logKeyDispatch("ToggleHostHidden")
		if h := m.SelectedHost(); h != nil {
			id := h.ID
			return m, func() tea.Msg { return types.HostToggleHiddenMsg{HostID: id} }, true
		}
		return m, nil, true

	case viewkeys.MatchKey(msg, m.quickConnectKeys):
		logKeyDispatch("QuickConnect")
		return m, func() tea.Msg { return types.QuickConnectRequestMsg{} }, true

	case viewkeys.MatchKey(msg, m.importSSHKeys):
		logKeyDispatch("ImportSSHConfig")
		return m, func() tea.Msg { return types.ImportSSHConfigPreviewMsg{} }, true

	case viewkeys.MatchKey(msg, m.sessionHistoryKeys):
		logKeyDispatch("SessionHistory")
		if h := m.SelectedHost(); h != nil {
			return m, func() tea.Msg { return types.OpenSessionHistoryMsg{HostID: h.ID} }, true
		}
		return m, nil, true

	case viewkeys.MatchKey(msg, m.toggleSelectKeys):
		if h := m.SelectedHost(); h != nil {
			if m.selectedHosts == nil {
				m.selectedHosts = make(map[uint]struct{})
			}
			if _, ok := m.selectedHosts[h.ID]; ok {
				delete(m.selectedHosts, h.ID)
			} else {
				m.selectedHosts[h.ID] = struct{}{}
			}
		}
		return m, nil, true

	case viewkeys.MatchKey(msg, m.batchTagKeys):
		ids := m.batchHostIDs()
		if len(ids) == 0 {
			return m, nil, true
		}
		return m, func() tea.Msg { return types.BatchTagRequestMsg{HostIDs: ids} }, true

	case viewkeys.MatchKey(msg, m.batchActionKeys):
		ids := m.batchActionHostIDs()
		if len(ids) == 0 {
			return m, nil, true
		}
		return m, func() tea.Msg { return types.BatchActionsRequestMsg{HostIDs: ids} }, true

	case viewkeys.MatchKey(msg, m.exportConfigKeys):
		logKeyDispatch("ExportConfig")
		return m, func() tea.Msg { return types.ExportConfigMsg{} }, true

	case msg.String() == "esc" || msg.String() == "escape":
		logKeyDispatch("EscMenu")
		return m, func() tea.Msg { return types.EscMenuRequestMsg{} }, true
	}
	logKeyNoShortcut(msg)
	return m, nil, false
}
