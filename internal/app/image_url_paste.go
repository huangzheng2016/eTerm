package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/clipboardblob"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/syncblob"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

var readClipboardBlob = clipboardblob.Read

type imagePasteFallbackMsg struct {
	streamID uint64
	msg      tea.Msg
}

func (a App) startImageURLPaste(fallback tea.Msg, forceUpload bool) (App, tea.Cmd) {
	if !a.activeTabIsSSH() {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("paste URL requires a shell tab")} }
	}
	if a.imageUploadProgressCh != nil {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("clipboard upload already in progress")} }
	}
	target := activeSSHView(&a)
	if target == nil {
		return a, nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	return a, startImageURLPasteCmd(&a, cfg, target.StreamID(), fallback, !forceUpload && a.activeTabIsLocalShell())
}

func startImageURLPasteCmd(a *App, cfg esync.Config, streamID uint64, fallback tea.Msg, localFileLink bool) tea.Cmd {
	ch := make(chan syncblob.Progress, 16)
	a.imageUploadProgressCh = ch
	return tea.Batch(waitImageUploadProgressCmd(ch, streamID), uploadImageURLCmd(ch, cfg, streamID, fallback, imageURLCacheSnapshot(a.imageURLCache, time.Now()), localFileLink))
}

func uploadImageURLCmd(ch chan<- syncblob.Progress, cfg esync.Config, streamID uint64, fallback tea.Msg, cache map[string]imageURLCacheEntry, localFileLink bool) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		blob, err := readClipboardBlob()
		if err == clipboardblob.ErrNoBlob {
			if fallback == nil {
				return types.ImageUploadDoneMsg{StreamID: streamID, Err: err}
			}
			return imagePasteFallbackMsg{streamID: streamID, msg: fallback}
		}
		if err != nil {
			return types.ImageUploadDoneMsg{StreamID: streamID, Err: err}
		}
		if localFileLink && blob.LocalPath != "" {
			return types.ImageUploadDoneMsg{StreamID: streamID, URL: fileURL(blob.LocalPath), Filename: blob.Filename}
		}
		cacheKey := imageCacheKey(blob)
		if entry, ok := cache[cacheKey]; ok {
			return types.ImageUploadDoneMsg{StreamID: streamID, URL: entry.URL, Filename: entry.Filename, CacheKey: cacheKey, ExpiresAt: entry.ExpiresAt}
		}
		if !cfg.Enabled || cfg.Mode != "http" || cfg.ServerURL == "" || cfg.APIKey == "" {
			return types.ImageUploadDoneMsg{StreamID: streamID, Err: fmt.Errorf("sync is not configured")}
		}
		client := &syncblob.Client{
			BaseURLs: esync.HTTPBaseURLCandidates(cfg.ServerURL),
			APIKey:   cfg.APIKey,
			Tenant:   cfg.TenantID(),
			HTTP:     esync.HTTPClient(2*time.Minute, cfg.InsecureTLS),
		}
		out, err := client.Upload(blob, func(p syncblob.Progress) {
			select {
			case ch <- p:
			default:
			}
		})
		if err != nil {
			return types.ImageUploadDoneMsg{StreamID: streamID, Err: err}
		}
		url := out.URL
		if strings.HasPrefix(url, "/") {
			url = out.BaseURL + url
		}
		return types.ImageUploadDoneMsg{StreamID: streamID, URL: url, Filename: blob.Filename, CacheKey: cacheKey, ExpiresAt: out.ExpiresAt}
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

func waitImageUploadProgressCmd(ch <-chan syncblob.Progress, streamID uint64) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return types.ImageUploadProgressMsg{StreamID: streamID, TotalBytes: p.TotalBytes, SentBytes: p.SentBytes}
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

func imageCacheKey(blob *clipboardblob.Blob) string {
	sum := sha256.Sum256(blob.Data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func imageURLCacheSnapshot(cache map[string]imageURLCacheEntry, now time.Time) map[string]imageURLCacheEntry {
	out := make(map[string]imageURLCacheEntry)
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
