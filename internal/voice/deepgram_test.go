package voice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDeepgramDescriptor(t *testing.T) {
	d, ok := EngineDescriptorByID("deepgram")
	if !ok {
		t.Fatal("deepgram engine not registered")
	}
	if d.Ready(map[string]string{}) {
		t.Fatal("ready without key")
	}
	if !d.Ready(map[string]string{"api_key": "k"}) {
		t.Fatal("not ready with key")
	}
	if got := FirstMissingParam(d, map[string]string{}); got != "Deepgram API key" {
		t.Fatalf("first missing = %q", got)
	}
	eng, err := d.New(map[string]string{"api_key": "k"}, FeedDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*streamFeedEngine); !ok {
		t.Fatalf("deepgram New = %T", eng)
	}
	eng.Close()
}

// deepgramServer is a fake Deepgram realtime endpoint. Each connection reads
// binary audio frames until a CloseStream text message, answers with a final
// result, and closes.
type deepgramServer struct {
	t           *testing.T
	connN       int32 // atomic
	audio       chan []byte
	closeStream chan struct{}
	conn2       chan struct{}
}

func newDeepgramServer(t *testing.T) *deepgramServer {
	return &deepgramServer{
		t:           t,
		audio:       make(chan []byte, 8),
		closeStream: make(chan struct{}, 4),
		conn2:       make(chan struct{}),
	}
}

func (s *deepgramServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Token test-key" {
		s.t.Errorf("Authorization: %q", r.Header.Get("Authorization"))
	}
	q := r.URL.Query()
	for k, want := range map[string]string{
		"encoding":        "linear16",
		"sample_rate":     "16000",
		"channels":        "1",
		"interim_results": "true",
		"model":           "nova-2-general",
		"language":        "zh",
	} {
		if q.Get(k) != want {
			s.t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}

	n := atomic.AddInt32(&s.connN, 1)
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.t.Errorf("accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	ctx := context.Background()
	if n == 2 {
		close(s.conn2)
	}

	interimSent := false
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			s.audio <- data
			if !interimSent {
				interimSent = true
				conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Results","is_final":false,"channel":{"alternatives":[{"transcript":"hel"}]}}`))
			}
			continue
		}
		if !strings.Contains(string(data), "CloseStream") {
			s.t.Errorf("unexpected text frame: %s", data)
			continue
		}
		s.closeStream <- struct{}{}
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Results","is_final":true,"channel":{"alternatives":[{"transcript":"hello"}]}}`))
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Results","is_final":true,"channel":{"alternatives":[{"transcript":"world"}]}}`))
		time.Sleep(50 * time.Millisecond)
		return
	}
}

func TestDeepgramEngineLifecycle(t *testing.T) {
	srv := newDeepgramServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewDeepgramEngine(DeepgramConfig{APIKey: "test-key", URL: wsURL})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := eng.WriteAudio([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-srv.audio:
		if string(got) != string([]byte{1, 2, 3}) {
			t.Fatalf("audio = %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive audio frame")
	}

	partial := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventPartial })
	if partial.Text != "hel" {
		t.Fatalf("partial: %q", partial.Text)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.closeStream:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive CloseStream")
	}
	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello world" {
		t.Fatalf("final: %q", final.Text)
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

func TestDeepgramEngineRequiresKey(t *testing.T) {
	eng := NewDeepgramEngine(DeepgramConfig{})
	if err := eng.Start(context.Background()); err == nil {
		t.Fatal("expected auth error")
	}
	eng.Close()
}

// Passthrough flow: fake helper audio lands as deepgram audio frames,
// utterance_end sends CloseStream, the final transcript surfaces, and the
// next utterance gets a fresh connection.
func TestDeepgramFeedRoutesPassthrough(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	srv := newDeepgramServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := newDeepgramFeed(DeepgramConfig{APIKey: "test-key", URL: wsURL},
		LocalConfig{BinPath: fakeHelperWrapper(t)})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for i, want := range [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}} {
		select {
		case got := <-srv.audio:
			if string(got) != string(want) {
				t.Fatalf("audio %d = %v, want %v", i, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("server did not receive audio frame %d", i)
		}
	}
	select {
	case <-srv.closeStream:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive CloseStream")
	}
	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello world" {
		t.Fatalf("final transcript = %q", final.Text)
	}

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session after utterance_end")
	}

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
