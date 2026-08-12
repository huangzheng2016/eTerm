package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/remote"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/syncshare"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

var (
	remoteOpenWithProgress            = remote.OpenWithProgress
	remoteOpenTmuxSessionWithProgress = remote.OpenTmuxSessionWithProgress
	remoteResumeOpenWithProgress      = remote.ResumeOpenWithProgress
	remoteResumeInfo                  = remote.ResumeInfo
	remoteListTmuxSessions            = remote.ListTmuxSessions
	remoteKillTmuxSession             = remote.KillTmuxSession
	remoteRenameTmuxSession           = remote.RenameTmuxSession
	syncshareCreate                   = syncshare.CreateShare
)

// syncHTTPBase returns the base URL for sync HTTP APIs. In SSH mode it opens
// a tunnel; the caller either closes it (short-lived calls) or attaches it to
// the session via AddCloser (long-lived relay sessions).
func (a App) syncHTTPBase(cfg esync.Config) (string, *esync.Tunnel, error) {
	if cfg.Mode != "ssh" {
		return cfg.ServerURL, nil, nil
	}
	if cfg.SSHHostID == 0 {
		return "", nil, fmt.Errorf("no SSH host configured for sync")
	}
	tunnel, err := esync.OpenTunnel(a.db, a.masterKey, cfg.SSHHostID, cfg.RemotePort)
	if err != nil {
		return "", nil, err
	}
	return tunnel.BaseURL(), tunnel, nil
}

func (a App) openRemoteShell(msg types.RemoteShellOpenMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)

	if msg.Tmux {
		peer := msg.Peer
		target, sessionID := msg.Target, msg.SessionID
		prefix := "Open remote tmux"
		var progress func(string)
		var progressCh chan string
		var progressCmd tea.Cmd
		a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "connect"))
		return a, tea.Batch(progressCmd, func() tea.Msg {
			defer close(progressCh)
			baseURL, tunnel, err := a.syncHTTPBase(cfg)
			if err != nil {
				return types.ConnErrorMsg{Err: err, Target: "[T]" + peer.Name}
			}
			is, newID, err := remoteOpenTmuxSessionWithProgress(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, target, sessionID, rows, cols, func(stage remote.OpenStage) {
				progress(connectStageText(prefix, string(stage)))
			})
			if err != nil {
				if tunnel != nil {
					tunnel.Close()
				}
				return types.ConnErrorMsg{Err: err, Target: "[T]" + peer.Name}
			}
			if tunnel != nil {
				is.AddCloser(tunnel)
			}
			reSessionID := sessionID
			if target == relay.TargetTmuxNew {
				reSessionID = newID
			}
			title := remoteTmuxTabTitle(peer.Name, reSessionID)
			spec := &types.RemoteReconnect{Peer: peer, Tmux: true, Target: relay.TargetTmuxAttach, SessionID: reSessionID}
			return remoteTerminalOpenedMsg{is: is, title: title, tabType: SSHTab, replaceTabAt: -1, reconnect: spec}
		})
	}

	title := "[R]" + msg.Peer.Name
	tabType := LocalTab
	if msg.Target == relay.TargetHost {
		title = "[R]" + msg.Peer.Name + "-" + msg.HostLabel
		tabType = SSHTab
	}
	peer := msg.Peer
	target, hostSyncID := msg.Target, msg.HostSyncID
	prefix := "Open relay shell"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "connect"))
	return a, tea.Batch(progressCmd, func() tea.Msg {
		defer close(progressCh)
		baseURL, tunnel, err := a.syncHTTPBase(cfg)
		if err != nil {
			return types.ConnErrorMsg{Err: err, Target: title}
		}
		is, err := remoteOpenWithProgress(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, target, hostSyncID, rows, cols, func(stage remote.OpenStage) {
			progress(connectStageText(prefix, string(stage)))
		})
		if err != nil {
			if tunnel != nil {
				tunnel.Close()
			}
			return types.ConnErrorMsg{Err: err, Target: title}
		}
		if tunnel != nil {
			is.AddCloser(tunnel)
		}
		spec := &types.RemoteReconnect{Peer: peer, Target: target, HostSyncID: hostSyncID}
		return remoteTerminalOpenedMsg{is: is, title: title, tabType: tabType, replaceTabAt: -1, reconnect: spec}
	})
}

