package aiview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func newSelectionModel() *Model {
	m := newTestModel(nil) // 100x32
	m.blocks = append(m.blocks,
		block{kind: blockUser, text: "hello world"},
		block{kind: blockSystem, text: "system note"},
	)
	m.renderAll()
	return m
}

func mouse(x, y int) tea.Mouse { return tea.Mouse{X: x, Y: y, Button: tea.MouseLeft} }

func TestDragSelectCopiesPlainText(t *testing.T) {
	m := newSelectionModel()

	// Viewport row 0 is overlay-local y=3; content col 0 is x=2.
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(10, 3)))
	if !m.sel.Active || !m.sel.Dragging {
		t.Fatal("drag did not activate selection")
	}
	if !strings.Contains(m.viewport.GetContent(), "\x1b[7m") {
		t.Fatal("selection not highlighted with reverse video")
	}

	_, cmd := m.Update(tea.MouseReleaseMsg(mouse(10, 3)))
	if cmd == nil {
		t.Fatal("release after drag returned no clipboard cmd")
	}
	text := m.sel.Text(strings.Split(m.viewport.GetContent(), "\n"))
	if text != "You: hell" {
		t.Fatalf("selected text = %q, want %q", text, "You: hell")
	}
	if m.sel.Dragging {
		t.Fatal("still dragging after release")
	}
}

func TestDragSelectAcrossLines(t *testing.T) {
	m := newSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(3, 3)))  // line 0, col 1
	m.Update(tea.MouseMotionMsg(mouse(8, 5))) // line 2, col 6
	_, cmd := m.Update(tea.MouseReleaseMsg(mouse(8, 5)))
	if cmd == nil {
		t.Fatal("release after drag returned no clipboard cmd")
	}
	text := m.sel.Text(strings.Split(m.viewport.GetContent(), "\n"))
	if text != "ou: hello world\n\nsystem" {
		t.Fatalf("selected text = %q", text)
	}
}

func TestDragSelectCopyShowsToast(t *testing.T) {
	m := newSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(10, 3)))
	_, cmd := m.Update(tea.MouseReleaseMsg(mouse(10, 3)))
	if cmd == nil {
		t.Fatal("release after drag returned no clipboard cmd")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd msg = %T, want tea.BatchMsg", cmd())
	}
	var success *types.SuccessMsg
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(types.SuccessMsg); ok {
			m := msg
			success = &m
		}
	}
	if success == nil || success.Message != "Copied 9 chars" {
		t.Fatalf("success msg = %+v", success)
	}
}

func TestClickWithoutDragSelectsNothing(t *testing.T) {
	m := newSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(5, 3)))
	_, cmd := m.Update(tea.MouseReleaseMsg(mouse(5, 3)))
	if cmd != nil {
		t.Fatal("click without drag should not copy")
	}
	if m.sel.Active {
		t.Fatal("click without drag left an active selection")
	}
}

func TestEscClearsSelection(t *testing.T) {
	m := newSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(10, 3)))
	m.Update(keyMsg(tea.KeyEscape, 0))
	if m.sel.Active {
		t.Fatal("esc did not clear the selection")
	}
	if strings.Contains(m.viewport.GetContent(), "\x1b[7m") {
		t.Fatal("highlight left behind after esc")
	}
}

func TestCtrlLClearsSelection(t *testing.T) {
	m := newSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(10, 3)))
	m.Update(keyMsg('l', tea.ModCtrl))
	if m.sel.Active {
		t.Fatal("ctrl+l did not clear the selection")
	}
}

func TestSelectionFollowsScrollOffset(t *testing.T) {
	m := newTestModel(nil) // 100x32, viewH=20
	for i := 0; i < 30; i++ {
		m.blocks = append(m.blocks, block{kind: blockSystem, text: "note"})
	}
	m.renderAll()
	m.viewport.GotoBottom()
	off := m.viewport.YOffset()
	if off == 0 {
		t.Fatal("expected scrolled viewport")
	}
	m.Update(tea.MouseClickMsg(mouse(2, 4))) // second visible row: first row is a blank separator
	m.Update(tea.MouseMotionMsg(mouse(6, 4)))
	m.Update(tea.MouseReleaseMsg(mouse(6, 4)))
	text := m.sel.Text(strings.Split(m.viewport.GetContent(), "\n"))
	if text != "note" {
		t.Fatalf("selected text = %q, want %q", text, "note")
	}
}

// newTallSelectionModel loads enough one-line blocks to make the conversation
// scrollable: 30 blocks -> 59 content lines against a 20-row viewport.
func newTallSelectionModel() *Model {
	m := newTestModel(nil) // 100x32, viewH=20, viewport rows y=3..22
	for i := 0; i < 30; i++ {
		m.blocks = append(m.blocks, block{kind: blockSystem, text: fmt.Sprintf("note%02d", i)})
	}
	m.renderAll()
	return m
}

