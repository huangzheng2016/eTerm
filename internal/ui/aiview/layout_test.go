package aiview

import (
	"strings"
	"testing"
)

func viewRows(s string) int { return strings.Count(s, "\n") + 1 }

// fillConversation loads a long mixed-kind conversation with wide lines.
func fillConversation(m *Model) {
	long := strings.Repeat("word ", 40)
	url := "https://example.com/" + strings.Repeat("u", 180)
	for i := 0; i < 15; i++ {
		m.blocks = append(m.blocks,
			block{kind: blockUser, text: long},
			block{kind: blockThinking, text: long},
			block{kind: blockAssistant, text: long + "\n\n" + url, final: true},
			block{kind: blockTool, text: "send_keys " + long, output: long + "\n" + long, toolDone: true},
			block{kind: blockSystem, text: long},
		)
	}
	m.renderAll()
}

// The AI panel renders fullscreen: the view must always be exactly the
// terminal height, or rows get pushed off screen.
func TestViewNeverExceedsFrame(t *testing.T) {
	sizes := [][2]int{{80, 24}, {100, 32}, {120, 40}, {60, 20}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]

		m := newTestModel(nil)
		m.SetSize(w, h)
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("empty %dx%d: view height = %d, want %d", w, h, n, h)
		}

		m = newTestModel(nil)
		m.SetSize(w, h)
		fillConversation(m)
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("conversation %dx%d: view height = %d, want %d", w, h, n, h)
		}

		// Streaming (non-final) assistant block.
		m.blocks = append(m.blocks, block{kind: blockAssistant, text: strings.Repeat("partial ", 30)})
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("streaming %dx%d: view height = %d, want %d", w, h, n, h)
		}

		// Error line with a full conversation.
		m.status = statusError
		m.errMsg = "provider unreachable: " + strings.Repeat("detail ", 20)
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("error %dx%d: view height = %d, want %d", w, h, n, h)
		}
		m.status = statusIdle
		m.errMsg = ""

		// Long multi-word input text.
		m.input.SetValue(strings.Repeat("word ", 60))
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("input %dx%d: view height = %d, want %d", w, h, n, h)
		}

		// Expanded tool output.
		m.expandTools = true
		m.renderAll()
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("expanded %dx%d: view height = %d, want %d", w, h, n, h)
		}
	}
}

func TestViewLongModelNameStaysInFrame(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	fake.Add(Provider{Name: strings.Repeat("very-long-provider-name-", 4), Type: "openai"})
	fake.Switch(strings.Repeat("very-long-provider-name-", 4), "x")
	m := New(fake, fake, fake)
	m.SetSize(80, 24)
	fillConversation(m)
	if n := viewRows(m.View().Content); n != 24 {
		t.Fatalf("long model name: view height = %d, want 24", n)
	}
}

func TestMultiLineErrorStaysOneRow(t *testing.T) {
	m := newTestModel(nil)
	m.SetSize(100, 32)
	fillConversation(m)
	m.status = statusError
	m.errMsg = "provider unreachable:\nupstream said no\nplease retry"
	out := m.View().Content
	if n := viewRows(out); n != 32 {
		t.Fatalf("multi-line error: view height = %d, want 32", n)
	}
	if !strings.Contains(plain(out), "error: provider unreachable: upstream said no please retry") {
		t.Fatal("error line not collapsed to one row")
	}
}

func TestCJKModelNameStaysInFrame(t *testing.T) {
	fake := NewFakeRunner()
	fake.Delay = 0
	name := strings.Repeat("モデル名", 12) // 48 runes, 96 cells
	fake.Add(Provider{Name: name, Type: "openai"})
	fake.Switch(name, "x")
	m := New(fake, fake, fake)
	m.SetSize(100, 32)
	fillConversation(m)
	if n := viewRows(m.View().Content); n != 32 {
		t.Fatalf("CJK model name: view height = %d, want 32", n)
	}
}
