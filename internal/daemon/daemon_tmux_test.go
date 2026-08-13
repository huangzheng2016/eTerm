package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/types"
)

type daemonFrameSink struct {
	frames chan relay.Frame
}

func testTmuxRuntime(t *testing.T) *runtimeConfig {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "eterm.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(database, tmux.SettingConfigFile, filepath.Join(t.TempDir(), "tmux.conf")); err != nil {
		t.Fatal(err)
	}
	return &runtimeConfig{db: database}
}

func newDaemonSink() *daemonFrameSink {
	return &daemonFrameSink{frames: make(chan relay.Frame, 256)}
}

// newTestSender drains a frameSender into a sink with the same control-first
// priority as frameSender.run.
func newTestSender() (*frameSender, *daemonFrameSink) {
	s := newFrameSender()
	sink := newDaemonSink()
	go func() {
		for {
			var f relay.Frame
			select {
			case f = <-s.ctrl:
			default:
				select {
				case f = <-s.ctrl:
				case f = <-s.data:
				case <-s.done:
					return
				}
			}
			sink.frames <- f
		}
	}()
	return s, sink
}

func waitDaemonFrame(t *testing.T, s *daemonFrameSink, typ relay.FrameType) relay.Frame {
	t.Helper()
	for {
		select {
		case f := <-s.frames:
			if f.Type == typ {
				return f
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for frame type 0x%02x", typ)
		}
	}
}

func waitDataBytes(t *testing.T, s *daemonFrameSink, want int) (uint64, []byte) {
	t.Helper()
	var out []byte
	var firstSeq uint64
	deadline := time.After(2 * time.Second)
	for len(out) < want {
		select {
		case f := <-s.frames:
			if f.Type != relay.FrameData {
				continue
			}
			seq, data, err := relay.ParseData(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) == 0 {
				firstSeq = seq
			} else if seq != firstSeq+uint64(len(out)) {
				t.Fatalf("seq gap: got %d, want %d", seq, firstSeq+uint64(len(out)))
			}
			out = append(out, data...)
		case <-deadline:
			t.Fatalf("timeout waiting for %d data bytes, got %d", want, len(out))
		}
	}
	return firstSeq, out
}

type daemonWriteCloser struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed bool
}

func (w *daemonWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *daemonWriteCloser) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *daemonWriteCloser) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return nil
}

func (w *daemonWriteCloser) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

type daemonOneByteReader struct {
	data []byte
	pos  int
}

func (r *daemonOneByteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
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
	tmuxListSessions = func(context.Context, string) ([]types.TmuxSession, error) {
		return []types.TmuxSession{{Name: "work", CreatedUnix: 7, Attached: true}}, nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxList})
	sender, out := newTestSender()

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 1, Payload: payload}, newSessionManager(), sender, context.Background(), context.Background())

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
	tmuxNewSession = func(_ context.Context, _ string, rows, cols int) (*internalssh.InteractiveSession, string, error) {
		gotRows, gotCols = rows, cols
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew, Rows: 11, Cols: 90})
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 2, Payload: payload}, mgr, sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenOK)
	if string(f.Payload) != "tmux-abc123" {
		t.Fatalf("payload = %q", f.Payload)
	}
	if gotRows != 11 || gotCols != 90 {
		t.Fatalf("pty = %dx%d", gotRows, gotCols)
	}
	if sr := mgr.get(2); sr == nil || sr.is != fake.is {
		t.Fatalf("session not registered")
	}
	go func() { _, _ = fake.stdout.Write([]byte("ok")) }()
	seq, data := waitDataBytes(t, out, 2)
	if seq != 0 || string(data) != "ok" {
		t.Fatalf("data seq=%d %q", seq, data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxNewCleansUpWhenOpenOKWriteFails(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	tmuxNewSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	mgr := newSessionManager()
	sender := newFrameSender()
	close(sender.done)

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 12, Payload: payload}, mgr, sender, context.Background(), context.Background())

	if mgr.get(12) != nil {
		t.Fatal("session registered after OpenOK write failed")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed after OpenOK write failed")
	}
}

func TestHandleOpenTmuxNewReturnsOpenErrWhenSessionExitsImmediately(t *testing.T) {
	restoreTmuxStubs(t)
	fake := newDaemonFakeSession()
	fake.done <- errors.New("tmux attach-session: exit status 1")
	killed := ""
	tmuxNewSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	tmuxKillSession = func(_ context.Context, _ string, name string) error {
		killed = name
		return nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	sender, out := newTestSender()
	mgr := newSessionManager()

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 15, Payload: payload}, mgr, sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != "tmux attach-session: exit status 1" {
		t.Fatalf("open err payload = %q", f.Payload)
	}
	if mgr.get(15) != nil {
		t.Fatal("session registered after immediate exit")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed after immediate exit")
	}
	if killed != "tmux-abc123" {
		t.Fatalf("killed = %q", killed)
	}
}

