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
	Peer   types.RemotePeer
	Hosts  []types.RemoteHost
	cursor int
}

func New(peer types.RemotePeer, hosts []types.RemoteHost) *Model {
	return &Model{Peer: peer, Hosts: hosts}
}

func (m *Model) Update(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	max := len(m.Hosts)
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < max {
			m.cursor++
		}
	case "enter":
		peer := m.Peer
		if m.cursor == 0 {
			return true, func() tea.Msg {
				return types.RemoteShellOpenMsg{Peer: peer, Target: "local", HostLabel: "LocalShell"}
			}
		}
		h := m.Hosts[m.cursor-1]
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
	rows = append(rows, ui.TitleStyle.Render("[R]"+m.Peer.Name), "")
	rows = append(rows, m.row(0, "LocalShell", "remote local terminal"))
	for i, h := range m.Hosts {
		rows = append(rows, m.row(i+1, remoteHostLabel(h), fmt.Sprintf("%s@%s:%d", h.Username, h.Hostname, h.Port)))
	}
	rows = append(rows, "", ui.DimStyle.Render("up/down navigate · enter open · esc close"))
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
