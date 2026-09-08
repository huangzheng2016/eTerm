package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type TabItem struct {
	Title string
	ID    string
}

type TabsModel struct {
	items     []TabItem
	activeIdx int
	width     int
	scrollIdx int
}

func NewTabs(items []TabItem) TabsModel {
	return TabsModel{
		items: items,
	}
}

func (t TabsModel) SetItems(items []TabItem) TabsModel {
	t.items = items
	if len(items) == 0 {
		t.activeIdx = 0
		t.scrollIdx = 0
		return t
	}
	if t.activeIdx >= len(items) {
		t.activeIdx = len(items) - 1
	}
	if t.scrollIdx >= len(items) {
		t.scrollIdx = len(items) - 1
	}
	t.clampScroll()
	return t
}

func TabStrip(items []TabItem, activeIdx int, width int) string {
	if len(items) == 0 {
		return ""
	}
	if activeIdx < 0 {
		activeIdx = 0
	}
	if activeIdx >= len(items) {
		activeIdx = len(items) - 1
	}
	t := NewTabs(items).SetActive(activeIdx)
	if width > 0 {
		t = t.SetWidth(width)
	}
	return strings.TrimRight(t.View(), "\n")
}

func (t TabsModel) SetWidth(w int) TabsModel {
	t.width = w
	t.clampScroll()
	return t
}

func (t TabsModel) ActiveID() string {
	if t.activeIdx >= 0 && t.activeIdx < len(t.items) {
		return t.items[t.activeIdx].ID
	}
	return ""
}

func (t TabsModel) ActiveIndex() int {
	return t.activeIdx
}

func (t TabsModel) SetActive(idx int) TabsModel {
	if idx >= 0 && idx < len(t.items) {
		t.activeIdx = idx
		t.ensureActiveVisible()
	}
	return t
}

func (t TabsModel) NextTab() TabsModel {
	if len(t.items) > 0 {
		t.activeIdx = (t.activeIdx + 1) % len(t.items)
		t.ensureActiveVisible()
	}
	return t
}

func (t TabsModel) PrevTab() TabsModel {
	if len(t.items) > 0 {
		t.activeIdx = (t.activeIdx - 1 + len(t.items)) % len(t.items)
		t.ensureActiveVisible()
	}
	return t
}

func (t TabsModel) HandleClick(x int) (TabsModel, bool) {

	layout := t.layout()
	lastVisible := t.scrollIdx - 1
	if len(layout.visible) > 0 {
		lastVisible = layout.visible[len(layout.visible)-1]
	}

	if layout.hasLeft && x >= tabBarPadLeft && x < tabBarPadLeft+arrowWidth {
		t.scrollIdx--
		if t.scrollIdx < 0 {
			t.scrollIdx = 0
		}
		return t, false
	}

	if layout.hasRight {
		rightStart := tabBarPadLeft + layout.used
		if layout.hasLeft {
			rightStart += arrowWidth
		}
		if x >= rightStart && x < rightStart+arrowWidth {
			t.scrollIdx++
			if t.scrollIdx >= len(t.items) {
				t.scrollIdx = len(t.items) - 1
			}
			return t, false
		}
	}

	offset := tabBarPadLeft
	if layout.hasLeft {
		offset += arrowWidth
	}

	for i := t.scrollIdx; i <= lastVisible; i++ {
		w := layout.widths[i]
		cellStart := offset
		if i > t.scrollIdx {
			cellStart++
		}
		if x >= cellStart && x < cellStart+w {
			if t.activeIdx != i {
				t.activeIdx = i
				return t, true
			}
			return t, false
		}
		offset = cellStart + w
	}
	return t, false
}

func (t TabsModel) ScrollLeft() TabsModel {
	if t.scrollIdx > 0 {
		t.scrollIdx--
	}
	return t
}

func (t TabsModel) ScrollRight() TabsModel {
	if t.layout().hasRight {
		t.scrollIdx++
	}
	return t
}

func (t TabsModel) Update(msg tea.Msg) (TabsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab":
			t = t.NextTab()
		case "shift+tab":
			t = t.PrevTab()
		default:
			if (msg.Mod.Contains(tea.ModCtrl) || msg.Mod.Contains(tea.ModAlt)) && msg.Code >= '1' && msg.Code <= '9' {
				idx := int(msg.Code - '1')
				t = t.SetActive(idx)
			}
		}
	case tea.MouseClickMsg:
		t, _ = t.HandleClick(msg.X)
	}
	return t, nil
}
