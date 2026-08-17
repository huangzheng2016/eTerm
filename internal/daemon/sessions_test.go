package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func stubLocalNewSession(t *testing.T, fakes *[]*daemonFakeSession) {
	t.Helper()
	old := localNewSession
	t.Cleanup(func() { localNewSession = old })
	localNewSession = func(string, int, int) (*internalssh.InteractiveSession, error) {
		f := newDaemonFakeSession()
		*fakes = append(*fakes, f)
		return f.is, nil
	}
}

func openTarget0(target, sessionID string, streamID uint32) relay.Frame {
	payload, _ := json.Marshal(relay.OpenRequest{Target: target, SessionID: sessionID})
	return relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload}
}

func TestDaemonSessionLifecycleWithoutTmux(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	// New: starts a persistent shell and answers with its name.
	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	f := waitDaemonFrame(t, out, relay.FrameOpenOK)
	name := string(f.Payload)
	if !strings.HasPrefix(name, "shell-") {
		t.Fatalf("name = %q", name)
	}

	// List: shows the session as attached.
	handleOpen(rt, openTarget0(relay.TargetTmuxList, "", 2), mgr, sender, ctx, ctx)
	lf := waitDaemonFrame(t, out, relay.FrameOpenOK)
	var listed []relay.TmuxSessionInfo
	if err := json.Unmarshal(lf.Payload, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != name || !listed[0].Attached || !listed[0].Daemon {
		t.Fatalf("listed = %+v", listed)
	}

	// Some output lands in the ring.
	go func() { _, _ = fakes[0].stdout.Write([]byte("hello")) }()
	if _, data := waitDataBytes(t, out, 5); string(data) != "hello" {
		t.Fatalf("data = %q", data)
	}

	// Closing the tab detaches instead of killing the shell.
	handleFrame(rt, relay.Frame{Type: relay.FrameClose, StreamID: 1}, mgr, sender, ctx)
	if mgr.get(1) == nil {
		t.Fatal("persistent session removed on tab close")
	}
	if fakes[0].stdin.isClosed() {
		t.Fatal("persistent session shell killed on tab close")
	}

	// Attach from a fresh stream replays retained output on the new stream id.
	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, name, 9), mgr, sender, ctx, ctx)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	deadline := time.After(2 * time.Second)
	var replay []byte
	for len(replay) < 5 {
		select {
		case f := <-out.frames:
			if f.Type != relay.FrameData {
				continue
			}
			if f.StreamID != 9 {
				t.Fatalf("data frame on stream %d after attach, want 9", f.StreamID)
			}
			_, data, err := relay.ParseData(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			replay = append(replay, data...)
		case <-deadline:
			t.Fatalf("timeout waiting for replay, got %q", replay)
		}
	}
	if string(replay) != "hello" {
		t.Fatalf("replay = %q", replay)
	}
	if mgr.get(1) != nil {
		t.Fatal("old stream still registered after attach")
	}
	if mgr.get(9) == nil {
		t.Fatal("attach stream not registered")
	}

	// Rename, then kill: the shell is closed and the session is gone.
	handleOpen(rt, func() relay.Frame {
		payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxRename, SessionID: name, Name: "work"})
		return relay.Frame{Type: relay.FrameOpen, StreamID: 10, Payload: payload}
	}(), mgr, sender, ctx, ctx)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if mgr.namedGet("work") == nil || mgr.namedGet(name) != nil {
		t.Fatal("rename did not retitle the session")
	}

	handleOpen(rt, openTarget0(relay.TargetTmuxKill, "work", 11), mgr, sender, ctx, ctx)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if mgr.namedGet("work") != nil || mgr.get(9) != nil {
		t.Fatal("session still registered after kill")
	}
	if !fakes[0].stdin.isClosed() {
		t.Fatal("shell not closed after kill")
	}
}

