package voice

import (
	"encoding/json"
	"strings"
	"unicode"
)

const (
	// DefaultContextMaxTokens is the corpus.context budget (Volcano docs:
	// context is capped at 800 tokens, excess is dropped server-side).
	DefaultContextMaxTokens = 800
	// DefaultContextMaxTurns caps context_data entries (Volcano docs: 20 turns).
	DefaultContextMaxTurns = 20
	// DefaultContextMaxLines caps terminal transcript lines fed into context.
	DefaultContextMaxLines = 20
)

const (
	contextSpeakerUser = "user"
	contextSpeakerBot  = "bot"
)

// ContextTurn is one dialog_ctx entry for the Volcano corpus.context field.
type ContextTurn struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type dialogContext struct {
	Hotwords    []struct{}    `json:"hotwords"`
	ContextType string        `json:"context_type"`
	ContextData []ContextTurn `json:"context_data"`
}

// BuildDialogContext marshals turns into the official dialog_ctx JSON string.
// Turns over the token budget are dropped oldest-first; a single oversized
// turn is truncated. Returns "" when nothing fits.
func BuildDialogContext(turns []ContextTurn, maxTokens int) string {
	if len(turns) == 0 {
		return ""
	}
	if maxTokens <= 0 {
		maxTokens = DefaultContextMaxTokens
	}
	turns = trimContextTurns(turns, maxTokens)
	if len(turns) == 0 {
		return ""
	}
	return marshalContext(turns)
}

func trimContextTurns(turns []ContextTurn, maxTokens int) []ContextTurn {
	for len(turns) > 1 && estimateContextTokens(marshalContext(turns)) > maxTokens {
		turns = turns[1:]
	}
	if len(turns) == 0 {
		return nil
	}
	if estimateContextTokens(marshalContext(turns)) <= maxTokens {
		return turns
	}
	// One turn alone exceeds the budget: truncate its text.
	runes := []rune(turns[0].Text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		turns[0].Text = string(runes[:mid])
		if estimateContextTokens(marshalContext(turns)) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	turns[0].Text = string(runes[:lo])
	return turns
}

func marshalContext(turns []ContextTurn) string {
	dc := dialogContext{Hotwords: []struct{}{}, ContextType: "dialog_ctx", ContextData: turns}
	body, err := json.Marshal(dc)
	if err != nil {
		return ""
	}
	return string(body)
}

// estimateContextTokens roughly counts Volcano context tokens: one per CJK
// rune, one per 4 ASCII bytes.
func estimateContextTokens(s string) int {
	cjk, ascii := 0, 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk++
		case r > unicode.MaxASCII:
			cjk++
		default:
			ascii++
		}
	}
	return cjk + (ascii+3)/4
}

// CleanTerminalContextLines normalizes raw terminal transcript lines for
// context: strips box-drawing/border runes, collapses whitespace, drops lines
// without letters/digits/CJK and adjacent duplicates, keeps the last maxLines.
func CleanTerminalContextLines(lines []string, maxLines int) []string {
	if maxLines <= 0 {
		maxLines = DefaultContextMaxLines
	}
	out := make([]string, 0, maxLines)
	prev := ""
	for _, raw := range lines {
		s := cleanContextLine(raw)
		if s == "" || !hasContextContent(s) || s == prev {
			continue
		}
		out = append(out, s)
		prev = s
	}
	if len(out) > maxLines {
		out = out[len(out)-maxLines:]
	}
	return out
}

func cleanContextLine(s string) string {
	var b []rune
	for _, r := range s {
		if isStructuralRune(r) {
			continue
		}
		b = append(b, r)
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

func isStructuralRune(r rune) bool {
	switch {
	case r >= 0x2500 && r <= 0x257F: // Box Drawing
		return true
	case r >= 0x2580 && r <= 0x259F: // Block Elements
		return true
	case r >= 0x25A0 && r <= 0x25FF: // Geometric Shapes
		return true
	case r >= 0xE0A0 && r <= 0xE0FF: // Powerline private use
		return true
	}
	return false
}

func hasContextContent(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return true
		}
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}
