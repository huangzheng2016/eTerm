package sshview

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func mkEmu(w, h int, s string) *vt.Emulator {
	e := vt.NewEmulator(w, h)
	e.WriteString(s)
	return e
}

func selModel(e *vt.Emulator, scroll int, anchor, caret selPoint) *Model {
	return &Model{emu: e, scrollOffset: scroll, sel: selection{active: true, anchor: anchor, caret: caret}}
}

func TestSelectedTextSingleLine(t *testing.T) {
	e := mkEmu(40, 24, "hello world\r\n")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{0, 4})
	if got := m.selectedText(); got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestSelectedTextMultiLine(t *testing.T) {
	e := mkEmu(40, 24, "line1\r\nline2\r\n")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{1, 4})
	if got := m.selectedText(); got != "line1\nline2" {
		t.Fatalf("got %q want %q", got, "line1\nline2")
	}
}

func TestSelectedTextTrimsTrailingSpaces(t *testing.T) {
	e := mkEmu(40, 24, "ab\r\n")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{0, 39})
	if got := m.selectedText(); got != "ab" {
		t.Fatalf("got %q want %q", got, "ab")
	}
}

func TestSelectedTextCJK(t *testing.T) {
	e := mkEmu(40, 24, "中文x\r\n")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{0, 4})
	if got := m.selectedText(); got != "中文x" {
		t.Fatalf("got %q want %q", got, "中文x")
	}
}

func TestSelectedTextScrollback(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\nL8\r\nL9\r\n")
	if e.ScrollbackLen() == 0 {
		t.Skip("no scrollback produced")
	}
	m := selModel(e, 0, selPoint{0, 0}, selPoint{0, 1})
	if got := m.selectedText(); got != "L0" {
		t.Fatalf("scrollback line 0 = %q want %q", got, "L0")
	}
}

func TestNormSelReversed(t *testing.T) {
	s := selection{anchor: selPoint{2, 3}, caret: selPoint{1, 5}}
	start, end := normSel(s)
	if start != (selPoint{1, 5}) || end != (selPoint{2, 3}) {
		t.Fatalf("normSel = %v,%v", start, end)
	}
}

func TestNormSelSameLine(t *testing.T) {
	s := selection{anchor: selPoint{1, 8}, caret: selPoint{1, 2}}
	start, end := normSel(s)
	if start != (selPoint{1, 2}) || end != (selPoint{1, 8}) {
		t.Fatalf("normSel = %v,%v", start, end)
	}
}

func TestInSelection(t *testing.T) {
	start, end := selPoint{1, 2}, selPoint{3, 4}
	cases := []struct {
		line, col int
		want      bool
	}{
		{0, 9, false},
		{1, 1, false},
		{1, 2, true},
		{2, 0, true},
		{2, 99, true},
		{3, 4, true},
		{3, 5, false},
		{4, 0, false},
	}
	for _, c := range cases {
		if got := inSelection(start, end, c.line, c.col); got != c.want {
			t.Errorf("inSelection(%d,%d) = %v want %v", c.line, c.col, got, c.want)
		}
	}
}

func TestVisibleAbsLineLiveView(t *testing.T) {
	e := mkEmu(20, 5, "a\r\nb\r\n")
	m := &Model{emu: e, scrollOffset: 0}
	sbLen := e.ScrollbackLen()
	for y := 0; y < 5; y++ {
		if got := m.visibleAbsLine(y); got != sbLen+y {
			t.Fatalf("visibleAbsLine(%d) = %d want %d", y, got, sbLen+y)
		}
	}
}

func TestVisibleAbsLineBottomPad(t *testing.T) {
	e := mkEmu(20, 5, "a\r\nb\r\n")
	sbLen := e.ScrollbackLen()
	m := &Model{emu: e, scrollOffset: 0, bottomPad: 2}
	for y := 0; y < 5; y++ {
		if got := m.visibleAbsLine(y); got != sbLen+2+y {
			t.Fatalf("visibleAbsLine(%d) = %d want %d", y, got, sbLen+2+y)
		}
	}
}

func wheel(b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg(tea.Mouse{X: 0, Y: 0, Button: b})
}

func TestWheelBottomPadStateTransitions(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\n")
	m := &Model{emu: e}

	// At live bottom, scrolling down enters bottomPad (clamped to max).
	m.Update(wheel(tea.MouseWheelDown))
	if m.bottomPad != bottomPadMax || m.scrollOffset != 0 {
		t.Fatalf("after down: bottomPad=%d scrollOffset=%d", m.bottomPad, m.scrollOffset)
	}

	// Scrolling up first collapses bottomPad before touching scrollback.
	m.Update(wheel(tea.MouseWheelUp))
	if m.bottomPad != 0 || m.scrollOffset != 0 {
		t.Fatalf("after up #1: bottomPad=%d scrollOffset=%d", m.bottomPad, m.scrollOffset)
	}

	// Next up enters scrollback history.
	m.Update(wheel(tea.MouseWheelUp))
	if m.scrollOffset == 0 {
		t.Fatalf("after up #2: expected scrollOffset>0, got %d", m.scrollOffset)
	}
}

