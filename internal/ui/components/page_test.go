package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestEmptyStateCentersBlock(t *testing.T) {
	out := EmptyState(40, 9, "No connections.", "n: new host", "?: all keys")
	lines := strings.Split(out, "\n")

	if len(lines) != 9 {
		t.Fatalf("height: got %d want 9", len(lines))
	}
	assertCenteredLine(t, lines, 40, "No connections.")
	assertCenteredLine(t, lines, 40, "n: new host")
	assertCenteredLine(t, lines, 40, "?: all keys")
	if strings.HasPrefix(findLine(lines, "n: new host"), "n:") {
		t.Fatalf("hint line is not centered: %q", findLine(lines, "n: new host"))
	}
}

func TestLoadingUsesSameCenteredTemplate(t *testing.T) {
	out := Loading(32, 7, "Loading session history")
	lines := strings.Split(out, "\n")

	if len(lines) != 7 {
		t.Fatalf("height: got %d want 7", len(lines))
	}
	assertCenteredLine(t, lines, 32, "Loading session history")
}

func TestEmptyStateTruncatesLongLines(t *testing.T) {
	out := EmptyState(12, 5, "This is a very long empty state line")

	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 12 {
			t.Fatalf("line width %d exceeds 12: %q", lipgloss.Width(line), line)
		}
	}
}

func TestPageCentersBodyBetweenHeaderAndFooter(t *testing.T) {
	out := Page{
		Width:      30,
		Height:     7,
		Header:     "HEADER",
		Body:       "EMPTY",
		Footer:     "FOOTER",
		CenterBody: true,
	}.Render()
	lines := strings.Split(out, "\n")

	if len(lines) != 7 {
		t.Fatalf("height: got %d want 7", len(lines))
	}
	if lines[0] != "HEADER" {
		t.Fatalf("header: %q", lines[0])
	}
	if strings.TrimSpace(lines[3]) != "EMPTY" {
		t.Fatalf("body: %#v", lines)
	}
	if lines[6] != "FOOTER" {
		t.Fatalf("footer: %q", lines[6])
	}
}

func assertCenteredLine(t *testing.T, lines []string, width int, want string) {
	t.Helper()
	line := findLine(lines, want)
	if line == "" {
		t.Fatalf("missing line %q in %#v", want, lines)
	}
	got := leadingSpaces(line)
	expected := (width - lipgloss.Width(want)) / 2
	if got != expected && got != expected+1 {
		t.Fatalf("line %q starts at %d, want around %d", line, got, expected)
	}
}

func findLine(lines []string, text string) string {
	for _, line := range lines {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}
