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

func sendFrame(c *websocket.Conn, ctx context.Context, f relay.Frame) {
	_ = c.Write(ctx, websocket.MessageBinary, relay.Encode(f))
}

func openServer(t *testing.T, fn func(ctx context.Context, c *websocket.Conn, f relay.Frame)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		fn(ctx, c, f)
	}))
}

func openSession(t *testing.T, url string) *internalssh.InteractiveSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	is, err := Open(ctx, url, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { is.Close() })
	return is
}

func resumeSession(t *testing.T, url string) *internalssh.InteractiveSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := relay.OpenRequest{PeerID: "peer-a", Target: "local", Rows: 24, Cols: 80}
	is, err := ResumeOpenWithProgress(ctx, url, "", "", false, op, 42, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { is.Close() })
	return is
}

func readStdout(t *testing.T, is *internalssh.InteractiveSession, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(is.Stdout, buf); err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

func waitDone(t *testing.T, is *internalssh.InteractiveSession) error {
	t.Helper()
	select {
	case err := <-is.Done:
		return err
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for done")
		return nil
	}
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

type errUnexpectedPayload string

func (e errUnexpectedPayload) Error() string {
	return "unexpected payload: " + string(e)
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("remote"))})
	})
	defer server.Close()

	is := openSession(t, server.URL)
	if got := readStdout(t, is, 6); got != "remote" {
		t.Fatalf("got %q want remote", got)
	}
	waitNextSeq(t, is, 6)
}

func TestOpenSessionSurvivesOpenContextCancel(t *testing.T) {
	sendData := make(chan struct{})
	server := openServer(t, func(_ context.Context, c *websocket.Conn, f relay.Frame) {
		if err := c.Write(context.Background(), websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})); err != nil {
			t.Error(err)
			return
		}
		<-sendData
		if err := c.Write(context.Background(), websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("after"))})); err != nil {
			t.Error(err)
		}
	})
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

func TestOpenWithProgressReportsStages(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	})
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		half := total / 2
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, bytes.Repeat([]byte("a"), half))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(uint64(half), bytes.Repeat([]byte("b"), total-half))})
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
	})
	defer server.Close()

	is := openSession(t, server.URL)
	readStdout(t, is, total)
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil {
			t.Error(err)
			return
		}
		if f.StreamID != 42 || op.ResumeFromSeq != 7 {
			t.Errorf("stream = %d resume_from_seq = %d, want 42/7", f.StreamID, op.ResumeFromSeq)
		}
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(7, []byte("tail"))})
	})
	defer server.Close()

	is := resumeSession(t, server.URL)
	if got := readStdout(t, is, 4); got != "tail" {
		t.Fatalf("got %q want tail", got)
	}
	waitNextSeq(t, is, 11)
	streamID, _, ok := ResumeInfo(is)
	if !ok || streamID != 42 {
		t.Fatalf("resume info stream = %d ok = %v, want 42/true", streamID, ok)
	}
}

func TestResumeOpenFailsWithOpenErr(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("resume unavailable")})
	})
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
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameHelloErr, Payload: []byte("unsupported protocol version 2")})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err == nil || err.Error() != "relay protocol rejected: unsupported protocol version 2" {
		t.Fatalf("err = %v", err)
	}
}

func TestFrameCloseEndsSession(t *testing.T) {
	tests := []struct {
		name    string
		frames  []relay.Frame
		read    int
		wantErr string
	}{
		{name: "payload", frames: []relay.Frame{{Type: relay.FrameClose, Payload: []byte("daemon disconnected")}}, wantErr: "daemon disconnected"},
		{name: "empty before data", frames: []relay.Frame{{Type: relay.FrameClose}}, wantErr: "remote terminal exited before output"},
		{name: "empty after data", frames: []relay.Frame{{Type: relay.FrameData, Payload: relay.DataPayload(0, []byte("x"))}, {Type: relay.FrameClose}}, read: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
				sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
				for _, fr := range tt.frames {
					fr.StreamID = f.StreamID
					sendFrame(c, ctx, fr)
				}
			})
			defer server.Close()

			is := openSession(t, server.URL)
			if tt.read > 0 {
				readStdout(t, is, tt.read)
			}
			err := waitDone(t, is)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("done err = %v, want nil", err)
				}
			} else if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("done err = %v, want %s", err, tt.wantErr)
			}
		})
	}
}

func TestOpenErrReturnsError(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("tmux not found in PATH")})
	})
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		attempts++
		if attempts < 3 {
			sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte("peer offline")})
			return
		}
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ok"))})
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	is, err := Open(ctx, server.URL, "", "", false, "peer-a", "local", "", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer is.Close()

	if got := readStdout(t, is, 2); got != "ok" {
		t.Fatalf("buf = %q", got)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestDataSeqGapEndsSession(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(5, []byte("xy"))})
	})
	defer server.Close()

	is := openSession(t, server.URL)
	readStdout(t, is, 2)
	if err := waitDone(t, is); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("done err = %v, want output gap", err)
	}
}

