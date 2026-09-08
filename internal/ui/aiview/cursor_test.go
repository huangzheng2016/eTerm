package aiview

import (
	"strings"
	"testing"
)

func TestViewReportsInputCursor(t *testing.T) {
	m := newTestModel(nil)
	m.Init()

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("chat view missing cursor")
	}
	if v.Cursor.X != 4 || v.Cursor.Y != 6 {
		t.Fatalf("cursor = %d,%d want 4,6", v.Cursor.X, v.Cursor.Y)
	}
	if m.input.VirtualCursor() {
		t.Fatal("virtual cursor still on: would double-render the caret")
	}
}

func TestViewCursorFollowsWrappedCJKCaret(t *testing.T) {
	m := newTestModel(nil)
	m.Init()
	fillConversation(m)

	m.input.SetValue(strings.Repeat("你好", 50))

	c := m.input.Cursor()
	if c == nil {
		t.Fatal("textarea reported no cursor")
	}
	if c.X != 20 || c.Y != 2 {
		t.Fatalf("textarea caret = %d,%d want 20,2", c.X, c.Y)
	}

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("chat view missing cursor")
	}
	_, _, _, vh := m.layout()
	wantX, wantY := 4+c.X, 5+vh+c.Y
	if v.Cursor.X != wantX || v.Cursor.Y != wantY {
		t.Fatalf("cursor = %d,%d want %d,%d", v.Cursor.X, v.Cursor.Y, wantX, wantY)
	}
}

func TestViewOmitsCursorOutsideChat(t *testing.T) {
	for _, md := range []mode{modeProviders, modeSessions, modeTasks} {
		m := newTestModel(nil)
		m.Init()
		m.mode = md
		if v := m.View(); v.Cursor != nil {
			t.Fatalf("mode %d: cursor = %v, want nil", md, v.Cursor)
		}
	}
}
