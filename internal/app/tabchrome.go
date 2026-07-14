package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

// layoutWidth returns terminal width for tab strip / body layout (minimum 80 when unknown).
func (a App) layoutWidth() int {
	w := a.width
	if w <= 0 {
		return 80
	}
	return w
}

// tabStripItems builds tab bar labels matching MainView.View (prefixes, numbering).
func (a App) tabStripItems() []components.TabItem {
	items := make([]components.TabItem, len(a.tabs))
	for i, tab := range a.tabs {
		title := tab.Title
		if !isListView(tab.Type) {
			switch tab.Type {
			case SSHTab:
				if strings.HasPrefix(tab.Title, "[R]") || tabDetaches(tab) {
					title = tab.Title
				} else {
					title = fmt.Sprintf("[S] %s", tab.Title)
				}
			case LocalTab:
				if strings.HasPrefix(tab.Title, "[R]") || tab.TmuxSession != "" {
					title = tab.Title
				} else {
					title = fmt.Sprintf("[L] %s", tab.Title)
				}
			case SFTPTab:
				title = fmt.Sprintf("[F] %s", tab.Title)
			case ForwardTab:
				title = fmt.Sprintf("[P] %s", tab.Title)
			case SnippetTab:
				title = fmt.Sprintf("[B] %s", tab.Title)
			case SessionHistoryTab:
				title = fmt.Sprintf("[L] %s", tab.Title)
			}
		}
		if i < 9 {
			title = fmt.Sprintf("%d:%s", i+1, title)
		}
		items[i] = components.TabItem{Title: title, ID: string(tab.Type)}
	}
	return items
}

// buildMainTabChrome renders the tab strip + divider line (everything above the tab body).
// The tab strip may wrap to multiple rows when many tabs are open or the terminal is narrow;
// callers must use lipgloss.Height on this string — not a fixed row count.
func (a App) buildMainTabChrome(layoutW int) string {
	if len(a.tabs) == 0 {
		return ""
	}
	tabBar := strings.TrimRight(a.tabBar.SetWidth(layoutW).View(), "\n")
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	toastView := strings.TrimSpace(a.toast.View())
	reconnectView := reconnectBadgeView(a.activeReconnectLabel())
	var secondLine string
	if toastView != "" || reconnectView != "" {
		const toastLeftInset = 3
		left := ""
		if toastView != "" {
			left = lipgloss.NewStyle().PaddingLeft(toastLeftInset).Render(toastView)
		}
		lw := lipgloss.Width(left)
		rw := lipgloss.Width(reconnectView)
		rest := layoutW - lw - rw
		if rest < 0 {
			rest = 0
		}
		rule := dividerStyle.Render(strings.Repeat("─", rest))
		secondLine = lipgloss.JoinHorizontal(lipgloss.Top, left, rule, reconnectView)
	} else {
		line := []rune(strings.Repeat("─", layoutW))
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) && isListView(a.tabs[a.activeTab].Type) && layoutW >= 52 && listSidebarWidth < len(line) {
			line[listSidebarWidth] = '┬'
		}
		secondLine = dividerStyle.Width(layoutW).Render(string(line))
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, secondLine)
}

func reconnectBadgeView(label string) string {
	if label == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#111111")).
		Background(lipgloss.Color("#f2cc60")).
		Bold(true).
		Padding(0, 1).
		Render(label)
}

func (a App) activeReconnectLabel() string {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return ""
	}
	tab := a.tabs[a.activeTab]
	if !isTerminalTab(tab.Type) {
		return ""
	}
	sm, ok := tab.Model.(*sshview.Model)
	if !ok {
		return ""
	}
	return sm.ReconnectingLabel()
}

// mainTabChromeTopLines returns the number of screen rows occupied by tab strip + divider.
func (a App) mainTabChromeTopLines() int {
	if len(a.tabs) == 0 {
		return 0
	}
	tc := a.buildMainTabChrome(a.layoutWidth())
	h := lipgloss.Height(strings.TrimRight(tc, "\n"))
	if h < 1 {
		return 2
	}
	return h
}