func (a App) applyRemoteShellReconnect(msg types.RemoteShellReconnectMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	idx := -1
	for i := range a.tabs {
		if sm, ok := a.tabs[i].Model.(*sshview.Model); ok && sm.StreamID() == msg.StreamID && sm.Disconnected() {
			idx = i
			break
		}
	}
	if idx < 0 {
		return a, nil
	}
	if msg.Auto {
		if msg.Attempt <= 0 {
			msg.Attempt = 1
		}
		if msg.MaxAttempts <= 0 {
			msg.MaxAttempts = 3
		}
		if sm, ok := a.tabs[idx].Model.(*sshview.Model); ok {
			sm.SetReconnecting(msg.Attempt, msg.MaxAttempts)
		}
	}
	var relayStreamID uint32
	var resumeSeq uint64
	canResume := false
	if sm, ok := a.tabs[idx].Model.(*sshview.Model); ok {
		relayStreamID, resumeSeq, canResume = remoteResumeInfo(sm.Session())
	}
	title := a.tabs[idx].Title
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)
	spec := msg.Spec
	streamID := msg.StreamID
	attempt := msg.Attempt
	maxAttempts := msg.MaxAttempts
	prefix := "Remote reconnect"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "connect"))
	return a, tea.Batch(progressCmd, func() tea.Msg {
		defer close(progressCh)
		baseURL, tunnel, err := a.syncHTTPBase(cfg)
		var is *internalssh.InteractiveSession
		resumed := false
		if err == nil {
			if canResume {
				// Try to resume the relay stream first: the daemon replays
				// retained output from resumeSeq and the tab keeps scrollback.
				op := relay.OpenRequest{PeerID: spec.Peer.ID, Target: spec.Target, HostSyncID: spec.HostSyncID, SessionID: spec.SessionID, Rows: rows, Cols: cols}
				ris, rerr := remoteResumeOpenWithProgress(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, op, relayStreamID, resumeSeq, func(stage remote.OpenStage) {
					progress(connectStageText(prefix, string(stage)))
				})
				if rerr == nil {
					is, resumed = ris, true
				}
			}
			if !resumed {
				if spec.Tmux {
					is, _, err = remoteOpenTmuxSessionWithProgress(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, spec.Peer.ID, spec.Target, spec.SessionID, rows, cols, func(stage remote.OpenStage) {
						progress(connectStageText(prefix, string(stage)))
					})
				} else {
					is, err = remoteOpenWithProgress(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, spec.Peer.ID, spec.Target, spec.HostSyncID, rows, cols, func(stage remote.OpenStage) {
						progress(connectStageText(prefix, string(stage)))
					})
				}
			}
		}
		if err != nil {
			if tunnel != nil {
				tunnel.Close()
			}
			if msg.Auto && attempt < maxAttempts {
				return types.RemoteShellReconnectMsg{StreamID: streamID, Spec: spec, Auto: true, Attempt: attempt + 1, MaxAttempts: maxAttempts}
			}
			return types.ConnErrorMsg{Err: err, Target: title, Retry: types.RemoteShellReconnectMsg{StreamID: streamID, Spec: spec}}
		}
		if tunnel != nil {
			is.AddCloser(tunnel)
		}
		specCopy := spec
		tabType := SSHTab
		if spec.Target == relay.TargetLocal {
			tabType = LocalTab
		}
		return remoteTerminalOpenedMsg{is: is, title: title, tabType: tabType, replaceTabAt: idx, reconnect: &specCopy, background: msg.Auto, resume: resumed}
	})
}

type remoteShareLinkMsg struct {
	url       string
	expiresAt time.Time
	label     string
	err       error
}

func (a App) shareDefaultMaxHours() int {
	if v, err := db.GetSetting(a.db, "share_max_hours"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 && n <= 168 {
			return n
		}
	}
	return 4
}

func (a App) shareRemoteShell(msg types.RemoteShareSubmitMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	label := msg.Label
	if label == "" {
		label = msg.Peer.Name
	}
	if cfg.ServerURL == "" || cfg.APIKey == "" {
		return a, func() tea.Msg {
			return remoteShareLinkMsg{label: label, err: fmt.Errorf("sync server not configured")}
		}
	}
	maxHours := msg.MaxHours
	if maxHours < 1 || maxHours > 168 {
		maxHours = 4
	}
	name := msg.Name
	if name == "" {
		name = msg.Peer.Name
	}
	peer := msg.Peer
	target, sessionID := msg.Target, msg.SessionID
	return a, func() tea.Msg {
		url, expiresAt, err := syncshareCreate(context.Background(), cfg, peer.ID, name, maxHours, target, sessionID)
		return remoteShareLinkMsg{url: url, expiresAt: expiresAt, label: label, err: err}
	}
}

