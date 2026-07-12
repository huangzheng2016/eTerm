package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
)

type hostListState int

const (
	hostListStateList hostListState = iota
	hostListStateAlias
	hostListStateRename
)

const hostListPageSize = 10
const importHostListRowWidth = 62

type importHostListModel struct {
	items           []importHostEntry
	allKeys         []parser.KeyRecord
	sshSource       bool
	exportMode      bool
	cursor          int
	page            int
	pageSize        int
	state           hostListState
	aliasCursor     int
	renameInput     textinput.Model
	renameFromAlias bool
}

func newImportHostList(items []importHostEntry) *importHostListModel {
	ti := textinput.New()
	ti.CharLimit = 64
	return &importHostListModel{
		items:       items,
		pageSize:    hostListPageSize,
		renameInput: ti,
	}
}

func (m *importHostListModel) setPageSize(windowHeight int) {
	// border(2) + padding(2) + title(1) + hint(1) + blank(1) + pager(1) = 8 overhead
	ps := windowHeight - 8
	if ps < 3 {
		ps = 3
	}
	m.pageSize = ps
	// clamp cursor/page to new size
	totalPages := (len(m.items) + m.pageSize - 1) / m.pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if m.page >= totalPages {
		m.page = totalPages - 1
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
}

func (m *importHostListModel) Update(msg tea.KeyPressMsg) (closed bool, proceed bool, cmd tea.Cmd) {
	switch m.state {
	case hostListStateList:
		totalPages := (len(m.items) + m.pageSize - 1) / m.pageSize
		if totalPages < 1 {
			totalPages = 1
		}
		pageStart := m.page * m.pageSize
		pageEnd := pageStart + m.pageSize
		if pageEnd > len(m.items) {
			pageEnd = len(m.items)
		}
		switch msg.String() {
		case "up", "k":
			if m.cursor > pageStart {
				m.cursor--
			} else if m.page > 0 {
				m.page--
				m.cursor = m.page*m.pageSize + m.pageSize - 1
				if m.cursor >= len(m.items) {
					m.cursor = len(m.items) - 1
				}
			}
		case "down", "j":
			if m.cursor < pageEnd-1 {
				m.cursor++
			} else if m.page < totalPages-1 {
				m.page++
				m.cursor = m.page * m.pageSize
			}
		case "left", "h":
			if m.page > 0 {
				m.page--
				m.cursor = m.page * m.pageSize
			}
		case "right", "l":
			if m.page < totalPages-1 {
				m.page++
				m.cursor = m.page * m.pageSize
			}
		case "space":
			if m.cursor < len(m.items) && !m.items[m.cursor].blocked {
				m.items[m.cursor].selected = !m.items[m.cursor].selected
			}
		case "enter":
			if m.cursor >= len(m.items) {
				break
			}
			item := &m.items[m.cursor]
			if m.exportMode {
				break
			} else if len(item.rec.Aliases) > 1 {
				m.state = hostListStateAlias
				m.aliasCursor = 0
			} else if item.nameConflict {
				m.state = hostListStateRename
				m.renameFromAlias = false
				m.renameInput.SetValue(item.chosenAlias)
				m.renameInput.Focus()
			}
		case "y":
			return false, true, nil
		case "esc", "escape":
			return true, false, nil
		}

	case hostListStateAlias:
		item := &m.items[m.cursor]
		maxIdx := len(item.rec.Aliases) // last = "输入新名..."
		switch msg.String() {
		case "up", "k":
			if m.aliasCursor > 0 {
				m.aliasCursor--
			}
		case "down", "j":
			if m.aliasCursor < maxIdx {
				m.aliasCursor++
			}
		case "enter":
			if m.aliasCursor < len(item.rec.Aliases) {
				item.chosenAlias = item.rec.Aliases[m.aliasCursor]
				m.state = hostListStateList
			} else {
				m.state = hostListStateRename
				m.renameFromAlias = true
				m.renameInput.SetValue(item.chosenAlias)
				m.renameInput.Focus()
			}
		case "esc", "escape":
			m.state = hostListStateList
		}

	case hostListStateRename:
		switch msg.String() {
		case "enter":
			m.items[m.cursor].chosenAlias = m.renameInput.Value()
			m.renameInput.Blur()
			if m.renameFromAlias {
				m.state = hostListStateAlias
			} else {
				m.state = hostListStateList
			}
		case "esc", "escape":
			m.renameInput.Blur()
			if m.renameFromAlias {
				m.state = hostListStateAlias
			} else {
				m.state = hostListStateList
			}
		default:
			var tiCmd tea.Cmd
			m.renameInput, tiCmd = m.renameInput.Update(msg)
			return false, false, tiCmd
		}
	}
	return false, false, nil
}

func (m *importHostListModel) View() string {
	switch m.state {
	case hostListStateAlias:
		return m.viewAlias()
	case hostListStateRename:
		return m.viewRename()
	default:
		return m.viewList()
	}
}

func (m *importHostListModel) viewList() string {
	action := "导入"
	enterHint := " · enter 改名"
	if m.exportMode {
		action = "导出"
		enterHint = ""
	}
	title := ui.TitleStyle.Render(action + "主机 (步骤 1/2)")
	hint := ui.DimStyle.Render("space 选择" + enterHint + " · y 下一步 · ←→ 翻页 · esc 返回")

	totalPages := (len(m.items) + m.pageSize - 1) / m.pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	pageStart := m.page * m.pageSize
	pageEnd := pageStart + m.pageSize
	if pageEnd > len(m.items) {
		pageEnd = len(m.items)
	}

	rows := make([]string, 0, pageEnd-pageStart)
	for i := pageStart; i < pageEnd; i++ {
		item := m.items[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		var checkbox string
		if item.blocked {
			checkbox = "[=]"
		} else if item.selected {
			checkbox = "[x]"
		} else {
			checkbox = "[ ]"
		}

		addr := fmt.Sprintf("%s@%s:%d", item.rec.Username, item.rec.Host, item.rec.Port)
		line := fmt.Sprintf("%s %s  %-20s", checkbox, item.chosenAlias, addr)

		if len(item.rec.Aliases) > 1 {
			line += fmt.Sprintf("  (%d aliases)", len(item.rec.Aliases))
		}
		if item.rec.KeyName != "" && !item.blocked {
			line += fmt.Sprintf("  [key: %s]", item.rec.KeyName)
		}
		if item.blocked {
			line += "  [已存在]"
		} else if item.existing {
			line += "  [已导出]"
		}

		var rowStyle lipgloss.Style
		if item.blocked {
			rowStyle = ui.DimStyle
		} else if i == m.cursor {
			rowStyle = ui.SelectedStyle
		} else if item.selected {
			rowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#aaaaff"))
		} else {
			rowStyle = lipgloss.NewStyle()
		}

		line = truncateImportHostLine(line, importHostListRowWidth-lipgloss.Width(cursor))
		rows = append(rows, cursor+rowStyle.Render(line))
	}

	pager := ui.DimStyle.Render(fmt.Sprintf("第 %d/%d 页", m.page+1, totalPages))
	content := lipgloss.JoinVertical(lipgloss.Left, title, hint, "", strings.Join(rows, "\n"), pager)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(70).
		Render(content)
}

func truncateImportHostLine(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	suffix := "..."
	runes := []rune(s)
	for i := len(runes); i >= 0; i-- {
		candidate := strings.TrimRight(string(runes[:i]), " ") + suffix
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return suffix
}

func (m *importHostListModel) viewAlias() string {
	item := m.items[m.cursor]
	title := ui.TitleStyle.Render("选择别名: " + item.chosenAlias)

	var rows string
	for i, a := range item.rec.Aliases {
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.aliasCursor {
			cursor = "▸ "
			style = ui.SelectedStyle
		}
		rows += cursor + style.Render(a) + "\n"
	}
	// "输入新名..." option
	lastIdx := len(item.rec.Aliases)
	cursor := "  "
	style := ui.DimStyle
	if m.aliasCursor == lastIdx {
		cursor = "▸ "
		style = ui.SelectedStyle
	}
	rows += cursor + style.Render("[ 输入新名... ]") + "\n"

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", rows)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}

func (m *importHostListModel) viewRename() string {
	item := m.items[m.cursor]
	var label string
	if item.nameConflict && !m.renameFromAlias {
		label = ui.WarningStyle.Render("名称冲突，请重命名:")
	} else {
		label = ui.DimStyle.Render("输入新名:")
	}
	content := lipgloss.JoinVertical(lipgloss.Left, label, m.renameInput.View())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(40).
		Render(content)
}
