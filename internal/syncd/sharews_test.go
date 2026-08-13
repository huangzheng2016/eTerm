package syncd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
)

func shareWSSetup(t *testing.T) (*httptest.Server, *Engine, *websocket.Conn, context.Context) {
	t.Helper()
	engine := testEngine(t)
	server := httptest.NewServer(NewHTTPHandler(engine, ""))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	daemon, _, err := websocket.Dial(ctx, base+"/api/v1/ws/daemon", nil)
	if err != nil {
		t.Fatal(err)
	}
	daemon.SetReadLimit(relay.MaxWebSocketMessageBytes)
	t.Cleanup(func() { daemon.CloseNow() })
	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: "tenant-a", PeerID: "peer-a", Name: "host-a", Version: relay.ProtocolVersion})
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		t.Fatal(err)
	}
	waitPeer(t, server.URL)
	return server, engine, daemon, ctx
}

func shareDaemonFrame(t *testing.T, ctx context.Context, daemon *websocket.Conn) relay.Frame {
	t.Helper()
	_, data, err := daemon.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := relay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func readGuestMsg(t *testing.T, ctx context.Context, guest *websocket.Conn) shareHostMsg {
	t.Helper()
	typ, data, err := guest.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("guest frame type = %v", typ)
	}
	var msg shareHostMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

func shareGuestDial(t *testing.T, ctx context.Context, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	guest, _, err := websocket.Dial(ctx, base+"/x/"+token+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guest.CloseNow() })
	return guest
}

func TestShareStateReuseResetsIdle(t *testing.T) {
	h := NewRelayHub(nil)
	st, created := h.shareState("tok")
	if !created {
		t.Fatal("first shareState not created")
	}
	h.mu.Lock()
	st.idleSince = time.Now().Add(-time.Minute)
	h.mu.Unlock()

	st2, created := h.shareState("tok")
	if created || st2 != st {
		t.Fatal("recent state not reused")
	}
	if !st2.idleSince.IsZero() {
		t.Fatal("idleSince not reset on reuse; a later prune could drop an in-use state")
	}
}

func TestDropShareStateIdentity(t *testing.T) {
	h := NewRelayHub(nil)
	old, _ := h.shareState("tok")
	h.mu.Lock()
	old.idleSince = time.Now().Add(-shareStateIdleTTL - time.Minute)
	h.mu.Unlock()

	fresh, created := h.shareState("tok")
	if !created || fresh == old {
		t.Fatal("idle state not pruned and recreated")
	}

	// A stale owner tearing down must not delete the new guest's state.
	h.dropShareState("tok", old)
	if got := h.shareStates["tok"]; got != fresh {
		t.Fatal("stale dropShareState removed the current state")
	}

	h.dropShareState("tok", fresh)
	if _, ok := h.shareStates["tok"]; ok {
		t.Fatal("owner dropShareState did not remove the state")
	}
}

func TestShareInvalidToken404(t *testing.T) {
	engine := testEngine(t)
	server := httptest.NewServer(NewHTTPHandler(engine, ""))
	defer server.Close()

	resp, err := http.Get(server.URL + "/x/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("page status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	_, wsResp, err := websocket.Dial(ctx, base+"/x/nope/ws", nil)
	if err == nil {
		t.Fatal("ws dial with bad token succeeded")
	}
	if wsResp != nil && wsResp.StatusCode != 404 {
		t.Fatalf("ws status = %d", wsResp.StatusCode)
	}
}

func TestSharePageServed(t *testing.T) {
	server, engine, _, _ := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "demo box", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(server.URL + "/x/" + share.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), share.Token) || !strings.Contains(string(body), "demo box") {
		t.Fatal("share page missing token or name")
	}
}

