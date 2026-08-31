package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// feedServer is a fake volcano endpoint. Every session serves audio frames
// until the client closes; a negative-seq final frame gets a final
// transcript. conn2 signals that a redialed session arrived.
type feedServer struct {
	t     *testing.T
	connN int32 // atomic
	audio chan []byte
	final chan int32
	conn2 chan struct{}
}

func newFeedServer(t *testing.T) *feedServer {
	return &feedServer{
		t:     t,
		audio: make(chan []byte, 8),
		final: make(chan int32, 4),
		conn2: make(chan struct{}),
	}
}

func (s *feedServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt32(&s.connN, 1)
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.t.Errorf("accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	ctx := context.Background()

	// full client request, then initial response
	if _, _, err := conn.Read(ctx); err != nil {
		return
	}
	if err := conn.Write(ctx, websocket.MessageBinary, serverFrame(s.t, 1, []byte(`{"result":{"text":""}}`))); err != nil {
		return
	}
	if n == 2 {
		close(s.conn2)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if data[1]&0x0f == flagNegWithSequence {
			seq := int32(binary.BigEndian.Uint32(data[4:]))
			s.final <- seq
			conn.Write(ctx, websocket.MessageBinary, serverFrame(s.t, -abs32(seq), []byte(`{"result":{"text":"hello"}}`)))
			continue
		}
		size := int(binary.BigEndian.Uint32(data[8:]))
		payload, err := gunzipData(data[12 : 12+size])
		if err != nil {
			s.t.Errorf("audio gunzip: %v", err)
			return
		}
		s.audio <- payload
	}
}

func waitFeedEvent(t *testing.T, ch <-chan Event, match func(Event) bool) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("events channel closed")
			}
			if match(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out waiting for event")
		}
	}
}

// Passthrough flow: fake helper audio events land as volcano audio frames,
// utterance_end sends the negative-seq final, the transcript surfaces as a
// final event, and the next utterance gets a fresh connection. The second
// cycle reuses the long-lived helper.
func TestVolcanoFeedRoutesPassthrough(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})

	assertCycle := func() {
		t.Helper()
		for i, want := range [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}} {
			select {
			case got := <-srv.audio:
				if !bytes.Equal(got, want) {
					t.Fatalf("audio %d = %v, want %v", i, got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("server did not receive audio frame %d", i)
			}
		}
		select {
		case seq := <-srv.final:
			if seq >= 0 {
				t.Fatalf("final frame seq should be negative, got %d", seq)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server did not receive final frame")
		}
		final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
		if final.Text != "hello" {
			t.Fatalf("final transcript = %q", final.Text)
		}
	}

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCycle()

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session after utterance_end")
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	// second session: the helper persists and streams again
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCycle()
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}

	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-eng.Events():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("events channel not closed after Close")
		}
	}
}

// A helper too old for passthrough (protocol < 2) fails the handshake.
func TestVolcanoFeedRejectsOldHelper(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "1")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx := context.Background()
		conn.Read(ctx)
		conn.Write(ctx, websocket.MessageBinary, serverFrame(t, 1, []byte(`{"result":{"text":""}}`)))
		time.Sleep(100 * time.Millisecond)
	}))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	err := eng.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("expected protocol error, got %v", err)
	}
	eng.Close()
}
