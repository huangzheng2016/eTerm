package app

import (
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestMainTabChromeShowsReconnectBadgeOnToastLine(t *testing.T) {
	tab := sshview.New(nil, "[T]remote-work", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = tab.Close() })
	tab.SetRemoteReconnect(&types.RemoteReconnect{Peer: types.RemotePeer{ID: "p1"}, Tmux: true, Target: relay.TargetTmuxAttach, SessionID: "work"})
	_, _ = tab.Update(sshview.StreamDoneMsg{StreamID: tab.StreamID(), Err: errConnectionResetForTest{}})

	a := App{
		tabs:      []Tab{{Type: SSHTab, Title: "[T]remote-work", Model: tab}},
		activeTab: 0,
		width:     80,
		toast:     components.NewToast(),
	}
	var cmd any
	a.toast, cmd = a.toast.Show("Remote reconnect - connect", components.ToastInfo, time.Second)
	if cmd == nil {
		t.Fatal("toast command should be set")
	}

	view := a.buildMainTabChrome(80)
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("tab chrome missing divider line:\n%s", view)
	}
	divider := lines[len(lines)-1]
	if !strings.Contains(divider, "Remote reconnect - connect") {
		t.Fatalf("divider missing toast:\n%s", divider)
	}
	if !strings.Contains(divider, "RECONNECTING (1/3)") {
		t.Fatalf("divider missing reconnect badge:\n%s", divider)
	}
	if strings.Contains(tab.View().Content, "RECONNECTING (1/3)") {
		t.Fatalf("ssh content should not render reconnect badge:\n%s", tab.View().Content)
	}
}

type errConnectionResetForTest struct{}

func (errConnectionResetForTest) Error() string { return "read: connection reset by peer" }
