package sync

import (
	"encoding/json"
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

func TestHTTPTransportPullReadsMoreThanSixteenMiB(t *testing.T) {
	payload := strings.Repeat("x", 16<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/records" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []SyncRecord{{
				SyncID:  "big",
				Type:    TypeSnippet,
				Payload: payload,
			}},
			"revision": int64(7),
		})
	}))
	defer server.Close()

	tr := NewHTTPTransportWithOptions(server.URL, "", "", false)
	records, rev, err := tr.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	if rev != 7 || len(records) != 1 || records[0].Payload != payload {
		t.Fatalf("rev=%d records=%d payload=%d", rev, len(records), len(records[0].Payload))
	}
}
