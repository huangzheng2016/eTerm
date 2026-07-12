package app

import (
	"strings"

	"github.com/huangzheng2016/eTerm/internal/ui/sshview"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (a App) View() tea.View {
	var view tea.View

	switch a.viewState {
	case LoginView:
		if a.loginModel != nil {
			view = a.loginModel.View()
		} else {
			view = tea.NewView("")
		}
	case MainView:
		// Render tabs from a.tabs (source of truth). The cached a.tabBar can lag and
		// would otherwise show a blank tab row after unlock / single-tab List view.
		layoutW := a.width
		if layoutW <= 0 {
			layoutW = 80
		}
		var tabChrome string
		if len(a.tabs) > 0 {
			tabChrome = a.buildMainTabChrome(layoutW)
		}

		var contentView string
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			tab := a.tabs[a.activeTab]
			if tab.Model != nil {
				contentView = tab.Model.View().Content
				if isListView(tab.Type) {
					contentView = renderListLayout(tab.Type, contentView, layoutW, a.mainContentHeightForType(tab.Type))
				}
			}
		}

		// Ensure contentView fills exactly the allocated height so status bar
		// lands on the last terminal line. TrimRight trailing \n first, then
		// pad or truncate to the exact allocated height.
		allocH := 0
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			topH := lipgloss.Height(strings.TrimRight(tabChrome, "\n"))
			allocH = a.height - topH - 1
			if allocH < 1 {
				allocH = 1
			}
		}
		contentView = strings.TrimRight(contentView, "\n")
		cvLines := strings.Count(contentView, "\n") + 1
		if allocH > 0 && cvLines < allocH {
			contentView += strings.Repeat("\n", allocH-cvLines)
		} else if allocH > 0 && cvLines > allocH {
			lines := strings.SplitN(contentView, "\n", cvLines+1)
			if len(lines) > allocH {
				lines = lines[:allocH]
			}
			contentView = strings.Join(lines, "\n")
		}

		statusBar := a.statusBar
		if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
			tab := a.tabs[a.activeTab]
			disc := false
			if isTerminalTab(tab.Type) {
				if sm, ok := tab.Model.(*sshview.Model); ok {
					disc = sm.Disconnected()
				}
			}
			statusBar = statusBar.SetText(mainViewStatusBarHint(a.keyMap, a.kbConfig, tab.Type, disc, tabDetaches(tab)))
		}
		statusView := statusBar.View()

		var parts []string
		parts = append(parts, strings.TrimRight(tabChrome, "\n"))
		parts = append(parts, contentView) // already padded to exact allocH; do not trim
		parts = append(parts, strings.TrimRight(statusView, "\n"))
		main := strings.Join(parts, "\n")

		// Overlay: confirm dialog, quick connect, or snippet picker
		if a.confirm.IsActive() {
			overlay := a.confirm.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.quickConnect != nil {
			overlay := a.quickConnect.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.connError != nil {
			overlay := a.connError.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.commandPalette != nil {
			overlay := a.commandPalette.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.renamePrompt != nil {
			overlay := a.renamePrompt.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.remoteMenu != nil {
			overlay := a.remoteMenu.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.tmuxMenu != nil {
			overlay := a.tmuxMenu.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.snippetPicker != nil {
			overlay := a.snippetPicker.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.batchActions != nil {
			overlay := a.batchActions.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.batchTag != nil {
			overlay := a.batchTag.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.importKeyList != nil {
			overlay := a.importKeyList.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.importHostList != nil {
			overlay := a.importHostList.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.importSourceMenu != nil {
			overlay := a.importSourceMenu.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.upgradePrompt != nil {
			overlay := upgradePromptView(a.upgradePrompt)
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.escMenu != nil {
			overlay := a.escMenu.View()
			main = lipgloss.Place(layoutW, a.height, lipgloss.Center, lipgloss.Center, overlay)
		} else if a.helpOverlay {
			// Keep tab chrome + status bar fixed like SSH/SFTP; only the content band shows the dialog.
			layoutH := a.height
			if layoutH <= 0 {
				layoutH = 24
			}
			tabStr := strings.TrimRight(strings.ReplaceAll(tabChrome, "\r\n", "\n"), "\n")
			statStr := strings.TrimRight(strings.ReplaceAll(statusView, "\r\n", "\n"), "\n")
			topH := lipgloss.Height(tabStr)
			if topH < 1 {
				topH = 1
			}
			stH := lipgloss.Height(statStr)
			if stH < 1 {
				stH = 1
			}
			midH := layoutH - topH - stH
			if midH < 3 {
				midH = 3
			}
			mid := a.helpOverlayPanel(layoutW, midH)
			main = lipgloss.JoinVertical(lipgloss.Left, tabStr, mid, statStr)
		}

		view = tea.NewView(main)
	default:
		view = tea.NewView("")
	}

	// Alternate screen keeps the frame at terminal size; inline mode would grow with
	// content and bubbletea drops overflow from the top, hiding the tab bar.
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func tabDetaches(tab Tab) bool {
	if tab.Type == LocalTab && tab.TmuxSession != "" {
		return true
	}
	if sm, ok := tab.Model.(*sshview.Model); ok {
		spec := sm.RemoteReconnect()
		return spec != nil && spec.Tmux
	}
	return false
}

func (a App) handleHelpOverlayKey(msg tea.KeyPressMsg) (App, tea.Cmd) {
	if key.Matches(msg, a.keyMap.Help) {
		a.helpOverlay = false
		return a, nil
	}
	switch msg.String() {
	case "esc", "escape", "q":
		a.helpOverlay = false
	}
	return a, nil
}

// helpOverlayPanel fills only the middle band (between tab chrome and status bar) with a centered dialog.
func (a App) helpOverlayPanel(layoutW, midH int) string {
	hmap := a.contextualHelpKeyMap()
	innerW := layoutW - 10
	if innerW < 28 {
		innerW = 28
	}
	maxInner := layoutW - 4
	if maxInner < 28 {
		maxInner = 28
	}
	if innerW > maxInner {
		innerW = maxInner
	}
	hb := a.helpBubble
	hb.SetWidth(innerW)
	body := hb.FullHelpView(hmap.FullHelp())
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#eeeeee")).Render("Shortcuts")
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("esc · " + helpLabel(a.kbConfig.Help) + " · q  close")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", footer)
	dialog := lipgloss.NewStyle().
		MaxWidth(layoutW-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
	dimBg := lipgloss.NewStyle().Background(lipgloss.Color("#111111"))
	return lipgloss.Place(layoutW, midH, lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceStyle(dimBg))
}
