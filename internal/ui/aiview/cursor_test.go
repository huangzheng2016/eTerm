package aiview

import (
	"strings"
	"testing"
)

// The chat view must report a real cursor at the textarea caret so the
// terminal hardware cursor (and IME candidate window) tracks the input.
func TestViewReportsInputCursor(t *testing.T) {
	m := newTestModel(nil)
	m.Init()

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("chat view missing cursor")
	}
	// Empty input: caret at textarea origin; body is the 1-row placeholder.
	// x = border(1)+padding(1)+input border(1)+padding(1) = 4
	// y = border(1)+title(1)+blank(1)+body(1)+blank(1)+input border(1) = 6
	if v.Cursor.X != 4 || v.Cursor.Y != 6 {
		t.Fatalf("cursor = %d,%d want 4,6", v.Cursor.X, v.Cursor.Y)
	}
	if m.input.VirtualCursor() {
		t.Fatal("virtual cursor still on: would double-render the caret")
	}
}

// CJK input wraps mid-box; the reported cursor must land on the wrapped
// row/column, not the logical line (the reported misalignment bug).
func TestViewCursorFollowsWrappedCJKCaret(t *testing.T) {
	m := newTestModel(nil)
	m.Init()
	fillConversation(m) // body becomes the full-height viewport

	// 100 runes = 200 cells; input wrap width is cw-6 = 90, so the caret
	// sits on wrapped row 2 at cell 20.
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

// Non-chat modes have no text input; they must not report a cursor.
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
