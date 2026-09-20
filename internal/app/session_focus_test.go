package app

import (
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func newFocusProbeView(t *testing.T, stdin *appTestStdin) *sshview.Model {
	t.Helper()
	m := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(40, 10)
	_, _ = m.Update(sshview.ChunkMsg{StreamID: m.StreamID(), Data: []byte("\x1b[?1004h")})
	return m
}

func TestTabSwitchRefocusesSessions(t *testing.T) {
	in1, in2 := &appTestStdin{}, &appTestStdin{}
	sv1 := newFocusProbeView(t, in1)
	sv2 := newFocusProbeView(t, in2)

	a := App{
		viewState:  MainView,
		tabs:       []Tab{{Type: SSHTab, Model: sv1}, {Type: SSHTab, Model: sv2}},
		activeTab:  1,
		winFocused: true,
	}
	a.refocusSessionOnTabSwitch(sv1)

	in1.waitContains(t, "\x1b[O")
	in2.waitContains(t, "\x1b[I")
}

func TestTabSwitchSkipsFocusWhileWindowBlurred(t *testing.T) {
	in1, in2 := &appTestStdin{}, &appTestStdin{}
	sv1 := newFocusProbeView(t, in1)
	sv2 := newFocusProbeView(t, in2)

	a := App{
		viewState:  MainView,
		tabs:       []Tab{{Type: SSHTab, Model: sv1}, {Type: SSHTab, Model: sv2}},
		activeTab:  1,
		winFocused: false,
	}
	a.refocusSessionOnTabSwitch(sv1)

	time.Sleep(200 * time.Millisecond)
	if in1.contains("\x1b[O") || in2.contains("\x1b[I") {
		t.Fatal("focus events sent while window is blurred")
	}
}
