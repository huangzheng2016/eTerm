package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/remote"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

func (a App) openRemoteShell(msg types.RemoteShellOpenMsg) (App, tea.Cmd) {
	cfg := esync.LoadConfig(a.db, a.masterKey)
	if cfg.Mode != "http" {
		return a, func() tea.Msg { return types.ErrorMsg{Err: fmt.Errorf("remote shell requires HTTP sync mode")} }
	}
	cols, rows := ptyFromAppSizeForTab(a, SSHTab)
	title := "[R]" + msg.Peer.Name
	tabType := LocalTab
	if msg.Target == "host" {
		title = "[R]" + msg.Peer.Name + "-" + msg.HostLabel
		tabType = SSHTab
	}
	var toastCmd tea.Cmd
	a.toast, toastCmd = a.toast.Show("Opening remote shell...", components.ToastInfo, 30*time.Second)
	return a, tea.Batch(toastCmd, func() tea.Msg {
		is, err := remote.Open(context.Background(), cfg.ServerURL, cfg.APIKey, cfg.TenantID(), cfg.InsecureTLS, msg.Peer.ID, msg.Target, msg.HostSyncID, rows, cols)
		if err != nil {
			return types.ConnErrorMsg{Err: err, Target: title}
		}
		return remoteTerminalOpenedMsg{is: is, title: title, tabType: tabType}
	})
}

func (a App) applyRemoteTerminalOpened(msg remoteTerminalOpenedMsg) (App, tea.Cmd) {
	a.toast = a.toast.Dismiss()
	sv := sshview.New(msg.is, msg.title, 0, BuildSSHKeys(a.kbConfig))
	if a.width > 0 {
		sv.SetSize(a.width, a.mainContentHeightForType(msg.tabType))
	}
	a.tabs = append(a.tabs, Tab{Type: msg.tabType, Title: msg.title, Model: sv})
	a.activeTab = len(a.tabs) - 1
	a.syncTabBar()
	return a, tea.Batch(sv.Init(), reflowWindow(a))
}
