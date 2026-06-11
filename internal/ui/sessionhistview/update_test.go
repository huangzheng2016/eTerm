package sessionhistview

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func TestMouseClickSelectsHistoryRow(t *testing.T) {
	m := &Model{
		width:     90,
		height:    20,
		loaded:    true,
		focusList: true,
		rows: []db.ConnectionHistory{
			{ConnectedAt: time.Unix(3, 0), Status: "success", Transcript: "third"},
			{ConnectedAt: time.Unix(2, 0), Status: "success", Transcript: "second"},
		},
	}

	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 3, Button: tea.MouseLeft}))
	got := next.(*Model)

	if got.sel != 1 {
		t.Fatalf("got selected row %d want 1", got.sel)
	}
	if !got.focusList {
		t.Fatalf("expected list focus")
	}
}

func TestMouseWheelScrollsFocusedTranscript(t *testing.T) {
	m := &Model{
		width:     90,
		height:    8,
		loaded:    true,
		focusList: false,
		rows: []db.ConnectionHistory{
			{ConnectedAt: time.Unix(1, 0), Status: "success", Transcript: "a\nb\nc\nd\ne\nf"},
		},
	}

	next, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 40, Y: 4, Button: tea.MouseWheelDown}))
	got := next.(*Model)

	if got.scroll != 1 {
		t.Fatalf("got scroll %d want 1", got.scroll)
	}
}
