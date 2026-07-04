package sshview

import (
	"bytes"
	"sync"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type probeStdin struct {
	mu  sync.Mutex
	buf bytes.Buffer
	ch  chan struct{}
}

func newProbeStdin() *probeStdin {
	return &probeStdin{ch: make(chan struct{}, 16)}
}

func (w *probeStdin) Write(p []byte) (int, error) {
	w.mu.Lock()
	_, _ = w.buf.Write(p)
	w.mu.Unlock()
	select {
	case w.ch <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (w *probeStdin) Close() error {
	return nil
}

func (w *probeStdin) contains(s string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Contains(w.buf.Bytes(), []byte(s))
}

func (w *probeStdin) waitContains(t *testing.T, s string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if w.contains(s) {
			return
		}
		select {
		case <-w.ch:
		case <-deadline:
			t.Fatalf("stdin missing %q", s)
		}
	}
}

func TestTmuxTerminalProbesReply(t *testing.T) {
	stdin := newProbeStdin()
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(40, 10)

	_, _ = m.Update(ChunkMsg{
		StreamID: m.StreamID(),
		Data:     []byte("\x1b[?996n\x1b[>q\x1b[18t"),
	})

	stdin.waitContains(t, "\x1b[?997;1n")
	stdin.waitContains(t, "\x1bP>|eTerm\x1b\\")
	stdin.waitContains(t, "\x1b[8;10;40t")
	if got := m.emu.Render(); got != "\n\n\n\n\n\n\n\n\n" {
		t.Fatalf("probes polluted screen: %q", got)
	}
}