func TestDataSeqDuplicateDropped(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(0, []byte("ab"))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(2, []byte("cd"))})
	})
	defer server.Close()

	is := openSession(t, server.URL)
	if got := readStdout(t, is, 4); got != "abcd" {
		t.Fatalf("got %q, want abcd (duplicate segment not dropped)", got)
	}
}

func TestFreshOpenRebasesFirstFrameSeq(t *testing.T) {
	const base = uint64(5 * 1024 * 1024)
	tail := bytes.Repeat([]byte("x"), ackThresholdBytes)
	total := base + 2 + uint64(len(tail))
	ackCh := make(chan uint64, 2)
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(base, []byte("ab"))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(base+2, tail)})
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
	})
	defer server.Close()

	is := openSession(t, server.URL)
	if got := readStdout(t, is, 2+len(tail)); got[:2] != "ab" {
		t.Fatalf("prefix = %q, want ab", got[:2])
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(7, []byte("ab"))})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(10, []byte("xy"))})
	})
	defer server.Close()

	is := resumeSession(t, server.URL)
	readStdout(t, is, 2)
	if err := waitDone(t, is); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("done err = %v, want output gap", err)
	}
}

func TestResumeOpenRebasesRaisedBaseline(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameData, StreamID: f.StreamID, Payload: relay.DataPayload(42, []byte("tail"))})
	})
	defer server.Close()

	is := resumeSession(t, server.URL)
	if got := readStdout(t, is, 4); got != "tail" {
		t.Fatalf("got %q want tail", got)
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		c.SetReadLimit(relay.MaxWebSocketMessageBytes)
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
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
	})
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
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID, Payload: []byte("bye")})
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		for {
			if _, _, err = c.Read(rctx); err != nil {
				break
			}
		}
		closed <- err
	})
	defer server.Close()

	is := openSession(t, server.URL)
	if err := waitDone(t, is); err == nil || err.Error() != "bye" {
		t.Fatalf("done err = %v, want bye", err)
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

func TestTimeoutContextAddsDeadline(t *testing.T) {
	tests := []struct {
		name string
		fn   func(context.Context) (context.Context, context.CancelFunc)
	}{
		{"open", openTimeoutContext},
		{"write", writeTimeoutContext},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.fn(context.Background())
			defer cancel()
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("expected deadline")
			}
		})
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
		okPayload  relay.TmuxSessionInfo
		wantTarget string
		wantID     string
	}{
		{name: "new", target: relay.TargetTmuxNew, okPayload: relay.TmuxSessionInfo{Name: "work", SessionID: "uuid-1"}, wantTarget: relay.TargetTmuxNew},
		{name: "attach", target: relay.TargetTmuxAttach, sessionID: "work", okPayload: relay.TmuxSessionInfo{SessionID: "work", Name: "work"}, wantTarget: relay.TargetTmuxAttach, wantID: "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
				var op relay.OpenRequest
				if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != tt.wantTarget || op.SessionID != tt.wantID || op.Rows != 31 || op.Cols != 111 {
					t.Errorf("bad open request: %+v err=%v", op, err)
				}
				payload, _ := json.Marshal(tt.okPayload)
				sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: payload})
			})
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			is, sessionInfo, err := OpenTmuxSession(ctx, server.URL, "", "", false, "peer-a", tt.target, tt.sessionID, 31, 111)
			if err != nil {
				t.Fatal(err)
			}
			defer is.Close()
			if sessionInfo != tt.okPayload {
				t.Fatalf("sessionInfo = %+v", sessionInfo)
			}
		})
	}
}

func TestKillTmuxSession(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxKill || op.SessionID != "work" {
			t.Errorf("bad kill request: %+v err=%v", op, err)
		}
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := KillTmuxSession(ctx, server.URL, "", "", false, "peer-a", "work"); err != nil {
		t.Fatal(err)
	}
}

func TestRenameTmuxSession(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxRename || op.SessionID != "x1" || op.Name != "work" {
			t.Errorf("bad rename request: %+v err=%v", op, err)
		}
		payload, _ := json.Marshal(relay.TmuxSessionInfo{Name: "work", SessionID: "work"})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: payload})
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newID, err := RenameTmuxSession(ctx, server.URL, "", "", false, "peer-a", "x1", "work")
	if err != nil {
		t.Fatal(err)
	}
	if newID != "work" {
		t.Fatalf("newID = %q, want %q", newID, "work")
	}
}

func TestRenameTmuxSessionWithoutIdentityKeepsSessionID(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newID, err := RenameTmuxSession(ctx, server.URL, "", "", false, "peer-a", "x1", "work")
	if err != nil {
		t.Fatal(err)
	}
	if newID != "x1" {
		t.Fatalf("newID = %q, want %q", newID, "x1")
	}
}

func TestListTmuxSessions(t *testing.T) {
	server := openServer(t, func(ctx context.Context, c *websocket.Conn, f relay.Frame) {
		var op relay.OpenRequest
		if err := json.Unmarshal(f.Payload, &op); err != nil || op.Target != relay.TargetTmuxList {
			t.Errorf("bad list request: %v target=%s", err, op.Target)
		}
		list, _ := json.Marshal([]relay.TmuxSessionInfo{{Name: "x1", CreatedUnix: 9, Attached: true}})
		sendFrame(c, ctx, relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: list})
	})
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
