package syncd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

func TestWebSocketRelayData(t *testing.T) {
	engine := testEngine(t)
	handler := NewHTTPHandler(engine, "")
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	base := "ws" + strings.TrimPrefix(server.URL, "http")

	daemon, _, err := websocket.Dial(ctx, base+"/api/v1/ws/daemon", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.CloseNow()
	client, _, err := websocket.Dial(ctx, base+"/api/v1/ws/client", &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ETerm-Tenant": []string{"tenant-a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()

	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: "tenant-a", PeerID: "peer-a", Name: "host-a", Version: 1})
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		t.Fatal(err)
	}
	waitPeer(t, server.URL)

	openPayload, _ := json.Marshal(relay.OpenRequest{PeerID: "peer-a", Target: "local"})
	if err := client.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpen, StreamID: 99, Payload: openPayload})); err != nil {
		t.Fatal(err)
	}

	_, data, err := daemon.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != relay.FrameOpen || f.StreamID != 99 {
		t.Fatalf("got frame %#v, want OPEN stream 99", f)
	}

	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: 99})); err != nil {
		t.Fatal(err)
	}
	_, data, err = client.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err = relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != relay.FrameOpenOK || f.StreamID != 99 {
		t.Fatalf("got frame %#v, want OPEN_OK stream 99", f)
	}

	ansiPayload := []byte("\x1b[48;2;47;52;58m  \x1b[0m\x1b]10;?\x1b\\")
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: ansiPayload})); err != nil {
		t.Fatal(err)
	}
	_, data, err = client.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err = relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != relay.FrameData || !bytes.Equal(f.Payload, ansiPayload) {
		t.Fatalf("got frame %#v, want DATA %q", f, ansiPayload)
	}
}

func TestCloseDaemonSessionsMarksCloseAsAbnormal(t *testing.T) {
	h := NewRelayHub(nil)
	client := make(chan relay.Frame, 1)
	daemon := make(chan relay.Frame, 1)
	h.sessions[7] = relaySession{client: client, daemon: daemon}

	h.closeDaemonSessions(daemon)

	select {
	case f := <-client:
		if f.Type != relay.FrameClose || f.StreamID != 7 {
			t.Fatalf("got frame %#v, want close stream 7", f)
		}
		if len(f.Payload) == 0 {
			t.Fatal("expected abnormal close payload")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for close frame")
	}
}

func TestCloseDaemonSessionsTimesOutWhenQueueIsFull(t *testing.T) {
	oldTimeout := relaySendTimeoutNanos.Swap(int64(10 * time.Millisecond))
	t.Cleanup(func() { relaySendTimeoutNanos.Store(oldTimeout) })

	h := NewRelayHub(nil)
	client := make(chan relay.Frame, 1)
	daemon := make(chan relay.Frame, 1)
	client <- relay.Frame{Type: relay.FrameData, StreamID: 1}
	h.sessions[7] = relaySession{client: client, daemon: daemon}

	done := make(chan struct{})
	go func() {
		h.closeDaemonSessions(daemon)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not time out")
	}
}

func TestDefaultRelaySendTimeoutIsFiveMinutes(t *testing.T) {
	if got := time.Duration(relaySendTimeoutNanos.Load()); got != 5*time.Minute {
		t.Fatalf("timeout = %s", got)
	}
}

func TestTrySendWaitsForQueueSpace(t *testing.T) {
	oldTimeout := relaySendTimeoutNanos.Swap(int64(time.Second))
	t.Cleanup(func() { relaySendTimeoutNanos.Store(oldTimeout) })

	ch := make(chan relay.Frame, 1)
	ch <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	done := make(chan bool, 1)
	go func() {
		done <- trySend(ch, relay.Frame{Type: relay.FrameData, StreamID: 2})
	}()

	select {
	case ok := <-done:
		t.Fatalf("trySend returned %v before queue space was available", ok)
	case <-time.After(20 * time.Millisecond):
	}

	<-ch
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("trySend returned false after queue space was available")
		}
	case <-time.After(time.Second):
		t.Fatal("trySend did not send after queue space was available")
	}

	got := <-ch
	if got.StreamID != 2 {
		t.Fatalf("stream id = %d", got.StreamID)
	}
}

func waitPeer(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/peers", nil)
		req.Header.Set("X-ETerm-Tenant", "tenant-a")
		resp, err := client.Do(req)
		if err == nil {
			var body struct {
				Peers []PeerInfo `json:"peers"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if len(body.Peers) == 1 && body.Peers[0].ID == "peer-a" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("peer did not register")
}
