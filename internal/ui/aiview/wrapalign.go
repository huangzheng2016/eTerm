package aiview

import (
	"strings"

	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

// alignBreaks marks how each wrapped display line connects to the previous
// one. Both line sets come from the same source text: the wrapped render at
// the content width and the unwrapped logical lines. A wrapper only deletes
// break-point spaces and re-inserts its indent on continuations, so a wrapped
// line is always a prefix of what remains of the current logical line once
// any inserted indent is skipped. A leading space in the remainder means the
// break ate a word-boundary space (BreakJoinSpace) rather than chopping a
// word (BreakJoin). On any mismatch the rest of the block falls back to real
// breaks, which keeps today's copy behavior.
func alignBreaks(wrapped, logical []string) []textselection.LineBreak {
	breaks := make([]textselection.LineBreak, len(wrapped))
	if len(logical) == 0 {
		return breaks
	}
	j := 0
	rem := strings.TrimRight(logical[0], " ")
	joinSpace := false
	for i, w0 := range wrapped {
		w0 = strings.TrimRight(w0, " ")
		if w0 == "" {
			if rem == "" && j+1 < len(logical) {
				j++
				rem = strings.TrimRight(logical[j], " ")
			}
			joinSpace = false
			continue
		}
		w := w0
		skip := 0
		if !strings.HasPrefix(rem, w) {
			// The wrapper may have re-inserted its indent (document margin,
			// list hanging indent) on the continuation line.
			if t := strings.TrimLeft(w, " "); t != w && strings.HasPrefix(rem, t) {
				skip = len(w) - len(t) // spaces: 1 cell each
				w = t
			} else if strings.HasPrefix(strings.TrimRight(logical[j], " "), w) {
				// Resync on the current logical line: a real break.
				rem = strings.TrimRight(logical[j], " ")
			} else {
				return breaks
			}
		}
		if rem != strings.TrimRight(logical[j], " ") {
			if joinSpace {
				breaks[i] = textselection.LineBreak{Kind: textselection.BreakJoinSpace, Skip: skip}
			} else {
				breaks[i] = textselection.LineBreak{Kind: textselection.BreakJoin, Skip: skip}
			}
		}
		rem = rem[len(w):]
		joinSpace = strings.HasPrefix(rem, " ")
		rem = strings.TrimLeft(rem, " ")
		if rem == "" && j+1 < len(logical) {
			j++
			rem = strings.TrimRight(logical[j], " ")
			joinSpace = false
		}
	}
	return breaks
}
