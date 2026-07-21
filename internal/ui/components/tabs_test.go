package components

import (
	"strings"
	"testing"
)

func testTabs() []TabItem {
	return []TabItem{
		{Title: "one", ID: "1"},
		{Title: "two", ID: "2"},
		{Title: "three", ID: "3"},
		{Title: "four", ID: "4"},
	}
}

func TestTabsScrollChangesVisibleRange(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18)
	before := tabs.View()
	tabs = tabs.ScrollRight()
	after := tabs.View()
	if before == after || !strings.Contains(after, "two") || strings.Contains(after, "one") {
		t.Fatalf("scroll did not change visible range:\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestTabsScrollRightStopsWhenLastTabIsVisible(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18)
	for range 10 {
		tabs = tabs.ScrollRight()
	}
	stoppedAt := tabs.scrollIdx
	tabs = tabs.ScrollRight()
	if tabs.scrollIdx != stoppedAt {
		t.Fatalf("scroll index advanced from %d to %d", stoppedAt, tabs.scrollIdx)
	}
	if !strings.Contains(tabs.View(), "four") {
		t.Fatalf("last visible range = %q", tabs.View())
	}
}

func TestTabsWideBarDoesNotScroll(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(80).ScrollRight()
	if tabs.scrollIdx != 0 {
		t.Fatalf("scroll index = %d, want 0", tabs.scrollIdx)
	}
}

func TestTabsWiderBarClampsExistingScroll(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18).ScrollRight().SetWidth(80)
	if tabs.scrollIdx != 0 {
		t.Fatalf("scroll index = %d, want 0", tabs.scrollIdx)
	}
}

func TestTabsArrowClicksAndScrolledTabHitRegion(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18).ScrollRight()
	tabs, changed := tabs.HandleClick(2)
	if changed || tabs.scrollIdx != 0 {
		t.Fatalf("left arrow click = scroll %d, changed %v", tabs.scrollIdx, changed)
	}

	tabs, changed = tabs.HandleClick(14)
	if changed || tabs.scrollIdx != 1 {
		t.Fatalf("right arrow click = scroll %d, changed %v", tabs.scrollIdx, changed)
	}

	tabs, changed = tabs.HandleClick(4)
	if !changed || tabs.ActiveIndex() != 1 {
		t.Fatalf("visible tab click = active %d, changed %v", tabs.ActiveIndex(), changed)
	}
}

func TestTabsSetItemsPreservesAndClampsScroll(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18).ScrollRight().ScrollRight()
	tabs = tabs.SetItems(testTabs()[:2])
	if tabs.scrollIdx != 0 {
		t.Fatalf("clamped scroll index = %d, want 0", tabs.scrollIdx)
	}
	if tabs.ActiveIndex() != 0 {
		t.Fatalf("active index = %d, want 0", tabs.ActiveIndex())
	}
}

func TestTabsSetActiveMakesSelectedTabVisible(t *testing.T) {
	tabs := NewTabs(testTabs()).SetWidth(18).SetActive(3)
	view := tabs.View()
	if !strings.Contains(view, "four") || strings.Contains(view, "one") {
		t.Fatalf("active tab is not visible: %q", view)
	}
}

func TestTabsNarrowLongTitleRendersClickableNavigation(t *testing.T) {
	tabs := NewTabs([]TabItem{
		{Title: "a very long first tab", ID: "1"},
		{Title: "second", ID: "2"},
	}).SetWidth(8)

	view := tabs.View()
	if !strings.Contains(view, ">") {
		t.Fatalf("narrow tab bar has no navigation affordance: %q", view)
	}

	tabs, changed := tabs.HandleClick(2)
	if changed || tabs.scrollIdx != 1 {
		t.Fatalf("rendered right arrow click = scroll %d, changed %v", tabs.scrollIdx, changed)
	}
	if !strings.Contains(tabs.View(), "<") {
		t.Fatalf("scrolled narrow tab bar has no left navigation affordance: %q", tabs.View())
	}
}
