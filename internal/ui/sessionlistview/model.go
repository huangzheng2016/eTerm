package sessionlistview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
	"gorm.io/gorm"
)

type Model struct {
	db            *gorm.DB
	width         int
	height        int
	loaded        bool
	rows          []db.ConnectionHistory
	cursor        int
	grid          components.GridLayout
	search        textinput.Model
	searching     bool
	detail        bool
	detailScroll  int
	showEmpty     bool
	showEmptyKeys []string
	selection     textselection.Selection
}

type loadedMsg struct {
	rows []db.ConnectionHistory
	err  error
}

func New(database *gorm.DB) *Model {
	input := textinput.New()
	input.Placeholder = "Search host, status, time, or transcript"
	return &Model{db: database, search: input, showEmptyKeys: []string{"h"}}
}

func (m *Model) SetShowEmptyKeys(keys []string) { m.showEmptyKeys = keys }

func (m *Model) Init() tea.Cmd { return m.reload() }

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.grid = components.ComputeGridWithCardHeight(width, height-2, 5)
	m.search.SetWidth(max(20, width-12))
}

func (m *Model) AllowsListNavigation() bool { return !m.searching && !m.detail }

func (m *Model) reload() tea.Cmd {
	query := strings.TrimSpace(m.search.Value())
	return func() tea.Msg {
		q := m.db.Preload("Host")
		if !m.showEmpty {
			q = q.Where("length(trim(transcript, char(9) || char(10) || char(13) || ' ')) > 0")
		}
		q = q.Order("connected_at DESC")
		if query != "" {
			like := "%" + query + "%"
			hostIDs := m.db.Model(&db.Host{}).Select("id").Where("alias LIKE ? OR hostname LIKE ? OR username LIKE ?", like, like, like)
			q = q.Where("label LIKE ? OR source LIKE ? OR status LIKE ? OR transcript LIKE ? OR strftime('%Y-%m-%d %H:%M', connected_at) LIKE ? OR host_id IN (?)", like, like, like, like, like, hostIDs)
		}
		var rows []db.ConnectionHistory
		err := q.Find(&rows).Error
		return loadedMsg{rows: rows, err: err}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case loadedMsg:
		if msg.err != nil {
			return m, func() tea.Msg { return types.ErrorMsg{Err: msg.err} }
		}
		m.rows, m.loaded = msg.rows, true
		if m.cursor >= len(m.rows) {
			m.cursor = max(0, len(m.rows)-1)
		}
		return m, nil
	case types.RefreshListMsg:
		return m, m.reload()
	case tea.PasteMsg:
		if m.searching {
			m.search.SetValue(m.search.Value() + msg.Content)
			return m, m.reload()
		}
	case tea.KeyPressMsg:
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				m.search.Blur()
				return m, nil
			case "esc", "escape":
				m.searching = false
				m.search.SetValue("")
				m.search.Blur()
				return m, m.reload()
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, tea.Batch(cmd, m.reload())
		}
		if m.detail {
			if viewkeys.MatchKey(msg, m.showEmptyKeys) {
				m.showEmpty = !m.showEmpty
				m.detail = false
				return m, m.reload()
			}
			switch msg.String() {
			case "esc", "escape":
				m.detail, m.detailScroll, m.selection = false, 0, textselection.Selection{}
			case "up", "k":
				m.detailScroll--
			case "down", "j":
				m.detailScroll++
			case "pgup":
				m.detailScroll -= m.detailPageSize()
			case "pgdown":
				m.detailScroll += m.detailPageSize()
			case "home", "g":
				m.detailScroll = 0
			case "end", "G":
				m.detailScroll = len(strings.Split(m.selectedTranscript(), "\n"))
			case "c":
				return m, copyTranscriptCmd(m.selectedTranscript())
			}
			m.clampDetailScroll()
			return m, nil
		}
		if viewkeys.MatchKey(msg, m.showEmptyKeys) {
			m.showEmpty = !m.showEmpty
			return m, m.reload()
		}
		switch msg.String() {
		case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
			if next, changed := components.GridMove(msg.String(), m.cursor, len(m.rows), m.grid); changed {
				m.cursor = next
			}
		case "enter":
			if len(m.rows) > 0 {
				m.detail, m.detailScroll = true, 0
			}
		case "/":
			m.searching = true
			return m, m.search.Focus()
		}
	case tea.MouseClickMsg:
		if m.detail && !m.searching && msg.Button == tea.MouseLeft {
			if line, col, ok := m.detailTextPoint(msg.X, msg.Y); ok {
				m.selection.Begin(line, col)
			}
			return m, nil
		}
		if !m.detail && !m.searching && msg.Button == tea.MouseLeft {
			page := 0
			if m.grid.PageSize > 0 {
				page = m.cursor / m.grid.PageSize
			}
			if idx, ok := components.GridIndexAtMouse(msg.X, msg.Y-1, len(m.rows), m.grid, page); ok {
				if idx == m.cursor {
					m.detail, m.detailScroll = true, 0
				} else {
					m.cursor = idx
				}
			}
		}
	case tea.MouseWheelMsg:
		if m.detail && !m.searching {
			switch msg.Button {
			case tea.MouseWheelUp, tea.MouseWheelLeft:
				m.detailScroll -= 6
			case tea.MouseWheelDown, tea.MouseWheelRight:
				m.detailScroll += 6
			}
			m.clampDetailScroll()
		}
	case tea.MouseMotionMsg:
		if m.detail && m.selection.Dragging {
			line, col, _ := m.detailTextPoint(msg.X, msg.Y)
			m.selection.Move(line, col)
		}
	case tea.MouseReleaseMsg:
		if m.detail && m.selection.Dragging {
			line, col, _ := m.detailTextPoint(msg.X, msg.Y)
			if m.selection.End(line, col) {
				return m, copyTranscriptCmd(m.selection.Text(strings.Split(m.selectedTranscript(), "\n")))
			}
		}
	}
	return m, nil
}

