package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
)

// The AI panel replaces the whole frame; the app must not add or drop rows,
// or the bottom border/input land off screen.
func TestAIOverlayFillsFrameExactly(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {100, 32}} {
		w, h := sz[0], sz[1]
		fake := aiview.NewFakeRunner()
		fake.Delay = 0
		av := aiview.New(fake, fake, fake)
		av.SetSize(w, h)
		a := App{
			viewState: MainView,
			width:     w,
			height:    h,
			aiView:    av,
			aiVisible: true,
		}
		out := a.View().Content
		if n := strings.Count(out, "\n") + 1; n != h {
			t.Fatalf("%dx%d: frame height = %d, want %d", w, h, n, h)
		}
		lines := strings.Split(out, "\n")
		if !strings.Contains(lines[len(lines)-1], "╰") {
			t.Fatalf("%dx%d: bottom border missing from last row: %q", w, h, lines[len(lines)-1])
		}
	}
}

// drainAI feeds command results back into the overlay until the run settles.
func drainAI(t *testing.T, av *aiview.Model, cmd tea.Cmd) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for cmd != nil {
		if time.Now().After(deadline) {
			t.Fatal("drain timed out")
		}
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drainAI(t, av, c)
			}
			return
		}
		var next tea.Cmd
		updated, next := av.Update(msg)
		if m, ok := updated.(*aiview.Model); ok {
			av = m
		}
		cmd = next
	}
}

func TestAIOverlayForwardsDragSelection(t *testing.T) {
	fake := aiview.NewFakeRunner()
	fake.Delay = 0
	fake.Events = []aiview.AgentEvent{
		{Kind: aiview.EventTextDelta, Text: "ok"},
		{Kind: aiview.EventDone},
	}
	av := aiview.New(fake, fake, fake)
	av.SetSize(80, 24)
	av.Init() // focuses the input
	// Load a conversation through the public API (paste prompt, enter, drain).
	av.Update(tea.PasteMsg{Content: "hello ai"})
	_, cmd := av.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	drainAI(t, av, cmd)
	if !av.Running() && !strings.Contains(av.View().Content, "hello ai") {
		t.Fatal("conversation did not load")
	}

	a := App{
		viewState: MainView,
		width:     80,
		height:    24,
		masterKey: security.NewMasterKeyManager(nil, nil, 0),
		aiView:    av,
		aiVisible: true,
	}

	// Drag over the conversation area: screen (4,3) is content line 0 col 2
	// (overlay origin 1,0; border+padding 2; border+title+blank 3).
	upd, _ := a.Update(tea.MouseClickMsg(tea.Mouse{X: 4, Y: 3, Button: tea.MouseLeft}))
	a = upd.(App)
	upd, _ = a.Update(tea.MouseMotionMsg(tea.Mouse{X: 12, Y: 3, Button: tea.MouseLeft}))
	a = upd.(App)
	_, cmd = a.Update(tea.MouseReleaseMsg(tea.Mouse{X: 12, Y: 3, Button: tea.MouseLeft}))
	if cmd == nil || cmd() == nil {
		t.Fatal("expected clipboard command from drag-select")
	}
}