func TestDragSelectAutoScrollsAtBottomEdge(t *testing.T) {
	m := newTallSelectionModel()
	m.viewport.ScrollUp(1000) // pull to the top so down-scrolling has room
	if m.viewport.YOffset() != 0 {
		t.Fatal("expected top-anchored viewport")
	}
	m.Update(tea.MouseClickMsg(mouse(2, 3))) // anchor at content line 0
	_, cmd := m.Update(tea.MouseMotionMsg(mouse(2, 22)))
	if cmd == nil {
		t.Fatal("drag at the bottom edge must schedule auto-scroll")
	}
	if m.selAutoScroll.Dir != 1 {
		t.Fatalf("auto-scroll dir = %d, want 1", m.selAutoScroll.Dir)
	}
	// Ticks scroll the viewport and stretch the caret to the bottom edge.
	for i := 0; i < 3; i++ {
		m.Update(selectionAutoScrollMsg{seq: m.selSeq})
	}
	if m.viewport.YOffset() != 3 {
		t.Fatalf("yoffset = %d, want 3", m.viewport.YOffset())
	}
	if !m.sel.Moved || m.sel.Caret.Line <= m.sel.Anchor.Line {
		t.Fatalf("caret did not extend: %+v", m.sel)
	}
	// Drain to the bottom: scrolling stops when the viewport hits the end.
	for i := 0; i < 60; i++ {
		m.Update(selectionAutoScrollMsg{seq: m.selSeq})
	}
	if m.selAutoScroll.Dir != 0 {
		t.Fatal("auto-scroll must stop at the bottom")
	}
	// The drag now spans beyond the initially visible rows (note00..note09).
	_, cmd = m.Update(tea.MouseReleaseMsg(mouse(2, 22)))
	if cmd == nil {
		t.Fatal("release after auto-scroll returned no clipboard cmd")
	}
	text := m.sel.Text(strings.Split(m.viewport.GetContent(), "\n"))
	if !strings.Contains(text, "note00") || !strings.Contains(text, "note28") {
		t.Fatalf("selected text must span the conversation, got %q", text)
	}
}

func TestDragSelectAutoScrollStopsOnRelease(t *testing.T) {
	m := newTallSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(2, 22)))
	m.Update(tea.MouseReleaseMsg(mouse(2, 22)))
	if m.selAutoScroll.Dir != 0 || m.selAutoScroll.Queued {
		t.Fatal("release must stop auto-scroll")
	}
	off := m.viewport.YOffset()
	m.Update(selectionAutoScrollMsg{seq: m.selSeq}) // stale tick after release
	if m.viewport.YOffset() != off {
		t.Fatal("stale tick scrolled the viewport")
	}
}

func TestDragSelectIgnoresStaleAutoScrollTick(t *testing.T) {
	m := newTallSelectionModel()
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(2, 22)))
	off := m.viewport.YOffset()
	m.Update(selectionAutoScrollMsg{seq: m.selSeq + 1}) // wrong generation
	if m.viewport.YOffset() != off {
		t.Fatal("stale-generation tick scrolled the viewport")
	}
}

// selectAll drags from the first content line to the last and returns what
// the release handler would copy.
func selectAll(t *testing.T, m *Model) string {
	t.Helper()
	lines := strings.Split(m.viewport.GetContent(), "\n")
	if len(lines) < 2 {
		t.Fatal("content did not wrap")
	}
	m.Update(tea.MouseClickMsg(mouse(2, 3)))
	m.Update(tea.MouseMotionMsg(mouse(98, 2+len(lines))))
	if _, cmd := m.Update(tea.MouseReleaseMsg(mouse(98, 2+len(lines)))); cmd == nil {
		t.Fatal("release returned no clipboard cmd")
	}
	return m.sel.TextJoined(strings.Split(m.viewport.GetContent(), "\n"), m.lineBreaks)
}

// A paragraph soft-wrapped for display must copy as one logical line, like
// the terminal view joins wrapped lines.
func TestCopyJoinsSoftWrappedParagraph(t *testing.T) {
	m := newTestModel(nil) // 100x32, content width 96
	para := strings.TrimSpace(strings.Repeat("word ", 40))
	m.blocks = append(m.blocks, block{kind: blockAssistant, text: para, final: true})
	m.renderAll()
	got := selectAll(t, m)
	// Glamour frames the document with blank lines; the paragraph itself
	// must come out as a single line.
	body := strings.TrimSpace(got)
	if strings.Contains(body, "\n") {
		t.Fatalf("wrapped paragraph copied with newlines: %q", got)
	}
	if strings.Join(strings.Fields(body), " ") != para {
		t.Fatalf("got %q, want %q", got, para)
	}
}

// An overlong word (URL) chopped mid-word must copy without injected spaces.
func TestCopyJoinsHardWrappedURL(t *testing.T) {
	url := "https://example.com/" + strings.Repeat("u", 180)
	m := newTestModel(nil)
	m.blocks = append(m.blocks, block{kind: blockAssistant, text: url, final: true})
	m.renderAll()
	if got := selectAll(t, m); !strings.Contains(got, url) {
		t.Fatalf("got %q, want it to contain %q", got, url)
	}
}
