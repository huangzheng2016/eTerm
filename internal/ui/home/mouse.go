package home

import (
	"charm.land/bubbles/v2/list"
)

// Mouse Y is already offset by App (tab row + divider line); do not subtract again.

// listMouseDefaults match bubbles list.NewDefaultDelegate(): Height 2, Spacing 1, and
// populatedView inserts (Spacing+1) newlines between items.
const (
	defaultDelegateItemHeight = 2
	defaultDelegateSpacing    = 1
)

func stridePerListItem() int {
	gapNL := defaultDelegateSpacing + 1
	return defaultDelegateItemHeight + gapNL
}

// globalIndexAtMouse returns the global index in VisibleItems for a cell-Y inside the
// connection list, after app-level Y offset (tab bar). localContentY is 0-based within
// the tab body (below tabs, above status bar).
func (m Model) globalIndexAtMouse(localContentY int) (int, bool) {
	listH := m.height - 2
	if listH < 1 {
		listH = 1
	}
	if localContentY < 0 || localContentY >= listH {
		return 0, false
	}

	paginationLines := 0
	if m.list.Paginator.TotalPages >= 2 {
		paginationLines = 1
	}
	const titleLines = 1
	if localContentY < titleLines || localContentY >= listH-paginationLines {
		return 0, false
	}

	yInItems := localContentY - titleLines
	stride := stridePerListItem()
	idxOnPage := yInItems / stride

	vis := m.list.VisibleItems()
	if len(vis) == 0 {
		return 0, false
	}
	start, end := m.list.Paginator.GetSliceBounds(len(vis))
	nPage := end - start
	if nPage <= 0 {
		return 0, false
	}
	if idxOnPage >= nPage {
		idxOnPage = nPage - 1
	}
	if idxOnPage < 0 {
		return 0, false
	}
	return start + idxOnPage, true
}

func screenYToContentY(msgY int) int {
	return msgY
}

func filteringBlocksMouse(m list.Model) bool {
	return m.FilterState() == list.Filtering
}
