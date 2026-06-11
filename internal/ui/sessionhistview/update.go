package sessionhistview

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case loadedMsg:
		m.hostTitle = msg.hostTitle
		m.rows = msg.rows
		m.loaded = true
		m.sel = 0
		m.scroll = 0
		return m, nil

	case types.RefreshListMsg:
		return m, m.reload()

	case tea.MouseClickMsg:
		if !m.loaded || len(m.rows) == 0 || msg.Button != tea.MouseLeft {
			return m, nil
		}
		listW, _, stacked := m.layoutWidths()
		if msg.Y < 2 {
			return m, nil
		}
		row := msg.Y - 2
		if stacked || msg.X < listW {
			if row >= 0 && row < len(m.rows) {
				m.sel = row
				m.scroll = 0
				m.focusList = true
			}
			return m, nil
		}
		m.focusList = false
		return m, nil

	case tea.MouseWheelMsg:
		if !m.loaded || len(m.rows) == 0 {
			return m, nil
		}
		listW, _, stacked := m.layoutWidths()
		if !stacked {
			m.focusList = msg.X < listW
		}
		delta := 0
		switch msg.Button {
		case tea.MouseWheelUp:
			delta = -1
		case tea.MouseWheelDown:
			delta = 1
		default:
			return m, nil
		}
		if m.focusList {
			next := m.sel + delta
			if next < 0 {
				next = 0
			}
			if next >= len(m.rows) {
				next = len(m.rows) - 1
			}
			if next != m.sel {
				m.sel = next
				m.scroll = 0
			}
			return m, nil
		}
		m.scroll += delta
		clampTranscriptScroll(m)
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			return m, func() tea.Msg { return types.CloseTabMsg{Index: -1} }
		case "tab":
			m.focusList = !m.focusList
			return m, nil
		case "up", "k":
			if m.focusList && m.sel > 0 {
				m.sel--
				m.scroll = 0
			} else if !m.focusList {
				m.scroll--
				if m.scroll < 0 {
					m.scroll = 0
				}
			}
			clampTranscriptScroll(m)
			return m, nil
		case "down", "j":
			if m.focusList && m.sel < len(m.rows)-1 {
				m.sel++
				m.scroll = 0
			} else if !m.focusList {
				m.scroll++
			}
			clampTranscriptScroll(m)
			return m, nil
		case "pgup":
			if !m.focusList {
				m.scroll -= 10
				if m.scroll < 0 {
					m.scroll = 0
				}
			}
			clampTranscriptScroll(m)
			return m, nil
		case "pgdown":
			if !m.focusList {
				m.scroll += 10
			}
			clampTranscriptScroll(m)
			return m, nil
		case "home", "g":
			if !m.focusList {
				m.scroll = 0
			}
			return m, nil
		case "end", "G":
			if !m.focusList {
				n := len(strings.Split(m.selectedTranscript(), "\n"))
				if n > 0 {
					m.scroll = n
				}
			}
			clampTranscriptScroll(m)
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) layoutWidths() (int, int, bool) {
	listW := m.width / 3
	if listW < 24 {
		listW = 24
	}
	if listW > m.width-20 {
		listW = m.width / 2
	}
	transW := m.width - listW - 2
	if transW < 20 {
		transW = m.width - 2
		listW = m.width
	}
	return listW, transW, listW >= m.width-2
}

func clampTranscriptScroll(m *Model) {
	lines := strings.Split(m.selectedTranscript(), "\n")
	maxS := 0
	if len(lines) > 0 {
		maxS = len(lines) - 1
	}
	if m.scroll > maxS {
		m.scroll = maxS
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func formatRowMeta(r db.ConnectionHistory) string {
	start := r.ConnectedAt.Format("2006-01-02 15:04")
	if r.DisconnectedAt != nil {
		end := r.DisconnectedAt.Format("15:04:05")
		return start + " - " + end + "  " + r.Status
	}
	return start + "  (open)  " + r.Status
}
