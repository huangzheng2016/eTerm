package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/types"
)

// overlayBounds calculates the top-left position and size of a centered overlay.
func (a App) overlayBounds(rendered string) (ox, oy, ow, oh int) {
	lines := strings.Split(rendered, "\n")
	oh = len(lines)
	for _, l := range lines {
		if w := lipgloss.Width(l); w > ow {
			ow = w
		}
	}
	layoutW := a.width
	if layoutW <= 0 {
		layoutW = 80
	}
	layoutH := a.height
	if layoutH <= 0 {
		layoutH = 24
	}
	ox = (layoutW - ow) / 2
	oy = (layoutH - oh) / 2
	return
}

// handleOverlayMouse checks if a click is inside the overlay.
// Outside click dismisses the active overlay. Inside click calls onClick if non-nil.
func (a App) handleOverlayMouse(msg tea.MouseClickMsg, rendered string, onClick func(lx, ly int) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	ox, oy, ow, oh := a.overlayBounds(rendered)
	lx := msg.X - ox
	ly := msg.Y - oy
	if lx < 0 || ly < 0 || lx >= ow || ly >= oh {
		// Click outside -- dismiss
		hadUpgradePrompt := a.upgradePrompt != nil
		a.escMenu = nil
		a.quickConnect = nil
		a.snippetPicker = nil
		a.batchActions = nil
		a.batchTag = nil
		a.importKeyList = nil
		a.importHostList = nil
		a.importSourceMenu = nil
		a.commandPalette = nil
		a.aiVisible = false
		a.helpOverlay = false
		a.upgradePrompt = nil
		a.connError = nil
		if a.confirm.IsActive() {
			a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
			cmd := a.processConfirmResult()
			return a, cmd
		}
		if hadUpgradePrompt {
			a.promptDeferredTmuxRestore()
		}
		return a, nil
	}
	if onClick != nil {
		return onClick(lx, ly)
	}
	return a, nil
}

// commandPaletteMouse handles clicks inside the command palette overlay.
// Layout: border(1) + padding(1) + title(1) + input(1) + blank(1) + items from ly=4.
func (a App) commandPaletteMouse(lx, ly int) (tea.Model, tea.Cmd) {
	if a.commandPalette == nil {
		return a, nil
	}
	if ly == 2 {
		return a, a.commandPalette.input.Focus()
	}
	itemY := ly - 4
	if itemY >= 0 && itemY < len(a.commandPalette.filtered) && itemY < 8 {
		a.commandPalette.cursor = itemY
		selected := a.commandPalette.selectedMsg()
		a.commandPalette = nil
		if selected == nil {
			return a, nil
		}
		return a, func() tea.Msg { return selected }
	}
	return a, nil
}

// escMenuMouse handles a click inside the ESC menu overlay.
// Layout: border(1) + padding(1) + title(1) + blank(1) + items at ly=4,5,6.
func (a App) escMenuMouse(lx, ly int) (tea.Model, tea.Cmd) {
	itemY := ly - 4
	if itemY >= 0 && itemY <= int(escMenuSync) {
		a.escMenu.cursor = escMenuItem(itemY)
		closed, cmd := a.escMenu.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		if closed {
			a.escMenu = nil
		}
		return a, cmd
	}
	return a, nil
}

// snippetPickerMouse handles a click inside the snippet picker.
// Layout: border(1) + padding(1) + title(1) + blank(1) + items at ly=4.
func (a App) snippetPickerMouse(lx, ly int) (tea.Model, tea.Cmd) {
	sp := a.snippetPicker
	if len(sp.snippets) == 0 {
		return a, nil
	}
	itemY := ly - 4
	if itemY >= 0 && itemY < len(sp.snippets) {
		sp.cursor = itemY
		cmd := sp.snippets[sp.cursor].Command
		a.snippetPicker = nil
		return a, func() tea.Msg { return types.SnippetSelectedMsg{Command: cmd} }
	}
	return a, nil
}

