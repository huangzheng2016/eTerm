package app

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
)

type keyListState int

const (
	keyListStateList keyListState = iota
	keyListStateAlias
	keyListStateRename
	keyListStateConfirm
)

type importKeyListModel struct {
	items           []importKeyEntry
	hostItems       []importHostEntry
	cursor          int
	page            int
	pageSize        int
	state           keyListState
	aliasCursor     int
	renameInput     textinput.Model
	renameFromAlias bool
	exportMode      bool
}

func newImportKeyList(items []importKeyEntry, hostItems []importHostEntry) *importKeyListModel {
	ti := textinput.New()
	ti.CharLimit = 64
	return &importKeyListModel{
		items:       items,
		hostItems:   hostItems,
		pageSize:    10,
		renameInput: ti,
	}
}

func (m *importKeyListModel) setPageSize(windowHeight int) {
	ps := windowHeight - 8
	if ps < 3 {
		ps = 3
	}
	m.pageSize = ps
	totalPages := (len(m.items) + m.pageSize - 1) / m.pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if m.page >= totalPages {
		m.page = totalPages - 1
	}
	if m.cursor >= len(m.items) && len(m.items) > 0 {
		m.cursor = len(m.items) - 1
	}
}

func (m *importKeyListModel) Update(msg tea.KeyPressMsg) (closed bool, confirmed bool, cmd tea.Cmd) {
	switch m.state {
	case keyListStateList:
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
			if m.cursor >= len(m.items) {
				break
			}
			item := &m.items[m.cursor]
			if !item.blocked && !item.locked {
				item.selected = !item.selected
			}
		case "enter":
			if m.cursor >= len(m.items) {
				break
			}
			item := &m.items[m.cursor]
			if m.exportMode {
				break
			} else if len(item.rec.Aliases) > 1 {
				m.aliasCursor = 0
				m.state = keyListStateAlias
			} else if item.nameConflict {
				m.renameInput.SetValue(item.chosenAlias)
				m.renameInput.Focus()
				m.renameFromAlias = false
				m.state = keyListStateRename
			}
		case "y":
			m.state = keyListStateConfirm
		case "esc":
			return true, false, nil
		}

	case keyListStateAlias:
		item := &m.items[m.cursor]
		maxCursor := len(item.rec.Aliases)
		switch msg.String() {
		case "up", "k":
			if m.aliasCursor > 0 {
				m.aliasCursor--
			}
		case "down", "j":
			if m.aliasCursor < maxCursor {
				m.aliasCursor++
			}
		case "enter":
			if m.aliasCursor < len(item.rec.Aliases) {
				item.chosenAlias = item.rec.Aliases[m.aliasCursor]
				m.state = keyListStateList
			} else {
				m.renameInput.SetValue(item.chosenAlias)
				m.renameInput.Focus()
				m.renameFromAlias = true
				m.state = keyListStateRename
			}
		case "esc":
			m.state = keyListStateList
		}

	case keyListStateRename:
		switch msg.String() {
		case "enter":
			m.items[m.cursor].chosenAlias = m.renameInput.Value()
			m.renameInput.Blur()
			if m.renameFromAlias {
				m.state = keyListStateAlias
			} else {
				m.state = keyListStateList
			}
		case "esc":
			m.renameInput.Blur()
			if m.renameFromAlias {
				m.state = keyListStateAlias
			} else {
				m.state = keyListStateList
			}
		default:
			var tiCmd tea.Cmd
			m.renameInput, tiCmd = m.renameInput.Update(msg)
			return false, false, tiCmd
		}

	case keyListStateConfirm:
		switch msg.String() {
		case "y":
			return false, true, nil
		case "n", "esc":
			m.state = keyListStateList
		}
	}

	return false, false, nil
}

