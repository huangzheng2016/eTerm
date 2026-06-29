package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
)

// CLIConnectInfo holds parsed CLI direct-connect arguments.
type CLIConnectInfo struct {
	Hostname string
	Port     int
	Username string
}

func (a App) SetPendingCLIConnect(hostname, username string, port int) App {
	a.pendingCLIConnect = &CLIConnectInfo{
		Hostname: hostname,
		Port:     port,
		Username: username,
	}
	return a
}

func (a App) handleCLIConnect(msg types.CLIConnectMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	prefix := "SSH connect"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "load"))
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)

	dial := func() tea.Msg {
		defer close(progressCh)
		// Try to find existing host by hostname + port
		var host db.Host
		err := database.Where("hostname = ? AND port = ?", msg.Hostname, msg.Port).First(&host).Error
		if err != nil {
			// Not found — create new host
			host = db.Host{
				Alias:      fmt.Sprintf("%s@%s", msg.Username, msg.Hostname),
				Hostname:   msg.Hostname,
				Port:       msg.Port,
				Username:   msg.Username,
				AuthMethod: "agent",
			}
			if err := database.Create(&host).Error; err != nil {
				return types.ErrorMsg{Err: fmt.Errorf("failed to create host: %w", err)}
			}
		}

		// Fingerprint pre-check
		progress(connectStageText(prefix, "verify"))
		if bm := hostFingerprintDialBlock(database, host.ID, host.Hostname, host.Port, "ssh", 0, 0); bm != nil {
			return bm
		}

		// Load key if needed
		if host.KeyID != nil {
			database.Preload("Key").First(&host, host.ID)
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
			Progress: func(stage internalssh.ConnectStage) {
				progress(connectStageText(prefix, string(stage)))
			},
		})
		if err != nil {
			database.Create(&db.ConnectionHistory{
				HostID: host.ID, ConnectedAt: time.Now(), Status: "failed",
			})
			return types.ConnErrorMsg{Err: err, Target: hostDisplayName(host), Retry: types.SSHConnectMsg{HostID: host.ID}}
		}

		now := time.Now()
		database.Model(&db.Host{}).Where("id = ?", host.ID).Update("last_connected_at", now)
		history := db.ConnectionHistory{
			HostID: host.ID, ConnectedAt: now, Status: "success",
		}
		database.Create(&history)

		progress(connectStageText(prefix, "shell"))
		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, host.ForwardAgent)
		if err != nil {
			client.Client.Close()
			for _, c := range client.Closers {
				_ = c.Close()
			}
			return types.ConnErrorMsg{Err: err, Target: hostDisplayName(host), Retry: types.SSHConnectMsg{HostID: host.ID}}
		}
		is.SetClosers(client.Closers)

		startPortForwards(database, client.Client, host.ID, is)

		alias := hostDisplayName(host)
		return openSSHUITabMsg{is: is, alias: alias, hostID: host.ID, historyID: history.ID, replaceTabAt: -1, initialCommands: initialSSHCommandsForHost(&host, "")}
	}
	return a, tea.Batch(progressCmd, dial)
}
