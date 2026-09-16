package voice

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
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
}

func TestHelperDownloadURL(t *testing.T) {
	got := helperDownloadURL("voicehelper/v1.2.3")
	want := "https://github.com/huangzheng2016/eTerm/releases/download/voicehelper/v1.2.3/voicehelper-" + runtime.GOOS + "-" + runtime.GOARCH + helperArchiveExt(runtime.GOOS)
	if got != want {
		t.Fatalf("helperDownloadURL = %q, want %q", got, want)
	}
}

func TestLatestHelperTag(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{"picks newest helper tag", []string{"v3.5.0", "voicehelper/v1.0.0", "voicehelper/v1.2.0"}, "voicehelper/v1.2.0"},
		{"numeric compare", []string{"voicehelper/v1.10.0", "voicehelper/v1.9.0"}, "voicehelper/v1.10.0"},
		{"main series only", []string{"v3.5.0", "v3.6.0"}, ""},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		if got := latestHelperTag(tc.tags); got != tc.want {
			t.Fatalf("%s: latestHelperTag = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeHelperVersion(t *testing.T) {
	if got := normalizeHelperVersion("voicehelper/v1.0.0"); got != "1.0.0" {
		t.Fatalf("normalizeHelperVersion helper tag = %q", got)
	}
	if got := normalizeHelperVersion("v3.5.0"); got != "v3.5.0" {
		t.Fatalf("normalizeHelperVersion main tag = %q", got)
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"voicehelper/v1.0.0", "voicehelper/v1.0.0", 0},
		{"voicehelper/v1.1.0", "voicehelper/v1.0.0", 1},
		{"voicehelper/v1.0.0", "voicehelper/v1.1.0", -1},
		{"voicehelper/v1.10.0", "voicehelper/v1.9.0", 1},
		{"v1.0.0", "voicehelper/v1.0.0", 0},
	}
	for _, tc := range cases {
		if got := compareVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("compareVersion(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestHelperUpdateAvailable(t *testing.T) {
	cases := []struct {
		name              string
		installed, latest string
		want              bool
	}{
		{"old main series migrates", "v3.5.0", "voicehelper/v1.0.0", true},
		{"dev still updates", "dev", "voicehelper/v1.0.0", true},
		{"0.1.0 still updates", "0.1.0", "voicehelper/v1.0.0", true},
		{"same helper version", "voicehelper/v1.0.0", "voicehelper/v1.0.0", false},
		{"older helper version", "voicehelper/v1.0.0", "voicehelper/v1.1.0", true},
		{"no helper release, old series", "v3.5.0", "", false},
		{"no helper release, new series", "voicehelper/v1.0.0", "", false},
	}
	for _, tc := range cases {
		if got := helperUpdateAvailable(tc.installed, tc.latest); got != tc.want {
			t.Fatalf("%s: helperUpdateAvailable(%q, %q) = %v, want %v", tc.name, tc.installed, tc.latest, got, tc.want)
		}
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