func TestHandleOpenControlSendsCloseAfterOpenOK(t *testing.T) {
	restoreTmuxStubs(t)
	tmuxKillSession = func(context.Context, string, string) error { return nil }
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"})
	sender, out := newTestSender()

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 13, Payload: payload}, newSessionManager(), sender, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	closeFrame := waitDaemonFrame(t, out, relay.FrameClose)
	if closeFrame.StreamID != 13 {
		t.Fatalf("close stream = %d", closeFrame.StreamID)
	}
}

func TestPumpSendsClosePayloadOnSessionError(t *testing.T) {
	fake := newDaemonFakeSession()
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(fake.is)
	mgr.add(14, sr)
	wantErr := errors.New("tmux attach-session: exit status 1")

	go sr.pump(context.Background(), 14, mgr)
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

func TestPumpDeliversAllOutput(t *testing.T) {
	done := make(chan error)
	is := &internalssh.InteractiveSession{
		Stdout: &daemonOneByteReader{data: []byte("abc")},
		Done:   done,
	}
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(is)
	mgr.add(16, sr)

	go sr.pump(context.Background(), 16, mgr)

	_, data := waitDataBytes(t, out, 3)
	if string(data) != "abc" {
		t.Fatalf("data payload = %q", data)
	}
	_ = waitDaemonFrame(t, out, relay.FrameClose)
}

func TestPumpCapsOutputFrameSize(t *testing.T) {
	done := make(chan error)
	is := &internalssh.InteractiveSession{
		Stdout: bytes.NewReader(bytes.Repeat([]byte("x"), 40*1024)),
		Done:   done,
	}
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(is)
	mgr.add(17, sr)

	go sr.pump(context.Background(), 17, mgr)

	var total int
	var wantSeq uint64
	for total < 40*1024 {
		data := waitDaemonFrame(t, out, relay.FrameData)
		seq, payload, err := relay.ParseData(data.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if seq != wantSeq {
			t.Fatalf("seq = %d, want %d", seq, wantSeq)
		}
		if len(payload) > maxOutputFrameBytes {
			t.Fatalf("payload len = %d, want <= %d", len(payload), maxOutputFrameBytes)
		}
		wantSeq += uint64(len(payload))
		total += len(payload)
	}
	if total != 40*1024 {
		t.Fatalf("total = %d", total)
	}
}

func TestPumpAppliesWindowBackpressure(t *testing.T) {
	done := make(chan error)
	is := &internalssh.InteractiveSession{
		Stdout: bytes.NewReader(bytes.Repeat([]byte("x"), 1024*1024)),
		Done:   done,
	}
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(is)
	mgr.add(50, sr)

	go sr.pump(context.Background(), 50, mgr)

	_, data := waitDataBytes(t, out, outputWindowBytes)
	if len(data) != outputWindowBytes {
		t.Fatalf("got %d bytes before window check", len(data))
	}
	select {
	case f := <-out.frames:
		t.Fatalf("frame sent with window exhausted: type 0x%02x", f.Type)
	case <-time.After(100 * time.Millisecond):
	}

	sr.setAck(outputWindowBytes)
	_, more := waitDataBytes(t, out, maxOutputFrameBytes)
	if len(more) == 0 {
		t.Fatal("pump did not resume after ack")
	}
}

func TestHandleOpenResumesFromRetainedOffset(t *testing.T) {
	fake := newDaemonFakeSession()
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(fake.is)
	mgr.add(60, sr)

	go sr.pump(context.Background(), 60, mgr)
	go func() { _, _ = fake.stdout.Write([]byte("hello ")) }()
	if _, data := waitDataBytes(t, out, 6); string(data) != "hello " {
		t.Fatalf("data = %q", data)
	}

	// Connection drops; output continues into the ring.
	mgr.clearSender(sender)
	go func() { _, _ = fake.stdout.Write([]byte("world")) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sr.mu.Lock()
		end := sr.ring.End()
		sr.mu.Unlock()
		if end == 11 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ring end = %d, want 11", end)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Client reconnects having consumed only "hel".
	sender2, out2 := newTestSender()
	mgr.setSender(sender2)
	payload, _ := json.Marshal(relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 3})
	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 60, Payload: payload}, mgr, sender2, context.Background(), context.Background())

	waitDaemonFrame(t, out2, relay.FrameOpenOK)
	seq, data := waitDataBytes(t, out2, 8)
	if seq != 3 || string(data) != "lo world" {
		t.Fatalf("replay seq=%d data=%q", seq, data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenResumeUnknownStreamFails(t *testing.T) {
	sender, out := newTestSender()
	payload, _ := json.Marshal(relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 10})

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 61, Payload: payload}, newSessionManager(), sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != resumeUnavailableErr {
		t.Fatalf("open err payload = %q", f.Payload)
	}
}

func TestHandleOpenResumeBeyondBufferFails(t *testing.T) {
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(62, sr)
	sender, out := newTestSender()
	payload, _ := json.Marshal(relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 100})

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 62, Payload: payload}, mgr, sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != resumeUnavailableErr {
		t.Fatalf("open err payload = %q", f.Payload)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxErrorTargetsReturnOpenErr(t *testing.T) {
	restoreTmuxStubs(t)
	wantErr := errors.New("tmux failed")
	tmuxListSessions = func(context.Context, string) ([]types.TmuxSession, error) { return nil, wantErr }
	tmuxNewSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, string, error) {
		return nil, "", wantErr
	}
	tmuxAttachSession = func(context.Context, string, string, int, int) (*internalssh.InteractiveSession, error) {
		return nil, wantErr
	}
	tmuxKillSession = func(context.Context, string, string) error { return wantErr }
	tmuxRenameSession = func(context.Context, string, string, string) error { return wantErr }

	tests := []relay.OpenRequest{
		{Target: relay.TargetTmuxList},
		{Target: relay.TargetTmuxNew},
		{Target: relay.TargetTmuxAttach, SessionID: "work"},
		{Target: relay.TargetTmuxKill, SessionID: "work"},
		{Target: relay.TargetTmuxRename, SessionID: "work", Name: "ops"},
	}
	for i, req := range tests {
		payload, _ := json.Marshal(req)
		sender, out := newTestSender()

		handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: uint32(i + 20), Payload: payload}, newSessionManager(), sender, context.Background(), context.Background())

		f := waitDaemonFrame(t, out, relay.FrameOpenErr)
		if string(f.Payload) != wantErr.Error() {
			t.Fatalf("%s payload = %q", req.Target, f.Payload)
		}
	}
}