func (m *Model) View() tea.View {
	if !m.loaded {
		return tea.NewView(components.Loading(m.width, m.height, "Loading sessions..."))
	}
	if m.detail {
		return tea.NewView(m.detailView())
	}
	header := ui.TitleStyle.Render("Sessions")
	if m.searching || m.search.Value() != "" {
		header += "  " + m.search.View()
	} else {
		header += "  " + ui.DimStyle.Render("/ search")
	}
	emptyHint := viewkeys.HelpLabel(m.showEmptyKeys) + " show empty"
	if m.showEmpty {
		emptyHint = viewkeys.HelpLabel(m.showEmptyKeys) + " hide empty"
	}
	header += "  " + ui.DimStyle.Render(emptyHint)
	if len(m.rows) == 0 {
		return tea.NewView(header + "\n" + components.EmptyState(m.width, m.height-1, "No matching saved sessions."))
	}
	cards := make([]string, len(m.rows))
	start, end := components.GridPageRange(len(m.rows), m.cursor, m.grid)
	for i := start; i < end; i++ {
		row := m.rows[i]
		cards[i] = components.RenderThreeLineCard(sessionTitle(row), sessionTime(row), sessionMeta(row), i == m.cursor, m.grid.CardW)
	}
	return tea.NewView(header + "\n" + components.RenderGridRows(cards, len(cards), m.cursor, m.grid))
}

func (m *Model) detailView() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	row := m.rows[m.cursor]
	transcript := row.Transcript
	if row.ANSITranscript != "" {
		transcript = row.ANSITranscript
	}
	lines := strings.Split(transcript, "\n")
	available := max(3, m.height-5)
	end := min(len(lines), m.detailScroll+available)
	body := ""
	if m.detailScroll < len(lines) && strings.TrimSpace(transcript) != "" {
		visible := append([]string(nil), lines[m.detailScroll:end]...)
		for i := range visible {
			visible[i] = m.selection.RenderLine(visible[i], m.detailScroll+i)
		}
		body = strings.Join(visible, "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		ui.TitleStyle.Render(sessionTitle(row)),
		ui.DimStyle.Render(sessionDescription(row)),
		"",
		body,
		"",
		ui.DimStyle.Render("j/k scroll · pgup/pgdown · mouse wheel · c copy transcript · esc back"),
	)
}

func (m *Model) detailTextPoint(x, y int) (int, int, bool) {
	line := m.detailScroll + y - 3
	maxLine := len(strings.Split(m.selectedTranscript(), "\n")) - 1
	return min(max(0, line), max(0, maxLine)), max(0, x), y >= 3 && y < 3+m.detailPageSize()
}

func (m *Model) selectedTranscript() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].Transcript
}

func (m *Model) clampDetailScroll() {
	maxScroll := max(0, len(strings.Split(m.selectedTranscript(), "\n"))-m.detailPageSize())
	m.detailScroll = min(max(0, m.detailScroll), maxScroll)
}

func (m *Model) detailPageSize() int {
	return max(3, m.height-5)
}

func sessionTitle(row db.ConnectionHistory) string {
	if row.Label != "" {
		return row.Label
	}
	name := row.Host.Alias
	if name == "" {
		name = fmt.Sprintf("%s@%s", row.Host.Username, row.Host.Hostname)
	}
	return name
}

func sessionDescription(row db.ConnectionHistory) string {
	return sessionTime(row) + " · " + sessionMeta(row)
}

func sessionTime(row db.ConnectionHistory) string {
	duration := "open"
	if row.DisconnectedAt != nil {
		duration = row.DisconnectedAt.Sub(row.ConnectedAt).Round(time.Second).String()
	}
	return fmt.Sprintf("%s · %s", row.ConnectedAt.Format("2006-01-02 15:04"), duration)
}

func sessionMeta(row db.ConnectionHistory) string {
	capture := "no transcript"
	if strings.TrimSpace(row.Transcript) != "" {
		capture = "transcript"
	}
	source := row.Source
	if source == "" {
		source = "ssh"
	}
	return fmt.Sprintf("%s · %s · %s", source, row.Status, capture)
}

func copyTranscriptCmd(text string) tea.Cmd {
	return tea.Batch(
		tea.SetClipboard(text),
		func() tea.Msg { return types.SuccessMsg{Message: fmt.Sprintf("Copied %d chars", len([]rune(text)))} },
	)
}
