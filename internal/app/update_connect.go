package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/eterm/eterm/internal/db"
	internalssh "github.com/eterm/eterm/internal/ssh"
	"github.com/eterm/eterm/internal/sftp"
	"github.com/eterm/eterm/internal/types"
	"github.com/eterm/eterm/internal/ui/components"
	"github.com/eterm/eterm/internal/ui/sftpview"
	"github.com/eterm/eterm/internal/ui/sshview"

	tea "charm.land/bubbletea/v2"
)

func (a App) applySSHConnect(msg types.SSHConnectMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	hostID := msg.HostID
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show("Connecting...", components.ToastInfo, 30*time.Second)
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
	appDebugf("SSHConnectMsg hostID=%d pty=%dx%d (from terminal size)", hostID, ptyCols, ptyRows)
	dial := func() tea.Msg {
		var host db.Host
		if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
			appDebugf("SSH connect aborted: load host: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
		}

		if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
			algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
			}
			return types.FingerprintConfirmMsg{
				HostID: hostID, Hostname: host.Hostname, Port: host.Port,
				Algorithm: algo, Fingerprint: fp, ConnType: "ssh",
			}
		}

		var jumpHost *db.Host
		var jumpKey *db.SSHKey
		if host.JumpHostID != nil {
			var jh db.Host
			if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
				jumpHost = &jh
				if jh.KeyID != nil {
					jumpKey = &jh.Key
				}
			}
		}

		var hostKey *db.SSHKey
		if host.KeyID != nil {
			hostKey = &host.Key
		}

		client, err := internalssh.Connect(internalssh.ConnectConfig{
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
		if err != nil {
			appDebugf("SSH dial failed: %v", err)
			database.Create(&db.ConnectionHistory{
				HostID: hostID, ConnectedAt: time.Now(), Status: "failed",
			})
			return types.ErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
		}
		appDebugf("SSH dial OK, opening session (PTY %dx%d)", ptyCols, ptyRows)

		now := time.Now()
		database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
		history := db.ConnectionHistory{
			HostID: hostID, ConnectedAt: now, Status: "success",
		}
		database.Create(&history)

		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, host.ForwardAgent)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			appDebugf("NewInteractiveSession failed: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("failed to start shell: %w", err)}
		}
		is.SetClosers(client.Closers)

		startPortForwards(database, client.Client, hostID, is)

		alias := hostDisplayName(host)
		initialCommands := initialSSHCommandsForHost(&host, "")
		return openSSHUITabMsg{is: is, alias: alias, hostID: hostID, historyID: history.ID, replaceTabAt: -1, initialCommands: initialCommands}
	}
	return a, tea.Batch(toastCmd, reflowWindow(a), dial)
}

func (a App) applySSHReconnect(msg types.SSHReconnectMsg) (App, tea.Cmd) {
	replaceAt := -1
	for i := range a.tabs {
		sm, ok := a.tabs[i].Model.(*sshview.Model)
		if !ok || sm.StreamID() != msg.StreamID {
			continue
		}
		if sm.HostID() != msg.HostID || msg.HostID == 0 || !sm.Disconnected() {
			return a, nil
		}
		replaceAt = i
		break
	}
	if replaceAt < 0 {
		return a, nil
	}
	database := a.db
	mk := a.masterKey
	hostID := msg.HostID
	idx := replaceAt
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show("Reconnecting...", components.ToastInfo, 30*time.Second)
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
	appDebugf("SSHReconnectMsg hostID=%d replaceTab=%d", hostID, idx)
	dial := func() tea.Msg {
		var host db.Host
		if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
			appDebugf("SSH reconnect aborted: load host: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
		}

		if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
			algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
			}
			return types.FingerprintConfirmMsg{
				HostID: hostID, Hostname: host.Hostname, Port: host.Port,
				Algorithm: algo, Fingerprint: fp, ConnType: "reconnect", StreamID: msg.StreamID,
			}
		}

		var jumpHost *db.Host
		var jumpKey *db.SSHKey
		if host.JumpHostID != nil {
			var jh db.Host
			if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
				jumpHost = &jh
				if jh.KeyID != nil {
					jumpKey = &jh.Key
				}
			}
		}

		var hostKey *db.SSHKey
		if host.KeyID != nil {
			hostKey = &host.Key
		}

		client, err := internalssh.Connect(internalssh.ConnectConfig{
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
		if err != nil {
			appDebugf("SSH reconnect dial failed: %v", err)
			database.Create(&db.ConnectionHistory{
				HostID: hostID, ConnectedAt: time.Now(), Status: "failed",
			})
			return types.ErrorMsg{Err: fmt.Errorf("SSH connection failed: %w", err)}
		}

		now := time.Now()
		database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
		history := db.ConnectionHistory{
			HostID: hostID, ConnectedAt: now, Status: "success",
		}
		database.Create(&history)

		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, host.ForwardAgent)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			appDebugf("NewInteractiveSession failed: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("failed to start shell: %w", err)}
		}
		is.SetClosers(client.Closers)

		startPortForwards(database, client.Client, hostID, is)

		alias := hostDisplayName(host)
		initialCommands := initialSSHCommandsForHost(&host, "")
		return openSSHUITabMsg{is: is, alias: alias, hostID: hostID, historyID: history.ID, replaceTabAt: idx, initialCommands: initialCommands}
	}
	return a, tea.Batch(toastCmd, reflowWindow(a), dial)
}

