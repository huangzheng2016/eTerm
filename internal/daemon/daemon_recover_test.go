package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestHandleFrameOpenHandlerPanicRecovered(t *testing.T) {
	restoreTmuxStubs(t)
	logs := captureStallLog(t)
	tmuxListSessions = func(context.Context, string) ([]types.TmuxSession, error) {
		panic("boom")
	}
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetTmuxList})
	sender, out := newTestSender()

	handleFrame(testTmuxRuntime(t), relay.Frame{Type: relay.FrameOpen, StreamID: 90, Payload: payload}, newSessionManager(), sender, context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(logs.String(), "boom") {
		if time.Now().After(deadline) {
			t.Fatalf("panic not logged: %q", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "open stream 90") {
		t.Fatalf("log missing stream id: %q", logs.String())
	}
	select {
	case f := <-out.frames:
		t.Fatalf("frame sent after panic: %+v", f)
	default:
	}
}

func TestHandleFramePanicRecovered(t *testing.T) {
	logs := captureStallLog(t)
	rt := testTmuxRuntime(t)

	handleFrame(rt, relay.Frame{Type: relay.FrameData, StreamID: 91, Payload: []byte("x")}, nil, nil, context.Background())

	if !strings.Contains(logs.String(), "handle frame") || !strings.Contains(logs.String(), "panic") {
		t.Fatalf("panic not logged: %q", logs.String())
	}

	fake := newDaemonFakeSession()
	mgr := newSessionManager()
	mgr.add(91, newStreamRelay(fake.is))
	handleFrame(rt, relay.Frame{Type: relay.FrameData, StreamID: 91, Payload: []byte("ok")}, mgr, nil, context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for fake.stdin.String() != "ok" {
		if time.Now().After(deadline) {
			t.Fatalf("handleFrame broken after recovered panic, stdin = %q", fake.stdin.String())
		}
		time.Sleep(time.Millisecond)
	}
}
