package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/relay"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

const (
	tmuxRestoreLocal  = "local"
	tmuxRestoreRemote = "remote"
)

var appAttachTmuxSession = tmux.AttachSession

type tmuxRestoreEntry struct {
	Kind     string `json:"kind"`
	Session  string `json:"session"`
	Title    string `json:"title,omitempty"`
	PeerID   string `json:"peer_id,omitempty"`
	PeerName string `json:"peer_name,omitempty"`
}

type tmuxRestoreFile struct {
	Version int                `json:"version"`
	Tabs    []tmuxRestoreEntry `json:"tabs"`
}

type tmuxRestoreOpenedMsg struct {
	opened []tmuxRestoreOpened
}

type tmuxRestoreOpened struct {
	entry tmuxRestoreEntry
	is    *internalssh.InteractiveSession
	err   error
}

func defaultTmuxRestorePath() string {
	return filepath.Join(config.ConfigDir(), "tmux_restore.json")
}

func readTmuxRestoreFile(path string) ([]tmuxRestoreEntry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f tmuxRestoreFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return validTmuxRestoreEntries(f.Tabs), nil
}

func writeTmuxRestoreFile(path string, entries []tmuxRestoreEntry) error {
	entries = validTmuxRestoreEntries(entries)
	if len(entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(tmuxRestoreFile{Version: 1, Tabs: entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func validTmuxRestoreEntries(entries []tmuxRestoreEntry) []tmuxRestoreEntry {
	out := make([]tmuxRestoreEntry, 0, len(entries))
	for _, entry := range entries {
		switch entry.Kind {
		case tmuxRestoreLocal:
			if entry.Session == "" {
				continue
			}
		case tmuxRestoreRemote:
			if entry.Session == "" || entry.PeerID == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (a App) tmuxRestoreEntries() []tmuxRestoreEntry {
	entries := make([]tmuxRestoreEntry, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab.Type == LocalTab && tab.TmuxSession != "" {
			entries = append(entries, tmuxRestoreEntry{
				Kind:    tmuxRestoreLocal,
				Session: tab.TmuxSession,
				Title:   tab.Title,
			})
			continue
		}
		m, ok := tab.Model.(*sshview.Model)
		if !ok {
			continue
		}
		spec := m.RemoteReconnect()
		if spec == nil || !spec.Tmux || spec.SessionID == "" || spec.Peer.ID == "" {
			continue
		}
		entries = append(entries, tmuxRestoreEntry{
			Kind:     tmuxRestoreRemote,
			Session:  spec.SessionID,
			Title:    tab.Title,
			PeerID:   spec.Peer.ID,
			PeerName: spec.Peer.Name,
		})
	}
	return entries
}

func (a App) persistTmuxRestoreSnapshot() {
	path := a.tmuxRestorePath
	if path == "" {
		return
	}
	_ = writeTmuxRestoreFile(path, a.tmuxRestoreEntries())
}

func (a *App) promptTmuxRestoreIfAvailable() {
	path := a.tmuxRestorePath
	if path == "" {
		path = defaultTmuxRestorePath()
		a.tmuxRestorePath = path
	}
	entries, err := readTmuxRestoreFile(path)
	if err != nil || len(entries) == 0 {
		return
	}
	a.pendingTmuxRestore = entries
	a.confirm = components.NewConfirm("Restore tmux sessions", restorePromptMessage(len(entries))).Show()
}

func restorePromptMessage(n int) string {
	if n == 1 {
		return "Restore 1 tmux session?"
	}
	return "Restore " + strconv.Itoa(n) + " tmux sessions?"
}

func (a App) clearTmuxRestoreFile() {
	path := a.tmuxRestorePath
	if path == "" {
		path = defaultTmuxRestorePath()
	}
	_ = writeTmuxRestoreFile(path, nil)
}

func (a App) restoreTmuxSessions(entries []tmuxRestoreEntry) tea.Cmd {
	entries = validTmuxRestoreEntries(entries)
	if len(entries) == 0 {
		return nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)
	return func() tea.Msg {
		opened := make([]tmuxRestoreOpened, 0, len(entries))
		for _, entry := range entries {
			switch entry.Kind {
			case tmuxRestoreLocal:
				is, err := appAttachTmuxSession(context.Background(), entry.Session, rows, cols)
				opened = append(opened, tmuxRestoreOpened{entry: entry, is: is, err: err})
			case tmuxRestoreRemote:
				is, _, err := remoteOpenTmuxSessionWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, entry.PeerID, relay.TargetTmuxAttach, entry.Session, rows, cols, nil)
				opened = append(opened, tmuxRestoreOpened{entry: entry, is: is, err: err})
			}
		}
		return tmuxRestoreOpenedMsg{opened: opened}
	}
}

func (a App) applyTmuxRestoreOpened(msg tmuxRestoreOpenedMsg) (App, tea.Cmd) {
	var cmds []tea.Cmd
	for _, item := range msg.opened {
		if item.err != nil || item.is == nil {
			continue
		}
		switch item.entry.Kind {
		case tmuxRestoreLocal:
			title := item.entry.Title
			if title == "" {
				title = tmuxTabTitle(item.entry.Session)
			}
			sv := sshview.New(item.is, title, 0, BuildSSHKeys(a.kbConfig))
			sv.SetHistoryID(createLocalSessionHistory(a.db, title, "tmux"))
			if a.width > 0 {
				sv.SetSize(a.width, a.mainContentHeightForType(LocalTab))
			}
			a.tabs = append(a.tabs, Tab{Type: LocalTab, Title: title, Model: sv, TmuxSession: item.entry.Session})
			cmds = append(cmds, sv.Init())
		case tmuxRestoreRemote:
			peer := types.RemotePeer{ID: item.entry.PeerID, Name: item.entry.PeerName}
			spec := &types.RemoteReconnect{Peer: peer, Target: relay.TargetTmuxAttach, Tmux: true, SessionID: item.entry.Session}
			title := item.entry.Title
			if title == "" {
				title = remoteTmuxTabTitle(peer.Name, item.entry.Session)
			}
			sv := sshview.New(item.is, title, 0, BuildSSHKeys(a.kbConfig))
			sv.SetHistoryID(createLocalSessionHistory(a.db, title, "remote-tmux"))
			sv.SetRemoteReconnect(spec)
			if a.width > 0 {
				sv.SetSize(a.width, a.mainContentHeightForType(SSHTab))
			}
			a.tabs = append(a.tabs, Tab{Type: SSHTab, Title: title, Model: sv})
			cmds = append(cmds, sv.Init())
		}
	}
	if len(a.tabs) > 0 {
		a.activeTab = len(a.tabs) - 1
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
	return a, tea.Batch(cmds...)
}
