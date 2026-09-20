package aiview

import (
	"slices"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

func TestAlignBreaks(t *testing.T) {
	nl := textselection.LineBreak{Kind: textselection.BreakNewline}
	cases := []struct {
		name             string
		wrapped, logical []string
		want             []textselection.LineBreak
	}{
		{"word wrap", []string{"hello", "world foo"}, []string{"hello world foo"},
			[]textselection.LineBreak{nl, {Kind: textselection.BreakJoinSpace}}},
		{"hard chop", []string{"http://ab", "cdef"}, []string{"http://abcdef"},
			[]textselection.LineBreak{nl, {Kind: textselection.BreakJoin}}},
		{"mixed paragraph", []string{"foo bar", "http://lon", "gword end"}, []string{"foo bar http://longword end"},
			[]textselection.LineBreak{nl, {Kind: textselection.BreakJoinSpace}, {Kind: textselection.BreakJoin}}},
		{"skips inserted indent on join", []string{"  https://lon", "  gword"}, []string{"  https://longword"},
			[]textselection.LineBreak{nl, {Kind: textselection.BreakJoin, Skip: 2}}},
		{"skips inserted indent on space join", []string{"  foo", "  bar"}, []string{"  foo bar"},
			[]textselection.LineBreak{nl, {Kind: textselection.BreakJoinSpace, Skip: 2}}},
		{"real and blank lines", []string{"line one", "", "line two"}, []string{"line one", "", "line two"},
			[]textselection.LineBreak{nl, nl, nl}},
		{"mismatch falls back", []string{"xxx", "yyy"}, []string{"aaa"},
			[]textselection.LineBreak{nl, nl}},
	}
	for _, tc := range cases {
		if got := alignBreaks(tc.wrapped, tc.logical); !slices.Equal(got, tc.want) {
			t.Errorf("%s: breaks = %v, want %v", tc.name, got, tc.want)
		}
	}
}