func TestShareWSBridge(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "demo", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.PeerID != "peer-a" || open.Target != relay.TargetLocal || open.Name != "demo" || open.Rows != 24 || open.Cols != 80 {
		t.Fatalf("open = %+v", open)
	}
	streamID := f.StreamID

	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID})); err != nil {
		t.Fatal(err)
	}

	// Guest input -> daemon FrameData (raw payload, no seq).
	in, _ := json.Marshal(shareGuestMsg{T: "in", D: base64.StdEncoding.EncodeToString([]byte("ls"))})
	if err := guest.Write(ctx, websocket.MessageText, in); err != nil {
		t.Fatal(err)
	}
	f = shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameData || f.StreamID != streamID || string(f.Payload) != "ls" {
		t.Fatalf("got frame %#v payload %q, want DATA ls", f, f.Payload)
	}

	// Daemon output -> guest out frame, then guest ack back to daemon.
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: streamID, Payload: relay.DataPayload(0, []byte("hello"))})); err != nil {
		t.Fatal(err)
	}
	msg := readGuestMsg(t, ctx, guest)
	if msg.T != "out" {
		t.Fatalf("guest msg = %+v, want out", msg)
	}
	out, err := base64.StdEncoding.DecodeString(msg.D)
	if err != nil || string(out) != "hello" {
		t.Fatalf("out = %q err = %v", out, err)
	}
	f = shareDaemonFrame(t, ctx, daemon)
	ack, ackErr := relay.ParseAck(f.Payload)
	if f.Type != relay.FrameAck || ackErr != nil || ack != 5 {
		t.Fatalf("got frame %#v, want ACK 5", f)
	}

	// Guest resize -> daemon FrameResize.
	rsz, _ := json.Marshal(shareGuestMsg{T: "resize", Rows: 40, Cols: 100})
	if err := guest.Write(ctx, websocket.MessageText, rsz); err != nil {
		t.Fatal(err)
	}
	f = shareDaemonFrame(t, ctx, daemon)
	rows, cols, err := relay.ParseResize(f.Payload)
	if f.Type != relay.FrameResize || err != nil || rows != 40 || cols != 100 {
		t.Fatalf("got frame %#v, want RESIZE 40x100", f)
	}

	// Daemon close -> guest exit frame with reason.
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: streamID, Payload: []byte("shell exited")})); err != nil {
		t.Fatal(err)
	}
	msg = readGuestMsg(t, ctx, guest)
	if msg.T != "exit" || msg.Reason != "shell exited" {
		t.Fatalf("guest msg = %+v, want exit/shell exited", msg)
	}
}

func TestShareWSOpenErr(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("no shell")})); err != nil {
		t.Fatal(err)
	}
	msg := readGuestMsg(t, ctx, guest)
	if msg.T != "exit" || msg.Reason != "no shell" {
		t.Fatalf("guest msg = %+v, want exit/no shell", msg)
	}
}

func TestShareWSSecondConnectionReplacesFirst(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := shareDaemonFrame(t, ctx, daemon)
	if f1.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f1)
	}

	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := shareDaemonFrame(t, ctx, daemon)
	if f2.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN for guest2", f2)
	}
	if f2.StreamID != f1.StreamID {
		t.Fatalf("replacement opened stream %d, want takeover of %d", f2.StreamID, f1.StreamID)
	}

	msg := readGuestMsg(t, ctx, guest1)
	if msg.T != "exit" || msg.Reason != "replaced" {
		t.Fatalf("guest1 msg = %+v, want exit/replaced", msg)
	}

	// The replacement connection is functional.
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f2.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f2.StreamID, Payload: relay.DataPayload(0, []byte("hi"))})); err != nil {
		t.Fatal(err)
	}
	msg = readGuestMsg(t, ctx, guest2)
	if msg.T != "out" {
		t.Fatalf("guest2 msg = %+v, want out", msg)
	}
}

