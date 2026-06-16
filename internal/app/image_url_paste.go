package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/clipboardimg"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/syncblob"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type imagePasteFallbackMsg struct {
	streamID uint64
	msg      tea.Msg
}

func (a App) startImageURLPaste(fallback tea.Msg) (App, tea.Cmd) {
	if !a.activeTabIsSSH() {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("image URL paste requires a shell tab")} }
	}
	if a.imageUploadProgressCh != nil {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("image upload already in progress")} }
	}
	target := activeSSHView(&a)
	if target == nil {
		return a, nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	return a, startImageURLPasteCmd(&a, cfg, target.StreamID(), fallback)
}

func startImageURLPasteCmd(a *App, cfg esync.Config, streamID uint64, fallback tea.Msg) tea.Cmd {
	ch := make(chan syncblob.Progress, 16)
	a.imageUploadProgressCh = ch
	return tea.Batch(waitImageUploadProgressCmd(ch, streamID), uploadImageURLCmd(ch, cfg, streamID, fallback, imageURLCacheSnapshot(a.imageURLCache, time.Now())))
}

func uploadImageURLCmd(ch chan<- syncblob.Progress, cfg esync.Config, streamID uint64, fallback tea.Msg, cache map[string]imageURLCacheEntry) tea.Cmd {
	return func() tea.Msg {
		defer close(ch)
		img, err := clipboardimg.Read()
		if err == clipboardimg.ErrNoImage {
			if fallback == nil {
				return types.ImageUploadDoneMsg{StreamID: streamID, Err: err}
			}
			return imagePasteFallbackMsg{streamID: streamID, msg: fallback}
		}
		if err != nil {
			return types.ImageUploadDoneMsg{StreamID: streamID, Err: err}
		}
		cacheKey := imageCacheKey(img)
		if entry, ok := cache[cacheKey]; ok {
			return types.ImageUploadDoneMsg{StreamID: streamID, URL: entry.URL, CacheKey: cacheKey, ExpiresAt: entry.ExpiresAt}
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
		out, err := client.Upload(img, func(p syncblob.Progress) {
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
		return types.ImageUploadDoneMsg{StreamID: streamID, URL: url, CacheKey: cacheKey, ExpiresAt: out.ExpiresAt}
	}
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

func imageCacheKey(img *clipboardimg.Image) string {
	sum := sha256.Sum256(img.Data)
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
