package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/batchresultview"
)

func (a App) runBatchOpenSessions(hostIDs []uint) (App, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(hostIDs))
	for _, hostID := range hostIDs {
		cmds = append(cmds, a.batchConnectHostCmd(hostID, ""))
	}
	return a, tea.Batch(cmds...)
}

func (a App) applyBatchSnippet(hostIDs []uint, snippet string) (App, tea.Cmd) {
	var cmds []tea.Cmd
	for _, hostID := range hostIDs {
		if tab := a.findSSHTabByHostID(hostID); tab != nil && !tab.Disconnected() {
			tab.PasteCommand(snippet + "\n")
			continue
		}
		cmds = append(cmds, a.batchConnectHostCmd(hostID, snippet))
	}
	return a, tea.Batch(cmds...)
}

func (a App) batchConnectHostCmd(hostID uint, extraCommand string) tea.Cmd {
	database := a.db
	mk := a.masterKey
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
	return func() tea.Msg {
		var host db.Host
		if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("batch connect host #%d: %w", hostID, err)}
		}
		if bm := hostFingerprintDialBlock(database, hostID, host.Hostname, host.Port, "ssh", 0, 0); bm != nil {
			if em, ok := bm.(types.ErrorMsg); ok {
				return em
			}
			return types.ErrorMsg{Err: fmt.Errorf("host fingerprint must be confirmed — connect once from List: %s", hostDisplayName(host))}
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
				return false
			},
		})
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("batch connect %s: %w", hostDisplayName(host), err)}
		}

		now := time.Now()
		database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
		history := db.ConnectionHistory{HostID: hostID, ConnectedAt: now, Status: "success"}
		database.Create(&history)

		is, err := internalssh.NewInteractiveSession(client.Client, ptyRows, ptyCols, host.ForwardAgent)
		if err != nil {
			client.Close()
			return types.ErrorMsg{Err: fmt.Errorf("batch shell %s: %w", hostDisplayName(host), err)}
		}
		is.SetClosers(client.Closers)
		startPortForwards(database, client.Client, hostID, is)

		return openSSHUITabMsg{
			is:              is,
			alias:           hostDisplayName(host),
			hostID:          hostID,
			historyID:       history.ID,
			replaceTabAt:    -1,
			initialCommands: initialSSHCommandsForHost(&host, extraCommand),
		}
	}
}

func (a App) openBatchResultTab(hostIDs []uint, command string) (App, tea.Cmd) {
	if len(hostIDs) == 0 {
		return a, nil
	}
	model := batchresultview.New(a.db, a.masterKey, hostIDs, command)
	if a.width > 0 {
		model.SetSize(a.width, a.mainContentHeightForType(BatchResultTab))
	}
	title := "Batch: " + strings.TrimSpace(command)
	if len(title) > 28 {
		title = title[:27] + "…"
	}
	tab := Tab{Type: BatchResultTab, Title: title, Model: model}
	a.tabs = append(a.tabs, tab)
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, tea.Batch(model.Init(), reflowWindow(a))
}
