package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/components"
	"github.com/huangzheng2016/eTerm/internal/ui/tmuxmenu"
	"gorm.io/gorm"
)

var (
	sshTmuxConnect         = internalssh.Connect
	sshTmuxProbe           = tmux.ProbeRemote
	sshTmuxEnsureConfig    = tmux.EnsureRemoteConfig
	sshTmuxListSessions    = tmux.ListRemoteSessions
	sshTmuxInteractive     = internalssh.NewInteractiveSession
	sshTmuxFingerprintGate = hostFingerprintDialBlock
)

const defaultSSHTmuxSessionName = "eterm"

type sshTmuxMenuReadyMsg struct {
	hostID     uint
	hostLabel  string
	sessions   []types.TmuxSession
	configWarn bool
	err        error
}

type sshTmuxDialResult struct {
	host   db.Host
	client *internalssh.ConnectResult
	msg    tea.Msg
}

func tmuxConfiguredPath(database *gorm.DB) string {
	s, err := db.GetSetting(database, tmux.SettingConfigFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func dialSSHTmuxHost(database *gorm.DB, mk *security.MasterKeyManager, hostID uint, prefix string, progress func(string), retry tea.Msg) sshTmuxDialResult {
	var host db.Host
	if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
		appDebugf("SSH tmux aborted: load host: %v", err)
		return sshTmuxDialResult{msg: types.ErrorMsg{Err: fmt.Errorf("host not found: %w", err)}}
	}

	progress(connectStageText(prefix, "verify"))
	if bm := sshTmuxFingerprintGate(database, hostID, host.Hostname, host.Port, "tmux", 0, 0); bm != nil {
		return sshTmuxDialResult{msg: bm}
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

	client, err := sshTmuxConnect(internalssh.ConnectConfig{
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
		appDebugf("SSH tmux dial failed: %v", err)
		database.Create(&db.ConnectionHistory{
			HostID: hostID, ConnectedAt: time.Now(), Status: "failed",
		})
		return sshTmuxDialResult{msg: types.ConnErrorMsg{Err: err, Target: hostDisplayName(host), Retry: retry}}
	}
	return sshTmuxDialResult{host: host, client: client}
}

func (a App) startSSHTmuxMenu(msg types.TmuxMenuMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	hostID := msg.HostID
	prefix := "Remote tmux"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "load"))
	probe := func() tea.Msg {
		defer close(progressCh)
		return probeSSHTmuxHost(database, mk, hostID, prefix, progress)
	}
	return a, tea.Batch(progressCmd, probe)
}

func probeSSHTmuxHost(database *gorm.DB, mk *security.MasterKeyManager, hostID uint, prefix string, progress func(string)) tea.Msg {
	res := dialSSHTmuxHost(database, mk, hostID, prefix, progress, types.TmuxMenuMsg{HostID: hostID})
	if res.msg != nil {
		return res.msg
	}
	defer res.client.Close()
	label := hostDisplayName(res.host)
	progress(connectStageText(prefix, "tmux probe"))
	if err := sshTmuxProbe(res.client.Client); err != nil {
		if errors.Is(err, tmux.ErrRemoteTmuxNotFound) {
			return sshTmuxMenuReadyMsg{hostID: hostID, hostLabel: label,
				err: fmt.Errorf("%s: tmux is not installed on the remote host; install tmux there first (e.g. apt/dnf/brew install tmux)", label)}
		}
		return sshTmuxMenuReadyMsg{hostID: hostID, hostLabel: label, err: fmt.Errorf("%s: tmux probe failed: %w", label, err)}
	}
	configFile, cfgErr := sshTmuxEnsureConfig(res.client.Client, tmuxConfiguredPath(database))
	sessions, err := sshTmuxListSessions(res.client.Client, configFile)
	if err != nil {
		return sshTmuxMenuReadyMsg{hostID: hostID, hostLabel: label, err: fmt.Errorf("%s: %w", label, err)}
	}
	return sshTmuxMenuReadyMsg{hostID: hostID, hostLabel: label, sessions: sessions, configWarn: cfgErr != nil}
}

func (a App) applySSHTmuxMenuReady(msg sshTmuxMenuReadyMsg) (App, tea.Cmd) {
	a = a.stopConnectProgress()
	if msg.err != nil {
		if a.tmuxMenu != nil && a.tmuxMenu.HostID() == msg.hostID {
			a.tmuxMenu.SetError(msg.err.Error())
			return a, nil
		}
		var tc tea.Cmd
		a.toast, tc = a.toast.Show(msg.err.Error(), components.ToastError, 6*time.Second)
		return a, tea.Batch(tc, reflowWindow(a))
	}
	a.tmuxMenu = tmuxmenu.NewSSH(msg.hostID, msg.hostLabel)
	a.tmuxMenu.SetSessions(msg.sessions)
	if msg.configWarn {
		var tc tea.Cmd
		a.toast, tc = a.toast.Show("tmux config not injected; using remote defaults", components.ToastWarning, 4*time.Second)
		return a, tc
	}
	return a, nil
}

func (a App) openSSHTmux(msg types.TmuxOpenMsg) (App, tea.Cmd) {
	database := a.db
	mk := a.masterKey
	prefix := "Open remote tmux"
	var progress func(string)
	var progressCh chan string
	var progressCmd tea.Cmd
	a, progressCh, progressCmd, progress = a.beginConnectProgress(connectStageText(prefix, "load"))
	ptyCols, ptyRows := ptyFromAppSizeForTab(a, SSHTab)
	open := func() tea.Msg {
		defer close(progressCh)
		return openSSHTmuxSession(database, mk, msg, prefix, progress, ptyRows, ptyCols)
	}
	return a, tea.Batch(progressCmd, open)
}

func openSSHTmuxSession(database *gorm.DB, mk *security.MasterKeyManager, msg types.TmuxOpenMsg, prefix string, progress func(string), ptyRows, ptyCols int) tea.Msg {
	hostID := msg.HostID
	res := dialSSHTmuxHost(database, mk, hostID, prefix, progress, msg)
	if res.msg != nil {
		return res.msg
	}
	configFile, cfgErr := sshTmuxEnsureConfig(res.client.Client, tmuxConfiguredPath(database))
	sessionName := msg.Name
	var tmuxCmd string
	if msg.New {
		sessionName = defaultSSHTmuxSessionName
		tmuxCmd = tmux.RemoteNewCommand(configFile, sessionName)
	} else {
		tmuxCmd = tmux.RemoteAttachCommand(configFile, sessionName)
	}
	progress(connectStageText(prefix, "shell"))
	is, err := sshTmuxInteractive(res.client.Client, ptyRows, ptyCols, res.host.ForwardAgent)
	if err != nil {
		res.client.Close()
		appDebugf("SSH tmux NewInteractiveSession failed: %v", err)
		return types.ConnErrorMsg{Err: err, Target: hostDisplayName(res.host), Retry: msg}
	}
	is.SetClosers(res.client.Closers)
	startPortForwards(database, res.client.Client, hostID, is)

	now := time.Now()
	database.Model(&db.Host{}).Where("id = ?", hostID).Update("last_connected_at", now)
	history := db.ConnectionHistory{HostID: hostID, ConnectedAt: now, Status: "success"}
	database.Create(&history)

	alias := "[T]" + hostDisplayName(res.host) + "-" + sessionName
	return openSSHUITabMsg{
		is:              is,
		alias:           alias,
		hostID:          hostID,
		historyID:       history.ID,
		initialCommands: []string{tmuxCmd},
		replaceTabAt:    -1,
		tmuxSession:     sessionName,
		configWarn:      cfgErr != nil,
	}
}
