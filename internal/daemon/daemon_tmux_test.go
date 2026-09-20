package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	database := testDaemonDB(t)
	if err := db.SetSetting(database, tmux.SettingConfigFile, filepath.Join(t.TempDir(), "tmux.conf")); err != nil {
		t.Fatal(err)
	}
	return &runtimeConfig{db: database, hasTmux: true}
}

func stubTmuxNewFake(t *testing.T, name string) *daemonFakeSession {
	t.Helper()
	fake := newDaemonFakeSession()
	tmuxNewSession = func(context.Context, string, int, int) (*internalssh.InteractiveSession, string, error) {
		return fake.is, name, nil
	}
	return fake
}

func callOpen(t *testing.T, sid uint32, req relay.OpenRequest, mgr *sessionManager, sender *frameSender) {
	t.Helper()
	payload, _ := json.Marshal(req)
	handleOpen(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: sid, Payload: payload}, mgr, sender, context.Background(), context.Background())
}

func wantOpenErr(t *testing.T, out *daemonFrameSink, want string) {
	t.Helper()
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != want {
		t.Fatalf("open err payload = %q, want %q", f.Payload, want)
	}
}

func startTestPump(t *testing.T, is *internalssh.InteractiveSession, sid uint32) (*streamRelay, *sessionManager, *daemonFrameSink) {
	t.Helper()
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)
	sr := newStreamRelay(is)
	mgr.add(sid, sr)
	go sr.pump(context.Background(), sid, mgr)
	return sr, mgr, out
}

func waitRingEnd(t *testing.T, sr *streamRelay, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sr.mu.Lock()
		end := sr.ring.End()
		sr.mu.Unlock()
		if end >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ring end = %d, want %d", end, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newDaemonSink() *daemonFrameSink {
	return &daemonFrameSink{frames: make(chan relay.Frame, 256)}
}

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
				case b := <-s.data:
					df, err := relay.Decode(b)
					if err != nil {
						return
					}
					f = df
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
	sender, out := newTestSender()

	callOpen(t, 1, relay.OpenRequest{Target: relay.TargetTmuxList}, newSessionManager(), sender)

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
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)

	callOpen(t, 2, relay.OpenRequest{Target: relay.TargetTmuxNew, Rows: 11, Cols: 90}, mgr, sender)

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
	fake := stubTmuxNewFake(t, "tmux-abc123")
	mgr := newSessionManager()
	sender := newFrameSender()
	close(sender.done)

	callOpen(t, 12, relay.OpenRequest{Target: relay.TargetTmuxNew}, mgr, sender)

	if mgr.get(12) != nil {
		t.Fatal("session registered after OpenOK write failed")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed after OpenOK write failed")
	}
}

func TestHandleOpenTmuxNewReturnsOpenErrWhenSessionExitsImmediately(t *testing.T) {
	restoreTmuxStubs(t)
	fake := stubTmuxNewFake(t, "tmux-abc123")
	fake.done <- errors.New("tmux attach-session: exit status 1")
	killed := ""
	tmuxKillSession = func(_ context.Context, _ string, name string) error {
		killed = name
		return nil
	}
	sender, out := newTestSender()
	mgr := newSessionManager()

	callOpen(t, 15, relay.OpenRequest{Target: relay.TargetTmuxNew}, mgr, sender)

	wantOpenErr(t, out, "tmux attach-session: exit status 1")
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
	sender, out := newTestSender()

	callOpen(t, 13, relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"}, newSessionManager(), sender)

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	closeFrame := waitDaemonFrame(t, out, relay.FrameClose)
	if closeFrame.StreamID != 13 {
		t.Fatalf("close stream = %d", closeFrame.StreamID)
	}
}

func TestPumpSendsClosePayloadOnSessionError(t *testing.T) {
	fake := newDaemonFakeSession()
	_, _, out := startTestPump(t, fake.is, 14)
	wantErr := errors.New("tmux attach-session: exit status 1")
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
	_, _, out := startTestPump(t, is, 16)

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
	_, _, out := startTestPump(t, is, 17)

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
		Stdout: bytes.NewReader(bytes.Repeat([]byte("x"), 2*outputWindowBytes)),
		Done:   done,
	}
	sr, _, out := startTestPump(t, is, 50)

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

	mgr.clearSender(sender)
	go func() { _, _ = fake.stdout.Write([]byte("world")) }()
	waitRingEnd(t, sr, 11)

	sender2, out2 := newTestSender()
	mgr.setSender(sender2)
	callOpen(t, 60, relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 3}, mgr, sender2)

	waitDaemonFrame(t, out2, relay.FrameOpenOK)
	seq, data := waitDataBytes(t, out2, 8)
	if seq != 3 || string(data) != "lo world" {
		t.Fatalf("replay seq=%d data=%q", seq, data)
	}
	_ = fake.stdout.Close()
}

func TestHandleOpenResumeUnknownStreamFails(t *testing.T) {
	sender, out := newTestSender()

	callOpen(t, 61, relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 10}, newSessionManager(), sender)

	wantOpenErr(t, out, resumeUnavailableErr)
}