func (a App) loadRemoteTmuxSessions(peer types.RemotePeer) tea.Cmd {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	return func() tea.Msg {
		baseURL, tunnel, err := a.syncHTTPBase(cfg)
		if err != nil {
			return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Err: err}
		}
		if tunnel != nil {
			defer tunnel.Close()
		}
		sessions, err := remoteListTmuxSessions(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
		return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Sessions: sessions, Err: err}
	}
}

func (a App) killRemoteTmuxSession(msg types.RemoteTmuxKillMsg) tea.Cmd {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	peer := msg.Peer
	sessionID := msg.SessionID
	return func() tea.Msg {
		baseURL, tunnel, err := a.syncHTTPBase(cfg)
		if err != nil {
			return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Err: err}
		}
		if tunnel != nil {
			defer tunnel.Close()
		}
		if err := remoteKillTmuxSession(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, sessionID); err != nil {
			return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Err: err}
		}
		sessions, err := remoteListTmuxSessions(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
		return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Sessions: sessions, Err: err}
	}
}

func (a App) renameRemoteTmuxSession(msg types.RemoteTmuxRenameMsg) (App, tea.Cmd) {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		return a, nil
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	peer := msg.Peer
	sessionID := msg.SessionID
	return a, func() tea.Msg {
		baseURL, tunnel, err := a.syncHTTPBase(cfg)
		if err != nil {
			return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Err: err}
		}
		if tunnel != nil {
			defer tunnel.Close()
		}
		if err := remoteRenameTmuxSession(context.Background(), baseURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, sessionID, name); err != nil {
			return types.RemoteTmuxSessionsLoadedMsg{Peer: peer, Err: err}
		}
		return remoteTmuxRenameAppliedMsg{Peer: peer, OldSessionID: sessionID, Name: name}
	}
}

func (a *App) renameRemoteTmuxTabs(peerID, sessionID, name string) {
	for i := range a.tabs {
		sm, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok {
			continue
		}
		spec := sm.RemoteReconnect()
		if spec == nil || !spec.Tmux || spec.Peer.ID != peerID || spec.SessionID != sessionID {
			continue
		}
		spec.SessionID = name
		spec.Target = relay.TargetTmuxAttach
		sm.SetRemoteReconnect(spec)
		a.tabs[i].Title = remoteTmuxTabTitle(spec.Peer.Name, name)
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
}

func remoteTmuxTabTitle(peerName, sessionID string) string {
	return "[T]" + peerName + "-" + sessionID
}

func (a App) applyRemoteTerminalOpened(msg remoteTerminalOpenedMsg) (App, tea.Cmd) {
	a = a.stopConnectProgress()
	if msg.resume && msg.replaceTabAt >= 0 && msg.replaceTabAt < len(a.tabs) {
		if old, ok := a.tabs[msg.replaceTabAt].Model.(*sshview.Model); ok {
			cmd := old.ResumeSession(msg.is)
			if !msg.background {
				a.activeTab = msg.replaceTabAt
			}
			a.syncTabBar()
			a.persistTmuxRestoreSnapshot()
			return a, tea.Batch(cmd, reflowWindow(a))
		}
	}
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	source := "remote"
	if msg.reconnect != nil && msg.reconnect.Tmux {
		source = "remote-tmux"
	}
	sv.SetHistoryID(createLocalSessionHistory(a.db, msg.title, source))
	configureSessionCapture(a.db, sv)
	if msg.reconnect != nil {
		sv.SetRemoteReconnect(msg.reconnect)
	}
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(msg.tabType))
	}
	tab := Tab{Type: msg.tabType, Title: msg.title, Model: sv}
	if msg.replaceTabAt >= 0 && msg.replaceTabAt < len(a.tabs) {
		if old, ok := a.tabs[msg.replaceTabAt].Model.(*sshview.Model); ok {
			finalizeSSHSession(a.db, old)
			_ = old.Close()
		}
		a.tabs[msg.replaceTabAt] = tab
		if !msg.background {
			a.activeTab = msg.replaceTabAt
		}
	} else {
		a.tabs = append(a.tabs, tab)
		a.activeTab = len(a.tabs) - 1
	}
	a.syncTabBar()
	a.persistTmuxRestoreSnapshot()
	return a, tea.Batch(sv.Init(), reflowWindow(a))
}
