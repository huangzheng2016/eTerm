package sshview

import (
	"testing"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func newCursorTestModel(t *testing.T) *Model {
	t.Helper()
	m := New(&internalssh.InteractiveSession{Stdin: newProbeStdin()}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(40, 10)
	return m
}

func TestViewReportsInnerCursor(t *testing.T) {
	m := newCursorTestModel(t)
	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("hello")})
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("view missing hardware cursor")
	}
	if v.Cursor.X != 5 || v.Cursor.Y != 0 {
		t.Fatalf("cursor = %d,%d want 5,0", v.Cursor.X, v.Cursor.Y)
	}
}

func TestViewOmitsCursorWhenHiddenOrScrolled(t *testing.T) {
	m := newCursorTestModel(t)
	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("hello")})
	m.cursorHidden = true
	if v := m.View(); v.Cursor != nil {
		t.Fatal("hidden cursor reported")
	}
	m.cursorHidden = false
	m.scrollOffset = 1
	if v := m.View(); v.Cursor != nil {
		t.Fatal("scrollback view reported live cursor")
	}
}
