package syncd

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

type lockedLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureSyncdLog(t *testing.T) *lockedLogBuf {
	t.Helper()
	b := &lockedLogBuf{}
	old := log.Writer()
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(old) })
	return b
}

func wsBase(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func relayServer(t *testing.T) (*httptest.Server, *Engine, context.Context) {
	t.Helper()
	engine := testEngine(t)
	server := httptest.NewServer(NewHTTPHandler(engine, ""))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return server, engine, ctx
}

func relaySetup(t *testing.T) (*httptest.Server, *Engine, *websocket.Conn, context.Context) {
	t.Helper()
	server, engine, ctx := relayServer(t)
	daemon := relayDial(t, ctx, wsBase(server), "/api/v1/ws/daemon", nil)
	daemonHello(t, ctx, daemon, "peer-a")
	waitPeer(t, server.URL)
	return server, engine, daemon, ctx
}

func relayDial(t *testing.T, ctx context.Context, base, path string, header http.Header) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, base+path, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(relay.MaxWebSocketMessageBytes)
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func daemonHello(t *testing.T, ctx context.Context, daemon *websocket.Conn, peerID string) {
	t.Helper()
	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: "tenant-a", PeerID: peerID, Name: "host", Version: relay.ProtocolVersion})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameHello, Payload: hello})
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

func readFrame(t *testing.T, ctx context.Context, c *websocket.Conn) relay.Frame {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func writeFrame(t *testing.T, ctx context.Context, c *websocket.Conn, f relay.Frame) {
	t.Helper()
	if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(f)); err != nil {
		t.Fatal(err)
	}
}

func expectFrame(t *testing.T, ctx context.Context, c *websocket.Conn, typ relay.FrameType, streamID uint32) relay.Frame {
	t.Helper()
	f := readFrame(t, ctx, c)
	if f.Type != typ || f.StreamID != streamID {
		t.Fatalf("got frame %#v, want type %#x stream %d", f, byte(typ), streamID)
	}
	return f
}

func expectAck(t *testing.T, ctx context.Context, c *websocket.Conn, want uint64) relay.Frame {
	t.Helper()
	f := readFrame(t, ctx, c)
	ack, err := relay.ParseAck(f.Payload)
	if f.Type != relay.FrameAck || err != nil || ack != want {
		t.Fatalf("got frame %#v, want ACK %d", f, want)
	}
	return f
}

func openLocal(t *testing.T, ctx context.Context, c *websocket.Conn, streamID uint32) {
	t.Helper()
	payload, _ := json.Marshal(relay.OpenRequest{PeerID: "peer-a", Target: "local"})
	writeFrame(t, ctx, c, relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload})
}

func setLaneSendTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := laneSendBlockTimeout
	laneSendBlockTimeout = d
	t.Cleanup(func() { laneSendBlockTimeout = old })
}

func blockedSend(t *testing.T, q *laneQueue, ctx context.Context, bulk bool) chan bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() { done <- q.send(ctx, relay.Frame{Type: relay.FrameData, StreamID: 2}, bulk) }()
	select {
	case ok := <-done:
		t.Fatalf("send returned %v with a full queue", ok)
	case <-time.After(20 * time.Millisecond):
	}
	return done
}

func sendResult(t *testing.T, done chan bool, want bool, trigger string) {
	t.Helper()
	select {
	case ok := <-done:
		if ok != want {
			t.Fatalf("send returned %v after %s, want %v", ok, trigger, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("send did not return after %s", trigger)
	}
}

func tenantHeader() http.Header {
	return http.Header{"X-ETerm-Tenant": []string{"tenant-a"}}
}

func TestWebSocketRelayData(t *testing.T) {
	server, _, daemon, ctx := relaySetup(t)
	client := relayDial(t, ctx, wsBase(server), "/api/v1/ws/client", tenantHeader())

	openLocal(t, ctx, client, 99)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 99)

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: 99})
	expectFrame(t, ctx, client, relay.FrameOpenOK, 99)

	ansiPayload := relay.DataPayload(0, []byte("\x1b[48;2;47;52;58m  \x1b[0m\x1b]10;?\x1b\\"))
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: ansiPayload})
	if f := readFrame(t, ctx, client); f.Type != relay.FrameData || !bytes.Equal(f.Payload, ansiPayload) {
		t.Fatalf("got frame %#v, want DATA %q", f, ansiPayload)
	}

	largePayload := relay.DataPayload(100, bytes.Repeat([]byte("x"), 40*1024))
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: largePayload})
	if f := readFrame(t, ctx, client); f.Type != relay.FrameData || !bytes.Equal(f.Payload, largePayload) {
		t.Fatalf("got frame type=%#v len=%d, want DATA len=%d", f.Type, len(f.Payload), len(largePayload))
	}

	writeFrame(t, ctx, client, relay.Frame{Type: relay.FrameAck, StreamID: 99, Payload: relay.AckPayload(41 * 1024)})
	expectAck(t, ctx, daemon, 41*1024)
}

