package sync

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBaseURLCandidates(t *testing.T) {
	got := HTTPBaseURLCandidates("sync.example.com:8443")
	want := []string{"https://sync.example.com:8443", "http://sync.example.com:8443"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v", got, want)
	}

	got = HTTPBaseURLCandidates("http://sync.example.com")
	if len(got) != 1 || got[0] != "http://sync.example.com" {
		t.Fatalf("got %#v want explicit http only", got)
	}
}

func TestWSURLCandidates(t *testing.T) {
	got := WSURLCandidates("sync.example.com:8443", "/api/v1/ws/client")
	want := []string{"wss://sync.example.com:8443/api/v1/ws/client", "ws://sync.example.com:8443/api/v1/ws/client"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestHTTPTransportFallbacksToHTTPWhenSchemeMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ping" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	base := strings.TrimPrefix(server.URL, "http://")
	tr := NewHTTPTransportWithOptions(base, "", "", false)
	if err := tr.Ping(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportAllowsInsecureTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tr := NewHTTPTransportWithOptions(server.URL, "", "", true)
	if err := tr.Ping(); err != nil {
		t.Fatal(err)
	}
}
