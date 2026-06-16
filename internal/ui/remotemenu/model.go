package remotemenu

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type Model struct {
	Peer      types.RemotePeer
	Hosts     []types.RemoteHost
	cursor    int
	page      int
	searching bool
	query     string
}

const pageSize = 8

func New(peer types.RemotePeer, hosts []types.RemoteHost) *Model {
	return &Model{Peer: peer, Hosts: hosts}
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
				return types.RemoteShellOpenMsg{Peer: peer, Target: "local", HostLabel: "LocalShell"}
			}
		}
		h := hosts[m.cursor-1]
		return true, func() tea.Msg {
			return types.RemoteShellOpenMsg{Peer: peer, Target: "host", HostSyncID: h.SyncID, HostLabel: remoteHostLabel(h)}
		}
	case "esc", "escape":
		return true, nil
	}
	return false, nil
}

func (m *Model) View() string {
	var rows []string
	rows = append(rows, ui.TitleStyle.Render(m.Peer.Name), "")
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
	rows = append(rows, "", ui.DimStyle.Render("up/down navigate · / search · pgup/pgdown page · enter open · esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(56).
		Render(strings.Join(rows, "\n"))
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
