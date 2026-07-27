package version

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var archiveHTTPClient = updateHTTPClient(0)

func UpgradeStagingDir(tag string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "eterm", "upgrade", strings.TrimPrefix(tag, "v"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// DownloadUpgradeArchive downloads verify extract to staging; returns extracted binary path and whether SHA256SUMS was used.
func DownloadUpgradeArchive(tag, archiveBase, innerName string) (string, bool, error) {
	stageDir, err := UpgradeStagingDir(tag)
	if err != nil {
		return "", false, err
	}
	archivePath := filepath.Join(stageDir, archiveBase)
	part := archivePath + ".part"

	if err := removeIfExist(part); err != nil {
		return "", false, err
	}
	if err := removeIfExist(archivePath); err != nil {
		return "", false, err
	}

	u := ReleaseAssetURL(tag, archiveBase)
	resp, err := archiveHTTPClient.Get(u)
	if err != nil {
		return "", false, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("download GET %s: %s", u, resp.Status)
	}

	dest, err := os.Create(part)
	if err != nil {
		return "", false, err
	}
	_, cerr := io.Copy(dest, resp.Body)
	cerr2 := dest.Close()
	if cerr != nil {
		_ = os.Remove(part)
		return "", false, fmt.Errorf("write archive: %w", cerr)
	}
	if cerr2 != nil {
		_ = os.Remove(part)
		return "", false, cerr2
	}

	if err := os.Rename(part, archivePath); err != nil {
		return "", false, err
	}

	sumsUsed, verr := verifyArchiveChecksum(tag, archivePath, archiveBase)
	if verr != nil {
		_ = os.Remove(archivePath)
		return "", sumsUsed, verr
	}

	if err := removeIfExist(filepath.Join(stageDir, innerName)); err != nil {
		return "", sumsUsed, err
	}

	if err := extractArchive(archivePath, stageDir, innerName); err != nil {
		return "", sumsUsed, err
	}

	binPath := filepath.Join(stageDir, innerName)
	return binPath, sumsUsed, nil
}

func removeIfExist(p string) error {
	if _, err := os.Stat(p); err == nil {
		return os.Remove(p)
	}
	return nil
}
