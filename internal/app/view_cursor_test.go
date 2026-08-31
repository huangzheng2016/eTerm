package app

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/ui/aiview"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestAppViewOffsetsTabCursorByChrome(t *testing.T) {
	tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetSize(80, 10)
	tab.Update(sshview.ChunkMsg{StreamID: tab.StreamID(), Data: []byte("hi")})

	a := App{
		viewState: MainView,
		width:     80,
		height:    20,
		tabs:      []Tab{{Type: SSHTab, Title: "ssh", Model: tab}},
		activeTab: 0,
	}
	v := a.View()
	if v.Cursor == nil {
		t.Fatal("app view missing propagated cursor")
	}
	wantY := a.MainViewChromeTopLines()
	if v.Cursor.X != 2 || v.Cursor.Y != wantY {
		t.Fatalf("cursor = %d,%d want %d,%d", v.Cursor.X, v.Cursor.Y, 2, wantY)
	}
}

// The AI overlay is fullscreen at the frame origin: its cursor must reach
// the app view unchanged (no chrome offset, not dropped).
func TestAppViewPropagatesAICursor(t *testing.T) {
	fake := aiview.NewFakeRunner()
	fake.Delay = 0
	av := aiview.New(fake, fake, fake)
	av.SetSize(80, 24)
	av.Init()
	av.InsertText("hi")

	inner := av.View().Cursor
	if inner == nil {
		t.Fatal("aiview reported no cursor")
	}

	a := App{
		viewState: MainView,
		width:     80,
		height:    24,
		aiView:    av,
		aiVisible: true,
	}
	v := a.View()
	if v.Cursor == nil {
		t.Fatal("app view dropped the AI overlay cursor")
	}
	if v.Cursor.X != inner.X || v.Cursor.Y != inner.Y {
		t.Fatalf("cursor = %d,%d want %d,%d", v.Cursor.X, v.Cursor.Y, inner.X, inner.Y)
	}
}
