package aiview

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

func TestAlignBreaksWordWrap(t *testing.T) {
	breaks := alignBreaks([]string{"hello", "world foo"}, []string{"hello world foo"})
	if breaks[0].Kind != textselection.BreakNewline || breaks[1].Kind != textselection.BreakJoinSpace {
		t.Fatalf("breaks = %v", breaks)
	}
}

func TestAlignBreaksHardChop(t *testing.T) {
	breaks := alignBreaks([]string{"http://ab", "cdef"}, []string{"http://abcdef"})
	if breaks[1].Kind != textselection.BreakJoin {
		t.Fatalf("breaks = %v", breaks)
	}
}

func TestAlignBreaksMixedParagraph(t *testing.T) {
	breaks := alignBreaks(
		[]string{"foo bar", "http://lon", "gword end"},
		[]string{"foo bar http://longword end"},
	)
	want := []byte{textselection.BreakNewline, textselection.BreakJoinSpace, textselection.BreakJoin}
	for i, b := range want {
		if breaks[i].Kind != b {
			t.Fatalf("breaks = %v, want %v", breaks, want)
		}
	}
}

// The wrapper re-inserts its indent (document margin, list hanging indent)
// on continuation lines; the skip must be recorded so copies drop it.
func TestAlignBreaksSkipsInsertedIndent(t *testing.T) {
	breaks := alignBreaks(
		[]string{"  https://lon", "  gword"},
		[]string{"  https://longword"},
	)
	if breaks[1].Kind != textselection.BreakJoin || breaks[1].Skip != 2 {
		t.Fatalf("breaks = %v", breaks)
	}
	breaks = alignBreaks(
		[]string{"  foo", "  bar"},
		[]string{"  foo bar"},
	)
	if breaks[1].Kind != textselection.BreakJoinSpace || breaks[1].Skip != 2 {
		t.Fatalf("breaks = %v", breaks)
	}
}

func TestAlignBreaksRealAndBlankLines(t *testing.T) {
	breaks := alignBreaks(
		[]string{"line one", "", "line two"},
		[]string{"line one", "", "line two"},
	)
	for i, b := range breaks {
		if b.Kind != textselection.BreakNewline {
			t.Fatalf("breaks[%d] = %v, all real breaks expected: %v", i, b, breaks)
		}
	}
}

func TestAlignBreaksMismatchFallsBack(t *testing.T) {
	breaks := alignBreaks([]string{"xxx", "yyy"}, []string{"aaa"})
	for i, b := range breaks {
		if b.Kind != textselection.BreakNewline {
			t.Fatalf("breaks[%d] = %v, mismatch must fall back to real breaks", i, b)
		}
	}
}