func TestHelloVersionMismatchRejected(t *testing.T) {
	server, _, ctx := relayServer(t)
	base := wsBase(server)

	for _, tc := range []struct {
		name     string
		path     string
		header   http.Header
		role     string
		versions []int
	}{
		{"daemon", "/api/v1/ws/daemon", nil, "daemon", []int{1}},
		{"client", "/api/v1/ws/client", tenantHeader(), "client", []int{0, 1}},
	} {
		for _, version := range tc.versions {
			conn := relayDial(t, ctx, base, tc.path, tc.header)
			hello, _ := json.Marshal(relay.HelloPayload{Role: tc.role, Tenant: "tenant-a", PeerID: "peer-a", Version: version})
			writeFrame(t, ctx, conn, relay.Frame{Type: relay.FrameHello, Payload: hello})
			f := readFrame(t, ctx, conn)
			if f.Type != relay.FrameHelloErr || !strings.Contains(string(f.Payload), "protocol version") {
				t.Fatalf("%s version %d: got frame %#v, want HELLO_ERR", tc.name, version, f)
			}
			conn.CloseNow()
		}
	}
}

func TestLaneQueueSendBlocksUntilSpace(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1)}
	q.ctrl <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	done := blockedSend(t, q, context.Background(), false)
	<-q.ctrl
	sendResult(t, done, true, "queue space became available")

	got := <-q.ctrl
	if got.StreamID != 2 {
		t.Fatalf("stream id = %d", got.StreamID)
	}
}

func TestLaneQueueSendReturnsFalseOnContextCancel(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1)}
	q.bulk <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	ctx, cancel := context.WithCancel(context.Background())
	done := blockedSend(t, q, ctx, true)
	cancel()
	sendResult(t, done, false, "context cancel")
}

func TestLaneQueueSendUnblocksWhenOwnerCloses(t *testing.T) {
	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1), done: make(chan struct{})}
	q.bulk <- relay.Frame{Type: relay.FrameData, StreamID: 1}

	done := blockedSend(t, q, context.Background(), true)
	q.close()
	sendResult(t, done, false, "owner closed")
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

func TestClientWSForeignStreamFrameDropped(t *testing.T) {
	server, _, daemon, ctx := relaySetup(t)
	base := wsBase(server)

	client1 := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())
	client2 := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())

	openLocal(t, ctx, client1, 99)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 99)

	writeFrame(t, ctx, client2, relay.Frame{Type: relay.FrameAck, StreamID: 99, Payload: relay.AckPayload(111)})
	time.Sleep(50 * time.Millisecond)
	writeFrame(t, ctx, client1, relay.Frame{Type: relay.FrameAck, StreamID: 99, Payload: relay.AckPayload(222)})
	expectAck(t, ctx, daemon, 222)
}

func TestDaemonWSForeignStreamFrameDropped(t *testing.T) {
	server, _, daemon1, ctx := relaySetup(t)
	base := wsBase(server)

	daemon2 := relayDial(t, ctx, base, "/api/v1/ws/daemon", nil)
	daemonHello(t, ctx, daemon2, "peer-b")

	client := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())
	openLocal(t, ctx, client, 99)
	expectFrame(t, ctx, daemon1, relay.FrameOpen, 99)

	writeFrame(t, ctx, daemon2, relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: relay.DataPayload(0, []byte("injected"))})
	time.Sleep(50 * time.Millisecond)
	writeFrame(t, ctx, daemon1, relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: relay.DataPayload(0, []byte("legit"))})
	if f := readFrame(t, ctx, client); f.Type != relay.FrameData || !bytes.Equal(f.Payload, relay.DataPayload(0, []byte("legit"))) {
		t.Fatalf("got frame %#v, want owner DATA legit (injected must be dropped)", f)
	}
}