func TestShareWSGuestDisconnectResumes(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := shareDaemonFrame(t, ctx, daemon)
	if f1.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f1)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f1.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f1.StreamID, Payload: relay.DataPayload(0, []byte("hello"))})); err != nil {
		t.Fatal(err)
	}
	if msg := readGuestMsg(t, ctx, guest1); msg.T != "out" {
		t.Fatalf("guest1 msg = %+v, want out", msg)
	}
	f := shareDaemonFrame(t, ctx, daemon)
	if ack, _ := relay.ParseAck(f.Payload); f.Type != relay.FrameAck || ack != 5 {
		t.Fatalf("got frame %#v, want ACK 5", f)
	}

	// Guest drops: the daemon must be told to keep the PTY, not kill it.
	guest1.CloseNow()
	f = shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameClose || f.StreamID != f1.StreamID || string(f.Payload) != relay.CloseClientDisconnected {
		t.Fatalf("got frame %#v payload %q, want CLOSE client-disconnected", f, f.Payload)
	}

	// Reconnect: same stream, resume from the acked offset.
	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := shareDaemonFrame(t, ctx, daemon)
	if f2.Type != relay.FrameOpen || f2.StreamID != f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN stream %d", f2, f1.StreamID)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f2.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.ResumeFromSeq != 5 {
		t.Fatalf("resume_from_seq = %d, want 5", open.ResumeFromSeq)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f2.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f2.StreamID, Payload: relay.DataPayload(5, []byte("world"))})); err != nil {
		t.Fatal(err)
	}
	msg := readGuestMsg(t, ctx, guest2)
	out, err := base64.StdEncoding.DecodeString(msg.D)
	if msg.T != "out" || err != nil || string(out) != "world" {
		t.Fatalf("guest2 msg = %+v, want out/world", msg)
	}
	f = shareDaemonFrame(t, ctx, daemon)
	if ack, _ := relay.ParseAck(f.Payload); f.Type != relay.FrameAck || ack != 10 {
		t.Fatalf("got frame %#v, want ACK 10", f)
	}
}

func TestShareWSResumeUnavailableFallsBack(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := shareDaemonFrame(t, ctx, daemon)
	if f1.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f1)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f1.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f1.StreamID, Payload: relay.DataPayload(0, []byte("hi"))})); err != nil {
		t.Fatal(err)
	}
	if msg := readGuestMsg(t, ctx, guest1); msg.T != "out" {
		t.Fatalf("guest1 msg = %+v, want out", msg)
	}
	f := shareDaemonFrame(t, ctx, daemon)
	if ack, _ := relay.ParseAck(f.Payload); f.Type != relay.FrameAck || ack != 2 {
		t.Fatalf("got frame %#v, want ACK 2", f)
	}
	guest1.CloseNow()
	f = shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameClose || string(f.Payload) != relay.CloseClientDisconnected {
		t.Fatalf("got frame %#v, want CLOSE client-disconnected", f)
	}

	// Reconnect, but the daemon no longer retains the stream.
	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := shareDaemonFrame(t, ctx, daemon)
	if f2.Type != relay.FrameOpen || f2.StreamID != f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN stream %d", f2, f1.StreamID)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenErr, StreamID: f2.StreamID, Payload: []byte("resume unavailable")})); err != nil {
		t.Fatal(err)
	}

	// The bridge falls back to a fresh session on a new stream ID.
	f3 := shareDaemonFrame(t, ctx, daemon)
	if f3.Type != relay.FrameOpen || f3.StreamID == f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN on a new stream", f3)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f3.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.ResumeFromSeq != 0 {
		t.Fatalf("resume_from_seq = %d, want 0", open.ResumeFromSeq)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f3.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f3.StreamID, Payload: relay.DataPayload(0, []byte("fresh"))})); err != nil {
		t.Fatal(err)
	}
	msg := readGuestMsg(t, ctx, guest2)
	out, err := base64.StdEncoding.DecodeString(msg.D)
	if msg.T != "out" || err != nil || string(out) != "fresh" {
		t.Fatalf("guest2 msg = %+v, want out/fresh", msg)
	}
}

