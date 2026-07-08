package debugpprof

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveAddrPrefersFlag(t *testing.T) {
	t.Setenv("ETERM_PPROF_ADDR", "127.0.0.1:6060")

	got := ResolveAddr("127.0.0.1:7070", "ETERM_PPROF_ADDR")

	if got != "127.0.0.1:7070" {
		t.Fatalf("addr = %q, want flag value", got)
	}
}

func TestResolveAddrUsesFirstEnv(t *testing.T) {
	t.Setenv("ETERM_PPROF_ADDR", "127.0.0.1:6060")
	t.Setenv("ETERM_DAEMON_PPROF_ADDR", "127.0.0.1:6061")

	got := ResolveAddr("", "ETERM_DAEMON_PPROF_ADDR", "ETERM_PPROF_ADDR")

	if got != "127.0.0.1:6061" {
		t.Fatalf("addr = %q, want command-specific env", got)
	}
}

func TestStartDisabledForEmptyAddr(t *testing.T) {
	srv, err := Start("test", "")
	if err != nil {
		t.Fatal(err)
	}
	if srv != nil {
		t.Fatalf("server = %#v, want nil", srv)
	}
}

func TestHandlerServesPprofIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rr := httptest.NewRecorder()

	NewHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "goroutine") {
		t.Fatalf("body = %q, want pprof index", rr.Body.String())
	}
}

func TestStartServesPprof(t *testing.T) {
	srv, err := Start("test", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Close(ctx)
	})

	resp, err := http.Get("http://" + srv.Addr() + "/debug/pprof/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "goroutine") {
		t.Fatalf("body = %q, want pprof index", string(body))
	}
}
