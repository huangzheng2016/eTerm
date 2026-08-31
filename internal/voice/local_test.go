package voice

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess acts as a fake voicehelper subprocess when spawned via
// the wrapper script from fakeHelperWrapper.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_FAKE_HELPER") != "1" {
		return
	}
	fakeHelperMain()
	os.Exit(0)
}

func fakeHelperMain() {
	protocol := 1
	if v := os.Getenv("GO_FAKE_PROTOCOL"); v != "" {
		protocol, _ = strconv.Atoi(v)
	}
	fmt.Printf(`{"type":"hello","version":"fake","protocol":%d}`+"\n", protocol)
	crashFile := os.Getenv("GO_FAKE_CRASH_FILE")

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, `"cmd":"set_vad_params"`):
			fmt.Printf(`{"type":"echo","text":%s}`+"\n", strconv.Quote(line))
		case strings.Contains(line, `"cmd":"start"`):
			if crashFile != "" {
				data, _ := os.ReadFile(crashFile)
				n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
				if n == 0 {
					os.WriteFile(crashFile, []byte("1"), 0o644)
					os.Exit(1)
				}
			}
			fmt.Println(`{"type":"state","state":"listening"}`)
		case strings.Contains(line, `"cmd":"stop"`):
			fmt.Println(`{"type":"final","text":"hello world"}`)
			fmt.Println(`{"type":"state","state":"idle"}`)
		}
	}
}

// fakeHelperWrapper writes a shell script that runs this test binary as the
// fake helper, and returns its path. GO_FAKE_HELPER is set by the script so
// the parent test process never sees it.
func fakeHelperWrapper(t *testing.T) string {
	t.Helper()
	wrapper := filepath.Join(t.TempDir(), "fakehelper")
	script := "#!/bin/sh\nGO_FAKE_HELPER=1 exec " + strconv.Quote(os.Args[0]) + " -test.run=TestHelperProcess --\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func waitEvent(t *testing.T, eng *LocalEngine, match func(Event) bool) Event {
	t.Helper()
	timeout := time.After(15 * time.Second)
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

func TestLocalEngineRoundTrip(t *testing.T) {
	eng := NewLocalEngine(LocalConfig{BinPath: fakeHelperWrapper(t)})
	ctx := context.Background()

	if err := eng.SetVAD(VADParams{Threshold: 0.7, TrailingSilence: 1.5, MaxSegment: 20}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}

	echo := waitEvent(t, eng, func(ev Event) bool { return ev.Type == "echo" })
	for _, want := range []string{`"cmd":"set_vad_params"`, `"threshold":0.7`, `"trailing_silence":1.5`, `"max_segment":20`} {
		if !strings.Contains(echo.Text, want) {
			t.Fatalf("set_vad_params echo missing %s: %s", want, echo.Text)
		}
	}

	waitEvent(t, eng, func(ev Event) bool { return ev.Type == EventState && ev.State == StateListening })

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	final := waitEvent(t, eng, func(ev Event) bool { return ev.Type == EventFinal })
	if final.Text != "hello world" {
		t.Fatalf("final text: %q", final.Text)
	}
	waitEvent(t, eng, func(ev Event) bool { return ev.Type == EventState && ev.State == StateIdle })

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

func TestLocalEngineRejectsOldProtocol(t *testing.T) {
	os.Setenv("GO_FAKE_PROTOCOL", "0")
	defer os.Unsetenv("GO_FAKE_PROTOCOL")

	eng := NewLocalEngine(LocalConfig{BinPath: fakeHelperWrapper(t)})
	defer eng.Close()
	err := eng.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("expected protocol error, got %v", err)
	}
}

func TestLocalEngineRestartOnCrash(t *testing.T) {
	crashFile := filepath.Join(t.TempDir(), "count")
	os.Setenv("GO_FAKE_CRASH_FILE", crashFile)
	defer os.Unsetenv("GO_FAKE_CRASH_FILE")

	eng := NewLocalEngine(LocalConfig{BinPath: fakeHelperWrapper(t)})
	defer eng.Close()
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// first helper crashes on start; engine restarts and re-enters listening
	waitEvent(t, eng, func(ev Event) bool {
		return ev.Type == EventError && strings.Contains(ev.Msg, "restarted")
	})
	waitEvent(t, eng, func(ev Event) bool { return ev.Type == EventState && ev.State == StateListening })

	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, eng, func(ev Event) bool { return ev.Type == EventFinal })
}

func TestEnsureHelperBinaryDownload(t *testing.T) {
	content := []byte("#!/bin/sh\necho fake helper\n")
	sum := sha256.Sum256(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}))
	defer srv.Close()

	cacheDir := t.TempDir()
	var pcts []float64
	path, err := ensureHelperBinary(context.Background(), LocalConfig{
		CacheDir:           cacheDir,
		DownloadURL:        srv.URL,
		SHA256Hex:          hex.EncodeToString(sum[:]),
		OnDownloadProgress: func(p float64) { pcts = append(pcts, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: %q", got)
	}
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Fatalf("progress callbacks: %v", pcts)
	}

	// cached: second call must not hit the server
	path2, err := ensureHelperBinary(context.Background(), LocalConfig{CacheDir: cacheDir, DownloadURL: "http://127.0.0.1:1/unreachable"})
	if err != nil || path2 != path {
		t.Fatalf("cached lookup: %v %s", err, path2)
	}

	// wrong sha256 must fail and leave no binary
	badDir := t.TempDir()
	_, err = ensureHelperBinary(context.Background(), LocalConfig{
		CacheDir:    badDir,
		DownloadURL: srv.URL,
		SHA256Hex:   hex.EncodeToString([]byte("wrong")),
	})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("expected sha256 error, got %v", err)
	}
	entries, _ := os.ReadDir(badDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("unexpected file left behind: %s", e.Name())
		}
	}
}

func TestEnsureHelperBinaryMissingExplicitPath(t *testing.T) {
	_, err := ensureHelperBinary(context.Background(), LocalConfig{BinPath: "/nonexistent/voicehelper"})
	if err == nil {
		t.Fatal("expected error for missing BinPath")
	}
}
