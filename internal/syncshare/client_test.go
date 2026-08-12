package syncshare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/sync"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/shares":
			var req struct {
				PeerID    string `json:"peer_id"`
				Name      string `json:"name"`
				MaxHours  int    `json:"max_hours"`
				Target    string `json:"target"`
				SessionID string `json:"session_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PeerID != "p1" || req.MaxHours != 4 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if req.Target == "tmux-attach" && req.SessionID == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"url":"/x/tok123","expires_at":"2026-08-12T13:40:00Z"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testConfig(serverURL string) sync.Config {
	return sync.Config{ServerURL: serverURL, APIKey: "key"}
}

func TestCreateShare(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	url, expiresAt, err := CreateShare(t.Context(), testConfig(srv.URL), "p1", "peer", 4, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if url != srv.URL+"/x/tok123" {
		t.Fatalf("url = %q", url)
	}
	if expiresAt.Format(time.RFC3339) != "2026-08-12T13:40:00Z" {
		t.Fatalf("expiresAt = %v", expiresAt)
	}
}

func TestCreateShareTmuxAttach(t *testing.T) {
	srv := testServer(t)
	defer srv.Close()

	url, _, err := CreateShare(t.Context(), testConfig(srv.URL), "p1", "peer", 4, "tmux-attach", "work")
	if err != nil {
		t.Fatal(err)
	}
	if url != srv.URL+"/x/tok123" {
		t.Fatalf("url = %q", url)
	}
}

func TestCreateShareOmitsEmptyTarget(t *testing.T) {
	var raw map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"/x/t","expires_at":"2026-08-12T13:40:00Z"}`)
	}))
	defer srv.Close()

	if _, _, err := CreateShare(t.Context(), testConfig(srv.URL), "p1", "peer", 4, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["target"]; ok {
		t.Fatalf("target should be omitted: %v", raw)
	}
	if _, ok := raw["session_id"]; ok {
		t.Fatalf("session_id should be omitted: %v", raw)
	}
}

func TestCreateShareUnauthorizedDoesNotRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "bad key")
	}))
	defer srv.Close()

	_, _, err := CreateShare(t.Context(), testConfig(srv.URL), "p1", "peer", 4, "", "")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}
