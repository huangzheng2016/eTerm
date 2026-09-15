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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type feedServer struct {
	t           *testing.T
	connN       int32
	audio       chan []byte
	audio2      chan []byte
	configs     chan []byte
	final       chan int32
	conn2       chan struct{}
	finalDelay  time.Duration
	hangSecond  bool
	secondDelay time.Duration
}

func newFeedServer(t *testing.T) *feedServer {
	return &feedServer{
		t:       t,
		audio:   make(chan []byte, 8),
		audio2:  make(chan []byte, 8),
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
	if n == 2 && s.secondDelay > 0 {
		time.Sleep(s.secondDelay)
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
		if n == 2 {
			s.audio2 <- payload
		} else {
			s.audio <- payload
		}
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

func TestVolcanoFeedRedialIsAsync(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	srv.secondDelay = 1500 * time.Millisecond
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

	start := time.Now()
	eng.onUtteranceEnd()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("onUtteranceEnd blocked for %v", elapsed)
	}

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session")
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoFeedRedialWindowAudioReplayed(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	srv := newFeedServer(t)
	srv.secondDelay = time.Second
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

	chunkA := bytes.Repeat([]byte{1}, 6400)
	tail := bytes.Repeat([]byte{3}, 1600)
	chunkB := bytes.Repeat([]byte{2}, 6400)

	eng.onAudio(chunkA)
	select {
	case got := <-srv.audio:
		if !bytes.Equal(got, chunkA) {
			t.Fatalf("conn1 audio = %d bytes, want chunkA", len(got))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conn1 did not receive chunkA")
	}

	eng.onAudio(tail)
	eng.onUtteranceEnd()
	eng.onAudio(chunkB)

	eng.mu.Lock()
	buffered := bytes.Equal(eng.buf, chunkB)
	eng.mu.Unlock()
	if !buffered {
		t.Fatal("audio during redial window was not buffered")
	}

	select {
	case got := <-srv.audio:
		if !bytes.Equal(got, tail) {
			t.Fatalf("conn1 tail = %d bytes, want %d", len(got), len(tail))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conn1 did not receive the buffered tail")
	}
	select {
	case seq := <-srv.final:
		if seq >= 0 {
			t.Fatalf("final frame seq should be negative, got %d", seq)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conn1 did not receive final frame")
	}

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session")
	}
	select {
	case got := <-srv.audio2:
		if !bytes.Equal(got, chunkB) {
			t.Fatalf("conn2 replayed audio = %d bytes, want chunkB", len(got))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("window audio was not replayed to the new connection")
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
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

// TestVolcanoLiveSingleConnFinals probes whether the bigmodel_async endpoint
// emits per-utterance finals on a single long-lived connection without a
// final frame: two synthesized utterances are streamed at realtime pace with
// silence gaps, then Stop sends the final frame. Runs only with
// ETERM_VOLCANO_API_KEY; requires macOS say/afconvert.
func TestVolcanoLiveSingleConnFinals(t *testing.T) {
	apiKey := os.Getenv("ETERM_VOLCANO_API_KEY")
	if apiKey == "" {
		t.Skip("ETERM_VOLCANO_API_KEY not set")
	}
	pcm1 := synthSpeechPCM(t, "hello world this is the first sentence")
	pcm2 := synthSpeechPCM(t, "please run git status and kubectl get pods")

	rid := os.Getenv("ETERM_VOLCANO_RESOURCE_ID")
	if rid == "" {
		rid = ResourceIDSeedASR
	}
	eng := NewVolcanoEngine(VolcanoConfig{APIKey: apiKey, ResourceID: rid, URL: os.Getenv("ETERM_VOLCANO_URL"), SampleRate: 16000, SmartFormat: true})
	start := time.Now()
	type evRec struct {
		at   time.Duration
		text string
	}
	var mu sync.Mutex
	var finals []evRec
	partials := 0
	doneCollect := make(chan struct{})
	go func() {
		defer close(doneCollect)
		for ev := range eng.Events() {
			mu.Lock()
			switch ev.Type {
			case EventFinal:
				finals = append(finals, evRec{time.Since(start), ev.Text})
			case EventPartial:
				partials++
			case EventError:
				t.Logf("error event: %s", ev.Msg)
			}
			mu.Unlock()
		}
	}()

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	stream := func(pcm []byte) {
		t.Helper()
		for off := 0; off < len(pcm); off += 3200 {
			end := off + 3200
			if end > len(pcm) {
				end = len(pcm)
			}
			if err := eng.WriteAudio(pcm[off:end]); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	silence := bytes.Repeat([]byte{0}, 32000) // 1s of 16kHz 16bit mono
	stream(pcm1)
	stream(silence)
	stream(silence)
	mu.Lock()
	n1 := len(finals)
	p1 := partials
	mu.Unlock()
	t.Logf("after utterance 1 + 2s silence: finals=%d partials=%d", n1, p1)

	stream(pcm2)
	stream(silence)
	stream(silence)
	mu.Lock()
	n2 := len(finals)
	mu.Unlock()
	t.Logf("after utterance 2 + 2s silence: finals=%d", n2)

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	eng.Close()
	<-doneCollect

	mu.Lock()
	defer mu.Unlock()
	for i, f := range finals {
		t.Logf("final %d at %v: %q", i, f.at.Round(time.Millisecond), f.text)
	}
	t.Logf("finals total %d (before Stop: %d)", len(finals), n2)
	if n1 != 0 || n2 != 0 {
		t.Fatalf("server auto-finalized %d+%d utterances without a final frame; per-utterance redial may now be skippable", n1, n2)
	}
	if len(finals) == 0 {
		t.Fatal("no final after Stop")
	}
}

// TestVolcanoFeedLiveMultiUtterance drives the feed engine against the real
// endpoint with two synthesized utterances, the second one starting inside
// the redial window, and expects both finals to arrive with no audio loss.
// Runs only with ETERM_VOLCANO_API_KEY; requires macOS say/afconvert.
func TestVolcanoFeedLiveMultiUtterance(t *testing.T) {
	apiKey := os.Getenv("ETERM_VOLCANO_API_KEY")
	if apiKey == "" {
		t.Skip("ETERM_VOLCANO_API_KEY not set")
	}
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	defer os.Unsetenv("GO_FAKE_NO_AUDIO")

	pcm1 := synthSpeechPCM(t, "hello world this is the first sentence")
	pcm2 := synthSpeechPCM(t, "please run git status now")

	rid := os.Getenv("ETERM_VOLCANO_RESOURCE_ID")
	if rid == "" {
		rid = ResourceIDSeedASR
	}
	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: apiKey, ResourceID: rid, URL: os.Getenv("ETERM_VOLCANO_URL"), SampleRate: 16000, SmartFormat: true},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	feed := func(pcm []byte) {
		for off := 0; off < len(pcm); off += pcmFlushBytes {
			eng.onAudio(pcm[off:min(off+pcmFlushBytes, len(pcm))])
			time.Sleep(50 * time.Millisecond)
		}
	}
	feed(pcm1)
	eng.onUtteranceEnd()
	feed(pcm2)
	eng.onUtteranceEnd()

	var finals []string
	deadline := time.After(30 * time.Second)
	for len(finals) < 2 {
		select {
		case ev := <-eng.Events():
			if ev.Type == EventFinal && strings.TrimSpace(ev.Text) != "" {
				finals = append(finals, ev.Text)
			}
			if ev.Type == EventError {
				t.Logf("error event: %s", ev.Msg)
			}
		case <-deadline:
			t.Fatalf("got %d finals, want 2: %v", len(finals), finals)
		}
	}
	t.Logf("final 1: %q", finals[0])
	t.Logf("final 2: %q", finals[1])
	if !strings.Contains(strings.ToLower(finals[0]), "first sentence") {
		t.Fatalf("final 1 = %q", finals[0])
	}
	if !strings.Contains(strings.ToLower(finals[1]), "git status") {
		t.Fatalf("final 2 = %q", finals[1])
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func feedConfigContext(t *testing.T, cfg []byte) string {
	t.Helper()
	var v struct {
		Corpus struct {
			Context string `json:"context"`
		} `json:"corpus"`
	}
	if err := json.Unmarshal(cfg, &v); err != nil {
		t.Fatalf("config frame: %v", err)
	}
	return v.Corpus.Context
}

func newContextFeedTestEngine(t *testing.T) (*VolcanoFeedEngine, *feedServer) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	t.Cleanup(func() { os.Unsetenv("GO_FAKE_PROTOCOL") })
	os.Setenv("GO_FAKE_NO_AUDIO", "1")
	t.Cleanup(func() { os.Unsetenv("GO_FAKE_NO_AUDIO") })

	srv := newFeedServer(t)
	httpSrv := httptest.NewServer(http.HandlerFunc(srv.serveHTTP))
	t.Cleanup(httpSrv.Close)
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")

	eng := NewVolcanoFeedEngine(VolcanoFeedConfig{
		Volcano: VolcanoConfig{APIKey: "test-key", URL: wsURL},
		Helper:  LocalConfig{BinPath: fakeHelperWrapper(t)},
	})
	return eng, srv
}

func TestVolcanoFeedRefreshesContextFromProviderOnRedial(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)

	current := `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"first"}]}`
	eng.SetContextProvider(func() string { return current })

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"first"`) {
		t.Fatalf("start dial did not use provider value: %q", got)
	}

	current = `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"second"}]}`
	eng.onUtteranceEnd()

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("no redialed session")
	}
	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"second"`) || strings.Contains(got, `"first"`) {
		t.Fatalf("redial did not use provider value: %q", got)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoFeedPanickingProviderDoesNotBreakRecording(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)
	eng.SetContextProvider(func() string { panic("boom") })
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); got != "" {
		t.Fatalf("panicking provider leaked context: %q", got)
	}

	eng.onUtteranceEnd()

	select {
	case <-srv.conn2:
	case <-time.After(5 * time.Second):
		t.Fatal("redial after provider panic did not happen")
	}
	if got := feedConfigContext(t, <-srv.configs); got != "" {
		t.Fatalf("panicking provider leaked context on redial: %q", got)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoFeedEmptyProviderMeansNoContext(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)
	if err := eng.SetContext(`{"context_type":"dialog_ctx"}`); err != nil {
		t.Fatal(err)
	}
	eng.SetContextProvider(func() string { return "" })
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); got != "" {
		t.Fatalf("empty provider result must drop static context: %q", got)
	}
}

func TestVolcanoFeedContextReusesLastGoodOnProviderPanic(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)

	mode := "good"
	eng.SetContextProvider(func() string {
		if mode == "panic" {
			panic("boom")
		}
		return `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"first"}]}`
	})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"first"`) {
		t.Fatalf("first dial context = %q", got)
	}

	mode = "panic"
	eng.onUtteranceEnd()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"first"`) {
		t.Fatalf("panic dial did not reuse last good context: %q", got)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoFeedContextEmptyThenPanicReusesLastGood(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)

	mode := "good"
	eng.SetContextProvider(func() string {
		switch mode {
		case "panic":
			panic("boom")
		case "empty":
			return ""
		default:
			return `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"cached"}]}`
		}
	})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"cached"`) {
		t.Fatalf("first dial context = %q", got)
	}

	mode = "empty"
	eng.onUtteranceEnd()
	if got := feedConfigContext(t, <-srv.configs); got != "" {
		t.Fatalf("empty provider result must mean no context: %q", got)
	}

	mode = "panic"
	eng.onUtteranceEnd()
	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"cached"`) {
		t.Fatalf("panic dial did not reuse the earlier good context: %q", got)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestVolcanoFeedNilProviderRestoresStaticContext(t *testing.T) {
	eng, srv := newContextFeedTestEngine(t)

	static := `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"static"}]}`
	if err := eng.SetContext(static); err != nil {
		t.Fatal(err)
	}
	eng.SetContextProvider(func() string {
		return `{"hotwords":[],"context_type":"dialog_ctx","context_data":[{"speaker":"user","text":"dynamic"}]}`
	})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"dynamic"`) {
		t.Fatalf("provider value not used: %q", got)
	}

	eng.SetContextProvider(nil)
	eng.onUtteranceEnd()

	if got := feedConfigContext(t, <-srv.configs); !strings.Contains(got, `"static"`) || strings.Contains(got, `"dynamic"`) {
		t.Fatalf("nil provider did not restore static context: %q", got)
	}

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
}
