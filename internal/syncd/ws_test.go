package syncd

import (
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

	hello, _ := json.Marshal(wsHello{Role: "daemon", Tenant: "tenant-a", PeerID: "peer-a", Name: "host-a", Version: 1})
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		t.Fatal(err)
	}
	waitPeer(t, server.URL)

	openPayload, _ := json.Marshal(wsOpen{PeerID: "peer-a", Target: "local"})
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

	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: 99, Payload: []byte("ok")})); err != nil {
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
	if f.Type != relay.FrameData || string(f.Payload) != "ok" {
		t.Fatalf("got frame %#v, want DATA ok", f)
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
