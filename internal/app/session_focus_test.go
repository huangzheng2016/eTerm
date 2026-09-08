package app

import (
	"bytes"
	"sync"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type focusProbeStdin struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *focusProbeStdin) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *focusProbeStdin) Close() error { return nil }

func (w *focusProbeStdin) contains(s string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Contains(w.buf.Bytes(), []byte(s))
}

func waitForStdin(t *testing.T, w *focusProbeStdin, s string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if w.contains(s) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stdin missing %q", s)
}

func newFocusProbeView(t *testing.T, stdin *focusProbeStdin) *sshview.Model {
	t.Helper()
	m := sshview.New(&internalssh.InteractiveSession{Stdin: stdin}, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(40, 10)
	_, _ = m.Update(sshview.ChunkMsg{StreamID: m.StreamID(), Data: []byte("\x1b[?1004h")})
	return m
}

func TestTabSwitchRefocusesSessions(t *testing.T) {
	in1, in2 := &focusProbeStdin{}, &focusProbeStdin{}
	sv1 := newFocusProbeView(t, in1)
	sv2 := newFocusProbeView(t, in2)

	a := App{
		viewState:  MainView,
		tabs:       []Tab{{Type: SSHTab, Model: sv1}, {Type: SSHTab, Model: sv2}},
		activeTab:  1,
		winFocused: true,
	}
	a.refocusSessionOnTabSwitch(sv1)

	waitForStdin(t, in1, "\x1b[O")
	waitForStdin(t, in2, "\x1b[I")
}

func TestTabSwitchSkipsFocusWhileWindowBlurred(t *testing.T) {
	in1, in2 := &focusProbeStdin{}, &focusProbeStdin{}
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