func TestDaemonSessionAttachClosesOldStream(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	name := string(waitDaemonFrame(t, out, relay.FrameOpenOK).Payload)

	// A second client attaches while the first still holds stream 1: the old
	// stream is closed with a takeover notice before the attach proceeds.
	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, name, 9), mgr, sender, ctx, ctx)
	f := waitDaemonFrame(t, out, relay.FrameClose)
	if f.StreamID != 1 || string(f.Payload) != relay.CloseSessionTakenOver {
		t.Fatalf("close = stream %d payload %q", f.StreamID, f.Payload)
	}
	if f := waitDaemonFrame(t, out, relay.FrameOpenOK); f.StreamID != 9 {
		t.Fatalf("open ok stream = %d", f.StreamID)
	}
	if mgr.get(1) != nil || mgr.get(9) == nil {
		t.Fatal("stream not re-keyed after takeover")
	}

	// A third attach evicts stream 9 the same way.
	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, name, 10), mgr, sender, ctx, ctx)
	f = waitDaemonFrame(t, out, relay.FrameClose)
	if f.StreamID != 9 || string(f.Payload) != relay.CloseSessionTakenOver {
		t.Fatalf("close = stream %d payload %q", f.StreamID, f.Payload)
	}
	if f := waitDaemonFrame(t, out, relay.FrameOpenOK); f.StreamID != 10 {
		t.Fatalf("open ok stream = %d", f.StreamID)
	}
}

func TestDaemonSessionAttachUnknownName(t *testing.T) {
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	sender, out := newTestSender()
	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, "nope", 3), newSessionManager(), sender, context.Background(), context.Background())
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "no such session") {
		t.Fatalf("payload = %q", f.Payload)
	}
}

func TestReapSkipsDaemonSessions(t *testing.T) {
	mgr := newSessionManager()
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	mgr.add(5, sr)
	mgr.namedAdd("shell-x", 5, time.Now().Add(-time.Hour))
	mgr.reapDetached(time.Now()) // no sender: everything else would be stamped
	if mgr.get(5) == nil {
		t.Fatal("persistent session reaped")
	}
	if !sr.detachedSince.IsZero() {
		t.Fatal("persistent session stamped detached by reaper")
	}
}

func TestDaemonSessionNewLimit(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	mgr := newSessionManager()
	for i := 0; i < maxDaemonSessions; i++ {
		mgr.namedAdd(fmt.Sprintf("shell-%d", i), uint32(100+i), time.Now())
	}
	sender, out := newTestSender()
	mgr.setSender(sender)

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != "too many sessions (max 32)" {
		t.Fatalf("payload = %q", f.Payload)
	}
	if len(fakes) != 0 {
		t.Fatal("shell spawned despite the session limit")
	}
}

func TestDaemonSessionShellExitRemovesNamedEntry(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	name := string(waitDaemonFrame(t, out, relay.FrameOpenOK).Payload)

	// The user types exit: the shell ends on its own.
	_ = fakes[0].stdout.Close()
	if f := waitDaemonFrame(t, out, relay.FrameClose); f.StreamID != 1 {
		t.Fatalf("close stream = %d", f.StreamID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for mgr.get(1) != nil || mgr.namedGet(name) != nil {
		if time.Now().After(deadline) {
			t.Fatal("stream or named entry left after shell exit")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// List must not show the dead session.
	handleOpen(rt, openTarget0(relay.TargetTmuxList, "", 2), mgr, sender, ctx, ctx)
	if f := waitDaemonFrame(t, out, relay.FrameOpenOK); string(f.Payload) != "[]" {
		t.Fatalf("listed = %s", f.Payload)
	}
}

func TestDaemonSessionConcurrentAttachKeepsSingleRegistration(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := &runtimeConfig{db: testTmuxRuntime(t).db, hasTmux: false}
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	name := string(waitDaemonFrame(t, out, relay.FrameOpenOK).Payload)

	// Two clients attach the same name at once; the later attach wins and the
	// stream must stay registered under exactly one id.
	var wg sync.WaitGroup
	for _, id := range []uint32{9, 10} {
		wg.Add(1)
		go func(id uint32) {
			defer wg.Done()
			handleOpen(rt, openTarget0(relay.TargetTmuxAttach, name, id), mgr, sender, ctx, ctx)
		}(id)
	}
	wg.Wait()

	mgr.mu.Lock()
	var registered []uint32
	for id := range mgr.streams {
		registered = append(registered, id)
	}
	ns := mgr.named[name]
	mgr.mu.Unlock()
	if len(registered) != 1 {
		t.Fatalf("streams registered = %v, want exactly 1", registered)
	}
	if ns == nil || ns.streamID != registered[0] {
		t.Fatalf("named entry = %+v, registered id = %d", ns, registered[0])
	}
	if got := mgr.get(registered[0]).sidV.Load(); got != registered[0] {
		t.Fatalf("sidV = %d, want %d", got, registered[0])
	}
}
