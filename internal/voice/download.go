package voice

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultHelperURL is the release artifact for this platform: a tarball with
// the helper binary and its cgo dylibs, produced by the CI recipe in
// cmd/voicehelper/README.md.
const DefaultHelperURL = "https://github.com/huangzheng2016/eTerm/releases/latest/download/voicehelper-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"

func helperBinaryName() string {
	if runtime.GOOS == "windows" {
		return "voicehelper.exe"
	}
	return "voicehelper"
}

// helperDir holds the helper binary and the dylibs it needs at
// @executable_path.
func helperDir(cacheDir string) string {
	return filepath.Join(cacheDir, "voicehelper")
}

// DefaultCacheDir is the eterm cache root; the helper and the voice models
// live under it.
func DefaultCacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "eterm")
}

// HelperInstallPath is where the managed helper binary lives.
func HelperInstallPath() string {
	return filepath.Join(helperDir(DefaultCacheDir()), helperBinaryName())
}

// HelperInstalled reports whether the managed helper binary exists.
func HelperInstalled() bool {
	_, err := os.Stat(HelperInstallPath())
	return err == nil
}

// HelperVersion runs the installed helper with -version and extracts the
// version token ("dev" for builds without an injected tag); "" when the
// helper is missing or predates the -version flag.
func HelperVersion() string {
	out, err := exec.Command(HelperInstallPath(), "-version").Output()
	if err != nil {
		return ""
	}
	// "voicehelper v1.2.3 (protocol 2)"
	f := strings.Fields(string(out))
	if len(f) >= 2 && f[0] == "voicehelper" {
		return f[1]
	}
	return ""
}

// LatestHelperVersion queries the tag of the latest GitHub release; the
// helper ships as a release asset of that tag.
func LatestHelperVersion(ctx context.Context) (string, error) {
	const latestURL = "https://api.github.com/repos/huangzheng2016/eTerm/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", latestURL, resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("latest release has no tag")
	}
	return rel.TagName, nil
}

// DownloadHelper installs the helper binary from url (DefaultHelperURL when
// empty), reporting download progress.
func DownloadHelper(ctx context.Context, url string, onProgress func(pct float64)) error {
	if url == "" {
		url = DefaultHelperURL
	}
	cacheDir := DefaultCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	if err := downloadAndExtract(ctx, url, cacheDir, "", onProgress); err != nil {
		return fmt.Errorf("download voice helper: %w", err)
	}
	binPath := HelperInstallPath()
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("voice helper archive did not contain %s", helperBinaryName())
	}
	return os.Chmod(binPath, 0o755)
}

// ensureHelperBinary locates or downloads the helper binary.
func ensureHelperBinary(ctx context.Context, cfg LocalConfig) (string, error) {
	if cfg.BinPath != "" {
		if _, err := os.Stat(cfg.BinPath); err != nil {
			return "", fmt.Errorf("voice helper: %w", err)
		}
		return cfg.BinPath, nil
	}

	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = DefaultCacheDir()
	}
	binPath := filepath.Join(helperDir(cacheDir), helperBinaryName())
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	url := cfg.DownloadURL
	if url == "" {
		url = DefaultHelperURL
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := downloadAndExtract(ctx, url, cacheDir, cfg.SHA256Hex, cfg.OnDownloadProgress); err != nil {
		return "", fmt.Errorf("download voice helper: %w", err)
	}
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("voice helper archive did not contain %s", helperBinaryName())
	}
	os.Chmod(binPath, 0o755)
	return binPath, nil
}

// downloadAndExtract downloads the helper tarball, verifies sha256Hex when
// non-empty, and extracts it into helperDir(cacheDir) via an atomic rename.
func downloadAndExtract(ctx context.Context, url, cacheDir, sha256Hex string, onProgress func(pct float64)) error {
	tmp := filepath.Join(cacheDir, ".voicehelper.tar.gz.tmp")
	defer os.Remove(tmp)
	if err := downloadFile(ctx, url, tmp, sha256Hex, onProgress); err != nil {
		return err
	}

	staging := filepath.Join(cacheDir, ".voicehelper-staging")
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := untar(tmp, staging); err != nil {
		return err
	}

	dest := helperDir(cacheDir)
	if _, err := os.Stat(dest); err == nil {
		// another process installed it meanwhile
		return nil
	}
	return os.Rename(staging, dest)
}

// downloadFile fetches url into dest with progress, verifying sha256Hex when
// non-empty. The file lands atomically (temp file + rename).
func downloadFile(ctx context.Context, url, dest, sha256Hex string, onProgress func(pct float64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	h := sha256.New()
	w := io.MultiWriter(f, h)
	total := resp.ContentLength
	var got int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			got += int64(n)
			if onProgress != nil && total > 0 {
				onProgress(float64(got) / float64(total) * 100)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if sha256Hex != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != sha256Hex {
			os.Remove(tmp)
			return fmt.Errorf("sha256 mismatch: got %s, want %s", got, sha256Hex)
		}
	}
	return os.Rename(tmp, dest)
}

// untar extracts a tar.gz or tar.bz2 archive (detected by magic bytes).
func untar(archive, destDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	magic, err := br.Peek(3)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	var r io.Reader
	switch {
	case magic[0] == 0x1f && magic[1] == 0x8b:
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	case magic[0] == 'B' && magic[1] == 'Z' && magic[2] == 'h':
		r = bzip2.NewReader(br)
	default:
		return fmt.Errorf("unknown archive format: %s", archive)
	}

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if strings.Contains(name, "..") {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		target := filepath.Join(destDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}
