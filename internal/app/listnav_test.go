package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestListNavigationCyclesResourceViews(t *testing.T) {
	a := App{tabs: []Tab{{Type: HomeTab, Title: "List"}}, activeTab: 0, width: 100, height: 30}

	a, _ = a.switchListView(1)
	if a.tabs[0].Type != KeyTab || a.tabs[0].Title != "List" {
		t.Fatalf("next list = %s %q, want keys List", a.tabs[0].Type, a.tabs[0].Title)
	}

	a, _ = a.switchListView(-1)
	if a.tabs[0].Type != HomeTab {
		t.Fatalf("previous list = %s, want home", a.tabs[0].Type)
	}
}

func TestListSidebarHasExactWidthAndSingleLineSeparators(t *testing.T) {
	view := ansi.Strip(renderListSidebar(HomeTab, 24))
	lines := strings.Split(view, "\n")
	if len(lines) != 24 {
		t.Fatalf("sidebar height = %d, want 24", len(lines))
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != listSidebarWidth {
			t.Fatalf("sidebar line %d width = %d, want %d: %q", i, got, listSidebarWidth, line)
		}
	}
	for _, row := range []int{3, 6, 9, 12, 15, 18} {
		if lines[row] != strings.Repeat("─", listSidebarWidth) {
			t.Fatalf("separator row %d = %q", row, lines[row])
		}
	}
}

func TestRenderListLayoutKeepsCardsAndHighlightsSection(t *testing.T) {
	card := "[ host card ]"
	view := renderListLayout(ForwardTab, card, 100, 20)
	for _, want := range []string{"Hosts", "Keys", "Forwards", "Snippets", "Sessions", card} {
		if !strings.Contains(view, want) {
			t.Fatalf("list layout missing %q", want)
		}
	}
	for i, line := range strings.Split(ansi.Strip(view), "\n") {
		if got := lipgloss.Width(line); got > 100 {
			t.Fatalf("layout line %d width = %d, exceeds 100", i, got)
		}
	}
}

func TestNarrowListLayoutHidesSidebar(t *testing.T) {
	const card = "[ host card ]"
	if got := renderListLayout(HomeTab, card, 40, 20); got != card {
		t.Fatalf("narrow layout = %q, want card content only", got)
	}
}
