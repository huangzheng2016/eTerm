package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"
)

func (a App) runSync() tea.Cmd {
	database := a.db
	mk := a.masterKey
	return func() tea.Msg {
		if mk.IsLocked() {
			return types.SyncResultMsg{}
		}
		cfg := esync.LoadConfig(database, mk)
		if !cfg.Enabled || cfg.Passphrase == "" {
			return types.SyncResultMsg{}
		}

		var tr esync.Transport
		var err error
		if cfg.Mode == "ssh" {
			if cfg.SSHHostID == 0 {
				return types.SyncResultMsg{Err: fmt.Errorf("no SSH host configured for sync")}
			}
			var tunnel *esync.Tunnel
			tunnel, err = esync.OpenTunnel(database, mk, cfg.SSHHostID, cfg.RemotePort)
			if err != nil {
				return types.SyncResultMsg{Err: err}
			}
			defer tunnel.Close()
			tr = esync.NewHTTPTransportWithOptions(tunnel.BaseURL(), cfg.APIKey, cfg.TenantID(), false)
		} else {
			tr = esync.NewHTTPTransportWithOptions(cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS)
		}
		if err != nil {
			return types.SyncResultMsg{Err: err}
		}
		defer tr.Close()

		records, newRev, err := tr.Pull(cfg.LastRev)
		if err != nil {
			return types.SyncResultMsg{Err: err}
		}
		mergeRes, err := esync.MergeRecords(database, mk, cfg.Passphrase, records)
		if err != nil {
			return types.SyncResultMsg{Err: fmt.Errorf("merge: %w", err)}
		}

		// Collect only records modified since last sync
		lastSyncAt := time.Time{}
		if ts, err := db.GetSetting(database, "sync_last_sync_at"); err == nil && ts != "" {
			lastSyncAt, _ = time.Parse(time.RFC3339, ts)
		}
		syncStartedAt := time.Now()
		dirty, err := esync.CollectDirty(database, mk, cfg.Passphrase, cfg.DeviceID, lastSyncAt)
		if err != nil {
			return types.SyncResultMsg{Pulled: mergeRes.Merged, Err: err}
		}
		if len(dirty) > 0 {
			if err := tr.Push(dirty); err != nil {
				return types.SyncResultMsg{Pulled: mergeRes.Merged, Err: err}
			}
		}

		db.SetSetting(database, "sync_last_rev", strconv.FormatInt(newRev, 10))
		db.SetSetting(database, "sync_last_sync_at", syncStartedAt.Format(time.RFC3339))
		return types.SyncResultMsg{Pulled: mergeRes.Merged, Pushed: len(dirty), Failed: mergeRes.Failed}
	}
}

func (a App) prepareSync(manual bool) (tea.Cmd, bool) {
	if a.masterKey.IsLocked() {
		if manual {
			return func() tea.Msg { return types.SyncResultMsg{Err: fmt.Errorf("sync unavailable while locked")} }, false
		}
		return nil, false
	}
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if !cfg.Enabled || cfg.Passphrase == "" {
		if manual {
			return func() tea.Msg { return types.SyncResultMsg{Err: fmt.Errorf("sync is disabled or missing passphrase")} }, false
		}
		return nil, false
	}
	return a.runSync(), true
}

func syncTickCmd(database *gorm.DB) tea.Cmd {
	interval, _ := db.GetSetting(database, "sync_interval")
	sec, _ := strconv.Atoi(interval)
	if sec <= 0 {
		sec = 300 // match LoadConfig default for empty/invalid sync_interval
	}
	enabled, _ := db.GetSetting(database, "sync_enabled")
	if enabled != "true" {
		return nil
	}
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg {
		return types.SyncTickMsg{}
	})
}

func (a App) closeCurrentTabIfAllowed() (App, tea.Cmd) {
	if len(a.tabs) > 1 && a.activeTab > 0 && !isListView(a.tabs[a.activeTab].Type) {
		closeCmd := closeTerminalTabCmd(a.db, a.tabs[a.activeTab])
		a.tabs = append(a.tabs[:a.activeTab], a.tabs[a.activeTab+1:]...)
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
		a.syncTabBar()
		a.persistTmuxRestoreSnapshot()
		return a, closeCmd
	}
	return a, nil
}

