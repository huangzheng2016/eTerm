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

func shareGuestDial(t *testing.T, ctx context.Context, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	guest, _, err := websocket.Dial(ctx, wsBase(server)+"/x/"+token+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { guest.CloseNow() })
	return guest
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

func expectOpen(t *testing.T, ctx context.Context, daemon *websocket.Conn) relay.Frame {
	t.Helper()
	f := readFrame(t, ctx, daemon)
	if f.Type != relay.FrameOpen {
		t.Fatalf("got frame %#v, want OPEN", f)
	}
	return f
}

func expectGuestOut(t *testing.T, ctx context.Context, guest *websocket.Conn, want string) {
	t.Helper()
	msg := readGuestMsg(t, ctx, guest)
	out, err := base64.StdEncoding.DecodeString(msg.D)
	if msg.T != "out" || err != nil || string(out) != want {
		t.Fatalf("guest msg = %+v, want out/%s", msg, want)
	}
}

func expectGuestExit(t *testing.T, ctx context.Context, guest *websocket.Conn, reason string) {
	t.Helper()
	msg := readGuestMsg(t, ctx, guest)
	if msg.T != "exit" || msg.Reason != reason {
		t.Fatalf("guest msg = %+v, want exit/%s", msg, reason)
	}
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
	server, _, ctx := relayServer(t)

	resp, err := http.Get(server.URL + "/x/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("page status = %d", resp.StatusCode)
	}

	_, wsResp, err := websocket.Dial(ctx, wsBase(server)+"/x/nope/ws", nil)
	if err == nil {
		t.Fatal("ws dial with bad token succeeded")
	}
	if wsResp != nil && wsResp.StatusCode != 404 {
		t.Fatalf("ws status = %d", wsResp.StatusCode)
	}
}

func TestSharePageServed(t *testing.T) {
	server, engine, _, _ := relaySetup(t)
	share := mustCreateShare(t, engine, "demo box", "", "", 4)

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
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "demo", "", "", 4)
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := expectOpen(t, ctx, daemon)
	var open relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.PeerID != "peer-a" || open.Target != relay.TargetLocal || open.Name != "demo" || open.Rows != 24 || open.Cols != 80 {
		t.Fatalf("open = %+v", open)
	}
	streamID := f.StreamID

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: streamID})

	in, _ := json.Marshal(shareGuestMsg{T: "in", D: base64.StdEncoding.EncodeToString([]byte("ls"))})
	if err := guest.Write(ctx, websocket.MessageText, in); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, ctx, daemon); f.Type != relay.FrameData || f.StreamID != streamID || string(f.Payload) != "ls" {
		t.Fatalf("got frame %#v payload %q, want DATA ls", f, f.Payload)
	}

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: streamID, Payload: relay.DataPayload(0, []byte("hello"))})
	expectGuestOut(t, ctx, guest, "hello")
	expectAck(t, ctx, daemon, 5)

	rsz, _ := json.Marshal(shareGuestMsg{T: "resize", Rows: 40, Cols: 100})
	if err := guest.Write(ctx, websocket.MessageText, rsz); err != nil {
		t.Fatal(err)
	}
	f = readFrame(t, ctx, daemon)
	rows, cols, err := relay.ParseResize(f.Payload)
	if f.Type != relay.FrameResize || err != nil || rows != 40 || cols != 100 {
		t.Fatalf("got frame %#v, want RESIZE 40x100", f)
	}

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameClose, StreamID: streamID, Payload: []byte("shell exited")})
	expectGuestExit(t, ctx, guest, "shell exited")
}

func TestShareWSOpenErr(t *testing.T) {
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "", "", "", 4)
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := expectOpen(t, ctx, daemon)
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("no shell")})
	expectGuestExit(t, ctx, guest, "no shell")
}

func TestShareWSSecondConnectionReplacesFirst(t *testing.T) {
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "", "", "", 4)
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := expectOpen(t, ctx, daemon)

	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := expectOpen(t, ctx, daemon)
	if f2.StreamID != f1.StreamID {
		t.Fatalf("replacement opened stream %d, want takeover of %d", f2.StreamID, f1.StreamID)
	}
	expectGuestExit(t, ctx, guest1, "replaced")

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f2.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f2.StreamID, Payload: relay.DataPayload(0, []byte("hi"))})
	expectGuestOut(t, ctx, guest2, "hi")
}