func TestScrollbackRenderDoesNotSpaceCJK(t *testing.T) {
	e := mkEmu(20, 5, "这是中文\r\n")
	m := &Model{emu: e}
	got := renderScreenLine(m, 20, 0)
	if strings.Contains(got, "这 是") || strings.Contains(got, "中 文") {
		t.Fatalf("CJK text was spaced out: %q", got)
	}
	if !strings.Contains(got, "这是中文") {
		t.Fatalf("expected contiguous CJK text, got %q", got)
	}
}

func TestPlainTranscriptDoesNotSpaceCJK(t *testing.T) {
	e := mkEmu(20, 5, "这是中文\r\n")
	m := &Model{emu: e}
	got := m.PlainTranscript(MaxTranscriptBytes)
	if strings.Contains(got, "这 是") || strings.Contains(got, "中 文") {
		t.Fatalf("CJK text was spaced out: %q", got)
	}
	if !strings.Contains(got, "这是中文") {
		t.Fatalf("expected contiguous CJK text, got %q", got)
	}
}

func TestCursorKeyModeControlsArrowEncoding(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.emu.WriteString("\x1b[?1h")
	if got := string(m.encodeKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))); got != "\x1bOA" {
		t.Fatalf("application cursor up = %q", got)
	}

	m.emu.WriteString("\x1b[?1l")
	if got := string(m.encodeKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))); got != "\x1b[A" {
		t.Fatalf("normal cursor up = %q", got)
	}
}

type captureWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *captureWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *captureWriteCloser) Close() error { return nil }

func (w *captureWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestAltScreenMouseModeForwardsWheelSequence(t *testing.T) {
	stdin := &captureWriteCloser{}
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.emu.WriteString("\x1b[?1049h\x1b[?1000h\x1b[?1006h")
	m.Update(wheel(tea.MouseWheelDown))

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(stdin.String(), "\x1b[<") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := stdin.String()
	if !strings.Contains(got, "\x1b[<") {
		t.Fatalf("expected SGR mouse sequence, got %q", got)
	}
	if strings.Contains(got, "\x1b[B") {
		t.Fatalf("expected mouse sequence instead of arrow fallback, got %q", got)
	}
}

func TestPasteMsgWritesRawTextWhenBracketedPasteDisabled(t *testing.T) {
	stdin := &captureWriteCloser{}
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.Update(tea.PasteMsg{Content: "hello"})

	if got := stdin.String(); got != "hello" {
		t.Fatalf("paste = %q, want raw text", got)
	}
}

func TestPasteMsgWritesBracketedPasteWhenEnabled(t *testing.T) {
	stdin := &captureWriteCloser{}
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.emu.WriteString("\x1b[?2004h")
	if !m.bracketedPaste {
		t.Fatal("bracketed paste mode was not tracked")
	}

	m.Update(tea.PasteMsg{Content: "hello"})

	want := "\x1b[200~hello\x1b[201~"
	if got := stdin.String(); got != want {
		t.Fatalf("paste = %q, want %q", got, want)
	}
}

func TestPasteMsgStopsBracketingAfterModeReset(t *testing.T) {
	stdin := &captureWriteCloser{}
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	m.emu.WriteString("\x1b[?2004h")
	m.emu.WriteString("\x1b[?2004l")
	if m.bracketedPaste {
		t.Fatal("bracketed paste mode was not cleared")
	}

	m.Update(tea.PasteMsg{Content: "hello"})

	if got := stdin.String(); got != "hello" {
		t.Fatalf("paste = %q, want raw text", got)
	}
}

func TestKeyPressClearsBottomPad(t *testing.T) {
	e := mkEmu(20, 5, "x\r\n")
	m := &Model{emu: e, bottomPad: 2}
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if m.bottomPad != 0 {
		t.Fatalf("keypress should clear bottomPad, got %d", m.bottomPad)
	}
}

func TestVisibleAbsLineScrolled(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\nL8\r\nL9\r\n")
	sbLen := e.ScrollbackLen()
	if sbLen < 2 {
		t.Skip("need scrollback")
	}
	m := &Model{emu: e, scrollOffset: 2}
	// Top 2 visible rows come from scrollback (oldest of the shown window).
	if got := m.visibleAbsLine(0); got != sbLen-2 {
		t.Fatalf("visibleAbsLine(0) = %d want %d", got, sbLen-2)
	}
	if got := m.visibleAbsLine(1); got != sbLen-1 {
		t.Fatalf("visibleAbsLine(1) = %d want %d", got, sbLen-1)
	}
	// Row 2 is first screen line.
	if got := m.visibleAbsLine(2); got != sbLen {
		t.Fatalf("visibleAbsLine(2) = %d want %d", got, sbLen)
	}
}
