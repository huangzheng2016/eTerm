package daemon

import (
	"io"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

type fakeSession struct {
	out     *io.PipeWriter
	resizes [][2]int
}

func newFakeSession() (*internalssh.InteractiveSession, *fakeSession) {
	pr, pw := io.Pipe()
	f := &fakeSession{out: pw}
	is := &internalssh.InteractiveSession{
		Stdin:  pw,
		Stdout: pr,
		Resize: func(rows, cols int) error {
			f.resizes = append(f.resizes, [2]int{rows, cols})
			return nil
		},
	}
	return is, f
}

func newTestManager() *shellManager {
	return newShellManager(func(rows, cols int) (*internalssh.InteractiveSession, error) {
		is, _ := newFakeSession()
		return is, nil
	}, func() string { return "/bin/zsh" })
}

type frameSink struct {
	frames chan relay.Frame
}

func newSink() *frameSink {
	return &frameSink{frames: make(chan relay.Frame, 64)}
}

func (s *frameSink) write(f relay.Frame) error {
	s.frames <- f
	return nil
}

func waitFrame(t *testing.T, s *frameSink, typ relay.FrameType) relay.Frame {
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

func TestShellManagerNewListKill(t *testing.T) {
	m := newTestManager()
	sink := newSink()
	s, err := m.create(10, 24, 80, sink.write)
	if err != nil {
		t.Fatal(err)
	}
	list := m.list()
	if len(list) != 1 || list[0].ID != s.id || !list[0].Busy || list[0].Shell != "/bin/zsh" {
		t.Fatalf("unexpected list: %+v", list)
	}
	m.kill(s.id)
	if len(m.list()) != 0 {
		t.Fatalf("shell not removed after kill")
	}
}

func TestShellManagerPumpForwardsAndReplays(t *testing.T) {
	m := newTestManager()
	sink := newSink()
	s, err := m.create(10, 24, 80, sink.write)
	if err != nil {
		t.Fatal(err)
	}
	s.start()
	s.is.Stdin.(*io.PipeWriter).Write([]byte("live"))
	f := waitFrame(t, sink, relay.FrameData)
	if string(f.Payload) != "live" || f.StreamID != 10 {
		t.Fatalf("got %q stream %d", f.Payload, f.StreamID)
	}

	m.detachStream(10)
	s.is.Stdin.(*io.PipeWriter).Write([]byte("buffered"))
	time.Sleep(50 * time.Millisecond)

	sink2 := newSink()
	if _, _, err := m.attach(s.id, 11, 30, 100, sink2.write); err != nil {
		t.Fatal(err)
	}
	replay := waitFrame(t, sink2, relay.FrameData)
	if string(replay.Payload) != "livebuffered" || replay.StreamID != 11 {
		t.Fatalf("replay = %q stream %d", replay.Payload, replay.StreamID)
	}
}

func TestShellManagerTakeover(t *testing.T) {
	m := newTestManager()
	sink1 := newSink()
	s, err := m.create(10, 24, 80, sink1.write)
	if err != nil {
		t.Fatal(err)
	}
	sink2 := newSink()
	_, displaced, err := m.attach(s.id, 11, 24, 80, sink2.write)
	if err != nil {
		t.Fatal(err)
	}
	if displaced != 10 {
		t.Fatalf("expected displaced stream 10, got %d", displaced)
	}
	closed := waitFrame(t, sink1, relay.FrameClose)
	if closed.StreamID != 10 {
		t.Fatalf("expected close on old stream 10, got %d", closed.StreamID)
	}
	s.mu.Lock()
	if s.stream != 11 {
		t.Fatalf("stream not rebound, got %d", s.stream)
	}
	s.mu.Unlock()
}

func TestShellManagerKillNotifiesAttached(t *testing.T) {
	m := newTestManager()
	sink := newSink()
	s, err := m.create(10, 24, 80, sink.write)
	if err != nil {
		t.Fatal(err)
	}
	m.kill(s.id)
	closed := waitFrame(t, sink, relay.FrameClose)
	if closed.StreamID != 10 {
		t.Fatalf("expected close on stream 10, got %d", closed.StreamID)
	}
}

func TestShellManagerAttachResize(t *testing.T) {
	m := newTestManager()
	is, fake := newFakeSession()
	m.newSession = func(rows, cols int) (*internalssh.InteractiveSession, error) { return is, nil }
	sink := newSink()
	s, err := m.create(10, 24, 80, sink.write)
	if err != nil {
		t.Fatal(err)
	}
	m.detachStream(10)
	sink2 := newSink()
	if _, _, err := m.attach(s.id, 11, 40, 120, sink2.write); err != nil {
		t.Fatal(err)
	}
	if len(fake.resizes) == 0 || fake.resizes[len(fake.resizes)-1] != [2]int{40, 120} {
		t.Fatalf("resize not applied: %+v", fake.resizes)
	}
}

func TestShellManagerAttachNotFound(t *testing.T) {
	m := newTestManager()
	if _, _, err := m.attach("nope", 1, 24, 80, newSink().write); err != errShellNotFound {
		t.Fatalf("got %v", err)
	}
}
