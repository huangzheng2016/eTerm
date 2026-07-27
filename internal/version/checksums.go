package version

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var httpChecksumClient = updateHTTPClient(120 * time.Second)

var ErrChecksumsUnavailable = errors.New("SHA256SUMS not in release")

// ParseChecksumsFile parses SHA256SUMS lines: "<hex>  <filename>" (GNU sha256sum).
func ParseChecksumsFile(content []byte) map[string]string {
	out := make(map[string]string)
	s := bufio.NewScanner(bytes.NewReader(content))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := filepath.Base(strings.TrimPrefix(parts[len(parts)-1], "*"))
		out[key] = strings.ToLower(parts[0])
	}
	return out
}

func fetchChecksums(tag string) (map[string]string, error) {
	u := ReleaseAssetURL(tag, ChecksumsFileName)
	resp, err := httpChecksumClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrChecksumsUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksums GET %s: %s", u, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseChecksumsFile(b), nil
}

func checksumFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyArchiveChecksum(tag, archivePath, archiveBasename string) (checksumsUsed bool, err error) {
	sumMap, err := fetchChecksums(tag)
	if errors.Is(err, ErrChecksumsUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fetch checksums: %w", err)
	}
	key := filepath.Base(archiveBasename)
	want := sumMap[key]
	if want == "" {
		return true, fmt.Errorf("no checksum entry for %q in %s", key, ChecksumsFileName)
	}
	got, err := checksumFileSHA256(archivePath)
	if err != nil {
		return true, err
	}
	if got != want {
		return true, fmt.Errorf("checksum mismatch for %s", key)
	}
	return true, nil
}
