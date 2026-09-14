package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func readOpen(t *testing.T, c *websocket.Conn, ctx context.Context) (relay.Frame, bool) {
	t.Helper()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return relay.Frame{}, false
		}
		f, err := relay.Decode(data)
		if err != nil {
			t.Error(err)
			return relay.Frame{}, false
		}
		if f.Type == relay.FrameHello {
			var hello relay.HelloPayload
			if json.Unmarshal(f.Payload, &hello) != nil || hello.Version != relay.ProtocolVersion {
				t.Errorf("bad client hello: %q", f.Payload)
			}
			continue
		}
		if f.Type == relay.FrameOpen {
			return f, true
		}
	}
}

func TestOpenWritesDataFrames(t *testing.T) {
	got := make(chan relay.Frame, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("missing auth header")
		}
		if r.Header.Get("X-ETerm-Tenant") != "tenant-a" {
			t.Errorf("missing tenant header")
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		got <- f
		if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
			t.Error(err)
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			f, err = relay.Decode(data)
			if err != nil {
				t.Error(err)
				return
			}
			if f.Type == relay.FrameData {
				got <- f
				return
			}
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "token", "tenant-a", false, "peer-a", "local", "", 33, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	if _, err := is.Stdin.Write([]byte("echo ok\n")); err != nil {
		t.Fatal(err)
	}

	open := <-got
	if open.Type != relay.FrameOpen {
		t.Fatalf("got %v want OPEN", open.Type)
	}
	var payload struct {
		Rows int `json:"rows"`
		Cols int `json:"cols"`
	}
	if err := json.Unmarshal(open.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Rows != 33 || payload.Cols != 120 {
		t.Fatalf("open payload pty = %dx%d, want 33x120", payload.Rows, payload.Cols)
	}
	data := <-got
	if data.Type != relay.FrameData || string(data.Payload) != "echo ok\n" {
		t.Fatalf("got %#v, want DATA echo ok", data)
	}
}

func TestOpenReadsDataFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("remote"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 6)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "remote" {
		t.Fatalf("got %q want remote", string(buf))
	}
	waitNextSeq(t, is, 6)
}

func waitNextSeq(t *testing.T, is *internalssh.InteractiveSession, want uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, nextSeq, ok := ResumeInfo(is); ok && nextSeq == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, nextSeq, _ := ResumeInfo(is)
	t.Fatalf("nextSeq = %d, want %d", nextSeq, want)
}

func TestOpenSessionSurvivesOpenContextCancel(t *testing.T) {
	sendData := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		f, ok := readOpen(t, c, r.Context())
		if !ok {
			return
		}
		if err := c.Write(context.Background(), websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
			t.Error(err)
			return
		}
		<-sendData
		if err := c.Write(context.Background(), websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("after"))})); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()
	cancel()
	close(sendData)

	got := make(chan error, 1)
	go func() {
		buf := make([]byte, 5)
		_, err := io.ReadFull(is.Stdout, buf)
		if err == nil && string(buf) != "after" {
			err = errUnexpectedPayload(string(buf))
		}
		got <- err
	}()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stdout")
	}
}

type errUnexpectedPayload string

func (e errUnexpectedPayload) Error() string {
	return "unexpected payload: " + string(e)
}

func TestOpenWithProgressReportsStages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
	}))
	defer server.Close()

	var got []OpenStage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := OpenWithProgress(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80, func(stage OpenStage) {
		got = append(got, stage)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	want := []OpenStage{OpenStageConnect, OpenStageRequest, OpenStageReply}
	if len(got) != len(want) {
		t.Fatalf("stages = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stages = %+v, want %+v", got, want)
		}
	}
}

func TestOpenSendsAckAfterThreshold(t *testing.T) {
	total := ackThresholdBytes + 64*1024
	ackCh := make(chan uint64, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		half := total / 2
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, bytes.Repeat([]byte("a"), half))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(uint64(half), bytes.Repeat([]byte("b"), total-half))}))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			ackFrame, err := relay.Decode(data)
			if err != nil {
				continue
			}
			if ackFrame.Type != relay.FrameAck {
				continue
			}
			ack, err := relay.ParseAck(ackFrame.Payload)
			if err != nil {
				t.Error(err)
				return
			}
			ackCh <- ack
			return
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, total)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	select {
	case ack := <-ackCh:
		if ack != uint64(total) {
			t.Fatalf("ack = %d, want %d", ack, total)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack")
	}
}

func TestResumeOpenSendsStreamAndOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil {
			t.Error(err)
			return
		}
		if f.StreamID != 42 || op.ResumeFromSeq != 7 {
			t.Errorf("stream = %d resume_from_seq = %d, want 42/7", f.StreamID, op.ResumeFromSeq)
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(7, []byte("tail"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := relay.OpenRequest{PeerID: "peer-a", Target: "local", Rows: 24, Cols: 80}
	is, err := ResumeOpenWithProgress(ctx, server.URL, "", "", false, op, 42, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "tail" {
		t.Fatalf("got %q want tail", buf)
	}
	waitNextSeq(t, is, 11)
	streamID, _, ok := ResumeInfo(is)
	if !ok || streamID != 42 {
		t.Fatalf("resume info stream = %d ok = %v, want 42/true", streamID, ok)
	}
}

func TestResumeOpenFailsWithOpenErr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("resume unavailable")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := relay.OpenRequest{PeerID: "peer-a", Target: "local"}
	_, err := ResumeOpenWithProgress(ctx, server.URL, "", "", false, op, 42, 7, nil)
	if err == nil || err.Error() != "resume unavailable" {
		t.Fatalf("err = %v", err)
	}
}

func TestHelloErrFailsOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f, err := relay.Decode(data)
		if err != nil || f.Type != relay.FrameHello {
			t.Errorf("first frame = %#v err = %v, want HELLO", f, err)
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHelloErr, Payload: []byte("unsupported protocol version 2")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err == nil || err.Error() != "relay protocol rejected: unsupported protocol version 2" {
		t.Fatalf("err = %v", err)
	}
}

func TestFrameClosePayloadEndsSessionWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("daemon disconnected")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	select {
	case err := <-is.Done:
		if err == nil || err.Error() != "daemon disconnected" {
			t.Fatalf("done err = %v, want daemon disconnected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
	}
}

func TestEmptyFrameCloseBeforeDataEndsSessionWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	select {
	case err := <-is.Done:
		if err == nil || err.Error() != "remote terminal exited before output" {
			t.Fatalf("done err = %v, want remote terminal exited before output", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
	}
}

func TestEmptyFrameCloseAfterDataEndsSessionNormally(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("x"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 1)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-is.Done:
		if err != nil {
			t.Fatalf("done err = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
	}
}

func TestOpenErrReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("tmux not found in PATH")}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err == nil || err.Error() != "tmux not found in PATH" {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenRetriesPeerOffline(t *testing.T) {
	oldDelay := peerOfflineRetryDelay
	peerOfflineRetryDelay = time.Millisecond
	t.Cleanup(func() { peerOfflineRetryDelay = oldDelay })

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		if attempts < 3 {
			_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("peer offline")}))
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ok"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 2)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ok" {
		t.Fatalf("buf = %q", buf)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestDataSeqGapEndsSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(5, []byte("xy"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 2)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-is.Done:
		if err == nil || !strings.Contains(err.Error(), "gap") {
			t.Fatalf("done err = %v, want output gap", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for gap error")
	}
}

func TestDataSeqDuplicateDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(2, []byte("cd"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "abcd" {
		t.Fatalf("got %q, want abcd (duplicate segment not dropped)", buf)
	}
}

func TestFreshOpenRebasesFirstFrameSeq(t *testing.T) {
	const base = uint64(5 * 1024 * 1024)
	tail := bytes.Repeat([]byte("x"), ackThresholdBytes)
	total := base + 2 + uint64(len(tail))
	ackCh := make(chan uint64, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(base, []byte("ab"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(base+2, tail)}))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			ackFrame, err := relay.Decode(data)
			if err != nil || ackFrame.Type != relay.FrameAck {
				continue
			}
			ack, err := relay.ParseAck(ackFrame.Payload)
			if err != nil {
				t.Error(err)
				return
			}
			ackCh <- ack
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 2+len(tail))
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf[:2]) != "ab" {
		t.Fatalf("prefix = %q, want ab", buf[:2])
	}
	select {
	case ack := <-ackCh:
		if ack != total {
			t.Fatalf("ack = %d, want %d (lastAck not aligned to rebased seq)", ack, total)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ack")
	}
	waitNextSeq(t, is, total)
}

func TestResumeOpenGapStillFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(7, []byte("ab"))}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(10, []byte("xy"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := relay.OpenRequest{PeerID: "peer-a", Target: "local", Rows: 24, Cols: 80}
	is, err := ResumeOpenWithProgress(ctx, server.URL, "", "", false, op, 42, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 2)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-is.Done:
		if err == nil || !strings.Contains(err.Error(), "gap") {
			t.Fatalf("done err = %v, want output gap", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for gap error")
	}
}

func TestResumeOpenRebasesRaisedBaseline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(42, []byte("tail"))}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := relay.OpenRequest{PeerID: "peer-a", Target: "local", Rows: 24, Cols: 80}
	is, err := ResumeOpenWithProgress(ctx, server.URL, "", "", false, op, 42, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "tail" {
		t.Fatalf("got %q want tail", buf)
	}
	waitNextSeq(t, is, 46)
}

func TestWriteChunksLargeInput(t *testing.T) {
	total := 2*relay.MaxWebSocketMessageBytes + 12345
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte(i)
	}
	got := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(relay.MaxWebSocketMessageBytes)
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		var acc []byte
		for len(acc) < total {
			typ, data, err := c.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			fr, err := relay.Decode(data)
			if err != nil || fr.Type != relay.FrameData {
				continue
			}
			if len(fr.Payload) > maxInputChunkBytes {
				t.Errorf("frame payload %d bytes exceeds chunk size %d", len(fr.Payload), maxInputChunkBytes)
			}
			acc = append(acc, fr.Payload...)
		}
		got <- acc
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	n, err := is.Stdin.Write(payload)
	if err != nil {
		t.Fatal(err)
	}
	if n != total {
		t.Fatalf("write n = %d, want %d", n, total)
	}
	select {
	case acc := <-got:
		if !bytes.Equal(acc, payload) {
			t.Fatal("reassembled input does not match written payload")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for chunked input")
	}
}

func TestReadLoopExitClosesConn(t *testing.T) {
	closed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("bye")}))
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for {
			if _, _, err = c.Read(rctx); err != nil {
				break
			}
		}
		closed <- err
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	select {
	case err := <-is.Done:
		if err == nil || err.Error() != "bye" {
			t.Fatalf("done err = %v, want bye", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
	}
	select {
	case err := <-closed:
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("server conn not closed by client, read err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for conn close")
	}
}

func TestOpenTimeoutContextAddsDeadline(t *testing.T) {
	ctx, cancel := openTimeoutContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline")
	}
}

func TestWriteTimeoutContextAddsDeadline(t *testing.T) {
	ctx, cancel := writeTimeoutContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected deadline")
	}
}

func TestParseTmuxSessionList(t *testing.T) {
	got, err := ParseTmuxSessionList([]byte(`[{"name":"work","created_unix":5,"attached":true}]`))
	if err != nil || len(got) != 1 || got[0].Name != "work" || got[0].CreatedUnix != 5 || !got[0].Attached {
		t.Fatalf("got %+v err %v", got, err)
	}
	empty, err := ParseTmuxSessionList(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty parse: %+v %v", empty, err)
	}
}

func TestOpenTmuxSession(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		sessionID  string
		okPayload  string
		wantTarget string
		wantID     string
	}{
		{name: "new", target: relay.TargetTmuxNew, okPayload: "tmux-abc123", wantTarget: relay.TargetTmuxNew},
		{name: "attach", target: relay.TargetTmuxAttach, sessionID: "work", wantTarget: relay.TargetTmuxAttach, wantID: "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer c.CloseNow()
				ctx := r.Context()
				f, ok := readOpen(t, c, ctx)
				if !ok {
					return
				}
				var op relay.OpenRequest
				if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != tt.wantTarget || op.SessionID != tt.wantID || op.Rows != 31 || op.Cols != 111 {
					t.Errorf("bad open request: %+v err=%v", op, err)
				}
				_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: []byte(tt.okPayload)}))
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			is, sessionID, err := OpenTmuxSession(ctx, server.URL, "", "", false, "peer-a", tt.target, tt.sessionID, 31, 111)
			if err != nil {
				t.Fatal(err)
			}
			defer is.Close()
			if sessionID != tt.okPayload {
				t.Fatalf("sessionID = %q", sessionID)
			}
		})
	}
}

func TestKillTmuxSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxKill || op.SessionID != "work" {
			t.Errorf("bad kill request: %+v err=%v", op, err)
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := KillTmuxSession(ctx, server.URL, "", "", false, "peer-a", "work"); err != nil {
		t.Fatal(err)
	}
}

func TestRenameTmuxSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxRename || op.SessionID != "x1" || op.Name != "work" {
			t.Errorf("bad rename request: %+v err=%v", op, err)
		}
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := RenameTmuxSession(ctx, server.URL, "", "", false, "peer-a", "x1", "work"); err != nil {
		t.Fatal(err)
	}
}

func TestListTmuxSessions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		f, ok := readOpen(t, c, ctx)
		if !ok {
			return
		}
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxList {
			t.Errorf("bad list request: %v target=%s", err, op.Target)
		}
		list, _ := json.Marshal([]relay.TmuxSessionInfo{{Name: "x1", CreatedUnix: 9, Attached: true}})
		_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: list}))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions, err := ListTmuxSessions(ctx, server.URL, "", "", false, "peer-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "x1" || sessions[0].CreatedUnix != 9 {
		t.Fatalf("got %+v", sessions)
	}
}
