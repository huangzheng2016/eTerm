package textselection

import "testing"

func TestTextAcrossLinesAndReversed(t *testing.T) {
	s := Selection{Active: true, Anchor: Point{Line: 1, Col: 2}, Caret: Point{Line: 0, Col: 1}}
	if got := s.Text([]string{"abcd", "efgh"}); got != "bcd\nefg" {
		t.Fatalf("got %q", got)
	}
}

func TestTextUsesTerminalColumnsForWideCharacters(t *testing.T) {
	s := Selection{Active: true, Anchor: Point{Line: 0, Col: 2}, Caret: Point{Line: 0, Col: 3}}
	if got := s.Text([]string{"中文x"}); got != "文" {
		t.Fatalf("got %q", got)
	}
}
