package sshview

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"testing"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type oneByteReader struct {
	data []byte
	pos  int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func TestReadLoopDoesNotDropChunksWhenChannelIsFull(t *testing.T) {
	m := &Model{
		sess: &internalssh.InteractiveSession{Stdout: &oneByteReader{data: []byte("abc")}},
		ch:   make(chan []byte, 1),
	}

	done := make(chan struct{})
	go func() {
		m.readLoop()
		close(done)
	}()

	var got []byte
	for b := range m.ch {
		got = append(got, b...)
	}
	<-done

	if string(got) != "abc" {
		t.Fatalf("got %q want %q", got, "abc")
	}
}

func TestWaitChunkCoalescesQueuedChunks(t *testing.T) {
	m := &Model{streamID: 7, ch: make(chan []byte, 4)}
	m.ch <- []byte("a")
	m.ch <- []byte("b")
	m.ch <- []byte("c")

	msg := waitChunk(m)()
	got, ok := msg.(ChunkMsg)
	if !ok {
		t.Fatalf("got %T want ChunkMsg", msg)
	}
	if got.StreamID != 7 || string(got.Data) != "abc" {
		t.Fatalf("got stream=%d data=%q", got.StreamID, got.Data)
	}
}

func TestWaitChunkDoesNotDropChunkAtCoalesceLimit(t *testing.T) {
	m := &Model{streamID: 7, ch: make(chan []byte, 2)}
	first := bytes.Repeat([]byte("a"), maxCoalescedChunkBytes-1)
	m.ch <- first
	m.ch <- []byte("bc")

	msg := waitChunk(m)()
	got, ok := msg.(ChunkMsg)
	if !ok {
		t.Fatalf("got %T want ChunkMsg", msg)
	}
	if len(got.Data) != maxCoalescedChunkBytes+1 || !bytes.HasSuffix(got.Data, []byte("bc")) {
		t.Fatalf("got len=%d suffix=%q", len(got.Data), got.Data[len(got.Data)-2:])
	}
}

func TestWaitNilClearsReadEOF(t *testing.T) {
	m := &Model{}
	m.setReadErr(io.EOF)
	m.setWaitErr(nil)
	if m.endErr != nil {
		t.Fatalf("got %v want nil", m.endErr)
	}
}

func TestAbnormalStreamDoneShowsReconnectDialog(t *testing.T) {
	m := New(nil, "host-a", 42, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	updated, cmd := m.Update(StreamDoneMsg{StreamID: m.StreamID(), Err: errors.New("read: connection reset by peer")})
	if !updated.(*Model).Disconnected() {
		t.Fatal("expected session to be marked disconnected")
	}
	if cmd == nil {
		t.Fatal("expected reconnect dialog command")
	}
	msg := cmd()
	got, ok := msg.(types.ConnErrorMsg)
	if !ok {
		t.Fatalf("got %T want types.ConnErrorMsg", msg)
	}
	if got.Target != "host-a" {
		t.Fatalf("target = %q", got.Target)
	}
	if _, ok := got.Retry.(types.SSHReconnectMsg); !ok {
		t.Fatalf("retry = %T want types.SSHReconnectMsg", got.Retry)
	}
}

func TestRemoteLocalShellAbnormalStreamDoneShowsConnectionError(t *testing.T) {
	m := New(nil, "[R]remote", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	updated, cmd := m.Update(StreamDoneMsg{StreamID: m.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	if !updated.(*Model).Disconnected() {
		t.Fatal("expected session to be marked disconnected")
	}
	if cmd == nil {
		t.Fatal("expected connection error command")
	}
	msg := cmd()
	got, ok := msg.(types.ConnErrorMsg)
	if !ok {
		t.Fatalf("got %T want types.ConnErrorMsg", msg)
	}
	if got.Target != "[R]remote" {
		t.Fatalf("target = %q", got.Target)
	}
	if got.Retry != nil {
		t.Fatalf("retry = %#v, want nil for remote local shell", got.Retry)
	}
}

func TestNormalStreamDoneClosesWithoutReconnectDialog(t *testing.T) {
	m := New(nil, "host-a", 42, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	updated, cmd := m.Update(StreamDoneMsg{StreamID: m.StreamID(), Err: nil})
	if updated.(*Model).Disconnected() {
		t.Fatal("expected normal exit, not disconnected")
	}
	if cmd == nil {
		t.Fatal("expected disconnect command")
	}
	msg := cmd()
	if _, ok := msg.(types.SSHDisconnectMsg); !ok {
		t.Fatalf("got %T want types.SSHDisconnectMsg", msg)
	}
}

func TestLocalShellExitStatusClosesWithoutReconnectDialog(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 130").Run()
	if err == nil {
		t.Fatal("expected exit error")
	}

	m := New(nil, "zsh", 0, viewkeys.SSHKeys{})
	t.Cleanup(func() { _ = m.Close() })

	updated, cmd := m.Update(StreamDoneMsg{StreamID: m.StreamID(), Err: err})
	if updated.(*Model).Disconnected() {
		t.Fatal("expected local shell exit, not disconnected")
	}
	if cmd == nil {
		t.Fatal("expected disconnect command")
	}
	msg := cmd()
	if _, ok := msg.(types.SSHDisconnectMsg); !ok {
		t.Fatalf("got %T want types.SSHDisconnectMsg", msg)
	}
}