func (m *importKeyListModel) View() string {
	var content string

	switch m.state {
	case keyListStateList:
		action := "导入"
		enterHint := " · enter 改名"
		if m.exportMode {
			action = "导出"
			enterHint = ""
		}
		title := ui.TitleStyle.Render(action + "密钥 (步骤 2/2)")
		hint := ui.DimStyle.Render("space 选择" + enterHint + " · y 确认 · ←→ 翻页 · esc 返回")
		totalPages := (len(m.items) + m.pageSize - 1) / m.pageSize
		if totalPages < 1 {
			totalPages = 1
		}
		pageStart := m.page * m.pageSize
		pageEnd := pageStart + m.pageSize
		if pageEnd > len(m.items) {
			pageEnd = len(m.items)
		}
		rows := ""
		for i := pageStart; i < pageEnd; i++ {
			item := m.items[i]
			var checkbox string
			var rowStyle lipgloss.Style
			if item.blocked {
				checkbox = "[=]"
				rowStyle = ui.DimStyle
			} else if item.locked {
				checkbox = "[*]"
				rowStyle = ui.SelectedStyle
			} else if item.selected {
				checkbox = "[x]"
				rowStyle = lipgloss.NewStyle()
			} else {
				checkbox = "[ ]"
				rowStyle = lipgloss.NewStyle()
			}

			if m.cursor == i {
				checkbox = ui.SelectedStyle.Render(checkbox)
			}

			name := item.chosenAlias
			aliasCount := len(item.rec.Aliases)
			aliasStr := fmt.Sprintf("(%d alias)", aliasCount)
			if aliasCount != 1 {
				aliasStr = fmt.Sprintf("(%d aliases)", aliasCount)
			}

			suffix := ""
			if item.locked {
				suffix = "   " + ui.WarningStyle.Render("[必须]")
			} else if item.blocked {
				suffix = "   " + ui.DimStyle.Render("[已存在]")
			}

			row := fmt.Sprintf("%s %-20s %-12s%s", checkbox, rowStyle.Render(name), ui.DimStyle.Render(aliasStr), suffix)
			rows += row + "\n"
		}
		pager := ui.DimStyle.Render(fmt.Sprintf("第 %d/%d 页", m.page+1, totalPages))
		content = lipgloss.JoinVertical(lipgloss.Left, title, "", rows, pager, hint)

	case keyListStateAlias:
		item := m.items[m.cursor]
		title := ui.TitleStyle.Render("选择别名: " + item.chosenAlias)
		rows := ""
		for i, alias := range item.rec.Aliases {
			cursor := "  "
			style := ui.DimStyle
			if i == m.aliasCursor {
				cursor = "▸ "
				style = ui.SelectedStyle
			}
			rows += fmt.Sprintf("%s%s\n", cursor, style.Render(alias))
		}
		// "enter new name" option
		newNameStyle := ui.DimStyle
		cursor := "  "
		if m.aliasCursor == len(item.rec.Aliases) {
			cursor = "▸ "
			newNameStyle = ui.SelectedStyle
		}
		rows += fmt.Sprintf("%s%s\n", cursor, newNameStyle.Render("[ 输入新名... ]"))
		hint := ui.DimStyle.Render("↑↓ navigate · enter select · esc back")
		content = lipgloss.JoinVertical(lipgloss.Left, title, "", rows, hint)

	case keyListStateRename:
		item := m.items[m.cursor]
		var label string
		if item.nameConflict && !m.renameFromAlias {
			label = ui.WarningStyle.Render("名称冲突，请重命名:")
		} else {
			label = ui.TitleStyle.Render("输入新名:")
		}
		content = lipgloss.JoinVertical(lipgloss.Left, label, "", m.renameInput.View())

	case keyListStateConfirm:
		hostCount := 0
		for _, h := range m.hostItems {
			if !h.blocked && h.selected {
				hostCount++
			}
		}
		keyCount := 0
		lockedCount := 0
		for _, k := range m.items {
			if k.blocked {
				continue
			}
			if k.locked {
				lockedCount++
				keyCount++
			} else if k.selected {
				keyCount++
			}
		}
		action := "导入"
		if m.exportMode {
			action = "导出"
		}
		title := ui.TitleStyle.Render(action + "确认")
		hostLine := fmt.Sprintf("  主机: %d 个", hostCount)
		keyLine := fmt.Sprintf("  密钥: %d 个（含 %d 个必须）", keyCount, lockedCount)
		hint := ui.DimStyle.Render("  y 确认  n/esc 取消")
		content = lipgloss.JoinVertical(lipgloss.Left, title, "", hostLine, keyLine, "", hint)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 3).
		Width(52).
		Render(content)
}
