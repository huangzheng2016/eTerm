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

func TestAutoScrollEdgeBands(t *testing.T) {
	var a AutoScroll
	// height 100: band = 5 rows at each end.
	if !a.Update(0, 100) || a.Dir != -1 {
		t.Fatal("top edge must scroll up")
	}
	if !a.Update(4, 100) || a.Dir != -1 {
		t.Fatal("row 4 is still inside the top band")
	}
	if a.Update(5, 100) {
		t.Fatal("row 5 is outside the band")
	}
	if a.Dir != 0 {
		t.Fatal("Dir must reset outside the band")
	}
	if !a.Update(95, 100) || a.Dir != 1 {
		t.Fatal("bottom edge must scroll down")
	}
	if !a.Update(99, 100) || a.Dir != 1 {
		t.Fatal("last row is inside the bottom band")
	}
	// Small heights clamp to a 2-row band.
	if !a.Update(1, 10) || a.Dir != -1 {
		t.Fatal("clamped band must cover row 1")
	}
	if a.Update(2, 10) {
		t.Fatal("row 2 is outside the clamped band")
	}
	// Zero height never scrolls.
	if a.Update(0, 0) {
		t.Fatal("zero height must not scroll")
	}
}

func TestAutoScrollStop(t *testing.T) {
	a := AutoScroll{Dir: 1, Queued: true}
	a.Stop()
	if a.Dir != 0 || a.Queued {
		t.Fatal("Stop must clear Dir and Queued")
	}
}

func TestTextJoinedHonorsBreakKinds(t *testing.T) {
	s := Selection{Active: true, Anchor: Point{Line: 0, Col: 0}, Caret: Point{Line: 3, Col: 99}}
	lines := []string{"foo", "  bar", "http://ab", "  cdef"}
	breaks := []LineBreak{
		{Kind: BreakNewline},
		{Kind: BreakJoinSpace, Skip: 2},
		{Kind: BreakNewline},
		{Kind: BreakJoin, Skip: 2},
	}
	if got := s.TextJoined(lines, breaks); got != "foo bar\nhttp://abcdef" {
		t.Fatalf("got %q", got)
	}
	// Nil breaks keeps the plain per-line behavior.
	if got := s.TextJoined(lines, nil); got != "foo\n  bar\nhttp://ab\n  cdef" {
		t.Fatalf("got %q", got)
	}
}
