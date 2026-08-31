package voice

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWhisperCompatDescriptor(t *testing.T) {
	d, ok := EngineDescriptorByID("whispercompat")
	if !ok {
		t.Fatal("whispercompat engine not registered")
	}
	if d.Ready(map[string]string{}) {
		t.Fatal("ready without key on default remote base URL")
	}
	if d.Ready(map[string]string{"base_url": "https://api.groq.com/openai/v1"}) {
		t.Fatal("ready without key on remote base URL")
	}
	if !d.Ready(map[string]string{"base_url": "http://localhost:8080/v1"}) {
		t.Fatal("localhost base URL should not need a key")
	}
	if !d.Ready(map[string]string{"base_url": "http://127.0.0.1:8080/v1"}) {
		t.Fatal("127.0.0.1 base URL should not need a key")
	}
	if !d.Ready(map[string]string{"api_key": "k"}) {
		t.Fatal("not ready with key")
	}
	eng, err := d.New(map[string]string{"api_key": "k"}, FeedDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.(*WhisperCompatFeedEngine); !ok {
		t.Fatalf("whispercompat New = %T", eng)
	}
	eng.Close()
}

// The buffered utterance lands as one WAV multipart POST; the JSON text
// field becomes the final transcript.
func TestWhisperCompatFeedPostsUtterance(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	type request struct {
		auth  string
		model string
		wav   []byte
	}
	got := make(chan request, 1)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		wav, _ := io.ReadAll(f)
		f.Close()
		got <- request{auth: r.Header.Get("Authorization"), model: r.FormValue("model"), wav: wav}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"hello"}`))
	}))
	defer httpSrv.Close()

	eng := NewWhisperCompatFeedEngine(WhisperCompatConfig{
		BaseURL: httpSrv.URL + "/v1",
		APIKey:  "test-key",
	}, LocalConfig{BinPath: fakeHelperWrapper(t)})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	final := waitFeedEvent(t, eng.Events(), func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello" {
		t.Fatalf("final transcript = %q", final.Text)
	}

	select {
	case req := <-got:
		if req.auth != "Bearer test-key" {
			t.Fatalf("Authorization: %q", req.auth)
		}
		if req.model != "whisper-1" {
			t.Fatalf("model: %q", req.model)
		}
		if !bytes.HasPrefix(req.wav, []byte("RIFF")) || !bytes.Contains(req.wav[:44], []byte("WAVEfmt ")) {
			t.Fatalf("not a WAV: % x", req.wav[:16])
		}
		if !bytes.Equal(req.wav[44:], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Fatalf("WAV payload = %v", req.wav[44:])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not receive transcription POST")
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

// A non-200 response surfaces as an error event.
func TestWhisperCompatFeedHTTPError(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "2")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer httpSrv.Close()

	eng := NewWhisperCompatFeedEngine(WhisperCompatConfig{
		BaseURL: httpSrv.URL,
		APIKey:  "test-key",
	}, LocalConfig{BinPath: fakeHelperWrapper(t)})

	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ev := waitFeedEvent(t, eng.Events(), func(ev Event) bool {
		return ev.Type == EventError && strings.Contains(ev.Msg, "401")
	})
	if !strings.Contains(ev.Msg, "bad key") {
		t.Fatalf("error event missing body: %q", ev.Msg)
	}
}