// quickConnectMouse handles clicks inside the quick connect overlay.
// Input row is ly=4; hint row is ly=6 where left half connects and right half cancels.
func (a App) quickConnectMouse(lx, ly int) (tea.Model, tea.Cmd) {
	if a.quickConnect == nil {
		return a, nil
	}
	if ly == 4 {
		return a, a.quickConnect.input.Focus()
	}
	if ly >= 6 {
		w := lipgloss.Width(a.quickConnect.View())
		if lx < w/2 {
			raw := strings.TrimSpace(a.quickConnect.input.Value())
			a.quickConnect = nil
			if raw == "" {
				return a, nil
			}
			hostname, port, username := parseQuickConnect(raw)
			return a, func() tea.Msg {
				return types.QuickConnectMsg{Hostname: hostname, Port: port, Username: username}
			}
		}
		a.quickConnect = nil
		return a, nil
	}
	return a, nil
}

// batchActionsMouse handles clicks inside the batch actions overlay.
// Step 0 rows are at ly=6..8, step 1 input row is ly=7.
func (a App) batchActionsMouse(lx, ly int) (tea.Model, tea.Cmd) {
	if a.batchActions == nil {
		return a, nil
	}
	if a.batchActions.step == 1 {
		if ly == 7 {
			return a, a.batchActions.command.Focus()
		}
		if ly >= 9 {
			w := lipgloss.Width(a.batchActions.View())
			if lx < w/2 {
				command := strings.TrimSpace(a.batchActions.command.Value())
				if command == "" {
					return a, nil
				}
				hostIDs := append([]uint(nil), a.batchActions.hostIDs...)
				a.batchActions = nil
				return a, func() tea.Msg {
					return types.BatchCommandSubmitMsg{HostIDs: hostIDs, Command: command, ReadOnly: true}
				}
			}
			a.batchActions.step = 0
			return a, nil
		}
		return a, nil
	}
	itemY := ly - 6
	if itemY < 0 || itemY > 2 {
		return a, nil
	}
	a.batchActions.cursor = batchActionItem(itemY)
	hostIDs := append([]uint(nil), a.batchActions.hostIDs...)
	switch a.batchActions.cursor {
	case batchActionOpen:
		a.batchActions = nil
		return a, func() tea.Msg { return types.BatchActionSelectedMsg{HostIDs: hostIDs, Action: "open"} }
	case batchActionSnippet:
		a.batchActions = nil
		return a, func() tea.Msg { return types.BatchActionSelectedMsg{HostIDs: hostIDs, Action: "snippet"} }
	case batchActionReadOnly:
		a.batchActions.step = 1
		return a, a.batchActions.command.Focus()
	}
	return a, nil
}

// batchTagMouse handles clicks inside the batch tag overlay.
// Input row is ly=6; hint row is ly=8 where left half applies and right half cancels.
func (a App) batchTagMouse(lx, ly int) (tea.Model, tea.Cmd) {
	if a.batchTag == nil {
		return a, nil
	}
	if ly == 6 {
		return a, a.batchTag.input.Focus()
	}
	if ly >= 8 {
		w := lipgloss.Width(a.batchTag.View())
		if lx < w/2 {
			raw := strings.TrimSpace(a.batchTag.input.Value())
			ids := append([]uint(nil), a.batchTag.ids...)
			a.batchTag = nil
			if raw == "" || len(ids) == 0 {
				return a, nil
			}
			return a, func() tea.Msg {
				return batchTagApplyMsg{HostIDs: ids, RawTags: raw}
			}
		}
		a.batchTag = nil
		return a, nil
	}
	return a, nil
}

func adjustMouse(msg tea.MouseClickMsg, lx, ly int) tea.MouseClickMsg {
	m := msg.Mouse()
	m.X = lx
	m.Y = ly
	return tea.MouseClickMsg(m)
}

// aiOverlayMouse shifts a mouse message into the AI overlay's local
// coordinate frame (the fullscreen border box, see overlayBounds).
func (a App) aiOverlayMouse(msg tea.Msg) tea.Msg {
	ox, oy, _, _ := a.overlayBounds(a.aiView.View().Content)
	switch m := msg.(type) {
	case tea.MouseMotionMsg:
		mm := m.Mouse()
		mm.X -= ox
		mm.Y -= oy
		return tea.MouseMotionMsg(mm)
	case tea.MouseReleaseMsg:
		mm := m.Mouse()
		mm.X -= ox
		mm.Y -= oy
		return tea.MouseReleaseMsg(mm)
	}
	return msg
}
