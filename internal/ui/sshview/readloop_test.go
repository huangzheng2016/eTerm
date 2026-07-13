package sshview

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
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

type unblockOnCloseReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newUnblockOnCloseReader() *unblockOnCloseReader {
	return &unblockOnCloseReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *unblockOnCloseReader) Read(p []byte) (int, error) {
	close(r.started)
	<-r.release
	p[0] = 'x'
	return 1, io.EOF
}

func (r *unblockOnCloseReader) Close() error {
	r.once.Do(func() { close(r.release) })
	return nil
}

func (r *unblockOnCloseReader) Write(p []byte) (int, error) {
	return len(p), nil
}

type blockingWriteCloser struct {
	started chan struct{}
	release chan struct{}
	start   sync.Once
	once    sync.Once
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{started: make(chan struct{}), release: make(chan struct{})}
}

func (w *blockingWriteCloser) Write(p []byte) (int, error) {
	w.start.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

func (w *blockingWriteCloser) Close() error {
	w.once.Do(func() { close(w.release) })
	return nil
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

func TestCloseWhileReadLoopHasPendingDataDoesNotPanic(t *testing.T) {
	stdio := newUnblockOnCloseReader()
	doneSession := make(chan error)
	m := New(&internalssh.InteractiveSession{Stdin: stdio, Stdout: stdio, Done: doneSession}, "tmux", 0, viewkeys.SSHKeys{})

	panicCh := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		m.readLoop()
	}()

	<-stdio.started
	_ = m.Close()
	<-done

	select {
	case p := <-panicCh:
		t.Fatalf("readLoop panic = %v", p)
	default:
	}
}

func TestKeyPressDoesNotBlockWhenSessionInputBlocks(t *testing.T) {
	stdin := newBlockingWriteCloser()
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	defer m.Close()

	done := make(chan struct{})
	go func() {
		_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("key update blocked on session stdin")
	}
}

func TestResizeDoesNotBlockWhenSessionResizeBlocks(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	m := New(&internalssh.InteractiveSession{
		Resize: func(rows, cols int) error {
			<-release
			return nil
		},
	}, "test", 0, viewkeys.SSHKeys{})
	defer func() {
		once.Do(func() { close(release) })
		_ = m.Close()
	}()

	done := make(chan struct{})
	go func() {
		_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("window size update blocked on session resize")
	}
}

func TestMouseForwardDoesNotBlockWhenSessionInputBlocks(t *testing.T) {
	stdin := newBlockingWriteCloser()
	m := New(&internalssh.InteractiveSession{Stdin: stdin}, "test", 0, viewkeys.SSHKeys{})
	defer m.Close()

	m.emu.WriteString("\x1b[?1049h\x1b[?1000h\x1b[?1006h")
	m.Update(wheel(tea.MouseWheelDown))

	select {
	case <-stdin.started:
	case <-time.After(time.Second):
		t.Fatal("first mouse event was not written")
	}

	done := make(chan struct{})
	go func() {
		_, _ = m.Update(wheel(tea.MouseWheelUp))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("mouse update blocked on session stdin")
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

func TestWaitChunkCoalescesBrieflyDelayedChunks(t *testing.T) {
	m := &Model{streamID: 7, ch: make(chan []byte, 4)}
	m.ch <- []byte("a")
	go func() {
		time.Sleep(2 * time.Millisecond)
		m.ch <- []byte("b")
	}()

	msg := waitChunk(m)()
	got, ok := msg.(ChunkMsg)
	if !ok {
		t.Fatalf("got %T want ChunkMsg", msg)
	}
	if got.StreamID != 7 || string(got.Data) != "ab" {
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

func TestRemoteTmuxDisconnectStartsAutoReconnect(t *testing.T) {
	m := New(nil, "[T]remote-work", 0, viewkeys.SSHKeys{Reconnect: []string{"r"}})
	t.Cleanup(func() { _ = m.Close() })
	m.SetRemoteReconnect(&types.RemoteReconnect{Peer: types.RemotePeer{ID: "p1"}, Tmux: true, Target: relay.TargetTmuxAttach, SessionID: "work"})

	_, cmd := m.Update(StreamDoneMsg{StreamID: m.StreamID(), Err: errors.New("websocket: close 1006 abnormal closure")})
	if cmd == nil {
		t.Fatal("expected auto reconnect command")
	}
	got, ok := cmd().(types.RemoteShellReconnectMsg)
	if !ok {
		t.Fatalf("got %T want types.RemoteShellReconnectMsg", got)
	}
	if got.Spec.SessionID != "work" || !got.Spec.Tmux || got.StreamID != m.StreamID() || !got.Auto || got.Attempt != 1 || got.MaxAttempts != 3 {
		t.Fatalf("bad reconnect msg %+v", got)
	}
	if !strings.Contains(m.View().Content, "RECONNECTING (1/3)") {
		t.Fatalf("view missing reconnecting state:\n%s", m.View().Content)
	}

	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: 'r', Text: "r"}))
	if cmd == nil {
		t.Fatal("r should emit a reconnect command")
	}
	if _, ok := cmd().(types.RemoteShellReconnectMsg); !ok {
		t.Fatalf("r emitted %T want types.RemoteShellReconnectMsg", cmd())
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
