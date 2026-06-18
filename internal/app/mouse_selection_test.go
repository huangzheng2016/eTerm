package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

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
	if got.Y != 0 {
		t.Fatalf("release Y = %d want clamped 0", got.Y)
	}
}
