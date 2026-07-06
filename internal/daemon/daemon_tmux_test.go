package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
)

type daemonFrameSink struct {
	frames chan relay.Frame
}

func newDaemonSink() *daemonFrameSink {
	return &daemonFrameSink{frames: make(chan relay.Frame, 64)}
}

func (s *daemonFrameSink) write(f relay.Frame) error {
	s.frames <- f
	return nil
}

func waitDaemonFrame(t *testing.T, s *daemonFrameSink, typ relay.FrameType) relay.Frame {
	t.Helper()
	for {
		select {
		case f := <-s.frames:
			if f.Type == typ {
				return f
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for frame type 0x%02x", typ)
		}
	}
}

type daemonWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (w *daemonWriteCloser) Close() error {
	w.closed = true
	return nil
}

type daemonFakeSession struct {
	is      *internalssh.InteractiveSession
	stdin   *daemonWriteCloser
	stdout  *io.PipeWriter
	done    chan error
	resizes [][2]int
}

func newDaemonFakeSession() *daemonFakeSession {
	pr, pw := io.Pipe()
	stdin := &daemonWriteCloser{}
	done := make(chan error, 1)
	f := &daemonFakeSession{stdin: stdin, stdout: pw, done: done}
	f.is = &internalssh.InteractiveSession{
		Stdin:  stdin,
		Stdout: pr,
		Done:   done,
		Resize: func(rows, cols int) error {
			f.resizes = append(f.resizes, [2]int{rows, cols})
			return nil
		},
	}
	return f
}

func restoreTmuxStubs(t *testing.T) {
	t.Helper()
	oldList := tmuxListSessions
	oldNew := tmuxNewSession
	oldAttach := tmuxAttachSession
	oldKill := tmuxKillSession
	oldRename := tmuxRenameSession
	t.Cleanup(func() {
		tmuxListSessions = oldList
		tmuxNewSession = oldNew
		tmuxAttachSession = oldAttach
		tmuxKillSession = oldKill
		tmuxRenameSession = oldRename
	})
}

func TestHandleOpenTmuxList(t *testing.T) {
	restoreTmuxStubs(t)
	tmuxListSessions = func(context.Context) ([]types.TmuxSession, error) {
		return []types.TmuxSession{{Name: "work", CreatedUnix: 7, Attached: true}}, nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxList})
	out := newDaemonSink()

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 1, Payload: payload}, &sync.Mutex{}, map[uint32]*internalssh.InteractiveSession{}, out.write, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenOK)
	var got []relay.TmuxSessionInfo
	if err := json.Unmarshal(f.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "work" || got[0].CreatedUnix != 7 || !got[0].Attached {
		t.Fatalf("got %+v", got)
	}
}

