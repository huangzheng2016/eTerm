package remotemenu

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type tab int

const (
	tabTmux tab = iota
	tabRelay
)

type Model struct {
	Peer      types.RemotePeer
	Hosts     []types.RemoteHost
	sessions  []relay.TmuxSessionInfo
	tab       tab
	cursor    int
	page      int
	tmuxLoad  bool
	tmuxErr   string
	searching bool
	query     string
}

const pageSize = 8

func New(peer types.RemotePeer, hosts []types.RemoteHost) *Model {
	return &Model{Peer: peer, Hosts: hosts}
}

func (m *Model) SetTmuxSessions(s []relay.TmuxSessionInfo) {
	m.sessions = s
	m.tmuxLoad = false
	m.tmuxErr = ""
	if m.tab == tabTmux {
		m.clampTmux()
	}
}

func (m *Model) SetTmuxLoading(loading bool) {
	m.tmuxLoad = loading
	if loading {
		m.tmuxErr = ""
	}
}

func (m *Model) SetTmuxError(err string) {
	m.tmuxErr = strings.TrimSpace(err)
	m.tmuxLoad = false
}

func (m *Model) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.searching {
		switch msg.String() {
		case "esc", "escape":
			m.searching = false
		case "enter":
			m.searching = false
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
			}
			m.clamp()
		default:
			if msg.Text != "" {
				m.query += msg.Text
				m.cursor = 0
				m.page = 0
			}
		}
		return false, nil
	}

	if msg.String() == "tab" {
		if m.tab == tabTmux {
			m.tab = tabRelay
		} else {
			m.tab = tabTmux
		}
		m.cursor = 0
		m.page = 0
		return false, nil
	}

	if msg.Text == "s" {
		share := types.RemoteShareMsg{Peer: m.Peer, Label: m.Peer.Name}
		if m.tab == tabTmux && m.cursor > 0 && m.cursor <= len(m.sessions) {
			session := m.sessions[m.cursor-1]
			share.Target = relay.TargetTmuxAttach
			share.SessionID = session.Name
			share.Label = session.Name
		}
		return true, func() tea.Msg {
			return share
		}
	}

	if m.tab == tabTmux {
		return m.updateTmux(msg)
	}
	return m.updateRelay(msg)
}

func (m *Model) updateTmux(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	max := len(m.sessions)
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.syncTmuxPage()
		}
	case "down", "j":
		if m.cursor < max {
			m.cursor++
			m.syncTmuxPage()
		}
	case "enter":
		peer := m.Peer
		if m.cursor == 0 {
			return true, func() tea.Msg {
				return types.RemoteShellOpenMsg{Peer: peer, Target: relay.TargetTmuxNew, Tmux: true}
			}
		}
		session := m.sessions[m.cursor-1]
		return true, func() tea.Msg {
			return types.RemoteShellOpenMsg{Peer: peer, Target: relay.TargetTmuxAttach, Tmux: true, SessionID: session.Name, HostLabel: session.Name}
		}
	case "d", "delete":
		if m.cursor > 0 {
			session := m.sessions[m.cursor-1]
			return false, func() tea.Msg {
				return types.RemoteTmuxKillRequestMsg{Peer: m.Peer, SessionID: session.Name}
			}
		}
	case "r":
		if m.cursor > 0 {
			session := m.sessions[m.cursor-1]
			return false, func() tea.Msg {
				return types.RemoteTmuxRenameRequestMsg{Peer: m.Peer, SessionID: session.Name, CurrentName: session.Name}
			}
		}
	case "pgup", "left":
		if m.page > 0 {
			m.page--
			m.cursor = m.page*pageSize + 1
		}
	case "pgdown", "right":
		if (m.page+1)*pageSize < max {
			m.page++
			m.cursor = m.page*pageSize + 1
		}
	case "esc", "escape":
		return true, nil
	}
	if msg.Text == "R" {
		m.SetTmuxLoading(true)
		peer := m.Peer
		hosts := m.Hosts
		return false, func() tea.Msg { return types.RemotePeerMenuMsg{Peer: peer, Hosts: hosts} }
	}
	return false, nil
}

