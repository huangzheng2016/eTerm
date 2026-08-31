package voice

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestModelCatalog(t *testing.T) {
	catalog := ModelCatalog()
	if len(catalog) != 3 {
		t.Fatalf("catalog size = %d", len(catalog))
	}
	seen := map[string]bool{}
	for _, m := range catalog {
		if m.ID == "" || m.Name == "" || m.Kind == "" || m.Dir == "" || m.File == "" || m.URL == "" || m.Size == "" {
			t.Fatalf("incomplete spec: %+v", m)
		}
		if seen[m.ID] {
			t.Fatalf("duplicate id %s", m.ID)
		}
		seen[m.ID] = true
		if !strings.HasPrefix(m.URL, "https://") {
			t.Fatalf("non-https URL: %s", m.URL)
		}
	}
	if ModelByID("no-such-model").ID != catalog[0].ID {
		t.Fatal("unknown id must fall back to the default")
	}
	if ModelByID(catalog[2].ID).ID != catalog[2].ID {
		t.Fatal("known id not resolved")
	}
}

func TestModelSpecInstalled(t *testing.T) {
	root := t.TempDir()
	spec := ModelSpec{ID: "x", Dir: "modeldir", File: "model.onnx"}
	if spec.Installed(root) {
		t.Fatal("installed in empty root")
	}
	dir := spec.ModelDir(root)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("x"), 0o644)
	if spec.Installed(root) {
		t.Fatal("installed without tokens.txt")
	}
	os.WriteFile(filepath.Join(dir, "tokens.txt"), []byte("x"), 0o644)
	if !spec.Installed(root) {
		t.Fatal("not installed with tokens+model")
	}
}

func downloadTestSpec() ModelSpec {
	return ModelSpec{
		ID: "test-model", Name: "Test", Kind: "sensevoice",
		Dir: "test-model-dir", File: "model.onnx",
	}
}

func makeTar(t *testing.T, files map[string]string) []byte {
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

func serveBytes(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadModelInstalls(t *testing.T) {
	tarball := makeTarGz(t, map[string]string{
		"test-model-dir/tokens.txt": "tok",
		"test-model-dir/model.onnx": "onnx",
	})
	srv := serveBytes(t, tarball)

	root := t.TempDir()
	spec := downloadTestSpec()
	var pcts []float64
	if err := DownloadModel(context.Background(), spec, root, srv.URL, func(p float64) { pcts = append(pcts, p) }); err != nil {
		t.Fatal(err)
	}
	if !spec.Installed(root) {
		t.Fatal("model not installed after download")
	}
	got, err := os.ReadFile(filepath.Join(spec.ModelDir(root), "tokens.txt"))
	if err != nil || string(got) != "tok" {
		t.Fatalf("tokens.txt: %v %q", err, got)
	}
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Fatalf("progress: %v", pcts)
	}
	// no leftover staging or archive entries
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("leftover temp entry: %s", e.Name())
		}
	}
}

func TestDownloadModelExtractsBz2(t *testing.T) {
	cmd := exec.Command("bzip2", "-c")
	cmd.Stdin = bytes.NewReader(makeTar(t, map[string]string{
		"test-model-dir/tokens.txt": "tok",
		"test-model-dir/model.onnx": "onnx-bz2",
	}))
	bz2, err := cmd.Output()
	if err != nil {
		t.Skipf("bzip2 not available: %v", err)
	}
	srv := serveBytes(t, bz2)

	root := t.TempDir()
	spec := downloadTestSpec()
	if err := DownloadModel(context.Background(), spec, root, srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(spec.ModelDir(root), "model.onnx"))
	if err != nil || string(got) != "onnx-bz2" {
		t.Fatalf("model.onnx: %v %q", err, got)
	}
}

func TestDownloadModelHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	root := t.TempDir()
	spec := downloadTestSpec()
	if err := DownloadModel(context.Background(), spec, root, srv.URL, nil); err == nil {
		t.Fatal("expected error for 404")
	}
	if spec.Installed(root) {
		t.Fatal("model installed after failed download")
	}
}
