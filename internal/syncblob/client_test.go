package syncblob

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/clipboardblob"
)

func TestClientUploadProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("auth = %q", got)
		}
		if got := r.Header.Get("X-ETerm-Blob-Mime"); got != "application/zip" {
			t.Fatalf("mime = %q", got)
		}
		if got := r.Header.Get("X-ETerm-Blob-Filename"); got != "a.zip" {
			t.Fatalf("filename = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "abc" {
			t.Fatalf("body = %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","url":"/api/v1/blobs/b1?t=x","mime":"image/png","bytes":3}`))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, APIKey: "key"}
	var last Progress
	blob := &clipboardblob.Blob{Data: []byte("abc"), Mime: "application/zip", Filename: "a.zip"}
	out, err := client.Upload(blob, func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != "/api/v1/blobs/b1?t=x" || last.SentBytes != 3 || last.TotalBytes != 3 {
		t.Fatalf("out=%#v progress=%#v", out, last)
	}
}

func TestClientUploadFallsBackToNextBaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","url":"/api/v1/blobs/b1?t=x","mime":"image/png","bytes":3}`))
	}))
	defer srv.Close()

	client := &Client{BaseURLs: []string{"http://127.0.0.1:1", srv.URL}, APIKey: "key", HTTP: srv.Client()}
	blob := &clipboardblob.Blob{Data: []byte("abc"), Mime: "application/zip", Filename: "a.zip"}
	out, err := client.Upload(blob, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.BaseURL != srv.URL {
		t.Fatalf("base url = %q", out.BaseURL)
	}
}