func TestDaemonWSDuplicatePeerReplaced(t *testing.T) {
	server, _, daemon1, ctx := relaySetup(t)
	base := wsBase(server)

	daemon2 := relayDial(t, ctx, base, "/api/v1/ws/daemon", nil)
	daemonHello(t, ctx, daemon2, "peer-a")

	if _, _, err := daemon1.Read(ctx); err == nil {
		t.Fatal("replaced daemon connection still readable")
	}

	client := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())
	openLocal(t, ctx, client, 42)
	expectFrame(t, ctx, daemon2, relay.FrameOpen, 42)
	writeFrame(t, ctx, daemon2, relay.Frame{Type: relay.FrameOpenOK, StreamID: 42})
	expectFrame(t, ctx, client, relay.FrameOpenOK, 42)
}

func TestWriteWSPanicRecovered(t *testing.T) {
	logs := captureSyncdLog(t)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		writeWS(r.Context(), c, nil, make(chan struct{}), done)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsBase(server), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writeWS did not return after panic")
	}
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("connection still readable after writer panic")
	}
	if !strings.Contains(logs.String(), "writeWS panic") {
		t.Fatalf("panic not logged: %q", logs.String())
	}
}

func TestLaneQueueSendTimeoutClosesConn(t *testing.T) {
	setLaneSendTimeout(t, 50*time.Millisecond)
	logs := captureSyncdLog(t)

	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1), done: make(chan struct{})}
	q.label = "client tenant=tenant-a addr=127.0.0.1:9000"
	connClosed := make(chan struct{})
	q.closeConn = func() { close(connClosed) }
	q.bulk <- relay.Frame{Type: relay.FrameData, StreamID: 7}

	if q.send(context.Background(), relay.Frame{Type: relay.FrameData, StreamID: 7}, true) {
		t.Fatal("send returned true with a permanently full queue")
	}
	select {
	case <-connClosed:
	case <-time.After(time.Second):
		t.Fatal("dead connection not closed after send timeout")
	}
	select {
	case <-q.done:
	default:
		t.Fatal("queue not closed after send timeout")
	}
	if out := logs.String(); !strings.Contains(out, "client tenant=tenant-a") || !strings.Contains(out, "stream=7") {
		t.Fatalf("timeout close not logged with peer/stream: %q", out)
	}
}

func TestLaneQueueSendTimeoutSparesSlowClient(t *testing.T) {
	setLaneSendTimeout(t, 200*time.Millisecond)
	logs := captureSyncdLog(t)

	q := &laneQueue{ctrl: make(chan relay.Frame, 1), bulk: make(chan relay.Frame, 1), done: make(chan struct{})}
	q.closeConn = func() { t.Error("closeConn called for live slow client") }
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-q.bulk:
				time.Sleep(40 * time.Millisecond)
			case <-stop:
				return
			}
		}
	}()

	for i := 0; i < 5; i++ {
		if !q.send(context.Background(), relay.Frame{Type: relay.FrameData, StreamID: 3}, true) {
			t.Fatalf("send %d returned false for live slow client", i)
		}
	}
	if out := logs.String(); strings.Contains(out, "send blocked") {
		t.Fatalf("live slow client killed: %q", out)
	}
}