func TestHandleFrameRoutesDataResizeAckAndCloseToSession(t *testing.T) {
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(31, sr)
	rt := testTmuxRuntime(t)

	sr.appendOutput([]byte("out"))
	sr.mu.Lock()
	sr.sent = 3
	sr.mu.Unlock()
	handleFrame(rt, relay.Frame{Type: relay.FrameData, StreamID: 31, Payload: []byte("input")}, mgr, nil, context.Background())
	// Input is written by the stream's input pump; wait for it before the
	// close below shuts the pump down.
	deadline := time.Now().Add(2 * time.Second)
	for fake.stdin.String() != "input" {
		if time.Now().After(deadline) {
			t.Fatalf("stdin = %q", fake.stdin.String())
		}
		time.Sleep(time.Millisecond)
	}
	handleFrame(rt, relay.Frame{Type: relay.FrameResize, StreamID: 31, Payload: relay.ResizePayload(40, 100)}, mgr, nil, context.Background())
	handleFrame(rt, relay.Frame{Type: relay.FrameAck, StreamID: 31, Payload: relay.AckPayload(3)}, mgr, nil, context.Background())
	handleFrame(rt, relay.Frame{Type: relay.FrameClose, StreamID: 31}, mgr, nil, context.Background())
	if len(fake.resizes) != 1 || fake.resizes[0] != [2]int{40, 100} {
		t.Fatalf("resizes = %+v", fake.resizes)
	}
	sr.mu.Lock()
	ack := sr.ack
	sr.mu.Unlock()
	if ack != 3 {
		t.Fatalf("ack = %d, want 3", ack)
	}
	if mgr.get(31) != nil {
		t.Fatal("session still registered")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed")
	}
}

