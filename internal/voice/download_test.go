package voice

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHelperArchiveExt(t *testing.T) {
	if got := helperArchiveExt("windows"); got != ".zip" {
		t.Fatalf("windows ext = %q", got)
	}
	for _, goos := range []string{"darwin", "linux"} {
		if got := helperArchiveExt(goos); got != ".tar.gz" {
			t.Fatalf("%s ext = %q", goos, got)
		}
	}
	wantSuffix := "voicehelper-" + runtime.GOOS + "-" + runtime.GOARCH + helperArchiveExt(runtime.GOOS)
	if !strings.HasSuffix(DefaultHelperURL, wantSuffix) {
		t.Fatalf("DefaultHelperURL = %q, want suffix %q", DefaultHelperURL, wantSuffix)
	}
}

func TestDownloadAndExtractZipFlatLayout(t *testing.T) {
	zipBytes := makeZip(t, map[string]string{
		helperBinaryName(): "helper-binary",
		"sherpa-onnx.dll":  "dll-a",
		"onnxruntime.dll":  "dll-b",
	})
	srv := serveBytes(t, zipBytes)

	cacheDir := t.TempDir()
	if err := downloadAndExtract(context.Background(), srv.URL, cacheDir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	dest := helperDir(cacheDir)
	for name, want := range map[string]string{
		helperBinaryName(): "helper-binary",
		"sherpa-onnx.dll":  "dll-a",
		"onnxruntime.dll":  "dll-b",
	} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s: %v %q", name, err, got)
		}
	}
	fi, err := os.Stat(filepath.Join(dest, helperBinaryName()))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() == 0 {
		t.Fatal("extracted binary has no permission bits")
	}
}

func TestUnzipRejectsTraversal(t *testing.T) {
	zipBytes := makeZip(t, map[string]string{"../escape.txt": "x"})
	archive := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(archive, zipBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unzip(archive, t.TempDir()); err == nil {
		t.Fatal("expected traversal error")
	}
}
