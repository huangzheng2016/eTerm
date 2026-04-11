package home

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/keymatch"
	"github.com/eterm/eterm/internal/types"
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
		m.allTags = collectAllTags(msg.hosts)
		m.loaded = true

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
		if m.list.FilterState() == list.Filtering {
			break
		}
		if msg.Button != tea.MouseLeft && msg.Button != tea.MouseRight {
			break
		}
		// Grid mouse mapping
		hosts := m.gridHosts()
		gl := m.gridLayout
		if m.mode == tagView && m.selectedTag != "" {
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
		globalIdx, ok := gridIndexAtMouse(msg.X, msg.Y, len(hosts), gl, page)
		if !ok {
			break
		}
		m.gridCursor = globalIdx

		switch msg.Button {
		case tea.MouseRight:
			if h := m.SelectedHost(); h != nil {
				id := h.ID
				return m, func() tea.Msg {
					return types.SFTPOpenMsg{HostID: id}
				}
			}
			return m, nil

		case tea.MouseLeft:
			now := time.Now()
			if globalIdx == m.lastClickIdx && now.Sub(m.lastClickAt) < doubleClickWindow {
				m.lastClickAt = time.Time{}
				m.lastClickIdx = -1
				if h := m.SelectedHost(); h != nil {
					return m, func() tea.Msg {
						return types.SSHConnectMsg{HostID: h.ID}
					}
				}
				return m, nil
			}
			m.lastClickAt = now
			m.lastClickIdx = globalIdx
			return m, nil
		}

	case tea.KeyPressMsg:
		logKeyPress("KeyPress", fmt.Sprint(m.list.FilterState()), len(m.list.Items()), msg)

		// Tag view: browsing tag list (no tag selected yet)
		if m.mode == tagView && m.selectedTag == "" {
			if m.tagList.FilterState() == list.Filtering {
				break
			}
			switch {
			case key.Matches(msg, m.keys.ToggleView):
				m.mode = groupView
				m.populateHostList(m.allHosts)
				m.SetSize(m.width, m.height)
				return m, nil
			case keymatch.MatchConnect(msg) || key.Matches(msg, m.keys.SSHConnect):
				// Select a tag
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
						return m, nil
					}
				}
				return m, nil
			case msg.String() == "esc" || msg.String() == "escape" || msg.String() == "backspace":
				m.mode = groupView
				m.populateHostList(m.allHosts)
				m.SetSize(m.width, m.height)
				return m, nil
			}
			// Forward to tag list for navigation
			var cmd tea.Cmd
			m.tagList, cmd = m.tagList.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Tag view: viewing filtered hosts — backspace goes back to tag list
		if m.mode == tagView && m.selectedTag != "" {
			if m.list.FilterState() != list.Filtering {
				switch {
				case key.Matches(msg, m.keys.ToggleView):
					m.mode = groupView
					m.selectedTag = ""
					m.populateHostList(m.allHosts)
					m.SetSize(m.width, m.height)
					return m, nil
				case msg.String() == "backspace" || msg.String() == "esc" || msg.String() == "escape":
					m.selectedTag = ""
					m.populateTagList()
					m.SetSize(m.width, m.height)
					return m, nil
				}
			}
		}

		// Group view (default) or tag view with selected tag — normal host list handling
		if m.list.FilterState() == list.Filtering {
			break
		}

		// Grid navigation — intercept arrow keys
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
			return m, nil
		}

		// Key-repeat (held Enter / s) can enqueue multiple connect messages before tea.Exec runs.
		if msg.Key().IsRepeat {
			if keymatch.MatchConnect(msg) || keymatch.MatchSFTP(msg) ||
				key.Matches(msg, m.keys.SSHConnect) || key.Matches(msg, m.keys.SFTPOpen) {
				logKeyRepeatSuppressed(msg)
				break
			}
		}

		// Dual strategy: keymatch (Keystroke / uv) OR bubbles key.Matches (Key.String() vs binding).
		// Real terminals differ; one path often works when the other does not.
		switch {
		case key.Matches(msg, m.keys.ToggleView):
			m.mode = tagView
			m.selectedTag = ""
			m.populateTagList()
			m.SetSize(m.width, m.height)
			return m, nil

		case keymatch.MatchConnect(msg) || key.Matches(msg, m.keys.SSHConnect):
			logKeyDispatch("SSHConnect")
			if h := m.SelectedHost(); h != nil {
				return m, func() tea.Msg {
					return types.SSHConnectMsg{HostID: h.ID}
				}
			}
			return m, func() tea.Msg {
				return types.ErrorMsg{Err: fmt.Errorf("enter: no host (items=%d loaded=%v)", len(m.list.Items()), m.loaded)}
			}

		case keymatch.MatchNewHost(msg) || key.Matches(msg, m.keys.NewHost):
			logKeyDispatch("NewHostTab")
			return m, func() tea.Msg {
				return types.NewTabMsg{Type: "editor", Title: "New Host"}
			}

		case keymatch.MatchEdit(msg) || key.Matches(msg, m.keys.EditHost):
			logKeyDispatch("EditHostTab")
			if h := m.SelectedHost(); h != nil {
				host := *h
				title := host.Alias
				if title == "" {
					title = host.Hostname
				}
				return m, func() tea.Msg {
					return types.NewTabMsg{Type: "editor", Title: "Edit: " + title, Data: host}
				}
			}
			return m, nil

		case keymatch.MatchDelete(msg) || key.Matches(msg, m.keys.DeleteHost):
			logKeyDispatch("DeleteHost")
			if h := m.SelectedHost(); h != nil {
				id := h.ID
				alias := h.Alias
				if alias == "" {
					alias = fmt.Sprintf("%s@%s", h.Username, h.Hostname)
				}
				return m, func() tea.Msg {
					return types.HostDeleteRequestMsg{ID: id, Alias: alias}
				}
			}
			return m, nil

		case keymatch.MatchSFTP(msg) || key.Matches(msg, m.keys.SFTPOpen):
			logKeyDispatch("SFTPOpen")
			if h := m.SelectedHost(); h != nil {
				return m, func() tea.Msg {
					return types.SFTPOpenMsg{HostID: h.ID}
				}
			}
			return m, func() tea.Msg {
				return types.ErrorMsg{Err: fmt.Errorf("sftp: no host (items=%d)", len(m.list.Items()))}
			}

		case keymatch.MatchCopy(msg) || key.Matches(msg, m.keys.CopySSH):
			logKeyDispatch("CopySSHCmd")
			if h := m.SelectedHost(); h != nil {
				cmd := fmt.Sprintf("ssh %s@%s -p %d", h.Username, h.Hostname, h.Port)
				return m, tea.SetClipboard(cmd)
			}
			return m, nil

		case key.Matches(msg, m.keys.CloneHost):
			logKeyDispatch("CloneHost")
			if h := m.SelectedHost(); h != nil {
				id := h.ID
				return m, func() tea.Msg { return types.HostCloneMsg{HostID: id} }
			}
			return m, nil

		case msg.String() == "H":
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
			msg := "Hidden hosts visible"
			if !m.showHidden {
				msg = "Hidden hosts hidden"
			}
			return m, func() tea.Msg { return types.SuccessMsg{Message: msg} }

		case msg.String() == "h":
			logKeyDispatch("ToggleHostHidden")
			if h := m.SelectedHost(); h != nil {
				id := h.ID
				return m, func() tea.Msg { return types.HostToggleHiddenMsg{HostID: id} }
			}
			return m, nil

		case msg.String() == "q":
			logKeyDispatch("QuickConnect")
			return m, func() tea.Msg { return types.QuickConnectRequestMsg{} }

		case msg.String() == "I":
			logKeyDispatch("ImportSSHConfig")
			return m, func() tea.Msg { return types.ImportSSHConfigMsg{} }

		case msg.String() == "E":
			logKeyDispatch("ExportConfig")
			return m, func() tea.Msg { return types.ExportConfigMsg{} }

		case msg.String() == "esc" || msg.String() == "escape":
			logKeyDispatch("QuitRequest")
			return m, func() tea.Msg { return types.QuitRequestMsg{} }
		}
		logKeyNoShortcut(msg)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}
