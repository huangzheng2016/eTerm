package syncblob

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/clipboardblob"
)

func TestClientUploadFailsOverOn5xx(t *testing.T) {
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv500.Close()
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"b1","url":"/b/x","mime":"image/png","bytes":3}`))
	}))
	defer srvOK.Close()

	client := &Client{BaseURLs: []string{srv500.URL, srvOK.URL}, APIKey: "key"}
	blob := &clipboardblob.Blob{Data: []byte("abc"), Mime: "image/png", Filename: "a.png"}
	out, err := client.Upload(blob, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.BaseURL != srvOK.URL {
		t.Fatalf("base url = %q", out.BaseURL)
	}
}

func TestClientUploadDoesNotFailOverOn4xx(t *testing.T) {
	srv403 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv403.Close()
	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"b1","url":"/b/x","mime":"image/png","bytes":3}`))
	}))
	defer srvOK.Close()

	client := &Client{BaseURLs: []string{srv403.URL, srvOK.URL}, APIKey: "key"}
	blob := &clipboardblob.Blob{Data: []byte("abc"), Mime: "image/png", Filename: "a.png"}
	if _, err := client.Upload(blob, nil); err == nil {
		t.Fatal("expected 4xx to abort without failover")
	}
}
