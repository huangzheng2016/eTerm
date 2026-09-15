package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type feedServer struct {
	t          *testing.T
	connN      int32
	audio      chan []byte
	configs    chan []byte
	final      chan int32
	conn2      chan struct{}
	finalDelay time.Duration
	hangSecond bool
}

func newFeedServer(t *testing.T) *feedServer {
	return &feedServer{
		t:       t,
		audio:   make(chan []byte, 8),
		configs: make(chan []byte, 4),
		final:   make(chan int32, 4),
		conn2:   make(chan struct{}),
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

	if n == 2 && s.hangSecond {
		close(s.conn2)
		conn.Read(ctx)
		return
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		return
	}
	if payload := fullClientPayload(data); payload != nil {
		s.configs <- payload
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
			if s.finalDelay > 0 {
				time.Sleep(s.finalDelay)
			}
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

// fullClientPayload extracts the gzipped JSON payload of a FullClientRequest
// frame sent by the engine.
func fullClientPayload(data []byte) []byte {
	if len(data) < 12 || data[1]>>4 != msgFullClientRequest {
		return nil
	}
	size := int(binary.BigEndian.Uint32(data[8:]))
	if len(data) < 12+size {
		return nil
	}
	payload, err := gunzipData(data[12 : 12+size])
	if err != nil {
		return nil
	}
	return payload
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

func TestVolcanoFeedRoutesPassthrough(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_CHUNK", "6400")
	defer os.Unsetenv("GO_FAKE_CHUNK")

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
		for i, want := range [][]byte{bytes.Repeat([]byte{1}, 6400), bytes.Repeat([]byte{2}, 6400)} {
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

func TestVolcanoFeedFirstChunkLands(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	want := bytes.Repeat([]byte{9}, 6400)
	eng.onAudio(want)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-srv.audio:
			if bytes.Equal(got, want) {
				return
			}
		case <-deadline:
			t.Fatal("first chunk after Start was dropped")
		}
	}
}

func TestVolcanoFeedAggregatesPCMTo200ms(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	quarter := bytes.Repeat([]byte{0x11}, 1600)
	for i := 0; i < 3; i++ {
		eng.onAudio(quarter)
	}
	select {
	case got := <-srv.audio:
		t.Fatalf("frame sent before 200ms accumulated: %d bytes", len(got))
	case <-time.After(300 * time.Millisecond):
	}

	eng.onAudio(bytes.Repeat([]byte{0x22}, 1600))
	want := append(bytes.Repeat([]byte{0x11}, 4800), bytes.Repeat([]byte{0x22}, 1600)...)
	select {
	case got := <-srv.audio:
		if !bytes.Equal(got, want) {
			t.Fatalf("aggregated frame = %d bytes, want %d", len(got), len(want))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("aggregated frame not sent")
	}
}

func TestVolcanoFeedStopFlushesBufferedPCM(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := bytes.Repeat([]byte{0x33}, 3200)
	eng.onAudio(want)
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-srv.audio:
		if !bytes.Equal(got, want) {
			t.Fatalf("flushed frame = %d bytes, want %d", len(got), len(want))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("buffered audio not flushed on Stop")
	}
	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
		t.Fatalf("final transcript = %q", final.Text)
	}
	eng.Close()
}

func TestVolcanoFeedCloseAbortsRedial(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	srv := newFeedServer(t)
	srv.hangSecond = true
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session")
	}

	done := make(chan struct{})
	go func() {
		eng.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close stalled on the hanging redial")
	}
}

func TestVolcanoFeedRedialOverlapsFinalWait(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_CHUNK", "6400")
	defer os.Unsetenv("GO_FAKE_CHUNK")

	srv := newFeedServer(t)
	srv.finalDelay = 2 * time.Second
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-srv.final:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive final frame")
	}
	select {
	case <-srv.conn2:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("redial did not overlap the final wait")
	}

	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
		t.Fatalf("final transcript = %q", final.Text)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	eng.Close()
}

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

func TestVolcanoFeedSetContextInheritsOnRedial(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	defer httpSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	select {
	case cfg := <-srv.configs:
		if strings.Contains(string(cfg), "corpus") {
			t.Fatalf("corpus present before SetContext: %s", cfg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first config frame not captured")
	}

	contextJSON := `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"kubectl get pods"}]}`
	if err := eng.SetContext(contextJSON); err != nil {
		t.Fatal(err)
	}

	eng.onUtteranceEnd()

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session")
	}
	select {
	case cfg := <-srv.configs:
		var v struct {
			Corpus struct {
				Context string `json:"context"`
			} `json:"corpus"`
		}
		if json.Unmarshal(cfg, &v) != nil || v.Corpus.Context != contextJSON {
			t.Fatalf("redialed config lost context: %s", cfg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("redialed config frame not captured")
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}