func closeTerminalTabCmd(gdb *gorm.DB, tab Tab) tea.Cmd {
	m, ok := tab.Model.(*sshview.Model)
	if ok && isTerminalTab(tab.Type) {
		return func() tea.Msg {
			finalizeSSHSession(gdb, m)
			_ = m.Close()
			return nil
		}
	}
	if closer, ok := tab.Model.(interface{ Close() error }); ok {
		return func() tea.Msg {
			_ = closer.Close()
			return nil
		}
	}
	return nil
}

func (a App) closeTabAt(idx int) (App, tea.Cmd) {
	if idx < 0 || idx >= len(a.tabs) || len(a.tabs) <= 1 {
		return a, nil
	}
	target := editorListType(a.tabs[idx].Type)
	closeCmd := closeTerminalTabCmd(a.db, a.tabs[idx])
	a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
	if a.activeTab >= len(a.tabs) {
		a.activeTab = len(a.tabs) - 1
	}
	a.syncTabBar()
	if target == "" {
		return a, closeCmd
	}
	a, listCmd := a.activateListView(target)
	return a, tea.Batch(closeCmd, listCmd)
}

func editorListType(tabType TabType) TabType {
	switch tabType {
	case EditorTab:
		return HomeTab
	case FwdEditorTab:
		return ForwardTab
	case SnippetEditorTab:
		return SnippetTab
	default:
		return ""
	}
}

func (a App) closeEditorTab(tabType TabType) (App, tea.Cmd) {
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) && a.tabs[a.activeTab].Type == tabType {
		return a.closeTabAt(a.activeTab)
	}
	for i := len(a.tabs) - 1; i >= 0; i-- {
		if a.tabs[i].Type == tabType {
			return a.closeTabAt(i)
		}
	}
	return a, nil
}

func (a App) lockSession() (App, tea.Cmd) {
	if a.noPasswordMode {
		return a, func() tea.Msg {
			return types.SuccessMsg{Message: "No-password mode stays unlocked"}
		}
	}
	a.masterKey.Lock()
	a.viewState = LoginView
	a.statusBar = a.statusBar.SetLocked(true)
	return a, func() tea.Msg { return types.MasterKeyLockedMsg{} }
}

func (a *App) syncTabBar() {
	a.tabBar = a.tabBar.SetItems(a.tabStripItems())
	if len(a.tabs) > 0 {
		if a.activeTab < 0 {
			a.activeTab = 0
		}
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
	}
	a.tabBar = a.tabBar.SetActive(a.activeTab)
	if a.width > 0 {
		a.tabBar = a.tabBar.SetWidth(a.width)
	}
}

func hostDisplayName(host db.Host) string {
	if host.Alias != "" {
		return host.Alias
	}
	if host.Username != "" {
		return fmt.Sprintf("%s@%s", host.Username, host.Hostname)
	}
	return host.Hostname
}

func autoLockTick() tea.Cmd {
	return tea.Tick(1*time.Minute, func(time.Time) tea.Msg {
		return types.AutoLockTickMsg{}
	})
}

func (a App) quitWithCheck() (tea.Model, tea.Cmd) {
	// Count active SSH sessions (SFTP doesn't need confirmation)
	var sshCount int
	for _, tab := range a.tabs {
		if isTerminalTab(tab.Type) {
			if m, ok := tab.Model.(*sshview.Model); ok && !m.Disconnected() {
				sshCount++
			}
		}
	}
	if sshCount == 0 {
		a = a.closeAllForwardSessions()
		return a, tea.Quit
	}
	msg := fmt.Sprintf("%d terminal session(s) still active. Quit anyway?", sshCount)
	a.pendingQuit = true
	a.confirm = components.NewConfirm("Quit eTerm", msg).Show()
	return a, nil
}

