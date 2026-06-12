package sshview

import (
	"strings"
	"unicode/utf8"
)

const MaxTranscriptBytes = 512 * 1024

// PlainTranscript returns scrollback plus visible screen as plain text (no ANSI), truncated to maxBytes.
func (m *Model) PlainTranscript(maxBytes int) string {
	if m == nil || m.emu == nil {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = MaxTranscriptBytes
	}
	var b strings.Builder
	b.Grow(min(maxBytes, 65536))
	w := m.emu.Width()
	h := m.emu.Height()
	if w <= 0 || h <= 0 {
		return ""
	}
	sbLen := m.emu.ScrollbackLen()
	for i := 0; i < sbLen; i++ {
		line := plainLineFromScrollback(m, w, i)
		if b.Len()+len(line)+1 > maxBytes {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for y := 0; y < h; y++ {
		line := plainLineFromScreen(m, w, y)
		if b.Len()+len(line)+1 > maxBytes {
			break
		}
		b.WriteString(line)
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	s := b.String()
	if len(s) > maxBytes {
		s = s[:maxBytes]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func plainLineFromScrollback(m *Model, w, idx int) string {
	var b strings.Builder
	for x := 0; x < w; x++ {
		cell := m.emu.ScrollbackCellAt(x, idx)
		if cell != nil && cell.Width == 0 {
			continue
		}
		if cell == nil || cell.Content == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(cell.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func plainLineFromScreen(m *Model, w, y int) string {
	var b strings.Builder
	for x := 0; x < w; x++ {
		cell := m.emu.CellAt(x, y)
		if cell != nil && cell.Width == 0 {
			continue
		}
		if cell == nil || cell.Content == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(cell.Content)
		}
	}
	return strings.TrimRight(b.String(), " ")
}
