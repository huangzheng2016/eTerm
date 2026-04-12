package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/eterm/eterm/internal/types"
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
		a.escMenu = nil
		a.quickConnect = nil
		a.snippetPicker = nil
		a.batchTag = nil
		a.importStratMenu = nil
		a.helpOverlay = false
		if a.confirm.IsActive() {
			a.confirm, _ = a.confirm.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
			cmd := a.processConfirmResult()
			return a, cmd
		}
		return a, nil
	}
	if onClick != nil {
		return onClick(lx, ly)
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

// importStratMenuMouse handles a click inside the import strategy menu.
// Layout: border(1) + padding(1) + title(1) + blank(1) + message(1) + blank(1) + items at ly=6,7.
func (a App) importStratMenuMouse(lx, ly int) (tea.Model, tea.Cmd) {
	itemY := ly - 6
	if itemY >= 0 && itemY <= int(stratOverwrite) {
		a.importStratMenu.cursor = importStratItem(itemY)
		closed, cmd := a.importStratMenu.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		if closed {
			a.importStratMenu = nil
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

func adjustMouse(msg tea.MouseClickMsg, lx, ly int) tea.MouseClickMsg {
	m := msg.Mouse()
	m.X = lx
	m.Y = ly
	return tea.MouseClickMsg(m)
}
