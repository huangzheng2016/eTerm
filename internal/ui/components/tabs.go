package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/eterm/eterm/internal/ui"
)

type TabItem struct {
	Title string
	ID    string
}

type TabsModel struct {
	items     []TabItem
	activeIdx int
	width     int
	scrollIdx int // index of first visible tab
}

func NewTabs(items []TabItem) TabsModel {
	return TabsModel{
		items: items,
	}
}

// TabStrip renders the tab row for the app chrome. activeIdx is clamped to items.
// width is the terminal width (pass 0 for natural width without full-width padding).
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

// Horizontal offset of first tab cell (matches View TitleBar PaddingLeft).
const tabBarPadLeft = 2

func tabItemCellWidth(activeIdx, i int, item TabItem) int {
	s := ui.InactiveTabStyle.Render(item.Title)
	if i == activeIdx {
		s = ui.ActiveTabStyle.Render(item.Title)
	}
	return lipgloss.Width(s)
}

func (t TabsModel) HandleClick(x int) (TabsModel, bool) {
	// Do NOT call ensureActiveVisible here — the user may have scrolled
	// the tab bar away from the active tab via mouse wheel; we must
	// compute hit regions based on the current scrollIdx.

	hasLeft := t.scrollIdx > 0
	// Compute hasRight
	widths := tabWidths(t.items, t.activeIdx)
	budget := t.width - tabBarPadLeft*2
	if hasLeft {
		budget -= arrowWidth
	}
	used := 0
	lastVisible := t.scrollIdx - 1
	for i := t.scrollIdx; i < len(t.items); i++ {
		need := widths[i]
		if i > t.scrollIdx {
			need += tabGap
		}
		if used+need > budget {
			break
		}
		if i < len(t.items)-1 && used+need+arrowWidth > budget {
			break
		}
		lastVisible = i
		used += need
	}
	hasRight := lastVisible < len(t.items)-1

	// Click on left arrow "< " → scroll left
	if hasLeft && x >= tabBarPadLeft && x < tabBarPadLeft+arrowWidth {
		t.scrollIdx--
		if t.scrollIdx < 0 {
			t.scrollIdx = 0
		}
		return t, false
	}

	// Click on right arrow " >" → scroll right
	if hasRight {
		// Right arrow is at the end of the visible row
		rightStart := tabBarPadLeft + used
		if hasLeft {
			rightStart += arrowWidth
		}
		// Add gaps between visible tabs
		if x >= rightStart && x < rightStart+arrowWidth {
			t.scrollIdx++
			if t.scrollIdx >= len(t.items) {
				t.scrollIdx = len(t.items) - 1
			}
			return t, false
		}
	}

	// Click on a tab
	offset := tabBarPadLeft
	if hasLeft {
		offset += arrowWidth
	}

	used = 0
	for i := t.scrollIdx; i <= lastVisible; i++ {
		w := widths[i]
		need := w
		if i > t.scrollIdx {
			need += tabGap
		}
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
		used += need
	}
	return t, false
}

// ScrollLeft scrolls the tab bar one position to the left.
func (t TabsModel) ScrollLeft() TabsModel {
	if t.scrollIdx > 0 {
		t.scrollIdx--
	}
	return t
}

// ScrollRight scrolls the tab bar one position to the right.
func (t TabsModel) ScrollRight() TabsModel {
	if t.scrollIdx < len(t.items)-1 {
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

func (t TabsModel) View() string {
	if len(t.items) == 0 {
		return ""
	}

	widths := tabWidths(t.items, t.activeIdx)

	avail := t.width
	if avail <= 0 {
		// No width constraint — render all tabs
		var tabs []string
		for i, item := range t.items {
			if i == t.activeIdx {
				tabs = append(tabs, ui.ActiveTabStyle.Render(item.Title))
			} else {
				tabs = append(tabs, ui.InactiveTabStyle.Render(item.Title))
			}
		}
		row := strings.Join(tabs, " ")
		return strings.TrimRight(lipgloss.NewStyle().Padding(0, 2, 0, 2).Render(row), "\n")
	}

	budget := avail - tabBarPadLeft*2
	hasLeft := t.scrollIdx > 0
	if hasLeft {
		budget -= arrowWidth
	}

	// Determine which tabs fit
	var visible []int
	used := 0
	for i := t.scrollIdx; i < len(t.items); i++ {
		need := widths[i]
		if len(visible) > 0 {
			need += tabGap
		}
		// Check if we need a right arrow
		if used+need > budget {
			break
		}
		// If adding this tab leaves no room for right arrow and there are more tabs
		if i < len(t.items)-1 && used+need+arrowWidth > budget {
			break
		}
		visible = append(visible, i)
		used += need
	}
	hasRight := len(visible) > 0 && visible[len(visible)-1] < len(t.items)-1

	// Build the row
	var parts []string
	if hasLeft {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowLeft))
	}
	for j, idx := range visible {
		item := t.items[idx]
		if idx == t.activeIdx {
			parts = append(parts, ui.ActiveTabStyle.Render(item.Title))
		} else {
			parts = append(parts, ui.InactiveTabStyle.Render(item.Title))
		}
		if j < len(visible)-1 {
			parts = append(parts, " ")
		}
	}
	if hasRight {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#888")).Render(arrowRight))
	}

	row := strings.Join(parts, "")
	padded := lipgloss.NewStyle().Padding(0, tabBarPadLeft, 0, tabBarPadLeft).MaxWidth(avail).Render(row)
	return strings.TrimRight(padded, "\n")
}
