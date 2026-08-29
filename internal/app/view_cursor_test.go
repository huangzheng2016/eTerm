package app

import (
	"testing"

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
