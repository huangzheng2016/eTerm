package home

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
)

// Re-export constants for local use.
const cardOuterH = components.CardOuterH

// gridLayout is a local alias.
type gridLayout = components.GridLayout

func computeGrid(width, height int) gridLayout {
	return components.ComputeGrid(width, height)
}

func gridMove(dir string, cursor, total int, gl gridLayout) (int, bool) {
	return components.GridMove(dir, cursor, total, gl)
}

func gridIndexAtMouse(x, y, total int, gl gridLayout, page int) (int, bool) {
	return components.GridIndexAtMouse(x, y, total, gl, page)
}

// Status dot styles
var (
	statusDotOnline  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00cc00")).Render("●")
	statusDotOffline = lipgloss.NewStyle().Foreground(lipgloss.Color("#cc0000")).Render("●")
	statusDotUnknown = lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("●")
)

func statusDot(s HostStatus) string {
	switch s {
	case StatusOnline:
		return statusDotOnline
	case StatusOffline:
		return statusDotOffline
	default:
		return statusDotUnknown
	}
}

func statusWord(s HostStatus) string {
	switch s {
	case StatusOnline:
		return "ON"
	case StatusOffline:
		return "OFF"
	default:
		return "?"
	}
}

// cardTitle returns the first line of a host card.
func cardTitle(h db.Host, status HostStatus, selected bool, showStatusWords bool) string {
	selMark := " "
	if selected {
		selMark = "*"
	}
	var prefix string
	if showStatusWords {
		prefix = selMark + statusDot(status) + lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(" "+statusWord(status)+" ") + groupPrefix(h.Group)
	} else {
		prefix = selMark + statusDot(status) + " " + groupPrefix(h.Group)
	}
	name := h.Alias
	if name == "" {
		name = h.Hostname
	}
	return prefix + name
}

func peerCardTitle(p types.RemotePeer, selected bool) string {
	selMark := " "
	if selected {
		selMark = "*"
	}
	return selMark + statusDotOnline + " [Daemon] " + p.Name
}

func peerCardDesc(p types.RemotePeer) string {
	return "remote eterm"
}

// cardDesc returns the second line of a host card.
func cardDesc(h db.Host) string {
	return fmt.Sprintf("%s@%s:%d", h.Username, h.Hostname, h.Port)
}

// renderGrid renders the grid of host cards for the current page.
func renderGrid(hosts []db.Host, cursor int, gl gridLayout, width int, hostStatus map[uint]HostStatus, selected map[uint]struct{}, showStatusWords bool) string {
	total := len(hosts)
	if total == 0 {
		return ""
	}
	cards := make([]string, total)
	start, end := components.GridPageRange(total, cursor, gl)
	for i := start; i < end; i++ {
		h := hosts[i]
		status := StatusUnknown
		if hostStatus != nil {
			if s, ok := hostStatus[h.ID]; ok {
				status = s
			}
		}
		sel := false
		if selected != nil {
			_, sel = selected[h.ID]
		}
		cards[i] = components.RenderCard(cardTitle(h, status, sel, showStatusWords), cardDesc(h), i == cursor, gl.CardW)
	}
	return components.RenderGridRows(cards, total, cursor, gl)
}

func renderGridEntries(entries []gridEntry, cursor int, gl gridLayout, width int, hostStatus map[uint]HostStatus, selected map[uint]struct{}, showStatusWords bool) string {
	total := len(entries)
	if total == 0 {
		return ""
	}
	cards := make([]string, total)
	start, end := components.GridPageRange(total, cursor, gl)
	for i := start; i < end; i++ {
		entry := entries[i]
		if entry.peer != nil {
			cards[i] = components.RenderCard(peerCardTitle(*entry.peer, false), peerCardDesc(*entry.peer), i == cursor, gl.CardW)
			continue
		}
		h := *entry.host
		status := StatusUnknown
		if hostStatus != nil {
			if s, ok := hostStatus[h.ID]; ok {
				status = s
			}
		}
		sel := false
		if selected != nil {
			_, sel = selected[h.ID]
		}
		cards[i] = components.RenderCard(cardTitle(h, status, sel, showStatusWords), cardDesc(h), i == cursor, gl.CardW)
	}
	return components.RenderGridRows(cards, total, cursor, gl)
}
