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

func testLocalRuntime(t *testing.T) *runtimeConfig {
	t.Helper()
	return &runtimeConfig{db: testDaemonDB(t), hasTmux: false}
}

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

func readDaemonSessionInfo(t *testing.T, f relay.Frame) relay.TmuxSessionInfo {
	t.Helper()
	var info relay.TmuxSessionInfo
	if err := json.Unmarshal(f.Payload, &info); err != nil {
		t.Fatal(err)
	}
	if info.Name == "" || info.SessionID == "" {
		t.Fatalf("invalid session info: %+v", info)
	}
	return info
}

func TestDaemonSessionLifecycleWithoutTmux(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	f := waitDaemonFrame(t, out, relay.FrameOpenOK)
	info := readDaemonSessionInfo(t, f)
	if !strings.HasPrefix(info.Name, "shell-") {
		t.Fatalf("name = %q", info.Name)
	}

	handleOpen(rt, openTarget0(relay.TargetTmuxList, "", 2), mgr, sender, ctx, ctx)
	lf := waitDaemonFrame(t, out, relay.FrameOpenOK)
	var listed []relay.TmuxSessionInfo
	if err := json.Unmarshal(lf.Payload, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != info.Name || listed[0].SessionID != info.SessionID || !listed[0].Attached || !listed[0].Daemon {
		t.Fatalf("listed = %+v", listed)
	}
	sessionID := info.SessionID

	go func() { _, _ = fakes[0].stdout.Write([]byte("hello")) }()
	if _, data := waitDataBytes(t, out, 5); string(data) != "hello" {
		t.Fatalf("data = %q", data)
	}

	handleFrame(rt, relay.Frame{Type: relay.FrameClose, StreamID: 1}, mgr, sender, ctx)
	if mgr.get(1) == nil {
		t.Fatal("persistent session removed on tab close")
	}
	if fakes[0].stdin.isClosed() {
		t.Fatal("persistent session shell killed on tab close")
	}

	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, sessionID, 9), mgr, sender, ctx, ctx)
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

	handleOpen(rt, func() relay.Frame {
		payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxRename, SessionID: sessionID, Name: "work"})
		return relay.Frame{Type: relay.FrameOpen, StreamID: 10, Payload: payload}
	}(), mgr, sender, ctx, ctx)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if mgr.namedGet(sessionID) == nil || mgr.namedGet(sessionID).name != "work" {
		t.Fatal("rename did not retitle the session")
	}

	handleOpen(rt, openTarget0(relay.TargetTmuxKill, sessionID, 11), mgr, sender, ctx, ctx)
	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	if mgr.namedGet(sessionID) != nil || mgr.get(9) != nil {
		t.Fatal("session still registered after kill")
	}
	if !fakes[0].stdin.isClosed() {
		t.Fatal("shell not closed after kill")
	}
}

func TestDaemonSessionAttachClosesOldStream(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	info := readDaemonSessionInfo(t, waitDaemonFrame(t, out, relay.FrameOpenOK))

	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, info.SessionID, 9), mgr, sender, ctx, ctx)
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

	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, info.SessionID, 10), mgr, sender, ctx, ctx)
	f = waitDaemonFrame(t, out, relay.FrameClose)
	if f.StreamID != 9 || string(f.Payload) != relay.CloseSessionTakenOver {
		t.Fatalf("close = stream %d payload %q", f.StreamID, f.Payload)
	}
	if f := waitDaemonFrame(t, out, relay.FrameOpenOK); f.StreamID != 10 {
		t.Fatalf("open ok stream = %d", f.StreamID)
	}
}

func TestDaemonSessionAttachResumeUnavailableKeepsExistingStream(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	info := readDaemonSessionInfo(t, waitDaemonFrame(t, out, relay.FrameOpenOK))

	handleOpen(rt, func() relay.Frame {
		payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxAttach, SessionID: info.SessionID, ResumeFromSeq: 100})
		return relay.Frame{Type: relay.FrameOpen, StreamID: 9, Payload: payload}
	}(), mgr, sender, ctx, ctx)
	if got := waitDaemonFrame(t, out, relay.FrameOpenErr); string(got.Payload) != resumeUnavailableErr {
		t.Fatalf("open err = %q", got.Payload)
	}
	if mgr.get(1) == nil || mgr.get(9) != nil {
		t.Fatal("failed attach changed stream registration")
	}
	if ns := mgr.namedGet(info.SessionID); ns == nil || ns.streamID != 1 {
		t.Fatalf("named stream = %+v, want stream 1", ns)
	}
	_ = fakes[0].stdout.Close()
}

