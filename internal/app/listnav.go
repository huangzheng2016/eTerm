package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui"
	"github.com/huangzheng2016/eTerm/internal/ui/fwdview"
	"github.com/huangzheng2016/eTerm/internal/ui/home"
	"github.com/huangzheng2016/eTerm/internal/ui/keyview"
	"github.com/huangzheng2016/eTerm/internal/ui/sessionlistview"
	"github.com/huangzheng2016/eTerm/internal/ui/snippetview"
)

const listSidebarWidth = 14

var listViewTypes = []TabType{HomeTab, KeyTab, ForwardTab, SnippetTab, SessionListTab}

func isListView(tabType TabType) bool {
	for _, item := range listViewTypes {
		if item == tabType {
			return true
		}
	}
	return false
}

func (a App) allowsListNavigation() bool {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) || !isListView(a.tabs[a.activeTab].Type) {
		return false
	}
	if guard, ok := a.tabs[a.activeTab].Model.(interface{ AllowsListNavigation() bool }); ok {
		return guard.AllowsListNavigation()
	}
	return true
}

func listContentWidth(width int) int {
	if width < 52 {
		return width
	}
	return width - listSidebarWidth - 1
}

func (a App) switchListView(delta int) (App, tea.Cmd) {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) || !isListView(a.tabs[a.activeTab].Type) {
		return a, nil
	}
	current := 0
	for i, tabType := range listViewTypes {
		if tabType == a.tabs[a.activeTab].Type {
			current = i
			break
		}
	}
	next := (current + delta + len(listViewTypes)) % len(listViewTypes)
	return a.openListView(listViewTypes[next])
}

func (a App) openListView(tabType TabType) (App, tea.Cmd) {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) || !isListView(a.tabs[a.activeTab].Type) {
		return a, nil
	}
	width := listContentWidth(a.width)
	height := a.mainContentHeightForType(tabType)
	var model tea.Model
	switch tabType {
	case HomeTab:
		m := home.New(a.db, a.masterKey, BuildHomeKeyConfig(a.kbConfig))
		m.SetSize(width, height)
		model = m
	case KeyTab:
		m := keyview.New(a.db, a.masterKey, BuildKeyViewKeys(a.kbConfig))
		m.SetSize(width, height)
		model = m
	case ForwardTab:
		m := fwdview.New(a.db, BuildFwdKeys(a.kbConfig))
		m.SetSize(width, height)
		model = m
	case SnippetTab:
		m := snippetview.New(a.db, BuildSnippetKeys(a.kbConfig))
		m.SetSize(width, height)
		model = m
	case SessionListTab:
		m := sessionlistview.New(a.db)
		m.SetShowEmptyKeys(a.kbConfig.ShowHidden)
		m.SetSize(width, height)
		model = m
	}
	a.tabs[a.activeTab] = Tab{Type: tabType, Title: "List", Model: model}
	a.syncTabBar()
	return a, model.Init()
}

func (a App) activateListView(tabType TabType) (App, tea.Cmd) {
	for i := range a.tabs {
		if isListView(a.tabs[i].Type) {
			a.activeTab = i
			a.tabBar = a.tabBar.SetActive(i)
			return a.openListView(tabType)
		}
	}
	return a, nil
}

func renderListLayout(tabType TabType, content string, width, height int) string {
	if width < 52 {
		return content
	}
	sidebar := renderListSidebar(tabType, height)
	dividerRows := make([]string, height)
	for i := range dividerRows {
		dividerRows[i] = "│"
	}
	for _, row := range []int{3, 6, 9, 12, 15, 18} {
		if row < len(dividerRows) {
			dividerRows[row] = "┤"
		}
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#45475A")).Render(strings.Join(dividerRows, "\n"))
	body := lipgloss.NewStyle().Width(listContentWidth(width)).Height(height).Render(content)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, divider, body)
}

func renderListSidebar(tabType TabType, height int) string {
	labels := []struct {
		tab   TabType
		label string
	}{
		{HomeTab, "Hosts"},
		{KeyTab, "Keys"},
		{ForwardTab, "Forwards"},
		{SnippetTab, "Snippets"},
		{SessionListTab, "Sessions"},
	}
	line := func(value string) string {
		return value + strings.Repeat(" ", max(0, listSidebarWidth-lipgloss.Width(value)))
	}
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#45475A"))
	separator := separatorStyle.Render(strings.Repeat("─", listSidebarWidth))
	rows := []string{line(""), line("  " + ui.DimStyle.Render("MENU")), line(""), separator}
	for _, item := range labels {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		itemSeparator := separator
		if item.tab == tabType {
			style = style.
				Foreground(lipgloss.Color("#FF79C6")).
				Bold(true)
			itemSeparator = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render(strings.Repeat("─", listSidebarWidth))
		}
		rows = append(rows, line(""), line("  "+style.Render(item.label)), itemSeparator)
	}
	rows = append(rows, line(""), line("  "+ui.DimStyle.Render("Tab next")), line("  "+ui.DimStyle.Render("S-Tab prev")))
	for len(rows) < height {
		rows = append(rows, line(""))
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}