func TestDaemonWSZombieClientReclaimed(t *testing.T) {
	setLaneSendTimeout(t, 200*time.Millisecond)
	logs := captureSyncdLog(t)

	server, _, daemon, ctx := relaySetup(t)
	base := wsBase(server)

	zombie := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())
	healthy := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())

	openLocal(t, ctx, zombie, 1)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 1)
	openLocal(t, ctx, healthy, 2)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 2)

	var stopFlood atomic.Bool
	floodDone := make(chan struct{})
	go func() {
		defer close(floodDone)
		payload := relay.DataPayload(0, bytes.Repeat([]byte("x"), 4096))
		for !stopFlood.Load() {
			if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: 1, Payload: payload})); err != nil {
				return
			}
		}
	}()

	f := readFrame(t, ctx, daemon)
	stopFlood.Store(true)
	<-floodDone
	if f.Type != relay.FrameClose || f.StreamID != 1 || string(f.Payload) != relay.CloseClientDisconnected {
		t.Fatalf("got frame %#v, want CLOSE stream 1 client-disconnected after zombie reclaim", f)
	}

	dataPayload := relay.DataPayload(1, []byte("after-reclaim"))
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: 2, Payload: dataPayload})
	if f := readFrame(t, ctx, healthy); f.Type != relay.FrameData || !bytes.Equal(f.Payload, dataPayload) {
		t.Fatalf("healthy stream got frame %#v, want DATA after-reclaim", f)
	}

	var zombieErr error
	for zombieErr == nil {
		_, _, zombieErr = zombie.Read(ctx)
	}
	if ctx.Err() != nil {
		t.Fatal("zombie connection not closed by reclaim")
	}
	if out := logs.String(); !strings.Contains(out, "client tenant=tenant-a") || !strings.Contains(out, "stream=1") {
		t.Fatalf("zombie reclaim not logged with peer/stream: %q", out)
	}
}

func TestClientWSDuplicateOpenTakesOver(t *testing.T) {
	server, _, daemon, ctx := relaySetup(t)
	base := wsBase(server)

	oldClient := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())
	newClient := relayDial(t, ctx, base, "/api/v1/ws/client", tenantHeader())

	openLocal(t, ctx, oldClient, 7)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 7)
	openLocal(t, ctx, newClient, 7)
	expectFrame(t, ctx, daemon, relay.FrameOpen, 7)
	if f := readFrame(t, ctx, oldClient); f.Type != relay.FrameClose || f.StreamID != 7 || string(f.Payload) != relay.CloseSessionTakenOver {
		t.Fatalf("old conn got frame %#v, want CLOSE stream 7 taken-over", f)
	}

	dataPayload := relay.DataPayload(1, []byte("after-takeover"))
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: 7, Payload: dataPayload})
	if f := readFrame(t, ctx, newClient); f.Type != relay.FrameData || !bytes.Equal(f.Payload, dataPayload) {
		t.Fatalf("new conn got frame %#v, want DATA after-takeover", f)
	}

	oldClient.CloseNow()
	time.Sleep(100 * time.Millisecond)
	writeFrame(t, ctx, newClient, relay.Frame{Type: relay.FrameAck, StreamID: 7, Payload: relay.AckPayload(15)})
	if f := expectAck(t, ctx, daemon, 15); f.StreamID != 7 {
		t.Fatalf("got frame %#v after old conn close, want ACK 15 stream 7 (taken-over stream must not be closed)", f)
	}
}

func TestClientWSDuplicateOpenSameConnNoTakeoverNotice(t *testing.T) {
	server, _, daemon, ctx := relaySetup(t)
	client := relayDial(t, ctx, wsBase(server), "/api/v1/ws/client", tenantHeader())

	for i := 0; i < 2; i++ {
		openLocal(t, ctx, client, 7)
		expectFrame(t, ctx, daemon, relay.FrameOpen, 7)
	}

	dataPayload := relay.DataPayload(1, []byte("still-mine"))
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: 7, Payload: dataPayload})
	if f := readFrame(t, ctx, client); f.Type != relay.FrameData || !bytes.Equal(f.Payload, dataPayload) {
		t.Fatalf("got frame %#v, want DATA still-mine (same-conn re-open must not self-close)", f)
	}
}

func TestCloseSessionIfOwner(t *testing.T) {
	h := NewRelayHub(nil)
	owner := newLaneQueue()
	other := newLaneQueue()
	h.sessions[7] = relaySession{client: owner, daemon: newLaneQueue()}

	h.closeSessionIfOwner(7, other)
	if _, ok := h.session(7); !ok {
		t.Fatal("session deleted by non-owner")
	}
	h.closeSessionIfOwner(7, owner)
	if _, ok := h.session(7); ok {
		t.Fatal("session not deleted by owner")
	}
}