func (m *Model) updateRelay(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	hosts := m.filteredHosts()
	max := len(hosts)
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.syncPage()
		}
	case "down", "j":
		if m.cursor < max {
			m.cursor++
			m.syncPage()
		}
	case "pgup", "left":
		if m.page > 0 {
			m.page--
			m.cursor = m.page*pageSize + 1
		}
	case "pgdown", "right":
		if (m.page+1)*pageSize < max {
			m.page++
			m.cursor = m.page*pageSize + 1
		}
	case "/", "ctrl+f":
		m.searching = true
		m.query = ""
		m.cursor = 0
		m.page = 0
	case "enter":
		peer := m.Peer
		if m.cursor == 0 {
			return true, func() tea.Msg {
				return types.RemoteShellOpenMsg{Peer: peer, Target: relay.TargetLocal, HostLabel: "LocalShell"}
			}
		}
		h := hosts[m.cursor-1]
		return true, func() tea.Msg {
			return types.RemoteShellOpenMsg{Peer: peer, Target: relay.TargetHost, HostSyncID: h.SyncID, HostLabel: remoteHostLabel(h)}
		}
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *Model) View() string {
	var rows []string
	rows = append(rows, ui.TitleStyle.Render(m.Peer.Name), m.tabHeader(), "")
	if m.tab == tabTmux {
		rows = append(rows, m.row(0, "+ New session", "start a remote tmux session"))
		if m.tmuxLoad {
			rows = append(rows, ui.DimStyle.Render("Loading tmux sessions..."))
		} else if m.tmuxErr != "" {
			rows = append(rows, ui.DimStyle.Render(m.tmuxErr))
		} else if len(m.sessions) == 0 {
			rows = append(rows, ui.DimStyle.Render("No tmux sessions"))
		} else {
			start := m.page * pageSize
			end := start + pageSize
			if end > len(m.sessions) {
				end = len(m.sessions)
			}
			for i, session := range m.sessions[start:end] {
				rows = append(rows, m.row(start+i+1, tmuxLabel(session), tmuxDesc(session)))
			}
			if len(m.sessions) > pageSize {
				rows = append(rows, "", ui.DimStyle.Render(fmt.Sprintf("page %d/%d", m.page+1, (len(m.sessions)+pageSize-1)/pageSize)))
			}
		}
		rows = append(rows, "", ui.DimStyle.Render("tab switch · up/down navigate · enter open · r rename · d kill · R refresh · s share session/link · esc close"))
	} else {
		if m.searching || m.query != "" {
			prompt := "/" + m.query
			if m.searching {
				prompt += "_"
			}
			rows = append(rows, ui.DimStyle.Render(prompt), "")
		}
		rows = append(rows, m.row(0, "LocalShell", "remote local terminal"))
		hosts := m.filteredHosts()
		start := m.page * pageSize
		end := start + pageSize
		if end > len(hosts) {
			end = len(hosts)
		}
		for i, h := range hosts[start:end] {
			idx := start + i + 1
			rows = append(rows, m.row(idx, remoteHostTitle(h), fmt.Sprintf("%s@%s:%d", h.Username, h.Hostname, h.Port)))
		}
		if len(hosts) > pageSize {
			rows = append(rows, "", ui.DimStyle.Render(fmt.Sprintf("page %d/%d", m.page+1, (len(hosts)+pageSize-1)/pageSize)))
		}
		rows = append(rows, "", ui.DimStyle.Render("tab switch · up/down navigate · / search · pgup/pgdown page · enter open · s share peer shell · esc close"))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(56).
		Render(strings.Join(rows, "\n"))
}

func (m *Model) tabHeader() string {
	tmuxTab, relayTab := "tmux", "Relay"
	if m.tab == tabTmux {
		tmuxTab = ui.SelectedStyle.Render(tmuxTab)
		relayTab = ui.DimStyle.Render(relayTab)
	} else {
		tmuxTab = ui.DimStyle.Render(tmuxTab)
		relayTab = ui.SelectedStyle.Render(relayTab)
	}
	return tmuxTab + "  " + relayTab
}

func tmuxLabel(session relay.TmuxSessionInfo) string {
	return session.Name
}

func tmuxDesc(session relay.TmuxSessionInfo) string {
	d := ""
	if session.CreatedUnix != 0 {
		d = time.Unix(session.CreatedUnix, 0).Format("15:04:05")
	}
	if session.Attached {
		if d != "" {
			d += " "
		}
		d += "attached"
	}
	return d
}

func (m *Model) row(idx int, title, desc string) string {
	cursor := "  "
	style := ui.DimStyle
	if idx == m.cursor {
		cursor = "> "
		style = ui.SelectedStyle
	}
	return cursor + style.Render(title) + " " + ui.DimStyle.Render(desc)
}

func remoteHostLabel(h types.RemoteHost) string {
	if strings.TrimSpace(h.Alias) != "" {
		return h.Alias
	}
	if strings.TrimSpace(h.Hostname) != "" {
		return h.Hostname
	}
	return h.SyncID
}

func remoteHostTitle(h types.RemoteHost) string {
	title := remoteHostLabel(h)
	var tags []string
	if g := strings.TrimSpace(h.Group); g != "" && !strings.EqualFold(g, "Default") {
		tags = append(tags, g)
	}
	for _, tag := range strings.Split(h.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	for _, tag := range tags {
		title += " [" + tag + "]"
	}
	return title
}

func (m *Model) filteredHosts() []types.RemoteHost {
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q == "" {
		return m.Hosts
	}
	var out []types.RemoteHost
	for _, h := range m.Hosts {
		haystack := strings.ToLower(strings.Join([]string{
			remoteHostLabel(h),
			h.Hostname,
			h.Username,
			h.Tags,
			h.Group,
		}, " "))
		if strings.Contains(haystack, q) {
			out = append(out, h)
		}
	}
	return out
}

func (m *Model) clamp() {
	max := len(m.filteredHosts())
	if m.cursor > max {
		m.cursor = max
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.page*pageSize > max {
		m.page = max / pageSize
	}
}

func (m *Model) syncPage() {
	if m.cursor == 0 {
		m.page = 0
		return
	}
	hostIdx := m.cursor - 1
	if hostIdx < m.page*pageSize {
		m.page = hostIdx / pageSize
	}
	if hostIdx >= (m.page+1)*pageSize {
		m.page = hostIdx / pageSize
	}
}

func (m *Model) clampTmux() {
	if m.cursor > len(m.sessions) {
		m.cursor = len(m.sessions)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.syncTmuxPage()
}

func (m *Model) syncTmuxPage() {
	if m.cursor == 0 {
		m.page = 0
		return
	}
	sessionIdx := m.cursor - 1
	if sessionIdx < m.page*pageSize {
		m.page = sessionIdx / pageSize
	}
	if sessionIdx >= (m.page+1)*pageSize {
		m.page = sessionIdx / pageSize
	}
}
