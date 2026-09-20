package sshview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestWheelForwardedInNormalScreenWhenMouseModeOn(t *testing.T) {
	m, stdin := newProbeModel(t)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte("\x1b[?1000h\x1b[?1006h"),
	})
	if m.emu.IsAltScreen() {
		t.Fatal("expected normal screen")
	}

	_, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelUp})
	stdin.waitContains(t, "\x1b[<64;11;6M")
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset=%d, want 0", m.scrollOffset)
	}
}

func TestWheelScrollsScrollbackWhenMouseModeOff(t *testing.T) {
	m, stdin := newProbeModel(t)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte(strings.Repeat("line\r\n", 30)),
	})
	if m.emu.ScrollbackLen() == 0 {
		t.Fatal("expected scrollback")
	}

	_, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelUp})
	if m.scrollOffset != 3 {
		t.Fatalf("scrollOffset=%d, want 3", m.scrollOffset)
	}
	time.Sleep(200 * time.Millisecond)
	if stdin.contains("\x1b[<64") || stdin.contains("\x1b[M`") {
		t.Fatal("wheel leaked to remote without mouse mode")
	}
}

func TestWheelAltScreenSendsArrowKeysWhenMouseModeOff(t *testing.T) {
	m, stdin := newProbeModel(t)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte("\x1b[?1049h"),
	})
	if !m.emu.IsAltScreen() {
		t.Fatal("expected alt screen")
	}

	_, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 5, Button: tea.MouseWheelDown})
	stdin.waitContains(t, "\x1b[B\x1b[B\x1b[B")
}
