package aiview

import (
	"strings"

	"github.com/huangzheng2016/eTerm/internal/ui/textselection"
)

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
			if t := strings.TrimLeft(w, " "); t != w && strings.HasPrefix(rem, t) {
				skip = len(w) - len(t)
				w = t
			} else if strings.HasPrefix(strings.TrimRight(logical[j], " "), w) {
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
