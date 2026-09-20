package sshview

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

type trackingWriteCloser struct {
	mu     sync.Mutex
	closed bool
}

func (t *trackingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (t *trackingWriteCloser) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	return nil
}

func (t *trackingWriteCloser) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

func TestResumeSessionDrainsPendingOutputAndContinues(t *testing.T) {
	pr1, _ := io.Pipe()
	done1 := make(chan error, 1)
	sess1 := &internalssh.InteractiveSession{Stdout: pr1, Done: done1}
	m := newTestModel(t, sess1)
	m.SetSize(80, 24)
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

func TestResumeSessionClosesReplacedSession(t *testing.T) {
	stdin1 := &trackingWriteCloser{}
	closer1 := &trackingWriteCloser{}
	pr1, _ := io.Pipe()
	done1 := make(chan error, 1)
	sess1 := &internalssh.InteractiveSession{Stdin: stdin1, Stdout: pr1, Done: done1}
	sess1.AddCloser(closer1)
	m := newTestModel(t, sess1)
	m.SetSize(80, 24)

	pr2, _ := io.Pipe()
	done2 := make(chan error, 1)
	sess2 := &internalssh.InteractiveSession{Stdout: pr2, Done: done2}
	m.ResumeSession(sess2)

	if !stdin1.isClosed() {
		t.Fatal("replaced session stdin not closed")
	}
	if !closer1.isClosed() {
		t.Fatal("replaced session closer not closed")
	}
}

func TestStaleWaitChunkReturnsNil(t *testing.T) {
	m := newTestModel(t, &internalssh.InteractiveSession{})
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
