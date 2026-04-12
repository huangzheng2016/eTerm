package components

import (
	"charm.land/lipgloss/v2"
	"github.com/eterm/eterm/internal/ui"
)

// Horizontal offset of first tab cell (matches View TitleBar PaddingLeft).
const tabBarPadLeft = 2

func tabItemCellWidth(activeIdx, i int, item TabItem) int {
	s := ui.InactiveTabStyle.Render(item.Title)
	if i == activeIdx {
		s = ui.ActiveTabStyle.Render(item.Title)
	}
	return lipgloss.Width(s)
}

// tabWidths returns the rendered cell width of each tab item.
func tabWidths(items []TabItem, activeIdx int) []int {
	widths := make([]int, len(items))
	for i, item := range items {
		widths[i] = tabItemCellWidth(activeIdx, i, item)
	}
	return widths
}

const (
	arrowLeft  = "< "
	arrowRight = " >"
	arrowWidth = 2
	tabGap     = 1 // space between tabs
)

// ensureActiveVisible adjusts scrollIdx so the active tab is within the visible window.
func (t *TabsModel) ensureActiveVisible() {
	if len(t.items) == 0 || t.width <= 0 {
		t.scrollIdx = 0
		return
	}
	widths := tabWidths(t.items, t.activeIdx)

	// Clamp scrollIdx
	if t.scrollIdx > t.activeIdx {
		t.scrollIdx = t.activeIdx
	}

	// Scroll right until active tab fits in the visible area
	for {
		avail := t.width - tabBarPadLeft*2 // left+right padding
		if avail <= 0 {
			// Terminal too narrow for any content; just pin to active tab.
			t.scrollIdx = t.activeIdx
			return
		}
		hasLeft := t.scrollIdx > 0
		if hasLeft {
			avail -= arrowWidth
		}

		used := 0
		activeVisible := false
		for i := t.scrollIdx; i < len(t.items); i++ {
			need := widths[i]
			if i > t.scrollIdx {
				need += tabGap
			}
			// Reserve space for right arrow if there are more tabs after
			remaining := avail - used - need
			hasRight := i < len(t.items)-1 && remaining < 0
			if hasRight && i > t.scrollIdx {
				break
			}
			if used+need > avail && i > t.scrollIdx {
				break
			}
			used += need
			if i == t.activeIdx {
				activeVisible = true
			}
		}
		if activeVisible {
			break
		}
		t.scrollIdx++
		if t.scrollIdx >= len(t.items) {
			t.scrollIdx = len(t.items) - 1
			break
		}
	}
}
