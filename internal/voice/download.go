package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultHelperURL is the release artifact for this platform. Published by
// the CI recipe in cmd/voicehelper/README.md.
const DefaultHelperURL = "https://github.com/huangzheng2016/eTerm/releases/latest/download/voicehelper-" + runtime.GOOS + "-" + runtime.GOARCH

func helperBinaryName() string {
	if runtime.GOOS == "windows" {
		return "voicehelper.exe"
	}
	return "voicehelper"
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
	path := filepath.Join(cacheDir, helperBinaryName())
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	url := cfg.DownloadURL
	if url == "" {
		url = DefaultHelperURL
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := downloadAndVerify(ctx, url, path, cfg.SHA256Hex, cfg.OnDownloadProgress); err != nil {
		return "", fmt.Errorf("download voice helper: %w", err)
	}
	return path, nil
}

// downloadAndVerify downloads url to dest (atomically), checking sha256Hex
// when non-empty.
func downloadAndVerify(ctx context.Context, url, dest, sha256Hex string, onProgress func(pct float64)) error {
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
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
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
	return os.Rename(tmp, dest)
}
