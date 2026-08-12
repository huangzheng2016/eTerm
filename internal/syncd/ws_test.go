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
	daemon.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer daemon.CloseNow()
	client, _, err := websocket.Dial(ctx, base+"/api/v1/ws/client", &websocket.DialOptions{
		HTTPHeader: http.Header{"X-ETerm-Tenant": []string{"tenant-a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer client.CloseNow()

	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: "tenant-a", PeerID: "peer-a", Name: "host-a", Version: relay.ProtocolVersion})
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

	ansiPayload := relay.DataPayload(0, []byte("\x1b[48;2;47;52;58m  \x1b[0m\x1b]10;?\x1b\\"))
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

	largePayload := relay.DataPayload(100, bytes.Repeat([]byte("x"), 40*1024))
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: largePayload})); err != nil {
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
	if f.Type != relay.FrameData || !bytes.Equal(f.Payload, largePayload) {
		t.Fatalf("got frame type=%#v len=%d, want DATA len=%d", f.Type, len(f.Payload), len(largePayload))
	}

	// Client -> daemon: ack and input frames pass through.
	if err := client.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameAck, StreamID: 99, Payload: relay.AckPayload(41 * 1024)})); err != nil {
		t.Fatal(err)
	}
	_, data, err = daemon.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err = relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := relay.ParseAck(f.Payload)
	if f.Type != relay.FrameAck || err != nil || ack != 41*1024 {
		t.Fatalf("got frame %#v, want ACK %d", f, 41*1024)
	}
}

func TestDaemonHelloVersionMismatchRejected(t *testing.T) {
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
	daemon.SetReadLimit(relay.MaxWebSocketMessageBytes)
	defer daemon.CloseNow()

	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: "tenant-a", PeerID: "peer-a", Version: 1})
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
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
	if f.Type != relay.FrameHelloErr || !strings.Contains(string(f.Payload), "protocol version") {
		t.Fatalf("got frame %#v, want HELLO_ERR", f)
	}
}

func TestLaneQueueSendBlocksUntilSpace(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1)}
	q.ctrl <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	ctx := context.Background()
	done := make(chan bool, 1)
	go func() {
		done <- q.send(ctx, relay.Frame{Type: relay.FrameData, StreamID: 2}, false)
	}()

	select {
	case ok := <-done:
		t.Fatalf("send returned %v before queue space was available", ok)
	case <-time.After(20 * time.Millisecond):
	}

	<-q.ctrl
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("send returned false after queue space was available")
		}
	case <-time.After(time.Second):
		t.Fatal("send did not unblock after queue space was available")
	}

	got := <-q.ctrl
	if got.StreamID != 2 {
		t.Fatalf("stream id = %d", got.StreamID)
	}
}

func TestLaneQueueSendReturnsFalseOnContextCancel(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1)}
	q.bulk <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- q.send(ctx, relay.Frame{Type: relay.FrameData, StreamID: 2}, true)
	}()
	select {
	case ok := <-done:
		t.Fatalf("send returned %v with full bulk queue", ok)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("send returned true after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("send did not return after context cancel")
	}
}

func TestCloseDaemonSessionsMarksCloseAsAbnormal(t *testing.T) {
	h := NewRelayHub(nil)
	client := newLaneQueue()
	daemon := newLaneQueue()
	h.sessions[7] = relaySession{client: client, daemon: daemon}

	h.closeDaemonSessions(daemon)

	select {
	case f := <-client.ctrl:
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

func TestCloseClientSessionsKeepsDaemonSide(t *testing.T) {
	h := NewRelayHub(nil)
	client := newLaneQueue()
	daemon := newLaneQueue()
	h.sessions[7] = relaySession{client: client, daemon: daemon}

	h.closeClientSessions(client)

	if _, ok := h.session(7); ok {
		t.Fatal("session mapping kept after client disconnect")
	}
	select {
	case f := <-daemon.ctrl:
		if f.Type != relay.FrameClose || f.StreamID != 7 || string(f.Payload) != relay.CloseClientDisconnected {
			t.Fatalf("got frame %#v, want close/client-disconnected stream 7", f)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon not notified of client disconnect")
	}
}

func TestLaneQueueSendUnblocksWhenOwnerCloses(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1), done: make(chan struct{})}
	q.bulk <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	done := make(chan bool, 1)
	go func() {
		done <- q.send(context.Background(), relay.Frame{Type: relay.FrameData, StreamID: 2}, true)
	}()
	select {
	case ok := <-done:
		t.Fatalf("send returned %v with full bulk queue", ok)
	case <-time.After(20 * time.Millisecond):
	}
	q.close()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("send returned true after owner closed")
		}
	case <-time.After(time.Second):
		t.Fatal("send did not return after owner closed")
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
