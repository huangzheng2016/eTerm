package app

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestAppViewEnablesMouseDragEvents(t *testing.T) {
	tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })

	a := App{
		viewState: MainView,
		width:     80,
		height:    20,
		tabs: []Tab{
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 0,
	}

	if got := a.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("MouseMode = %v want %v", got, tea.MouseModeCellMotion)
	}
}

func TestAppForwardsMouseReleaseOutsideContentDuringSSHSelection(t *testing.T) {
	tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetSize(80, 10)
	tab.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 2, Button: tea.MouseLeft}))

	a := App{
		viewState: MainView,
		width:     80,
		height:    20,
		tabs: []Tab{
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 0,
	}

	msg := appAdjustMouseForTabContent(a, tea.MouseReleaseMsg(tea.Mouse{X: 2, Y: 0, Button: tea.MouseLeft}))
	if msg == nil {
		t.Fatal("expected outside release to be forwarded during SSH selection")
	}
	got := msg.(tea.MouseReleaseMsg)
	if got.Y != -a.MainViewChromeTopLines() {
		t.Fatalf("release Y = %d want %d", got.Y, -a.MainViewChromeTopLines())
	}
}

func TestAppUpdateCompletesSSHSelectionWhenReleaseIsAboveContent(t *testing.T) {
	tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetSize(80, 10)
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.SetupNoPassword()
	a := App{
		masterKey: mk,
		viewState: MainView,
		width:     80,
		height:    20,
		tabs: []Tab{
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 0,
	}
	top := a.MainViewChromeTopLines()

	next, _ := a.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: top + 2, Button: tea.MouseLeft}))
	a = next.(App)
	if !tab.DraggingSelection() {
		t.Fatal("expected selection drag to start")
	}

	next, _ = a.Update(tea.MouseReleaseMsg(tea.Mouse{X: 8, Y: top - 1, Button: tea.MouseLeft}))
	a = next.(App)
	if tab.DraggingSelection() {
		t.Fatal("expected outside release to finish selection")
	}
}

func TestAppUpdateCopiesSSHSelectionWhenReleaseLeavesContent(t *testing.T) {
	cases := []struct {
		name     string
		startX   int
		motionX  int
		motionY  func(top, contentH int) int
		releaseX int
		releaseY func(top, contentH int) int
		want     string
	}{
		{"above", 0, 4, func(top, contentH int) int { return top }, 4, func(top, contentH int) int { return top - 1 }, "hello"},
		{"below", 0, 4, func(top, contentH int) int { return top }, 4, func(top, contentH int) int { return top + contentH }, "hello"},
		{"left", 0, -1, func(top, contentH int) int { return top }, -1, func(top, contentH int) int { return top }, "h"},
		{"right", 0, 20, func(top, contentH int) int { return top }, 20, func(top, contentH int) int { return top }, "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
			t.Cleanup(func() { _ = tab.Close() })
			tab.SetSize(20, 5)
			tab.Update(sshview.ChunkMsg{StreamID: tab.StreamID(), Data: []byte("hello world\r\n")})
			mk := security.NewMasterKeyManager(nil, nil, time.Minute)
			mk.SetupNoPassword()
			a := App{
				masterKey: mk,
				viewState: MainView,
				width:     20,
				height:    12,
				tabs: []Tab{
					{Type: SSHTab, Title: "ssh", Model: tab},
				},
				activeTab: 0,
			}
			top := a.MainViewChromeTopLines()
			contentH := a.mainContentHeight()

			next, _ := a.Update(tea.MouseClickMsg(tea.Mouse{X: tc.startX, Y: top, Button: tea.MouseLeft}))
			a = next.(App)
			next, _ = a.Update(tea.MouseMotionMsg(tea.Mouse{X: tc.motionX, Y: tc.motionY(top, contentH), Button: tea.MouseLeft}))
			a = next.(App)
			next, cmd := a.Update(tea.MouseReleaseMsg(tea.Mouse{X: tc.releaseX, Y: tc.releaseY(top, contentH), Button: tea.MouseLeft}))
			a = next.(App)

			if cmd == nil {
				t.Fatal("expected copy command")
			}
			msg := cmd()
			batch, ok := msg.(tea.BatchMsg)
			if !ok {
				t.Fatalf("copy command message = %T want tea.BatchMsg", msg)
			}
			if len(batch) == 0 {
				t.Fatal("expected clipboard command in batch")
			}
			if got := fmt.Sprint(batch[0]()); got != tc.want {
				t.Fatalf("clipboard = %q want %q", got, tc.want)
			}
		})
	}
}

func TestAppUpdateForwardsOutsideMotionToStartSelectionAutoScroll(t *testing.T) {
	tab := sshview.New(nil, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetSize(20, 5)
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.SetupNoPassword()
	a := App{
		masterKey: mk,
		viewState: MainView,
		width:     20,
		height:    12,
		tabs: []Tab{
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 0,
	}
	top := a.MainViewChromeTopLines()

	next, _ := a.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: top + 2, Button: tea.MouseLeft}))
	a = next.(App)
	next, cmd := a.Update(tea.MouseMotionMsg(tea.Mouse{X: 2, Y: top - 1, Button: tea.MouseLeft}))
	a = next.(App)

	if cmd == nil {
		t.Fatal("expected outside motion to schedule selection auto-scroll")
	}
}