func TestDaemonSessionAttachUnknownName(t *testing.T) {
	rt := testLocalRuntime(t)
	sender, out := newTestSender()
	handleOpen(rt, openTarget0(relay.TargetTmuxAttach, "nope", 3), newSessionManager(), sender, context.Background(), context.Background())
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "no such session") {
		t.Fatalf("payload = %q", f.Payload)
	}
}

func TestDaemonSessionKillUnknownName(t *testing.T) {
	rt := testLocalRuntime(t)
	sender, out := newTestSender()
	handleOpen(rt, openTarget0(relay.TargetTmuxKill, "nope", 4), newSessionManager(), sender, context.Background(), context.Background())
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "no such session") {
		t.Fatalf("payload = %q", f.Payload)
	}
}

func TestDaemonSessionRenameRejectsEmptyName(t *testing.T) {
	mgr := newSessionManager()
	mgr.namedAdd("id-old", "old", 1, time.Now())
	sender, out := newTestSender()
	daemonSessionRename(mgr, sender, 4, "old", "   ")
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if string(f.Payload) != "empty session name" {
		t.Fatalf("payload = %q", f.Payload)
	}
	if mgr.namedGet("id-old") == nil {
		t.Fatal("empty rename removed the existing session")
	}
}

func TestReapSkipsDaemonSessions(t *testing.T) {
	mgr := newSessionManager()
	fake := newDaemonFakeSession()
	sr := newStreamRelay(fake.is)
	mgr.add(5, sr)
	mgr.namedAdd("id-shell-x", "shell-x", 5, time.Now().Add(-time.Hour))
	mgr.reapDetached(time.Now())
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
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	for i := 0; i < maxDaemonSessions; i++ {
		id := fmt.Sprintf("id-%d", i)
		mgr.namedAdd(id, fmt.Sprintf("shell-%d", i), uint32(100+i), time.Now())
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
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	info := readDaemonSessionInfo(t, waitDaemonFrame(t, out, relay.FrameOpenOK))

	_ = fakes[0].stdout.Close()
	if f := waitDaemonFrame(t, out, relay.FrameClose); f.StreamID != 1 {
		t.Fatalf("close stream = %d", f.StreamID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for mgr.get(1) != nil || mgr.namedGet(info.SessionID) != nil {
		if time.Now().After(deadline) {
			t.Fatal("stream or named entry left after shell exit")
		}
		time.Sleep(5 * time.Millisecond)
	}

	handleOpen(rt, openTarget0(relay.TargetTmuxList, "", 2), mgr, sender, ctx, ctx)
	if f := waitDaemonFrame(t, out, relay.FrameOpenOK); string(f.Payload) != "[]" {
		t.Fatalf("listed = %s", f.Payload)
	}
}

func TestDaemonSessionConcurrentAttachKeepsSingleRegistration(t *testing.T) {
	var fakes []*daemonFakeSession
	stubLocalNewSession(t, &fakes)
	rt := testLocalRuntime(t)
	mgr := newSessionManager()
	sender, out := newTestSender()
	mgr.setSender(sender)
	ctx := context.Background()

	handleOpen(rt, openTarget0(relay.TargetTmuxNew, "", 1), mgr, sender, ctx, ctx)
	info := readDaemonSessionInfo(t, waitDaemonFrame(t, out, relay.FrameOpenOK))

	var wg sync.WaitGroup
	for _, id := range []uint32{9, 10} {
		wg.Add(1)
		go func(id uint32) {
			defer wg.Done()
			handleOpen(rt, openTarget0(relay.TargetTmuxAttach, info.SessionID, id), mgr, sender, ctx, ctx)
		}(id)
	}
	wg.Wait()

	mgr.mu.Lock()
	var registered []uint32
	for id := range mgr.streams {
		registered = append(registered, id)
	}
	ns := mgr.named[info.SessionID]
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
