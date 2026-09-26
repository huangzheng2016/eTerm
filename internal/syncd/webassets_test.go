package syncd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func webTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	RegisterWebRoutes(mux)
	return mux
}

func TestWebRoutes(t *testing.T) {
	mux := webTestMux()
	for _, tc := range []struct{ path, wantType string }{
		{"/login", "text/html"},
		{"/app", "text/html"},
		{"/app/deep/link", "text/html"},
		{"/", "text/html"},
		{"/some/unknown/page", "text/html"},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("GET %s = %d", tc.path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
			t.Fatalf("GET %s content-type = %q", tc.path, ct)
		}
		body, _ := io.ReadAll(w.Result().Body)
		if !strings.Contains(string(body), "/webstatic/web.js") {
			t.Fatalf("GET %s shell missing web.js reference", tc.path)
		}
	}
}

func TestWebFallbackKeepsAPIAndShare404(t *testing.T) {
	mux := webTestMux()
	for _, path := range []string{"/api/v1/nope", "/x/sometoken", "/b/blobtoken"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("GET %s = %d, want 404", path, w.Code)
		}
	}
}

func TestWebStatic(t *testing.T) {
	mux := webTestMux()
	for _, tc := range []struct{ path, wantType string }{
		{"/webstatic/web.js", "text/javascript"},
		{"/webstatic/web.css", "text/css"},
		{"/webstatic/xterm.js", "text/javascript"},
		{"/webstatic/xterm.css", "text/css"},
		{"/webstatic/xterm-addon-fit.js", "text/javascript"},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("GET %s = %d", tc.path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
			t.Fatalf("GET %s content-type = %q", tc.path, ct)
		}
	}
	for _, path := range []string{"/webstatic/missing.js", "/webstatic/", "/webstatic/web.html"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("GET %s = %d, want 404", path, w.Code)
		}
	}
}

func TestSharePageTemplateContract(t *testing.T) {
	for _, ph := range []string{"{{.Token}}", "{{.Name}}", "{{.ExpiresAt}}"} {
		if !strings.Contains(sharePageHTML, ph) {
			t.Fatalf("sharepage.html lost template placeholder %s", ph)
		}
	}
	var sb strings.Builder
	if err := sharePageTmpl.Execute(&sb, map[string]interface{}{
		"Token":     "tok123",
		"Name":      `demo "quoted" <name>`,
		"ExpiresAt": time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, `token: "tok123"`) {
		t.Fatalf("rendered share page missing token: %s", out)
	}
	if strings.Contains(out, `<name>`) {
		t.Fatalf("rendered share page has unescaped name: %s", out)
	}
	if !strings.Contains(out, "/webstatic/web.js") {
		t.Fatalf("rendered share page missing web.js reference")
	}
}

func TestWebAppShellAndContractMarkers(t *testing.T) {
	if strings.Contains(webAppHTML, "{{") {
		t.Fatal("webapp.html must be served raw, without template placeholders")
	}
	js, err := webStaticFS.ReadFile("webstatic/web.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, marker := range []string{
		"/api/v1/web/login",
		"/api/v1/web/logout",
		"/api/v1/web/me",
		"/api/v1/ws/client",
		"fingerprint_unconfirmed",
		"host-fingerprint-accept",
		"tmux-list",
		"tmux-attach",
		"resume_from_seq",
	} {
		if !strings.Contains(src, marker) {
			t.Fatalf("web.js missing contract marker %q", marker)
		}
	}
}
