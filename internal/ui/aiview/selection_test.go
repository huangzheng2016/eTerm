package aiview

import (
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
