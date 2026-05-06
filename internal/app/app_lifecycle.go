package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
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
			return nil
		}
		cfg := esync.LoadConfig(database, mk)
		if !cfg.Enabled || cfg.Passphrase == "" {
			return nil
		}

		var tr esync.Transport
		var err error
		switch cfg.Mode {
		case "ssh":
			if cfg.SSHHostID == 0 {
				return types.SyncResultMsg{Err: fmt.Errorf("no SSH host configured for sync")}
			}
			var host db.Host
			if e := database.Preload("Key").First(&host, cfg.SSHHostID).Error; e != nil {
				return types.SyncResultMsg{Err: fmt.Errorf("load sync host: %w", e)}
			}
			var hostKey *db.SSHKey
			if host.KeyID != nil {
				hostKey = &host.Key
			}
			var jumpHost *db.Host
			var jumpKey *db.SSHKey
			if host.JumpHostID != nil {
				var jh db.Host
				if database.Preload("Key").First(&jh, *host.JumpHostID).Error == nil {
					jumpHost = &jh
					if jh.KeyID != nil {
						jumpKey = &jh.Key
					}
				}
			}
			result, cerr := internalssh.Connect(internalssh.ConnectConfig{
				Host:      &host,
				Key:       hostKey,
				JumpHost:  jumpHost,
				JumpKey:   jumpKey,
				MasterKey: mk,
				DB:        database,
				FingerprintCallback: func(hostname string, port int, algorithm string, fingerprint string) bool {
					return true
				},
			})
			if cerr != nil {
				return types.SyncResultMsg{Err: fmt.Errorf("ssh connect: %w", cerr)}
			}
			tr, err = esync.NewSSHTransport(result.Client, result.Closers, cfg.RemoteBin, cfg.RemoteDB)
		default:
			tr = esync.NewHTTPTransport(cfg.ServerURL, cfg.APIKey)
		}
		if err != nil {
			return types.SyncResultMsg{Err: err}
		}
		defer tr.Close()

		records, newRev, err := tr.Pull(cfg.LastRev)
		if err != nil {
			return types.SyncResultMsg{Err: err}
		}
		mergeRes := esync.MergeRecords(database, mk, cfg.Passphrase, records)

		// Collect only records modified since last sync
		lastSyncAt := time.Time{}
		if ts, err := db.GetSetting(database, "sync_last_sync_at"); err == nil && ts != "" {
			lastSyncAt, _ = time.Parse(time.RFC3339, ts)
		}
		dirty, err := esync.CollectDirty(database, mk, cfg.Passphrase, cfg.DeviceID, lastSyncAt)
		if err != nil {
			return types.SyncResultMsg{Pulled: mergeRes.Merged, Err: err}
		}
		if len(dirty) > 0 {
			newRev, err = tr.Push(dirty)
			if err != nil {
				return types.SyncResultMsg{Pulled: mergeRes.Merged, Err: err}
			}
		}

		db.SetSetting(database, "sync_last_rev", strconv.FormatInt(newRev, 10))
		db.SetSetting(database, "sync_last_sync_at", time.Now().Format(time.RFC3339))
		return types.SyncResultMsg{Pulled: mergeRes.Merged, Pushed: len(dirty), Failed: mergeRes.Failed}
	}
}

func syncTickCmd(database *gorm.DB) tea.Cmd {
	interval, _ := db.GetSetting(database, "sync_interval")
	sec, _ := strconv.Atoi(interval)
	if sec <= 0 {
		return nil
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
	if len(a.tabs) > 1 && a.activeTab > 0 {
		if m, ok := a.tabs[a.activeTab].Model.(*sshview.Model); ok {
			finalizeSSHSession(a.db, m)
			_ = m.Close()
		}
		a.tabs = append(a.tabs[:a.activeTab], a.tabs[a.activeTab+1:]...)
		if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
		a.syncTabBar()
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
	items := make([]components.TabItem, len(a.tabs))
	for i, tab := range a.tabs {
		title := tab.Title
		switch tab.Type {
		case SSHTab:
			title = fmt.Sprintf("[S] %s", tab.Title)
		case SFTPTab:
			title = fmt.Sprintf("[F] %s", tab.Title)
		case ForwardTab:
			title = fmt.Sprintf("[P] %s", tab.Title)
		case SnippetTab:
			title = fmt.Sprintf("[B] %s", tab.Title)
		case SessionHistoryTab:
			title = fmt.Sprintf("[L] %s", tab.Title)
		}
		if i < 9 {
			title = fmt.Sprintf("%d:%s", i+1, title)
		}
		items[i] = components.TabItem{Title: title, ID: string(tab.Type)}
	}
	a.tabBar = components.NewTabs(items)
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
		if tab.Type == SSHTab {
			if m, ok := tab.Model.(*sshview.Model); ok && !m.Disconnected() {
				sshCount++
			}
		}
	}
	if sshCount == 0 {
		a = a.closeAllForwardSessions()
		return a, tea.Quit
	}
	msg := fmt.Sprintf("%d SSH session(s) still active. Quit anyway?", sshCount)
	a.pendingQuit = true
	a.confirm = components.NewConfirm("Quit eTerm", msg).Show()
	return a, nil
}

func (a *App) processConfirmResult() tea.Cmd {
	confirmed := a.confirm.Result()

	// Handle pending quit
	if a.pendingQuit {
		a.pendingQuit = false
		if confirmed {
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
				_ = database.Delete(&db.Snippet{}, id).Error
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
				_ = database.Delete(&db.PortForward{}, id).Error
				return types.ForwardRuleDeletedMsg{ID: id}
			}
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