func TestHandleOpenTmuxNewStartsStream(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	var gotRows, gotCols int
	tmuxNewSession = func(_ context.Context, rows, cols int) (*internalssh.InteractiveSession, string, error) {
		gotRows, gotCols = rows, cols
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew, Rows: 11, Cols: 90})
	out := newDaemonSink()
	sessions := map[uint32]*internalssh.InteractiveSession{}

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 2, Payload: payload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenOK)
	if string(f.Payload) != "tmux-abc123" {
		t.Fatalf("payload = %q", f.Payload)
	}
	if gotRows != 11 || gotCols != 90 {
		t.Fatalf("pty = %dx%d", gotRows, gotCols)
	}
	if sessions[2] != fake.is {
		t.Fatalf("session not registered")
	}
	go func() { _, _ = fake.stdout.Write([]byte("ok")) }()
	data := waitDaemonFrame(t, out, relay.FrameData)
	if data.StreamID != 2 || string(data.Payload) != "ok" {
		t.Fatalf("data = %+v", data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxNewCleansUpWhenOpenOKWriteFails(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	tmuxNewSession = func(context.Context, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	sessions := map[uint32]*internalssh.InteractiveSession{}

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 12, Payload: payload}, &sync.Mutex{}, sessions, func(relay.Frame) error {
		return errors.New("write failed")
	}, context.Background(), context.Background())

	if sessions[12] != nil {
		t.Fatal("session registered after OpenOK write failed")
	}
	if !fake.stdin.closed {
		t.Fatal("session not closed after OpenOK write failed")
	}
}

func TestHandleOpenTmuxNewReturnsOpenErrWhenSessionExitsImmediately(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	fake.done <- errors.New("tmux attach-session: exit status 1")
	killed := ""
	tmuxNewSession = func(context.Context, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	tmuxKillSession = func(_ context.Context, name string) error {
		killed = name
		return nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	out := newDaemonSink()
	sessions := map[uint32]*internalssh.InteractiveSession{}

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 15, Payload: payload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != "tmux attach-session: exit status 1" {
		t.Fatalf("open err payload = %q", f.Payload)
	}
	if sessions[15] != nil {
		t.Fatal("session registered after immediate exit")
	}
	if !fake.stdin.closed {
		t.Fatal("session not closed after immediate exit")
	}
	if killed != "tmux-abc123" {
		t.Fatalf("killed = %q", killed)
	}
}

func TestHandleOpenControlSendsCloseAfterOpenOK(t *testing.T) {
	restoreTmuxStubs(t)
	tmuxKillSession = func(context.Context, string) error { return nil }
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"})
	out := newDaemonSink()

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 13, Payload: payload}, &sync.Mutex{}, map[uint32]*internalssh.InteractiveSession{}, out.write, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	closeFrame := waitDaemonFrame(t, out, relay.FrameClose)
	if closeFrame.StreamID != 13 {
		t.Fatalf("close stream = %d", closeFrame.StreamID)
	}
}

func TestPumpSessionSendsClosePayloadOnSessionError(t *testing.T) {
	fake := newDaemonFakeSession()
	out := newDaemonSink()
	wantErr := errors.New("tmux attach-session: exit status 1")

	go pumpSession(context.Background(), 14, fake.is, out.write, func(uint32, *internalssh.InteractiveSession) bool {
		return true
	})
	fake.done <- wantErr

	closeFrame := waitDaemonFrame(t, out, relay.FrameClose)
	if closeFrame.StreamID != 14 {
		t.Fatalf("close stream = %d", closeFrame.StreamID)
	}
	if string(closeFrame.Payload) != wantErr.Error() {
		t.Fatalf("close payload = %q", closeFrame.Payload)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxErrorTargetsReturnOpenErr(t *testing.T) {
	restoreTmuxStubs(t)
	wantErr := errors.New("tmux failed")
	tmuxListSessions = func(context.Context) ([]types.TmuxSession, error) { return nil, wantErr }
	tmuxNewSession = func(context.Context, int, int) (*internalssh.InteractiveSession, string, error) {
		return nil, "", wantErr
	}
	tmuxAttachSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, error) { return nil, wantErr }
	tmuxKillSession = func(context.Context, string) error { return wantErr }
	tmuxRenameSession = func(context.Context, string, string) error { return wantErr }

	tests := []relay.OpenRequest{
		{Target: relay.TargetTmuxList},
		{Target: relay.TargetTmuxNew},
		{Target: relay.TargetTmuxAttach, SessionID: "work"},
		{Target: relay.TargetTmuxKill, SessionID: "work"},
		{Target: relay.TargetTmuxRename, SessionID: "work", Name: "ops"},
	}
	for i, req := range tests {
		payload, _ := json.Marshal(req)
		out := newDaemonSink()

		handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: uint32(i + 20), Payload: payload}, &sync.Mutex{}, map[uint32]*internalssh.InteractiveSession{}, out.write, context.Background(), context.Background())

		f := waitDaemonFrame(t, out, relay.FrameOpenErr)
		if string(f.Payload) != wantErr.Error() {
			t.Fatalf("%s payload = %q", req.Target, f.Payload)
		}
	}
}

func TestHandleFrameRoutesDataResizeAndCloseToSession(t *testing.T) {
	fake := newDaemonFakeSession()
	sessions := map[uint32]*internalssh.InteractiveSession{31: fake.is}
	mu := sync.Mutex{}

	handleFrame(&runtimeConfig{}, relay.Frame{Type: relay.FrameData, StreamID: 31, Payload: []byte("input")}, &mu, sessions, nil, context.Background())
	handleFrame(&runtimeConfig{}, relay.Frame{Type: relay.FrameResize, StreamID: 31, Payload: relay.ResizePayload(40, 100)}, &mu, sessions, nil, context.Background())
	handleFrame(&runtimeConfig{}, relay.Frame{Type: relay.FrameClose, StreamID: 31}, &mu, sessions, nil, context.Background())

	if fake.stdin.String() != "input" {
		t.Fatalf("stdin = %q", fake.stdin.String())
	}
	if len(fake.resizes) != 1 || fake.resizes[0] != [2]int{40, 100} {
		t.Fatalf("resizes = %+v", fake.resizes)
	}
	if sessions[31] != nil {
		t.Fatal("session still registered")
	}
	if !fake.stdin.closed {
		t.Fatal("session not closed")
	}
}

func TestHandleFrameDispatchesOpenWithoutBlocking(t *testing.T) {
	restoreTmuxStubs(t)
	started := make(chan struct{})
	release := make(chan struct{})
	tmuxListSessions = func(context.Context) ([]types.TmuxSession, error) {
		close(started)
		<-release
		return nil, nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxList})
	out := newDaemonSink()

	handleFrame(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 41, Payload: payload}, &sync.Mutex{}, map[uint32]*internalssh.InteractiveSession{}, out.write, context.Background())

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("open handler did not start")
	}
	select {
	case f := <-out.frames:
		t.Fatalf("open completed before release: %+v", f)
	default:
	}
	close(release)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
}