func TestShareStaticFiles(t *testing.T) {
	engine := testEngine(t)
	server := httptest.NewServer(NewHTTPHandler(engine, ""))
	defer server.Close()

	for _, tc := range []struct{ file, contentType string }{
		{"xterm.js", "text/javascript; charset=utf-8"},
		{"xterm.css", "text/css; charset=utf-8"},
		{"xterm-addon-fit.js", "text/javascript; charset=utf-8"},
	} {
		resp, err := http.Get(server.URL + "/x/static/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s status = %d", tc.file, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != tc.contentType {
			t.Fatalf("%s content-type = %q", tc.file, got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "max-age=86400" {
			t.Fatalf("%s cache-control = %q", tc.file, got)
		}
		if len(body) == 0 {
			t.Fatalf("%s empty", tc.file)
		}
	}

	resp, err := http.Get(server.URL + "/x/static/evil.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("non-whitelisted file status = %d", resp.StatusCode)
	}
}

func shareHTTPSetup(t *testing.T) (*httptest.Server, *Engine) {
	t.Helper()
	engine := testEngine(t)
	peers := NewPeerRegistry()
	peers.Register("tenant-a", PeerInfo{ID: "peer-a", Name: "host-a"}, newLaneQueue())
	server := httptest.NewServer(NewHTTPHandlerWithPeers(engine, "", peers))
	t.Cleanup(server.Close)
	return server, engine
}

func postShare(t *testing.T, server *httptest.Server, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", server.URL+"/api/v1/shares", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ETerm-Tenant", "tenant-a")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestShareCreateTargetValidation(t *testing.T) {
	server, engine := shareHTTPSetup(t)

	for _, body := range []string{
		`{"peer_id":"peer-a","target":"tmux-kill","session_id":"main"}`,
		`{"peer_id":"peer-a","target":"tmux-rename","session_id":"main"}`,
		`{"peer_id":"peer-a","target":"tmux-list"}`,
		`{"peer_id":"peer-a","target":"host","session_id":"h1"}`,
		`{"peer_id":"peer-a","target":"tmux-attach"}`,
		`{"peer_id":"peer-a","target":"tmux-attach","session_id":""}`,
	} {
		resp := postShare(t, server, body)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("body %s status = %d, want 400", body, resp.StatusCode)
		}
	}

	resp := postShare(t, server, `{"peer_id":"peer-a","target":"tmux-attach","session_id":"main","name":"pair"}`)
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(out.URL, "/x/") {
		t.Fatalf("tmux-attach status = %d url = %q", resp.StatusCode, out.URL)
	}
	share, err := engine.GetShareByToken(strings.TrimPrefix(out.URL, "/x/"))
	if err != nil {
		t.Fatal(err)
	}
	if share.Target != relay.TargetTmuxAttach || share.SessionID != "main" {
		t.Fatalf("share target=%q session=%q", share.Target, share.SessionID)
	}

	resp = postShare(t, server, `{"peer_id":"peer-a"}`)
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("default target status = %d", resp.StatusCode)
	}
	share, err = engine.GetShareByToken(strings.TrimPrefix(out.URL, "/x/"))
	if err != nil {
		t.Fatal(err)
	}
	if share.Target != relay.TargetLocal || share.SessionID != "" {
		t.Fatalf("share target=%q session=%q", share.Target, share.SessionID)
	}
}

func TestShareWSTmuxAttach(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "pair", "tmux-attach", "main", 4)
	if err != nil {
		t.Fatal(err)
	}
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.PeerID != "peer-a" || open.Target != relay.TargetTmuxAttach || open.SessionID != "main" {
		t.Fatalf("open = %+v", open)
	}

	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("shared"))})); err != nil {
		t.Fatal(err)
	}
	msg := readGuestMsg(t, ctx, guest)
	out, err := base64.StdEncoding.DecodeString(msg.D)
	if msg.T != "out" || err != nil || string(out) != "shared" {
		t.Fatalf("guest msg = %+v, want out/shared", msg)
	}

	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("tmux detached")})); err != nil {
		t.Fatal(err)
	}
	msg = readGuestMsg(t, ctx, guest)
	if msg.T != "exit" || msg.Reason != "tmux detached" {
		t.Fatalf("guest msg = %+v, want exit/tmux detached", msg)
	}
}

func TestShareWSExpiryDisconnects(t *testing.T) {
	server, engine, daemon, ctx := shareWSSetup(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(300*time.Millisecond))

	guest := shareGuestDial(t, ctx, server, share.Token)
	f := shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f)
	}
	if err := daemon.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
		t.Fatal(err)
	}

	msg := readGuestMsg(t, ctx, guest)
	if msg.T != "exit" || msg.Reason != "share expired" {
		t.Fatalf("guest msg = %+v, want exit/share expired", msg)
	}
	f = shareDaemonFrame(t, ctx, daemon)
	if f.Type != relay.FrameClose || len(f.Payload) != 0 {
		t.Fatalf("got frame %#v, want CLOSE kill (empty payload)", f)
	}
}
