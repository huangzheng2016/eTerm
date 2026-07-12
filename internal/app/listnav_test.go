package app

import (
	"strings"
	"testing"
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

func TestRenderListLayoutKeepsCardsAndHighlightsSection(t *testing.T) {
	card := "[ host card ]"
	view := renderListLayout(ForwardTab, card, 100, 20)
	for _, want := range []string{"Hosts", "Keys", "Forwards", "Snippets", card} {
		if !strings.Contains(view, want) {
			t.Fatalf("list layout missing %q", want)
		}
	}
}

func TestNarrowListLayoutHidesSidebar(t *testing.T) {
	const card = "[ host card ]"
	if got := renderListLayout(HomeTab, card, 40, 20); got != card {
		t.Fatalf("narrow layout = %q, want card content only", got)
	}
}
