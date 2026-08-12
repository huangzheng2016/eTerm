package sshview

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

func TestResumeSessionDrainsPendingOutputAndContinues(t *testing.T) {
	pr1, _ := io.Pipe()
	done1 := make(chan error, 1)
	sess1 := &internalssh.InteractiveSession{Stdout: pr1, Done: done1}
	m := New(sess1, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(80, 24)
	// Output acked and queued in the chunk channel but never rendered.
	m.ch <- []byte("queued")
	m.closeChFor(m.ch)
	m.disconnected = true

	pr2, pw2 := io.Pipe()
	done2 := make(chan error, 1)
	sess2 := &internalssh.InteractiveSession{Stdout: pr2, Done: done2}

	cmd := m.ResumeSession(sess2)
	if m.Disconnected() {
		t.Fatal("still disconnected after resume")
	}
	if got := m.PlainTranscript(0); !strings.Contains(got, "queued") {
		t.Fatalf("transcript = %q, want drained output", got)
	}

	go func() { _, _ = pw2.Write([]byte("live")) }()
	msg := cmd()
	chunk, ok := msg.(ChunkMsg)
	if !ok {
		t.Fatalf("got %T, want ChunkMsg", msg)
	}
	if !strings.Contains(string(chunk.Data), "live") {
		t.Fatalf("chunk = %q, want live output", chunk.Data)
	}
	_ = pw2.Close()
}

func TestStaleWaitChunkReturnsNil(t *testing.T) {
	m := New(&internalssh.InteractiveSession{}, "t", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })
	m.SetSize(80, 24)
	stale := waitChunk(m)
	m.closeChFor(m.ch)

	pr, _ := io.Pipe()
	sess := &internalssh.InteractiveSession{Stdout: pr, Done: make(chan error, 1)}
	m.ResumeSession(sess)

	got := make(chan tea.Msg, 1)
	go func() { got <- stale() }()
	select {
	case msg := <-got:
		if msg != nil {
			t.Fatalf("stale waitChunk returned %T, want nil", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("stale waitChunk did not return")
	}
}
