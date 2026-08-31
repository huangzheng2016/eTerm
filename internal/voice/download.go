package voice

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
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
		d, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		cacheDir = filepath.Join(d, "eterm")
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

	tmp := filepath.Join(cacheDir, ".voicehelper.tar.gz.tmp")
	defer os.Remove(tmp)
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
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}

	if sha256Hex != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != sha256Hex {
			return fmt.Errorf("sha256 mismatch: got %s, want %s", got, sha256Hex)
		}
	}

	staging := filepath.Join(cacheDir, ".voicehelper-staging")
	os.RemoveAll(staging)
	defer os.RemoveAll(staging)
	if err := untarGz(tmp, staging); err != nil {
		return err
	}

	dest := helperDir(cacheDir)
	if _, err := os.Stat(dest); err == nil {
		// another process installed it meanwhile
		return nil
	}
	return os.Rename(staging, dest)
}

func untarGz(archive, destDir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
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
