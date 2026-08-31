package voice

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type volcanoServer struct {
	t        *testing.T
	sawAudio chan int32
	sawFinal chan int32
}

func (s *volcanoServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != "test-key" {
		s.t.Errorf("X-Api-Key: %q", r.Header.Get("X-Api-Key"))
	}
	if r.Header.Get("X-Api-Resource-Id") != ResourceIDSeedASR {
		s.t.Errorf("X-Api-Resource-Id: %q", r.Header.Get("X-Api-Resource-Id"))
	}
	if r.Header.Get("X-Api-Connect-Id") == "" {
		s.t.Error("missing X-Api-Connect-Id")
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.t.Errorf("accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	ctx := context.Background()

	// full client request
	_, data, err := conn.Read(ctx)
	if err != nil {
		s.t.Errorf("read config: %v", err)
		return
	}
	if data[0] != 0x11 || data[1] != 0x11 {
		s.t.Errorf("config frame header: % x", data[:4])
	}
	if seq := int32(binary.BigEndian.Uint32(data[4:])); seq != 1 {
		s.t.Errorf("config seq = %d", seq)
	}
	size := int(binary.BigEndian.Uint32(data[8:]))
	payload, err := gunzipData(data[12 : 12+size])
	if err != nil {
		s.t.Errorf("config gunzip: %v", err)
		return
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		s.t.Errorf("config JSON: %v", err)
		return
	}
	if value["audio"].(map[string]any)["format"] != "pcm" {
		s.t.Errorf("config audio: %v", value["audio"])
	}

	// initial response: empty result, seq 1
	if err := conn.Write(ctx, websocket.MessageBinary, serverFrame(s.t, 1, []byte(`{"result":{"text":""}}`))); err != nil {
		s.t.Errorf("write initial: %v", err)
		return
	}

	// one audio frame
	_, data, err = conn.Read(ctx)
	if err != nil {
		s.t.Errorf("read audio: %v", err)
		return
	}
	if data[1]>>4 != msgAudioOnlyRequest {
		s.t.Errorf("audio msgType: %x", data[1]>>4)
	}
	seq := int32(binary.BigEndian.Uint32(data[4:]))
	s.sawAudio <- seq

	// partial
	conn.Write(ctx, websocket.MessageBinary, serverFrame(s.t, seq, []byte(`{"result":{"text":"hel"}}`)))

	// final audio frame (negative seq)
	_, data, err = conn.Read(ctx)
	if err != nil {
		s.t.Errorf("read final: %v", err)
		return
	}
	finalSeq := int32(binary.BigEndian.Uint32(data[4:]))
	if finalSeq >= 0 {
		s.t.Errorf("final frame seq should be negative, got %d", finalSeq)
	}
	s.sawFinal <- finalSeq

	// final transcript
	conn.Write(ctx, websocket.MessageBinary, serverFrame(s.t, -abs32(finalSeq), []byte(`{"result":{"text":"hello"}}`)))
	time.Sleep(100 * time.Millisecond)
}

func waitVolcanoEvent(t *testing.T, eng *VolcanoEngine, match func(Event) bool) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-eng.Events():
			if match(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out waiting for event")
		}
	}
}

func TestVolcanoEngineLifecycle(t *testing.T) {
	srv := &volcanoServer{t: t, sawAudio: make(chan int32, 1), sawFinal: make(chan int32, 1)}
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoEngine(VolcanoConfig{APIKey: "test-key", URL: wsURL, SampleRate: 16000})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := eng.WriteAudio([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-srv.sawAudio:
		if seq != 2 {
			t.Fatalf("audio seq = %d", seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive audio frame")
	}

	partial := waitVolcanoEvent(t, eng, func(ev Event) bool { return ev.Type == EventPartial })
	if partial.Text != "hel" {
		t.Fatalf("partial: %q", partial.Text)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case seq := <-srv.sawFinal:
		if seq != -3 {
			t.Fatalf("final seq = %d", seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive final frame")
	}
	final := waitVolcanoEvent(t, eng, func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
		t.Fatalf("final: %q", final.Text)
	}

	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	// drain buffered events; the channel must end up closed
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

func TestVolcanoEngineAppKeyAuth(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-App-Key") != "app" || r.Header.Get("X-Api-Access-Key") != "access" {
			t.Errorf("app key headers: %q %q", r.Header.Get("X-Api-App-Key"), r.Header.Get("X-Api-Access-Key"))
		}
		if r.Header.Get("X-Api-Key") != "" {
			t.Errorf("unexpected X-Api-Key: %q", r.Header.Get("X-Api-Key"))
		}
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

	eng := NewVolcanoEngine(VolcanoConfig{AppKey: "app", AccessKey: "access", URL: wsURL})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoEngineInitialError(t *testing.T) {
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx := context.Background()
		conn.Read(ctx)
		payload, _ := json.Marshal(map[string]any{"code": 45000001, "message": "invalid resource"})
		conn.Write(ctx, websocket.MessageBinary, serverFrame(t, 1, payload))
		time.Sleep(100 * time.Millisecond)
	}))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoEngine(VolcanoConfig{APIKey: "test-key", URL: wsURL})
	err := eng.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid resource") {
		t.Fatalf("expected initial error, got %v", err)
	}
	eng.Close()
}

func TestVolcanoEngineRequiresAuth(t *testing.T) {
	eng := NewVolcanoEngine(VolcanoConfig{})
	if err := eng.Start(context.Background()); err == nil {
		t.Fatal("expected auth error")
	}
	eng.Close()
}
