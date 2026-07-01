package app

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/remote"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

func (a App) openRemoteShell(msg types.RemoteShellOpenMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("remote shell requires HTTP sync mode")} }
	}
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)

	if msg.Active {
		peer := msg.Peer
		target, shellID := msg.Target, msg.ShellID
		prefix := "Open active shell"
		var progress func(string)
		var progressCh chan string
		var progressCmd tea.Cmd
		a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "connect"))
		return a, tea.Batch(progressCmd, func() tea.Msg {
			defer close(progressCh)
			is, newID, err := remote.OpenActiveShellWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, target, shellID, rows, cols, func(stage remote.OpenStage) {
				progress(connectStageText(prefix, string(stage)))
			})
			if err != nil {
				return types.ConnErrorMsg{Err: err, Target: "[A]" + peer.Name}
			}
			reShellID := shellID
			if target == relay.TargetActiveNew {
				reShellID = newID
			}
			title := activeShellTabTitle(peer.Name, reShellID, msg.HostLabel)
			spec := &types.RemoteReconnect{Peer: peer, Active: true, Target: relay.TargetActiveAttach, ShellID: reShellID}
			return remoteTerminalOpenedMsg{is: is, title: title, tabType: SSHTab, replaceTabAt: -1, reconnect: spec}
		})
	}

	title := "[R]" + msg.Peer.Name
	tabType := LocalTab
	if msg.Target == "host" {
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
		is, err := remote.OpenWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, target, hostSyncID, rows, cols, func(stage remote.OpenStage) {
			progress(connectStageText(prefix, string(stage)))
		})
		if err != nil {
			return types.ConnErrorMsg{Err: err, Target: title}
		}
		spec := &types.RemoteReconnect{Peer: peer, Target: target, HostSyncID: hostSyncID}
		return remoteTerminalOpenedMsg{is: is, title: title, tabType: tabType, replaceTabAt: -1, reconnect: spec}
	})
}

func (a App) applyRemoteShellReconnect(msg types.RemoteShellReconnectMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return a, nil
	}
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
	title := a.tabs[idx].Title
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)
	spec := msg.Spec
	streamID := msg.StreamID
	prefix := "Remote reconnect"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "connect"))
	return a, tea.Batch(progressCmd, func() tea.Msg {
		defer close(progressCh)
		var is *internalssh.InteractiveSession
		var err error
		if spec.Active {
			is, _, err = remote.OpenActiveShellWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, spec.Peer.ID, spec.Target, spec.ShellID, rows, cols, func(stage remote.OpenStage) {
				progress(connectStageText(prefix, string(stage)))
			})
		} else {
			is, err = remote.OpenWithProgress(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, spec.Peer.ID, spec.Target, spec.HostSyncID, rows, cols, func(stage remote.OpenStage) {
				progress(connectStageText(prefix, string(stage)))
			})
		}
		if err != nil {
			return types.ConnErrorMsg{Err: err, Target: title, Retry: types.RemoteShellReconnectMsg{StreamID: streamID, Spec: spec}}
		}
		specCopy := spec
		return remoteTerminalOpenedMsg{is: is, title: title, tabType: SSHTab, replaceTabAt: idx, reconnect: &specCopy}
	})
}

func (a App) loadActiveShells(peer types.RemotePeer) tea.Cmd {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return nil
	}
	return func() tea.Msg {
		shells, err := remote.ListActiveShells(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
		return types.RemoteActiveShellsLoadedMsg{Peer: peer, Shells: shells, Err: err}
	}
}

func (a App) killActiveShell(msg types.RemoteShellKillMsg) tea.Cmd {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return nil
	}
	peer := msg.Peer
	shellID := msg.ShellID
	return func() tea.Msg {
		_ = remote.KillActiveShell(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, shellID)
		shells, err := remote.ListActiveShells(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
		return types.RemoteActiveShellsLoadedMsg{Peer: peer, Shells: shells, Err: err}
	}
}

func (a App) renameActiveShell(msg types.RemoteShellRenameMsg) (App, tea.Cmd) {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		return a, nil
	}
	a.renameActiveShellTabs(msg.Peer.ID, msg.ShellID, name)
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return a, nil
	}
	peer := msg.Peer
	shellID := msg.ShellID
	return a, func() tea.Msg {
		if err := remote.RenameActiveShell(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID, shellID, name); err != nil {
			return types.ErrorMsg{Err: err}
		}
		shells, err := remote.ListActiveShells(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, peer.ID)
		return types.RemoteActiveShellsLoadedMsg{Peer: peer, Shells: shells, Err: err}
	}
}

func (a *App) renameActiveShellTabs(peerID, shellID, name string) {
	for i := range a.tabs {
		sm, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok {
			continue
		}
		spec := sm.RemoteReconnect()
		if spec == nil || !spec.Active || spec.Peer.ID != peerID || spec.ShellID != shellID {
			continue
		}
		a.tabs[i].Title = activeShellTabTitle(spec.Peer.Name, shellID, name)
	}
	a.syncTabBar()
}

func activeShellTabTitle(peerName, shellID, label string) string {
	suffix := strings.TrimSpace(label)
	if suffix == "" {
		suffix = defaultActiveShellName(shellID)
	}
	if suffix == "" {
		suffix = shellID
	}
	return "[A]" + peerName + "-" + suffix
}

func defaultActiveShellName(shellID string) string {
	if shellID == "" {
		return ""
	}
	return "active-" + shellID
}

func (a App) applyRemoteTerminalOpened(msg remoteTerminalOpenedMsg) (App, tea.Cmd) {
	a = a.stopConnectProgress()
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	if msg.reconnect != nil {
		sv.SetRemoteReconnect(msg.reconnect)
	}
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(msg.tabType))
	}
	tab := Tab{Type: msg.tabType, Title: msg.title, Model: sv}
	if msg.replaceTabAt >= 0 && msg.replaceTabAt < len(a.tabs) {
		if old, ok := a.tabs[msg.replaceTabAt].Model.(*sshview.Model); ok {
			_ = old.Close()
		}
		a.tabs[msg.replaceTabAt] = tab
		a.activeTab = msg.replaceTabAt
	} else {
		a.tabs = append(a.tabs, tab)
		a.activeTab = len(a.tabs) - 1
	}
	a.syncTabBar()
	return a, tea.Batch(sv.Init(), reflowWindow(a))
}
