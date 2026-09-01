package sshview

import (
	"bytes"
	"fmt"
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

func TestSelectedTextJoinsSoftWrappedLines(t *testing.T) {
	e := mkEmu(4, 4, "abcdefgh")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{1, 3})
	if got := m.selectedText(); got != "abcdefgh" {
		t.Fatalf("got %q want %q", got, "abcdefgh")
	}
}

func TestSelectedTextKeepsHardNewlineAfterFullLine(t *testing.T) {
	e := mkEmu(4, 4, "abcd\r\nefgh")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{1, 3})
	if got := m.selectedText(); got != "abcd\nefgh" {
		t.Fatalf("got %q want %q", got, "abcd\nefgh")
	}
}

func TestSelectedTextJoinsSoftWrapAcrossScrollback(t *testing.T) {
	e := mkEmu(4, 2, "abcdefghijkl")
	if e.ScrollbackLen() != 1 {
		t.Fatalf("scrollback length = %d want 1", e.ScrollbackLen())
	}
	m := selModel(e, 0, selPoint{0, 0}, selPoint{2, 3})
	if got := m.selectedText(); got != "abcdefghijkl" {
		t.Fatalf("got %q want %q", got, "abcdefghijkl")
	}
}

func TestSelectedTextJoinsSoftWrapOnAltScreen(t *testing.T) {
	e := mkEmu(4, 4, "\x1b[?1049habcdefgh")
	m := selModel(e, 0, selPoint{0, 0}, selPoint{1, 3})
	if got := m.selectedText(); got != "abcdefgh" {
		t.Fatalf("got %q want %q", got, "abcdefgh")
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

func TestHiddenCursorDoesNotRenderManualCursor(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("abc\x1b[?25l")})

	if !m.cursorHidden {
		t.Fatal("expected cursor hidden")
	}
	if got, want := m.renderScreenWithCursor(), m.emu.Render(); got != want {
		t.Fatalf("hidden cursor rendered extra cell:\ngot  %q\nwant %q", got, want)
	}
}

func TestShowCursorRestoresManualCursor(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	_, _ = m.Update(ChunkMsg{StreamID: m.StreamID(), Data: []byte("abc\x1b[?25l\x1b[?25h")})

	if m.cursorHidden {
		t.Fatal("expected cursor visible")
	}
	if got, want := m.renderScreenWithCursor(), m.emu.Render(); got == want {
		t.Fatal("visible cursor was not rendered")
	}
}

func TestAltScreenRenderKeepsClearedCells(t *testing.T) {
	m := New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(20, 4)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte("\x1b[?1049hABCDEFGHIJ\x1b[Habc\x1b[K\x1b[?25l"),
	})

	got := m.renderScreenWithCursor()
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d want 4: %q", len(lines), got)
	}
	if lines[0] != "abc"+strings.Repeat(" ", 17) {
		t.Fatalf("first line = %q", lines[0])
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] != strings.Repeat(" ", 20) {
			t.Fatalf("line %d = %q", i, lines[i])
		}
	}
	if strings.Contains(got, "DEFG") {
		t.Fatalf("cleared cells leaked old content: %q", got)
	}
}

func TestScrollbackViewShowsCompactTopRightPosition(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\n")
	m := &Model{emu: e, scrollOffset: 2, scrollIndicatorUntil: time.Now().Add(time.Second)}

	view := m.View().Content
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("line count = %d want 5:\n%s", len(lines), view)
	}
	want := fmt.Sprintf("[%d/%d]", m.scrollOffset, e.ScrollbackLen())
	if !strings.Contains(lines[0], want) {
		t.Fatalf("top line missing %q:\n%s", want, lines[0])
	}
	if strings.Contains(view, "scrollback") || strings.Contains(view, "lines up") {
		t.Fatalf("view contains old scrollback banner:\n%s", view)
	}
}

func TestScrollbackViewHidesExpiredPosition(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\n")
	m := &Model{emu: e, scrollOffset: 2, scrollIndicatorUntil: time.Now().Add(-time.Second)}

	view := m.View().Content
	want := fmt.Sprintf("[%d/%d]", m.scrollOffset, e.ScrollbackLen())
	if strings.Contains(view, want) {
		t.Fatalf("view contains expired indicator %q:\n%s", want, view)
	}
}

func TestWheelScrollbackShowsPositionTemporarily(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\n")
	m := &Model{emu: e}

	_, cmd := m.Update(wheel(tea.MouseWheelUp))

	if m.scrollOffset == 0 {
		t.Fatal("expected scrollOffset > 0")
	}
	if cmd == nil {
		t.Fatal("expected timeout command")
	}
	if !m.scrollIndicatorUntil.After(time.Now()) {
		t.Fatalf("scrollIndicatorUntil = %v", m.scrollIndicatorUntil)
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

func (w *captureWriteCloser) waitString(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := w.String(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if got := w.String(); got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
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

	stdin.waitString(t, "hello")
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
	stdin.waitString(t, want)
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

	stdin.waitString(t, "hello")
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

func TestDragSelectionAtTopStartsAutoScroll(t *testing.T) {
	e := mkEmu(20, 5, "L0\r\nL1\r\nL2\r\nL3\r\nL4\r\nL5\r\nL6\r\nL7\r\nL8\r\nL9\r\n")
	if e.ScrollbackLen() == 0 {
		t.Skip("need scrollback")
	}
	m := &Model{
		emu: e,
		sel: selection{active: true, dragging: true, anchor: selPoint{line: e.ScrollbackLen(), col: 0}, caret: selPoint{line: e.ScrollbackLen(), col: 0}},
	}

	_, cmd := m.Update(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: 0}))
	if cmd == nil {
		t.Fatal("expected drag at top to schedule auto-scroll")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(*Model)
	if m.scrollOffset == 0 {
		t.Fatal("expected auto-scroll to enter scrollback")
	}
}

func TestDragSelectionAutoScrollUsesFivePercentEdge(t *testing.T) {
	e := mkEmu(20, 100, strings.Repeat("x\r\n", 110))
	m := &Model{emu: e, sel: selection{active: true, dragging: true}}

	_, cmd := m.Update(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: 4}))
	if cmd == nil {
		t.Fatal("expected auto-scroll inside 5 percent edge")
	}

	m.selAutoScroll.Dir = 0
	m.selAutoScroll.Queued = false
	_, cmd = m.Update(tea.MouseMotionMsg(tea.Mouse{X: 0, Y: 5}))
	if cmd != nil || m.selAutoScroll.Dir != 0 {
		t.Fatal("unexpected auto-scroll outside 5 percent edge")
	}
}
