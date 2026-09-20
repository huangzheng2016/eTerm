package aiview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func viewRows(s string) int { return strings.Count(s, "\n") + 1 }

func TestViewFillsFrameWidth(t *testing.T) {
	sizes := [][2]int{{80, 24}, {100, 32}, {120, 40}, {60, 20}}
	for _, sz := range sizes {
		m := newTestModel(nil)
		m.SetSize(sz[0], sz[1])
		for i, l := range strings.Split(m.View().Content, "\n") {
			if w := lipgloss.Width(l); w != sz[0] {
				t.Fatalf("%dx%d row %d: width = %d, want %d", sz[0], sz[1], i, w, sz[0])
			}
		}
	}
}

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

		m.blocks = append(m.blocks, block{kind: blockAssistant, text: strings.Repeat("partial ", 30)})
		m.renderBlock(len(m.blocks) - 1)
		m.rebuild()
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("streaming %dx%d: view height = %d, want %d", w, h, n, h)
		}

		m.status = statusError
		m.errMsg = "provider unreachable: " + strings.Repeat("detail ", 20)
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("error %dx%d: view height = %d, want %d", w, h, n, h)
		}
		m.status = statusIdle
		m.errMsg = ""

		m.input.SetValue(strings.Repeat("word ", 60))
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("input %dx%d: view height = %d, want %d", w, h, n, h)
		}

		m.expandTools = true
		m.renderAll()
		if n := viewRows(m.View().Content); n != h {
			t.Errorf("expanded %dx%d: view height = %d, want %d", w, h, n, h)
		}
	}
}

func TestModelNameStaysInFrame(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{strings.Repeat("very-long-provider-name-", 4), 80, 24},
		{strings.Repeat("モデル名", 12), 100, 32},
	}
	for _, tc := range cases {
		m, fake := newFakeModel()
		fake.Add(Provider{Name: tc.name, Type: "openai"})
		fake.Switch(tc.name, "x")
		m.SetSize(tc.w, tc.h)
		fillConversation(m)
		if n := viewRows(m.View().Content); n != tc.h {
			t.Errorf("model name %q: view height = %d, want %d", tc.name, n, tc.h)
		}
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