func (a App) applyOpenSSHUITab(msg openSSHUITabMsg) (App, tea.Cmd) {
	appDebugf("openSSHUITabMsg host=%q replaceTabAt=%d", msg.alias, msg.replaceTabAt)
	a.toast = a.toast.Dismiss()
	sv := sshview.New(msg.is, msg.alias, msg.hostID, BuildSSHKeys(a.kbConfig))
	sv.SetHistoryID(msg.historyID)
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(SSHTab))
	}
	tab := Tab{Type: SSHTab, Title: msg.alias, Model: sv}
	if msg.replaceTabAt >= 0 && msg.replaceTabAt < len(a.tabs) {
		if old, ok := a.tabs[msg.replaceTabAt].Model.(*sshview.Model); ok {
			finalizeSSHSession(a.db, old)
			_ = old.Close()
		}
		a.tabs[msg.replaceTabAt] = tab
		a.activeTab = msg.replaceTabAt
	} else {
		a.tabs = append(a.tabs, tab)
		a.activeTab = len(a.tabs) - 1
	}
	a.syncTabBar()
	for _, cmd := range msg.initialCommands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		sv.PasteCommand(cmd + "\n")
	}
	return a, tea.Batch(sv.Init(), reflowWindow(a))
}

func (a App) applySSHDisconnect(msg types.SSHDisconnectMsg) (App, tea.Cmd) {
	idx := -1
	if msg.StreamID != 0 {
		for i := range a.tabs {
			if a.tabs[i].Type != SSHTab {
				continue
			}
			if m, ok := a.tabs[i].Model.(*sshview.Model); ok && m.StreamID() == msg.StreamID {
				idx = i
				break
			}
		}
	}
	if idx < 0 && msg.Alias != "" {
		for i := range a.tabs {
			if a.tabs[i].Type == SSHTab && a.tabs[i].Title == msg.Alias {
				idx = i
				break
			}
		}
	}
	if idx >= 0 {
		if m, ok := a.tabs[idx].Model.(*sshview.Model); ok {
			finalizeSSHSession(a.db, m)
			_ = m.Close()
		}
		a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
		if a.activeTab > idx {
			a.activeTab--
		} else if a.activeTab >= len(a.tabs) {
			a.activeTab = len(a.tabs) - 1
		}
		if len(a.tabs) > 0 && a.activeTab < 0 {
			a.activeTab = 0
		}
		a.syncTabBar()
	}
	var tc tea.Cmd
	if msg.Err != nil {
		a.toast, tc = a.toast.Show(fmt.Sprintf("SSH session ended: %v", msg.Err), components.ToastWarning, 3*time.Second)
	} else {
		a.toast, tc = a.toast.Show("SSH session ended", components.ToastInfo, 2*time.Second)
	}
	return a, tea.Batch(tc, reflowWindow(a), func() tea.Msg { return types.RefreshListMsg{} })
}

func (a App) applySFTPOpen(msg types.SFTPOpenMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	hostID := msg.HostID
	appDebugf("SFTPOpenMsg hostID=%d", hostID)
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show("Opening SFTP...", components.ToastInfo, 60*time.Second)
	sftpAsync := func() tea.Msg {
		var host db.Host
		if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
			appDebugf("SFTP aborted: load host: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}
		}

		if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
			algo, fp, err := internalssh.ProbeHostKey(host.Hostname, host.Port, 10*time.Second)
			if err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
			}
			return types.FingerprintConfirmMsg{
				HostID: hostID, Hostname: host.Hostname, Port: host.Port,
				Algorithm: algo, Fingerprint: fp, ConnType: "sftp",
			}
		}

		var hostKey *db.SSHKey
		if host.KeyID != nil {
			hostKey = &host.Key
		}

		var jumpHost *db.Host
		var jumpKey *db.SSHKey
		if host.JumpHostID != nil {
			var jh db.Host
			if err := database.Preload("Key").First(&jh, *host.JumpHostID).Error; err == nil {
				jumpHost = &jh
				if jh.KeyID != nil {
					jumpKey = &jh.Key
				}
			}
		}

		client, err := internalssh.Connect(internalssh.ConnectConfig{
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
		if err != nil {
			appDebugf("SFTP SSH dial failed: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("SFTP connection failed: %w", err)}
		}
		appDebugf("SFTP SSH dial OK, creating SFTP layer")

		sftpClient, err := sftp.NewClient(client.Client)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			appDebugf("sftp.NewClient failed: %v", err)
			return types.ErrorMsg{Err: fmt.Errorf("SFTP client failed: %w", err)}
		}

		appDebugf("SFTP ready, opening tab for %q", hostDisplayName(host))
		return sftpOpenedMsg{client: sftpClient, hostAlias: hostDisplayName(host)}
	}
	return a, tea.Batch(toastCmd, reflowWindow(a), sftpAsync)
}

func (a App) applySftpOpened(msg sftpOpenedMsg) (App, tea.Cmd) {
	appDebugf("sftpOpenedMsg: new tab SFTP: %s", msg.hostAlias)
	a.toast = a.toast.Dismiss()
	sv := sftpview.New(msg.client, msg.hostAlias, BuildSFTPKeys(a.kbConfig))
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(SFTPTab))
	}
	tab := Tab{Type: SFTPTab, Title: msg.hostAlias, Model: sv}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, tea.Batch(sv.Init(), reflowWindow(a))
}

func initialSSHCommandsForHost(host *db.Host, extra string) []string {
	var cmds []string
	if host != nil && strings.TrimSpace(host.RemoteCommand) != "" {
		cmds = append(cmds, host.RemoteCommand)
	}
	if strings.TrimSpace(extra) != "" {
		cmds = append(cmds, extra)
	}
	return cmds
}