func TestHandleFrameTmuxOpenKeepsStreamAfterRequestReturns(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	tmuxNewSession = func(context.Context, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	out := newDaemonSink()
	sessions := map[uint32]*internalssh.InteractiveSession{}

	handleFrame(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 42, Payload: payload}, &sync.Mutex{}, sessions, out.write, context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	select {
	case f := <-out.frames:
		if f.Type == relay.FrameClose {
			t.Fatal("stream closed after open request returned")
		}
	case <-time.After(100 * time.Millisecond):
	}
	go func() { _, _ = fake.stdout.Write([]byte("ok")) }()
	data := waitDaemonFrame(t, out, relay.FrameData)
	if data.StreamID != 42 || string(data.Payload) != "ok" {
		t.Fatalf("data = %+v", data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxAttachKeepsExistingStream(t *testing.T) {
	restoreTmuxStubs(t)
	first := newDaemonFakeSession()
	second := newDaemonFakeSession()
	var calls int
	tmuxAttachSession = func(_ context.Context, name string, rows, cols int) (*internalssh.InteractiveSession, error) {
		if name != "work" {
			t.Fatalf("name = %q", name)
		}
		calls++
		if calls == 1 {
			return first.is, nil
		}
		return second.is, nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxAttach, SessionID: "work"})
	out := newDaemonSink()
	sessions := map[uint32]*internalssh.InteractiveSession{}

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 3, Payload: payload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())
	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 4, Payload: payload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if len(sessions) != 2 || sessions[3] != first.is || sessions[4] != second.is {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestHandleOpenTmuxKillAndRename(t *testing.T) {
	restoreTmuxStubs(t)
	var killed, renamedFrom, renamedTo string
	tmuxKillSession = func(_ context.Context, name string) error {
		killed = name
		return nil
	}
	tmuxRenameSession = func(_ context.Context, oldName, newName string) error {
		renamedFrom = oldName
		renamedTo = newName
		return nil
	}
	out := newDaemonSink()
	sessions := map[uint32]*internalssh.InteractiveSession{}
	killPayload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"})
	renamePayload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxRename, SessionID: "work", Name: "ops"})

	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 5, Payload: killPayload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())
	handleOpen(&runtimeConfig{}, relay.Frame{Type: relay.FrameOpen, StreamID: 6, Payload: renamePayload}, &sync.Mutex{}, sessions, out.write, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if killed != "work" {
		t.Fatalf("killed = %q", killed)
	}
	if renamedFrom != "work" || renamedTo != "ops" {
		t.Fatalf("rename = %q -> %q", renamedFrom, renamedTo)
	}
}

func TestPumpSessionRemovesEndedStream(t *testing.T) {
	fake := newDaemonFakeSession()
	out := newDaemonSink()
	done := make(chan struct{}, 1)
	sessions := map[uint32]*internalssh.InteractiveSession{7: fake.is}

	go pumpSession(context.Background(), 7, fake.is, out.write, func(streamID uint32, is *internalssh.InteractiveSession) bool {
		delete(sessions, streamID)
		done <- struct{}{}
		return true
	})

	_ = fake.stdout.Close()
	_ = waitDaemonFrame(t, out, relay.FrameClose)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for cleanup")
	}
	if _, ok := sessions[7]; ok {
		t.Fatal("ended stream still registered")
	}
}
