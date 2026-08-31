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

func TestAssemblyAIDescriptor(t *testing.T) {
	d, ok := EngineDescriptorByID("assemblyai")
	if !ok {
		t.Fatal("assemblyai engine not registered")
	}
	if d.Ready(map[string]string{}) {
		t.Fatal("ready without key")
	}
	if !d.Ready(map[string]string{"api_key": "k"}) {
		t.Fatal("not ready with key")
	}
	if got := FirstMissingParam(d, map[string]string{}); got != "AssemblyAI API key" {
		t.Fatalf("first missing = %q", got)
	}
	eng, err := d.New(map[string]string{"api_key": "k"}, FeedDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*streamFeedEngine); !ok {
		t.Fatalf("assemblyai New = %T", eng)
	}
	eng.Close()
}

// assemblyAIServer is a fake AssemblyAI realtime endpoint. Each connection
// reads binary audio frames until a Terminate text message, answers with a
// final transcript, and closes.
type assemblyAIServer struct {
	t         *testing.T
	connN     int32 // atomic
	audio     chan []byte
	terminate chan struct{}
	conn2     chan struct{}
}

func newAssemblyAIServer(t *testing.T) *assemblyAIServer {
	return &assemblyAIServer{
		t:         t,
		audio:     make(chan []byte, 8),
		terminate: make(chan struct{}, 4),
		conn2:     make(chan struct{}),
	}
}

func (s *assemblyAIServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "test-key" {
		s.t.Errorf("Authorization: %q", r.Header.Get("Authorization"))
	}
	if r.URL.Query().Get("sample_rate") != "16000" {
		s.t.Errorf("sample_rate = %q", r.URL.Query().Get("sample_rate"))
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
				conn.Write(ctx, websocket.MessageText, []byte(`{"message_type":"PartialTranscript","text":"hel"}`))
			}
			continue
		}
		if !strings.Contains(string(data), "Terminate") {
			s.t.Errorf("unexpected text frame: %s", data)
			continue
		}
		s.terminate <- struct{}{}
		conn.Write(ctx, websocket.MessageText, []byte(`{"message_type":"FinalTranscript","text":"hello"}`))
		time.Sleep(50 * time.Millisecond)
		return
	}
}

func TestAssemblyAIEngineLifecycle(t *testing.T) {
	srv := newAssemblyAIServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewAssemblyAIEngine(AssemblyAIConfig{APIKey: "test-key", URL: wsURL})
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
	case <-srv.terminate:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive Terminate")
	}
	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
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

func TestAssemblyAIEngineRequiresKey(t *testing.T) {
	eng := NewAssemblyAIEngine(AssemblyAIConfig{})
	if err := eng.Start(context.Background()); err == nil {
		t.Fatal("expected auth error")
	}
	eng.Close()
}

// Passthrough flow: fake helper audio lands as assemblyai audio frames,
// utterance_end sends Terminate, the final transcript surfaces, and the next
// utterance gets a fresh connection.
func TestAssemblyAIFeedRoutesPassthrough(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	srv := newAssemblyAIServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := newAssemblyAIFeed(AssemblyAIConfig{APIKey: "test-key", URL: wsURL},
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
	case <-srv.terminate:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive Terminate")
	}
	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
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