func TestShareWSGuestDisconnectResumes(t *testing.T) {
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "", "", "", 4)
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := expectOpen(t, ctx, daemon)

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f1.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f1.StreamID, Payload: relay.DataPayload(0, []byte("hello"))})
	expectGuestOut(t, ctx, guest1, "hello")
	expectAck(t, ctx, daemon, 5)

	guest1.CloseNow()
	if f := readFrame(t, ctx, daemon); f.Type != relay.FrameClose || f.StreamID != f1.StreamID || string(f.Payload) != relay.CloseClientDisconnected {
		t.Fatalf("got frame %#v payload %q, want CLOSE client-disconnected", f, f.Payload)
	}

	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := expectOpen(t, ctx, daemon)
	if f2.StreamID != f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN stream %d", f2, f1.StreamID)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f2.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.ResumeFromSeq != 5 {
		t.Fatalf("resume_from_seq = %d, want 5", open.ResumeFromSeq)
	}
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f2.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f2.StreamID, Payload: relay.DataPayload(5, []byte("world"))})
	expectGuestOut(t, ctx, guest2, "world")
	expectAck(t, ctx, daemon, 10)
}

func TestShareWSResumeUnavailableFallsBack(t *testing.T) {
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "", "", "", 4)
	guest1 := shareGuestDial(t, ctx, server, share.Token)
	f1 := expectOpen(t, ctx, daemon)

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f1.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f1.StreamID, Payload: relay.DataPayload(0, []byte("hi"))})
	expectGuestOut(t, ctx, guest1, "hi")
	expectAck(t, ctx, daemon, 2)

	guest1.CloseNow()
	if f := readFrame(t, ctx, daemon); f.Type != relay.FrameClose || string(f.Payload) != relay.CloseClientDisconnected {
		t.Fatalf("got frame %#v, want CLOSE client-disconnected", f)
	}

	guest2 := shareGuestDial(t, ctx, server, share.Token)
	f2 := expectOpen(t, ctx, daemon)
	if f2.StreamID != f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN stream %d", f2, f1.StreamID)
	}
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenErr, StreamID: f2.StreamID, Payload: []byte("resume unavailable")})

	f3 := expectOpen(t, ctx, daemon)
	if f3.StreamID == f1.StreamID {
		t.Fatalf("got frame %#v, want OPEN on a new stream", f3)
	}
	var open relay.OpenRequest
	if err := json.Unmarshal(f3.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.ResumeFromSeq != 0 {
		t.Fatalf("resume_from_seq = %d, want 0", open.ResumeFromSeq)
	}
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f3.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f3.StreamID, Payload: relay.DataPayload(0, []byte("fresh"))})
	expectGuestOut(t, ctx, guest2, "fresh")
}

func TestShareStaticFiles(t *testing.T) {
	server, _, _ := relayServer(t)

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
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "pair", "tmux-attach", "main", 4)
	guest := shareGuestDial(t, ctx, server, share.Token)

	f := expectOpen(t, ctx, daemon)
	var open relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &open); err != nil {
		t.Fatal(err)
	}
	if open.PeerID != "peer-a" || open.Target != relay.TargetTmuxAttach || open.SessionID != "main" {
		t.Fatalf("open = %+v", open)
	}

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("shared"))})
	expectGuestOut(t, ctx, guest, "shared")

	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("tmux detached")})
	expectGuestExit(t, ctx, guest, "tmux detached")
}

func TestShareWSExpiryDisconnects(t *testing.T) {
	server, engine, daemon, ctx := relaySetup(t)
	share := mustCreateShare(t, engine, "", "", "", 1)
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(300*time.Millisecond))

	guest := shareGuestDial(t, ctx, server, share.Token)
	f := expectOpen(t, ctx, daemon)
	writeFrame(t, ctx, daemon, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})

	expectGuestExit(t, ctx, guest, "share expired")
	if f := readFrame(t, ctx, daemon); f.Type != relay.FrameClose || len(f.Payload) != 0 {
		t.Fatalf("got frame %#v, want CLOSE kill (empty payload)", f)
	}
}

func TestShareForwardPanicRecovered(t *testing.T) {
	logs := captureSyncdLog(t)
	h := NewRelayHub(nil)
	st, _ := h.shareState("tok")
	cause := -1
	dieWith := func(why int) { cause = why }

	h.shareForward(context.Background(), nil, nil, newLaneQueue(), st, st.streamID, time.Now().Add(time.Minute), make(chan struct{}), make(chan struct{}), dieWith)

	if cause != shareExitFatal {
		t.Fatalf("dieWith cause = %d, want %d", cause, shareExitFatal)
	}
	if !strings.Contains(logs.String(), "share forward") || !strings.Contains(logs.String(), "panic") {
		t.Fatalf("panic not logged: %q", logs.String())
	}
}
