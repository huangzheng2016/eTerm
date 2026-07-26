package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	id    uint64
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

func (a *App) restoreTmuxSessions(entries []tmuxRestoreEntry) tea.Cmd {
	entries = validTmuxRestoreEntries(entries)
	if len(entries) == 0 {
		return nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	var cmds []tea.Cmd
	for _, entry := range entries {
		a.tmuxRestoreSeq++
		id := a.tmuxRestoreSeq
		title := entry.Title
		tabType := LocalTab
		if entry.Kind == tmuxRestoreLocal {
			if title == "" {
				title = tmuxTabTitle(entry.Session)
			}
		} else {
			tabType = SSHTab
			if title == "" {
				title = remoteTmuxTabTitle(entry.PeerName, entry.Session)
			}
		}
		sv := sshview.New(nil, title, 0, BuildSSHKeys(a.kbConfig))
		sv.SetReconnecting(1, 1)
		if entry.Kind == tmuxRestoreRemote {
			sv.SetRemoteReconnect(&types.RemoteReconnect{
				Peer:      types.RemotePeer{ID: entry.PeerID, Name: entry.PeerName},
				Target:    relay.TargetTmuxAttach,
				Tmux:      true,
				SessionID: entry.Session,
			})
		}
		if a.width > 0 {
			sv.SetSize(a.width, a.mainContentHeightForType(tabType))
		}
		a.tabs = append(a.tabs, Tab{Type: tabType, Title: title, Model: sv, TmuxSession: localTmuxSession(entry), tmuxRestoreID: id})

		entry := entry
		cols, rows := ptyFromAppSizeForTab(*a, tabType)
		cmds = append(cmds, func() tea.Msg {
			switch entry.Kind {
			case tmuxRestoreLocal:
				configFile, err := a.resolveTmuxConfig()
				if err != nil {
					return tmuxRestoreOpenedMsg{id: id, entry: entry, err: err}
				}
				is, err := appAttachTmuxSession(context.Background(), configFile, entry.Session, rows, cols)
				return tmuxRestoreOpenedMsg{id: id, entry: entry, is: is, err: err}
			case tmuxRestoreRemote:
				is, _, err := remoteOpenTmuxSessionWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, entry.PeerID, relay.TargetTmuxAttach, entry.Session, rows, cols, nil)
				return tmuxRestoreOpenedMsg{id: id, entry: entry, is: is, err: err}
			}
			return tmuxRestoreOpenedMsg{id: id, entry: entry, err: errors.New("invalid tmux restore entry")}
		})
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
	return tea.Batch(cmds...)
}

func (a App) applyTmuxRestoreOpened(msg tmuxRestoreOpenedMsg) (App, tea.Cmd) {
	idx := -1
	for i := range a.tabs {
		if a.tabs[i].tmuxRestoreID == msg.id {
			idx = i
			break
		}
	}
	if idx < 0 {
		if msg.is != nil {
			_ = msg.is.Close()
		}
		return a, nil
	}
	placeholder, _ := a.tabs[idx].Model.(*sshview.Model)
	if msg.err != nil || msg.is == nil {
		if isMissingTmuxSession(msg.err) {
			if placeholder != nil {
				_ = placeholder.Close()
			}
			a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
			if idx < a.activeTab || a.activeTab >= len(a.tabs) {
				a.activeTab = max(0, a.activeTab-1)
			}
			a.syncTabBar()
			a.persistTmuxRestoreSnapshot()
			return a, nil
		}
		if placeholder != nil {
			placeholder.SetReconnecting(0, 0)
			placeholder.SetDisconnected(msg.err)
		}
		return a, nil
	}
	if placeholder != nil {
		_ = placeholder.Close()
	}
	var sv *sshview.Model
	switch msg.entry.Kind {
	case tmuxRestoreLocal:
		title := msg.entry.Title
		if title == "" {
			title = tmuxTabTitle(msg.entry.Session)
		}
		sv = sshview.New(msg.is, title, 0, BuildSSHKeys(a.kbConfig))
		sv.SetHistoryID(createLocalSessionHistory(a.db, title, "tmux"))
		configureSessionCapture(a.db, sv)
		if a.width > 0 {
			sv.SetSize(a.width, a.mainContentHeightForType(LocalTab))
		}
		a.tabs[idx] = Tab{Type: LocalTab, Title: title, Model: sv, TmuxSession: msg.entry.Session}
	case tmuxRestoreRemote:
		peer := types.RemotePeer{ID: msg.entry.PeerID, Name: msg.entry.PeerName}
		spec := &types.RemoteReconnect{Peer: peer, Target: relay.TargetTmuxAttach, Tmux: true, SessionID: msg.entry.Session}
		title := msg.entry.Title
		if title == "" {
			title = remoteTmuxTabTitle(peer.Name, msg.entry.Session)
		}
		sv = sshview.New(msg.is, title, 0, BuildSSHKeys(a.kbConfig))
		sv.SetHistoryID(createLocalSessionHistory(a.db, title, "remote-tmux"))
		configureSessionCapture(a.db, sv)
		sv.SetRemoteReconnect(spec)
		if a.width > 0 {
			sv.SetSize(a.width, a.mainContentHeightForType(SSHTab))
		}
		a.tabs[idx] = Tab{Type: SSHTab, Title: title, Model: sv}
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
	return a, sv.Init()
}

func localTmuxSession(entry tmuxRestoreEntry) string {
	if entry.Kind == tmuxRestoreLocal {
		return entry.Session
	}
	return ""
}

func isMissingTmuxSession(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "can't find session") ||
		strings.Contains(s, "no server running") ||
		strings.Contains(s, "no sessions") ||
		(strings.Contains(s, "error connecting to") && strings.Contains(s, "no such file or directory"))
}