func TestHandleFrameDispatchesOpenWithoutBlocking(t *testing.T) {
	restoreTmuxStubs(t)
	started := make(chan struct{})
	release := make(chan struct{})
	tmuxListSessions = func(context.Context, string) ([]types.TmuxSession, error) {
		close(started)
		<-release
		return nil, nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxList})
	sender, out := newTestSender()

	handleFrame(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 41, Payload: payload}, newSessionManager(), sender, context.Background())

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
	tmuxNewSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, "tmux-abc123", nil
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxNew})
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)

	handleFrame(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 42, Payload: payload}, mgr, sender, context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	select {
	case f := <-out.frames:
		if f.Type == relay.FrameClose {
			t.Fatal("stream closed after open request returned")
		}
	case <-time.After(100 * time.Millisecond):
	}
	go func() { _, _ = fake.stdout.Write([]byte("ok")) }()
	_, data := waitDataBytes(t, out, 2)
	if string(data) != "ok" {
		t.Fatalf("data = %q", data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenTmuxAttachKeepsExistingStream(t *testing.T) {
	restoreTmuxStubs(t)
	first := newDaemonFakeSession()
	second := newDaemonFakeSession()
	var calls int
	tmuxAttachSession = func(_ context.Context, _ string, name string, rows, cols int) (*internalssh.InteractiveSession, error) {
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
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 3, Payload: payload}, mgr, sender, context.Background(), context.Background())
	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 4, Payload: payload}, mgr, sender, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if mgr.get(3) == nil || mgr.get(3).is != first.is || mgr.get(4) == nil || mgr.get(4).is != second.is {
		t.Fatal("sessions not registered")
	}
	_ = first.stdout.Close()
	_ = second.stdout.Close()
}

func TestHandleOpenTmuxKillAndRename(t *testing.T) {
	restoreTmuxStubs(t)
	var killed, renamedFrom, renamedTo string
	tmuxKillSession = func(_ context.Context, _ string, name string) error {
		killed = name
		return nil
	}
	tmuxRenameSession = func(_ context.Context, _ string, oldName, newName string) error {
		renamedFrom = oldName
		renamedTo = newName
		return nil
	}
	sender, out := newTestSender()
	mgr := newSessionManager()
	killPayload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"})
	renamePayload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxRename, SessionID: "work", Name: "ops"})

	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 5, Payload: killPayload}, mgr, sender, context.Background(), context.Background())
	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 6, Payload: renamePayload}, mgr, sender, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if killed != "work" {
		t.Fatalf("killed = %q", killed)
	}
	if renamedFrom != "work" || renamedTo != "ops" {
		t.Fatalf("rename = %q -> %q", renamedFrom, renamedTo)
	}
}

