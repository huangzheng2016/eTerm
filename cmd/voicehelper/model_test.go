package main

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func makeTarBz2(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bzip2Compress compresses via the system bzip2 (stdlib has no bzip2 writer).
func bzip2Compress(t *testing.T, raw []byte) []byte {
	t.Helper()
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("bzip2 not available: %v", err)
	}
	return out
}

func TestAsrModelPathsPrefersFp32(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "model.int8.onnx"), []byte("i8"), 0o644)

	model, tokens, err := asrModelPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(model) != "model.int8.onnx" || filepath.Base(tokens) != "tokens.txt" {
		t.Fatalf("got %s %s", model, tokens)
	}

	os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("fp32"), 0o644)
	model, _, err = asrModelPaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(model) != "model.onnx" {
		t.Fatalf("expected fp32 preferred, got %s", model)
	}

	if _, _, err := asrModelPaths(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestDownloadToWithProgress(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10000")
		w.Write(body)
	}))
	defer srv.Close()

	ev := newEventWriter(io.Discard)
	dest := filepath.Join(t.TempDir(), "out.bin")
	if err := downloadTo(context.Background(), srv.URL, dest, ev); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(body))
	}
}

func TestDownloadToHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ev := newEventWriter(io.Discard)
	dest := filepath.Join(t.TempDir(), "out.bin")
	if err := downloadTo(context.Background(), srv.URL, dest, ev); err == nil {
		t.Fatal("expected error for 404")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("dest should not exist after failed download")
	}
}

func TestUntarBz2(t *testing.T) {
	// build a tar.bz2 via the system bzip2 through compress/bzip2's inverse:
	// no stdlib bzip2 writer, so shell out is avoided; instead craft with tar
	// and compress with an external step is fragile in tests. Use a fixed
	// pre-compressed fixture produced by: tar -cjf fixture.tar.bz2 files
	fixture := filepath.Join(t.TempDir(), "fixture.tar.bz2")
	raw := makeTarBz2(t, map[string]string{
		"modeldir/tokens.txt":    "tok",
		"modeldir/model.onnx":    "onnx",
		"modeldir/sub/extra.txt": "e",
	})
	bz2 := bzip2Compress(t, raw)
	os.WriteFile(fixture, bz2, 0o644)

	dest := t.TempDir()
	if err := untarBz2(fixture, dest); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"modeldir/tokens.txt":    "tok",
		"modeldir/model.onnx":    "onnx",
		"modeldir/sub/extra.txt": "e",
	} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s: got %q want %q", name, got, want)
		}
	}
}

func TestUntarBz2RejectsTraversal(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "evil.tar.bz2")
	raw := makeTarBz2(t, map[string]string{"../escape.txt": "x"})
	os.WriteFile(fixture, bzip2Compress(t, raw), 0o644)
	if err := untarBz2(fixture, t.TempDir()); err == nil {
		t.Fatal("expected traversal error")
	}
}
