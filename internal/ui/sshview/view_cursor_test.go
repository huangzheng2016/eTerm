package sshview

import (
	"testing"
)

func TestViewReportsInnerCursor(t *testing.T) {
	m, _ := newProbeModel(t)
	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("hello")})
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("view missing hardware cursor")
	}
	if v.Cursor.X != 5 || v.Cursor.Y != 0 {
		t.Fatalf("cursor = %d,%d want 5,0", v.Cursor.X, v.Cursor.Y)
	}
}

func TestViewReportsHiddenCursorPosition(t *testing.T) {
	m, _ := newProbeModel(t)
	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("hello")})
	m.cursorHidden = true
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("hidden cursor position not reported")
	}
	if v.Cursor.X != 5 || v.Cursor.Y != 0 {
		t.Fatalf("cursor = %d,%d want 5,0", v.Cursor.X, v.Cursor.Y)
	}
}

func TestViewOmitsCursorWhenScrolled(t *testing.T) {
	m, _ := newProbeModel(t)
	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("hello")})
	m.scrollOffset = 1
	if v := m.View(); v.Cursor != nil {
		t.Fatal("scrollback view reported live cursor")
	}
}
