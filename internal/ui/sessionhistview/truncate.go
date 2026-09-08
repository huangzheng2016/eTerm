package sessionhistview

import "unicode/utf8"

func truncateUTF8BytesEllipsis(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return "…"
	}
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