func TestHandleOpenResumeBeyondBufferFails(t *testing.T) {
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(62, sr)
	sender, out := newTestSender()

	callOpen(t, 62, relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 100}, mgr, sender)

	wantOpenErr(t, out, resumeUnavailableErr)
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
		sender, out := newTestSender()

		callOpen(t, uint32(i+20), req, newSessionManager(), sender)

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
	fake := stubTmuxNewFake(t, "tmux-abc123")
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
	req := relay.OpenRequest{Target: relay.TargetTmuxAttach, SessionID: "work"}
	sender, out := newTestSender()
	mgr := newSessionManager()
	mgr.setSender(sender)

	callOpen(t, 3, req, mgr, sender)
	callOpen(t, 4, req, mgr, sender)

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

	callOpen(t, 5, relay.OpenRequest{Target: relay.TargetTmuxKill, SessionID: "work"}, mgr, sender)
	callOpen(t, 6, relay.OpenRequest{Target: relay.TargetTmuxRename, SessionID: "work", Name: "ops"}, mgr, sender)

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
	_, mgr, out := startTestPump(t, fake.is, 7)

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
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(70, sr)

	go sr.pump(context.Background(), 70, mgr)
	go func() { _, _ = fake.stdout.Write([]byte("tail")) }()
	waitRingEnd(t, sr, 4)
	_ = fake.stdout.Close()

	time.Sleep(50 * time.Millisecond)
	if mgr.get(70) == nil {
		t.Fatal("ended detached stream was removed")
	}

	sender, out := newTestSender()
	mgr.setSender(sender)
	callOpen(t, 70, relay.OpenRequest{PeerID: "p", Target: relay.TargetLocal, ResumeFromSeq: 0}, mgr, sender)

	waitDaemonFrame(t, out, relay.FrameOpenOK)
	if _, data := waitDataBytes(t, out, 4); string(data) != "tail" {
		t.Fatalf("replay = %q, want tail", data)
	}
	_ = waitDaemonFrame(t, out, relay.FrameClose)
	deadline := time.Now().Add(2 * time.Second)
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

func TestClosePayload(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ignores wrapped EIO", &os.PathError{Op: "read", Path: "/dev/ptmx", Err: syscall.EIO}, ""},
		{"includes non-EIO error", errors.New("read failed"), "read failed"},
	}
	for _, tt := range tests {
		if got := string(closePayload(tt.err)); got != tt.want {
			t.Fatalf("%s: close payload = %q, want %q", tt.name, got, tt.want)
		}
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

func TestHandleFrameCloseUnknownStreamLoggedAndRecorded(t *testing.T) {
	out := captureStallLog(t)
	mgr := newSessionManager()

	handleFrame(nil, relay.Frame{Type: relay.FrameClose, StreamID: 5, Payload: []byte("gone")}, mgr, nil, context.Background())

	logs := out.String()
	if !strings.Contains(logs, "close stream=5") || !strings.Contains(logs, "dropped: no such stream") {
		t.Fatalf("swallowed close not logged: %q", logs)
	}
	mgr.mu.Lock()
	pending := mgr.pendingClose[5]
	mgr.mu.Unlock()
	if !pending {
		t.Fatal("early close not recorded")
	}
}

func TestHandleOpenAbortsWhenCloseArrivedFirst(t *testing.T) {
	restoreTmuxStubs(t)
	out := captureStallLog(t)
	fake := stubTmuxNewFake(t, "tmux-race")
	mgr := newSessionManager()
	sender, sink := newTestSender()
	mgr.setSender(sender)
	mgr.notePendingClose(2)

	callOpen(t, 2, relay.OpenRequest{Target: relay.TargetTmuxNew}, mgr, sender)

	if mgr.get(2) != nil {
		t.Fatal("stream registered after raced close")
	}
	if !fake.stdin.isClosed() {
		t.Fatal("session not closed after raced close")
	}
	if !strings.Contains(out.String(), "stream=2 aborted") {
		t.Fatalf("abort not logged: %q", out.String())
	}
	select {
	case f := <-sink.frames:
		t.Fatalf("frame sent for aborted stream: %+v", f)
	default:
	}
	mgr.mu.Lock()
	pending := mgr.pendingClose[2]
	mgr.mu.Unlock()
	if pending {
		t.Fatal("pending close entry leaked")
	}
}

func TestHandleOpenResumeUnavailableLogged(t *testing.T) {
	out := captureStallLog(t)
	sender, sink := newTestSender()

	callOpen(t, 9, relay.OpenRequest{Target: relay.TargetLocal, ResumeFromSeq: 9}, newSessionManager(), sender)

	wantOpenErr(t, sink, resumeUnavailableErr)
	logs := out.String()
	if !strings.Contains(logs, "stream=9 resume=9 rejected") {
		t.Fatalf("resume rejection not logged: %q", logs)
	}
}

func TestReapDetachedLogsReapedStream(t *testing.T) {
	out := captureStallLog(t)
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	mgr.add(80, sr)
	sr.mu.Lock()
	sr.detachedSince = time.Now().Add(-detachedStreamTTL - time.Minute)
	sr.mu.Unlock()

	mgr.reapDetached(time.Now())

	if mgr.get(80) != nil {
		t.Fatal("expired detached stream kept")
	}
	logs := out.String()
	if !strings.Contains(logs, "stream 80 reaped detached after") {
		t.Fatalf("reap not logged: %q", logs)
	}
}

func TestClearSenderLogsMarkedDetached(t *testing.T) {
	out := captureStallLog(t)
	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	sr := newStreamRelay(fake.is)
	t.Cleanup(sr.shutdown)
	mgr.add(33, sr)
	sender := newFrameSender()
	mgr.setSender(sender)

	mgr.clearSender(sender)

	if !strings.Contains(out.String(), "1 stream(s) marked detached") {
		t.Fatalf("detach marking not logged: %q", out.String())
	}
	sr.mu.Lock()
	detached := !sr.detachedSince.IsZero()
	sr.mu.Unlock()
	if !detached {
		t.Fatal("stream not marked detached")
	}
}
