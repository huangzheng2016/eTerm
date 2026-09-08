package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/clipboardblob"
	"github.com/huangzheng2016/eTerm/internal/security"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/syncblob"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

var readClipboardBlob = clipboardblob.Read

var openTunnel = func(database *gorm.DB, mk *security.MasterKeyManager, hostID uint, remotePort int) (string, io.Closer, error) {
	t, err := esync.OpenTunnel(database, mk, hostID, remotePort)
	if err != nil {
		return "", nil, err
	}
	return t.BaseURL(), t, nil
}

type blobPasteFallbackMsg struct {
	streamID uint64
	msg      tea.Msg
}

func (a App) startBlobURLPaste(fallback tea.Msg, forceUpload bool) (App, tea.Cmd) {
	if !a.activeTabIsSSH() {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("paste URL requires a shell tab")} }
	}
	if a.blobUploadProgressCh != nil {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("clipboard upload already in progress")} }
	}
	target := activeSSHView(&a)
	if target == nil {
		return a, nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	return a, startBlobURLPasteCmd(&a, cfg, target.StreamID(), fallback, !forceUpload && a.activeTabIsLocalShell())
}

func startBlobURLPasteCmd(a *App, cfg esync.Config, streamID uint64, fallback tea.Msg, localFileLink bool) tea.Cmd {
	ch := make(chan syncblob.Progress, 16)
	a.blobUploadProgressCh = ch
	return tea.Batch(waitBlobUploadProgressCmd(ch, streamID), uploadBlobURLCmd(ch, cfg, a.db, a.masterKey, streamID, fallback, blobURLCacheSnapshot(a.blobURLCache, time.Now()), localFileLink))
}

func uploadBlobURLCmd(ch chan<- syncblob.Progress, cfg esync.Config, database *gorm.DB, mk *security.MasterKeyManager, streamID uint64, fallback tea.Msg, cache map[string]blobURLCacheEntry, localFileLink bool) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		blob, err := readClipboardBlob()
		if err == clipboardblob.ErrNoBlob {
			if fallback == nil {
				return types.BlobUploadDoneMsg{StreamID: streamID, Err: err}
			}
			return blobPasteFallbackMsg{streamID: streamID, msg: fallback}
		}
		if err != nil {
			return types.BlobUploadDoneMsg{StreamID: streamID, Err: err}
		}
		if localFileLink && blob.LocalPath != "" {
			return types.BlobUploadDoneMsg{StreamID: streamID, URL: fileURL(blob.LocalPath), Filename: blob.Filename}
		}
		if len(blob.Data) == 0 {
			return types.BlobUploadDoneMsg{StreamID: streamID, Err: fmt.Errorf("clipboard folder cannot be uploaded")}
		}
		cacheKey := blobCacheKey(blob)
		if entry, ok := cache[cacheKey]; ok {
			return types.BlobUploadDoneMsg{StreamID: streamID, URL: entry.URL, Filename: entry.Filename, CacheKey: cacheKey, ExpiresAt: entry.ExpiresAt}
		}
		if !cfg.Enabled || cfg.APIKey == "" {
			return types.BlobUploadDoneMsg{StreamID: streamID, Err: fmt.Errorf("sync is not configured")}
		}
		baseURLs := esync.HTTPBaseURLCandidates(cfg.ServerURL)
		insecureTLS := cfg.InsecureTLS
		publicBase := ""
		if cfg.Mode == "ssh" {
			if cfg.SSHHostID == 0 {
				return types.BlobUploadDoneMsg{StreamID: streamID, Err: fmt.Errorf("sync is not configured")}
			}
			tunnelURL, tunnelCloser, err := openTunnel(database, mk, cfg.SSHHostID, cfg.RemotePort)
			if err != nil {
				return types.BlobUploadDoneMsg{StreamID: streamID, Err: err}
			}
			defer tunnelCloser.Close()
			baseURLs = []string{tunnelURL}
			insecureTLS = false
			publicBase = fmt.Sprintf("http://127.0.0.1:%d", cfg.RemotePort)
		}
		client := &syncblob.Client{
			BaseURLs: baseURLs,
			APIKey:   cfg.APIKey,
			Tenant:   cfg.TenantID(),
			HTTP:     esync.HTTPClient(2*time.Minute, insecureTLS),
		}
		out, err := client.Upload(blob, func(p syncblob.Progress) {
			select {
			case ch <- p:
			default:
			}
		})
		if err != nil {
			return types.BlobUploadDoneMsg{StreamID: streamID, Err: err}
		}
		url := out.URL
		if strings.HasPrefix(url, "/") {
			if publicBase != "" {
				url = publicBase + url
			} else {
				url = out.BaseURL + url
			}
		}
		return types.BlobUploadDoneMsg{StreamID: streamID, URL: url, Filename: blob.Filename, CacheKey: cacheKey, ExpiresAt: out.ExpiresAt}
	}
}

func (a App) activeTabIsLocalShell() bool {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return false
	}
	if a.tabs[a.activeTab].Type != LocalTab {
		return false
	}
	m, ok := a.tabs[a.activeTab].Model.(*sshview.Model)
	return ok && m.RemoteReconnect() == nil
}

func waitBlobUploadProgressCmd(ch <-chan syncblob.Progress, streamID uint64) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return types.BlobUploadProgressMsg{StreamID: streamID, TotalBytes: p.TotalBytes, SentBytes: p.SentBytes}
	}
}

func activeSSHView(a *App) *sshview.Model {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return nil
	}
	m, _ := a.tabs[a.activeTab].Model.(*sshview.Model)
	return m
}

func sshViewByStreamID(a *App, streamID uint64) *sshview.Model {
	for i := range a.tabs {
		m, ok := a.tabs[i].Model.(*sshview.Model)
		if ok && m.StreamID() == streamID {
			return m
		}
	}
	return nil
}

func blobCacheKey(blob *clipboardblob.Blob) string {
	sum := sha256.Sum256(blob.Data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func blobURLCacheSnapshot(cache map[string]blobURLCacheEntry, now time.Time) map[string]blobURLCacheEntry {
	out := make(map[string]blobURLCacheEntry)
	for key, entry := range cache {
		if entry.URL != "" && entry.ExpiresAt.After(now) {
			out[key] = entry
		}
	}
	return out
}

func markdownBlobLink(filename, url string) string {
	if filename == "" {
		return url
	}
	return "[" + strings.NewReplacer("[", "\\[", "]", "\\]").Replace(filename) + "](" + url + ")"
}

func fileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
