package vt

import (
	"strings"
	"testing"
)

func TestOSC8HyperlinkLandsInCells(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	term.WriteString("\x1b]8;;https://example.com\aCLICK\x1b]8;;\a")

	for x := 0; x < 5; x++ {
		c := term.CellAt(x, 0)
		if c == nil || c.Link.URL != "https://example.com" {
			t.Fatalf("cell %d link = %+v", x, c)
		}
	}
	if c := term.CellAt(5, 0); c != nil && c.Link.URL != "" {
		t.Fatalf("link leaked past reset: %+v", c.Link)
	}

	// Params survive the round trip.
	term.WriteString("\x1b]8;id=1;https://example.com/2\aX\x1b]8;;\a")
	c := term.CellAt(5, 0)
	if c == nil || c.Link.URL != "https://example.com/2" || c.Link.Params != "id=1" {
		t.Fatalf("cell link = %+v", c)
	}
}

func TestOSC8HyperlinkRenderRoundTrip(t *testing.T) {
	term := newTestTerminal(t, 80, 24)
	term.WriteString("\x1b]8;;https://example.com\aCLICK\x1b]8;;\a")

	got := term.Render()
	want := "\x1b]8;;https://example.com\aCLICK\x1b]8;;\a"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("render = %q want prefix %q", got, want)
	}
}