func (a *App) processConfirmResult() tea.Cmd {
	confirmed := a.confirm.Result()

	if a.pendingTmuxRestore != nil {
		entries := append([]tmuxRestoreEntry(nil), a.pendingTmuxRestore...)
		a.pendingTmuxRestore = nil
		a.clearTmuxRestoreFile()
		if confirmed {
			return a.restoreTmuxSessions(entries)
		}
		return nil
	}

	// Handle pending quit
	if a.pendingQuit {
		a.pendingQuit = false
		if confirmed {
			a.finalizeTerminalSessions()
			a.persistTmuxRestoreSnapshot()
			return tea.Quit
		}
		return nil
	}

	// Handle pending delete
	if a.pendingDeleteID > 0 {
		id := a.pendingDeleteID
		a.pendingDeleteID = 0
		if confirmed {
			return func() tea.Msg { return types.HostDeletedMsg{ID: id} }
		}
		return nil
	}

	// Handle pending snippet delete
	if a.pendingSnippetDeleteID > 0 {
		id := a.pendingSnippetDeleteID
		a.pendingSnippetDeleteID = 0
		if confirmed {
			database := a.db
			return func() tea.Msg {
				_ = db.DeleteSnippetForSync(database, id)
				return types.SnippetDeletedMsg{ID: id}
			}
		}
		return nil
	}

	// Handle pending forward rule delete
	if a.pendingFwdDeleteID > 0 {
		id := a.pendingFwdDeleteID
		a.pendingFwdDeleteID = 0
		if confirmed {
			database := a.db
			return func() tea.Msg {
				_ = db.DeletePortForwardForSync(database, id)
				return types.ForwardRuleDeletedMsg{ID: id}
			}
		}
		return nil
	}

	if a.pendingRemoteTmuxKill != nil {
		msg := *a.pendingRemoteTmuxKill
		a.pendingRemoteTmuxKill = nil
		if confirmed {
			return func() tea.Msg { return msg }
		}
		return nil
	}

	if a.pendingTmuxKill != nil {
		msg := *a.pendingTmuxKill
		a.pendingTmuxKill = nil
		if confirmed {
			return func() tea.Msg { return msg }
		}
		return nil
	}

	// Handle pending fingerprint
	if a.pendingFingerprint != nil {
		fp := a.pendingFingerprint
		a.pendingFingerprint = nil
		if confirmed {
			database := a.db
			return func() tea.Msg {
				rec := db.HostFingerprint{
					Hostname:    fp.Hostname,
					Port:        fp.Port,
					Algorithm:   fp.Algorithm,
					Fingerprint: fp.Fingerprint,
					TrustedAt:   time.Now(),
				}
				if fp.PreviousFingerprint != "" {
					_ = database.Model(&db.HostFingerprint{}).
						Where("hostname = ? AND port = ?", fp.Hostname, fp.Port).
						Updates(map[string]interface{}{
							"algorithm":   rec.Algorithm,
							"fingerprint": rec.Fingerprint,
							"trusted_at":  rec.TrustedAt,
						}).Error
				} else {
					_ = database.Create(&rec).Error
				}
				return types.FingerprintAcceptedMsg{
					HostID:        fp.HostID,
					ConnType:      fp.ConnType,
					StreamID:      fp.StreamID,
					ForwardRuleID: fp.ForwardRuleID,
				}
			}
		}
		if fp.ConnType == "quick" {
			a.pendingQuickConnect = nil
		}
		return func() tea.Msg {
			return types.SuccessMsg{Message: "Connection cancelled"}
		}
	}

	if len(a.pendingBatchOpenHosts) > 0 {
		hosts := append([]uint(nil), a.pendingBatchOpenHosts...)
		a.pendingBatchOpenHosts = nil
		if confirmed {
			cmds := make([]tea.Cmd, 0, len(hosts))
			for _, hostID := range hosts {
				cmds = append(cmds, a.batchConnectHostCmd(hostID, ""))
			}
			return tea.Batch(cmds...)
		}
		return nil
	}

	return nil
}

func (a *App) finalizeTerminalSessions() {
	for i := range a.tabs {
		if a.tabs[i].Type == SFTPTab {
			if closer, ok := a.tabs[i].Model.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
			continue
		}
		if !isTerminalTab(a.tabs[i].Type) {
			continue
		}
		m, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok {
			continue
		}
		finalizeSSHSession(a.db, m)
		_ = m.Close()
	}
}