func TestPumpRemovesEndedStream(t *testing.T) {
	fake := newDaemonFakeSession()
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(fake.is)
	mgr.add(7, sr)

	go sr.pump(context.Background(), 7, mgr)

	_ = fake.stdout.Close()
	_ = waitDaemonFrame(t, out, relay.FrameClose)
	deadline := time.Now().Add(2 * time.Second)
	for mgr.get(7) != nil {
		if time.Now().After(deadline) {
			t.Fatal("ended stream still registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !fake.stdin.isClosed() {
		t.Fatal("ended session not closed")
	}
}

func TestOutputRingRoundTrip(t *testing.T) {
	r := newOutputRing()
	r.Write([]byte("hello"))
	r.Write([]byte(" world"))
	if r.End() != 11 {
		t.Fatalf("end = %d", r.End())
	}
	if got := r.ReadFrom(0, 64); string(got) != "hello world" {
		t.Fatalf("read = %q", got)
	}
	if got := r.ReadFrom(6, 64); string(got) != "world" {
		t.Fatalf("read from 6 = %q", got)
	}
	if got := r.ReadFrom(6, 2); string(got) != "wo" {
		t.Fatalf("read limited = %q", got)
	}
	if got := r.ReadFrom(11, 64); got != nil {
		t.Fatalf("read at end = %q", got)
	}
}

func TestOutputRingOverflowDropsOldest(t *testing.T) {
	r := newOutputRing()
	chunk := bytes.Repeat([]byte("a"), outputRingBytes/2)
	r.Write(chunk)
	r.Write(bytes.Repeat([]byte("b"), outputRingBytes/2+10))
	if r.End() != uint64(outputRingBytes+10) {
		t.Fatalf("end = %d", r.End())
	}
	if r.base != 10 {
		t.Fatalf("base = %d, want 10", r.base)
	}
	got := r.ReadFrom(r.base, outputRingBytes)
	want := append(bytes.Repeat([]byte("a"), outputRingBytes/2-10), bytes.Repeat([]byte("b"), outputRingBytes/2+10)...)
	if !bytes.Equal(got, want) {
		t.Fatal("ring content mismatch after overflow")
	}
	// Offsets older than base clamp to base.
	if got := r.ReadFrom(0, 3); !bytes.Equal(got, bytes.Repeat([]byte("a"), 3)) {
		t.Fatalf("clamped read = %q", got[:3])
	}
}

func TestOutputRingLargeWriteKeepsTail(t *testing.T) {
	r := newOutputRing()
	big := make([]byte, outputRingBytes+100)
	for i := range big {
		big[i] = byte(i)
	}
	r.Write([]byte("old"))
	r.Write(big)
	if r.base != uint64(103) || r.End() != uint64(len(big)+3) {
		t.Fatalf("base = %d end = %d", r.base, r.End())
	}
	got := r.ReadFrom(r.base, outputRingBytes)
	if !bytes.Equal(got, big[len(big)-outputRingBytes:]) {
		t.Fatal("ring does not hold the tail after large write")
	}
}

func TestPumpKeepsEndedStreamForResume(t *testing.T) {
	fake := newDaemonFakeSession()
	mgr := newSessionManager() // no sender: client detached
	sr := newStreamRelay(fake.is)
	mgr.add(70, sr)

	go sr.pump(context.Background(), 70, mgr)
	go func() { _, _ = fake.stdout.Write([]byte("tail")) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sr.mu.Lock()
		end := sr.ring.End()
		sr.mu.Unlock()
		if end == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ring end = %d, want 4", end)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = fake.stdout.Close()

	// Ended while detached: the stream stays registered with its ring.
	time.Sleep(50 * time.Millisecond)
	if mgr.get(70) == nil {
		t.Fatal("ended detached stream was removed")
	}

	sender, out := newTestSender()
	mgr.setSender(sender)
	payload, _ := json.Marshal(relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 0})
	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 70, Payload: payload}, mgr, sender, context.Background(), context.Background())

	waitDaemonFrame(t, out, relay.FrameOpenOK)
	if _, data := waitDataBytes(t, out, 4); string(data) != "tail" {
		t.Fatalf("replay = %q, want tail", data)
	}
	_ = waitDaemonFrame(t, out, relay.FrameClose)
	deadline = time.Now().Add(2 * time.Second)
	for mgr.get(70) != nil {
		if time.Now().After(deadline) {
			t.Fatal("stream not removed after final flush")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReapDetachedRemovesExpiredStreams(t *testing.T) {
	expired := newDaemonFakeSession()
	fresh := newDaemonFakeSession()
	mgr := newSessionManager()
	srExpired := newStreamRelay(expired.is)
	srFresh := newStreamRelay(fresh.is)
	mgr.add(80, srExpired)
	mgr.add(81, srFresh)
	mgr.setSender(newFrameSender())

	srExpired.mu.Lock()
	srExpired.detachedSince = time.Now().Add(-detachedStreamTTL - time.Minute)
	srExpired.mu.Unlock()

	mgr.reapDetached(time.Now())

	if mgr.get(80) != nil {
		t.Fatal("expired detached stream kept")
	}
	if mgr.get(81) == nil {
		t.Fatal("attached stream reaped")
	}
	if !expired.stdin.isClosed() {
		t.Fatal("reaped session not closed")
	}
	if fresh.stdin.isClosed() {
		t.Fatal("attached session closed")
	}
}

func TestHandleFrameClientDisconnectedKeepsSession(t *testing.T) {
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(32, sr)

	handleFrame(testTmuxRuntime(t), relay.Frame{Type: relay.FrameClose, StreamID: 32, Payload: []byte(relay.CloseClientDisconnected)}, mgr, nil, context.Background())

	if mgr.get(32) == nil {
		t.Fatal("session removed on client disconnect")
	}
	if fake.stdin.isClosed() {
		t.Fatal("session closed on client disconnect")
	}
	sr.mu.Lock()
	detached := !sr.detachedSince.IsZero()
	sr.mu.Unlock()
	if !detached {
		t.Fatal("stream not marked detached")
	}
}

func TestClosePayloadIgnoresWrappedEIO(t *testing.T) {
	err := &os.PathError{Op: "read", Path: "/dev/ptmx", Err: syscall.EIO}

	if got := closePayload(err); len(got) != 0 {
		t.Fatalf("close payload = %q", got)
	}
}

func TestClosePayloadIncludesNonEIOError(t *testing.T) {
	err := errors.New("read failed")

	if got := string(closePayload(err)); got != err.Error() {
		t.Fatalf("close payload = %q", got)
	}
}

func TestSessionDoneErrPrefersProcessFailure(t *testing.T) {
	readErr := &os.PathError{Op: "read", Path: "/dev/ptmx", Err: syscall.EIO}
	wantErr := errors.New("exit status 1")
	done := make(chan error, 1)
	done <- wantErr

	if got := sessionDoneErr(readErr, done); !errors.Is(got, wantErr) {
		t.Fatalf("session error = %v", got)
	}
}
