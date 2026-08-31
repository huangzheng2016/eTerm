package sshview

import (
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

const (
	linkSeqOpen  = "\x1b]8;;https://example.com\a"
	linkSeqClose = "\x1b]8;;\a"
)

func TestAltScreenRenderKeepsHyperlinks(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(20, 4)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte("\x1b[?1049h" + linkSeqOpen + "CLICK" + linkSeqClose),
	})

	got := m.renderScreen()
	if !strings.Contains(got, linkSeqOpen+"CLICK"+linkSeqClose) {
		t.Fatalf("alt-screen render dropped hyperlink: %q", got)
	}
}

func TestScrollbackRenderKeepsHyperlinks(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(20, 4)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte(linkSeqOpen + "CLICK" + linkSeqClose + "\r\n1\r\n2\r\n3\r\n4\r\n5\r\n"),
	})
	if m.emu.ScrollbackLen() == 0 {
		t.Fatal("no scrollback produced")
	}

	got := renderScrollbackLine(m, 20, 0)
	if !strings.Contains(got, linkSeqOpen+"CLICK"+linkSeqClose) {
		t.Fatalf("scrollback render dropped hyperlink: %q", got)
	}

	m.scrollOffset = m.emu.ScrollbackLen()
	if got := m.renderScrollback(); !strings.Contains(got, linkSeqOpen+"CLICK"+linkSeqClose) {
		t.Fatalf("scrollback view dropped hyperlink: %q", got)
	}
}

func TestSelectionRenderKeepsHyperlinks(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(20, 4)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte(linkSeqOpen + "CLICK" + linkSeqClose),
	})

	// Select the middle of the link so both the styled-selection and the
	// plain cell branches run inside the same linked run.
	sbLen := m.emu.ScrollbackLen()
	m.sel = selection{active: true, anchor: selPoint{line: sbLen, col: 1}, caret: selPoint{line: sbLen, col: 3}}
	got := m.renderWithSelection()
	if !strings.Contains(got, linkSeqOpen) {
		t.Fatalf("selection render dropped hyperlink open: %q", got)
	}
	if !strings.Contains(got, linkSeqClose) {
		t.Fatalf("selection render dropped hyperlink close: %q", got)
	}
}
