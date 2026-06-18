package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

// reflowWindow re-sends the last known size so tab models pick up chrome (e.g. help row) changes.
func reflowWindow(a App) tea.Cmd {
	if a.width <= 0 || a.height <= 0 {
		return nil
	}
	return func() tea.Msg {
		return tea.WindowSizeMsg{Width: a.width, Height: a.height}
	}
}

// layoutTabModels applies current terminal dimensions to every tab (per-tab content height).
// Use after WindowSizeMsg updates a.width/a.height, or immediately when switching tabs so the
// active view paints with correct list/viewport size on the same frame (reflowWindow alone is async).
func layoutTabModels(a App) (App, tea.Cmd) {
	var cmds []tea.Cmd
	for i := range a.tabs {
		if a.tabs[i].Model == nil {
			continue
		}
		h := a.mainContentHeightForType(a.tabs[i].Type)
		sizeMsg := tea.WindowSizeMsg{Width: a.width, Height: h}
		updated, cmd := a.tabs[i].Model.Update(sizeMsg)
		a.tabs[i].Model = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return a, tea.Batch(cmds...)
}

// mainContentHeight is the inner height passed to the active tab (below tab chrome, above status).
func (a App) mainContentHeight() int {
	if len(a.tabs) == 0 {
		return max(1, a.height-3)
	}
	return a.mainContentHeightForType(a.tabs[a.activeTab].Type)
}

func (a App) mainContentHeightForType(tabType TabType) int {
	_ = tabType
	if a.height <= 0 {
		return 24
	}
	if len(a.tabs) == 0 {
		return max(1, a.height-3)
	}
	// Tab strip may wrap to multiple rows (many tabs / narrow terminal); measure real chrome height.
	top := a.mainTabChromeTopLines()
	h := a.height - top - 1 // status bar row
	if h < 1 {
		return 1
	}
	return h
}

// MainViewChromeTopLines is the number of terminal rows above the active tab body:
// tab strip (may wrap) + divider line (toast text is drawn on the left of this line when visible).
func (a App) MainViewChromeTopLines() int {
	if len(a.tabs) == 0 {
		return 2
	}
	return a.mainTabChromeTopLines()
}

// appAdjustMouseForTabContent maps screen mouse coordinates to tab-content coordinates.
// Clicks on the tab strip / divider (including toast on the divider line) return nil so tab models do not mis-handle them.
func appAdjustMouseForTabContent(a App, msg tea.Msg) tea.Msg {
	top := a.MainViewChromeTopLines()
	contentH := a.mainContentHeight()
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		if m.Y < top || m.Y >= top+contentH {
			return nil
		}
		mm := m.Mouse()
		mm.Y -= top
		return tea.MouseClickMsg(mm)
	case tea.MouseWheelMsg:
		if m.Y < top || m.Y >= top+contentH {
			return nil
		}
		mm := m.Mouse()
		mm.Y -= top
		return tea.MouseWheelMsg(mm)
	case tea.MouseMotionMsg:
		if m.Y < top || m.Y >= top+contentH {
			if !activeSSHDraggingSelection(a) {
				return nil
			}
			mm := m.Mouse()
			mm.X, mm.Y = clampContentMouse(a.width, contentH, mm.X, mm.Y-top)
			return tea.MouseMotionMsg(mm)
		}
		mm := m.Mouse()
		mm.Y -= top
		return tea.MouseMotionMsg(mm)
	case tea.MouseReleaseMsg:
		if m.Y < top || m.Y >= top+contentH {
			if !activeSSHDraggingSelection(a) {
				return nil
			}
			mm := m.Mouse()
			mm.X, mm.Y = clampContentMouse(a.width, contentH, mm.X, mm.Y-top)
			return tea.MouseReleaseMsg(mm)
		}
		mm := m.Mouse()
		mm.Y -= top
		return tea.MouseReleaseMsg(mm)
	}
	return msg
}

func activeSSHDraggingSelection(a App) bool {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	switch a.tabs[a.activeTab].Type {
	case SSHTab, LocalTab:
	default:
		return false
	}
	m, ok := a.tabs[a.activeTab].Model.(*sshview.Model)
	return ok && m.DraggingSelection()
}

func clampContentMouse(w, h, x, y int) (int, int) {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if w > 0 && x >= w {
		x = w - 1
	}
	if h > 0 && y >= h {
		y = h - 1
	}
	return x, y
}

func ptyFromAppSizeForTab(a App, tabType TabType) (cols, rows int) {
	cols = a.width
	if cols < 40 {
		cols = 80
	}
	rows = a.mainContentHeightForType(tabType)
	if rows < 5 {
		rows = 24
	}
	return cols, rows
}
