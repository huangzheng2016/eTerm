package syncd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlobHTTPUploadDownload(t *testing.T) {
	engine := testEngine(t)
	srv := httptest.NewServer(NewHTTPHandler(engine, "secret"))
	defer srv.Close()

	req, err := http.NewRequest("POST", srv.URL+"/api/v1/blobs", bytes.NewReader([]byte("abc")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-ETerm-Blob-Mime", "image/png")
	req.Header.Set("X-ETerm-Blob-Filename", "archive.zip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload status = %d body = %s", resp.StatusCode, body)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.URL, "/b/") || strings.Contains(out.URL, "?") {
		t.Fatalf("url = %q", out.URL)
	}
	if len(out.URL) > 32 {
		t.Fatalf("url too long: %q", out.URL)
	}
	get, err := http.Get(srv.URL + out.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer get.Body.Close()
	if get.StatusCode != 200 {
		t.Fatalf("download status = %d", get.StatusCode)
	}
	if got := get.Header.Get("Content-Disposition"); got != `attachment; filename=archive.zip` {
		t.Fatalf("content disposition = %q", got)
	}
	body, _ := io.ReadAll(get.Body)
	if string(body) != "abc" {
		t.Fatalf("download = %q", body)
	}
}

func TestBlobHTTPRejectsOver10MiB(t *testing.T) {
	engine := testEngine(t)
	srv := httptest.NewServer(NewHTTPHandler(engine, "secret"))
	defer srv.Close()

	body := make([]byte, MaxBlobBytes+1)
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/blobs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
