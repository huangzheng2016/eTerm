package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/types"
	"github.com/huangzheng2016/eTerm/internal/ui/tmuxmenu"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

func stubSSHTmux(t *testing.T) {
	t.Helper()
	oldConnect := sshTmuxConnect
	oldProbe := sshTmuxProbe
	oldEnsure := sshTmuxEnsureConfig
	oldList := sshTmuxListSessions
	oldKill := sshTmuxKillSession
	oldRename := sshTmuxRenameSession
	oldInteractive := sshTmuxInteractive
	oldGate := sshTmuxFingerprintGate
	t.Cleanup(func() {
		sshTmuxConnect = oldConnect
		sshTmuxProbe = oldProbe
		sshTmuxEnsureConfig = oldEnsure
		sshTmuxListSessions = oldList
		sshTmuxKillSession = oldKill
		sshTmuxRenameSession = oldRename
		sshTmuxInteractive = oldInteractive
		sshTmuxFingerprintGate = oldGate
	})
	sshTmuxConnect = func(internalssh.ConnectConfig) (*internalssh.ConnectResult, error) {
		return &internalssh.ConnectResult{}, nil
	}
	sshTmuxFingerprintGate = func(database *gorm.DB, hostID uint, hostname string, port int, connType string, streamID uint64, forwardRuleID uint) tea.Msg {
		return nil
	}
	sshTmuxProbe = func(*ssh.Client) error { return nil }
	sshTmuxEnsureConfig = func(*ssh.Client, string) (string, error) {
		return "/cfg/tmux.conf", nil
	}
	sshTmuxListSessions = func(*ssh.Client, string) ([]types.TmuxSession, error) {
		return []types.TmuxSession{{Name: "work"}}, nil
	}
	sshTmuxKillSession = func(*ssh.Client, string, string) error { return nil }
	sshTmuxRenameSession = func(*ssh.Client, string, string, string) error { return nil }
	sshTmuxInteractive = func(*ssh.Client, int, int, bool) (*internalssh.InteractiveSession, error) {
		return &internalssh.InteractiveSession{}, nil
	}
}

func sshTmuxTestDB(t *testing.T) (*gorm.DB, db.Host) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	host := db.Host{Alias: "prod", Hostname: "example.com", Port: 22, Username: "alice", AuthMethod: "key"}
	if err := database.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	return database, host
}

func TestProbeSSHTmuxHostMissingTmux(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)
	sshTmuxProbe = func(*ssh.Client) error { return tmux.ErrRemoteTmuxNotFound }

	msg := probeSSHTmuxHost(database, nil, host.ID, "Remote tmux", func(string) {})

	ready, ok := msg.(sshTmuxMenuReadyMsg)
	if !ok {
		t.Fatalf("got %T want sshTmuxMenuReadyMsg", msg)
	}
	if ready.err == nil || !strings.Contains(ready.err.Error(), "tmux is not installed on the remote host") {
		t.Fatalf("err = %v, want not-installed message", ready.err)
	}
	if !strings.Contains(ready.err.Error(), "prod") {
		t.Fatalf("err = %v, want host label", ready.err)
	}

	a := App{}
	next, cmd := a.applySSHTmuxMenuReady(ready)
	a = next
	if a.tmuxMenu != nil {
		t.Fatal("menu must stay closed when tmux is missing")
	}
	if cmd == nil {
		t.Fatal("expected toast command for the error")
	}
}

func TestProbeSSHTmuxHostListsSessions(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)

	msg := probeSSHTmuxHost(database, nil, host.ID, "Remote tmux", func(string) {})

	ready, ok := msg.(sshTmuxMenuReadyMsg)
	if !ok {
		t.Fatalf("got %T want sshTmuxMenuReadyMsg", msg)
	}
	if ready.err != nil {
		t.Fatalf("err = %v", ready.err)
	}
	if len(ready.sessions) != 1 || ready.sessions[0].Name != "work" {
		t.Fatalf("sessions = %#v", ready.sessions)
	}
	if ready.configWarn {
		t.Fatal("configWarn = true, want false")
	}

	a := App{}
	next, _ := a.applySSHTmuxMenuReady(ready)
	a = next
	if a.tmuxMenu == nil {
		t.Fatal("tmux menu not opened")
	}
	if a.tmuxMenu.HostID() != host.ID {
		t.Fatalf("menu HostID = %d want %d", a.tmuxMenu.HostID(), host.ID)
	}
	if !strings.Contains(a.tmuxMenu.View(), "work") {
		t.Fatalf("menu missing session:\n%s", a.tmuxMenu.View())
	}
}

func TestOpenSSHTmuxSessionAttach(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)

	msg := openSSHTmuxSession(database, nil, types.TmuxOpenMsg{HostID: host.ID, Name: "work"}, "Open remote tmux", func(string) {}, 24, 80)

	opened, ok := msg.(openSSHUITabMsg)
	if !ok {
		t.Fatalf("got %T want openSSHUITabMsg", msg)
	}
	if opened.tmuxSession != "work" {
		t.Fatalf("tmuxSession = %q", opened.tmuxSession)
	}
	if len(opened.initialCommands) != 1 || opened.initialCommands[0] != "tmux -f '/cfg/tmux.conf' source-file '/cfg/tmux.conf' && exec tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
		t.Fatalf("initialCommands = %#v", opened.initialCommands)
	}
	if opened.alias != "[T]prod-work" {
		t.Fatalf("alias = %q", opened.alias)
	}
	if opened.hostID != host.ID {
		t.Fatalf("hostID = %d want %d", opened.hostID, host.ID)
	}
}

func TestOpenSSHTmuxSessionNew(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)

	msg := openSSHTmuxSession(database, nil, types.TmuxOpenMsg{HostID: host.ID, New: true}, "Open remote tmux", func(string) {}, 24, 80)

	opened, ok := msg.(openSSHUITabMsg)
	if !ok {
		t.Fatalf("got %T want openSSHUITabMsg", msg)
	}
	if !strings.HasPrefix(opened.tmuxSession, defaultSSHTmuxSessionPrefix+"-") {
		t.Fatalf("tmuxSession = %q", opened.tmuxSession)
	}
	if len(opened.initialCommands) != 1 || !strings.Contains(opened.initialCommands[0], "exec tmux") || !strings.Contains(opened.initialCommands[0], "new-session -A -s '"+opened.tmuxSession+"'") {
		t.Fatalf("initialCommands = %#v", opened.initialCommands)
	}
}

func TestOpenSSHTmuxSessionConfigError(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)
	sshTmuxEnsureConfig = func(*ssh.Client, string) (string, error) {
		return "", errors.New("read-only home")
	}

	msg := openSSHTmuxSession(database, nil, types.TmuxOpenMsg{HostID: host.ID, Name: "work"}, "Open remote tmux", func(string) {}, 24, 80)

	opened, ok := msg.(types.ConnErrorMsg)
	if !ok {
		t.Fatalf("got %T want ConnErrorMsg", msg)
	}
	if opened.Err == nil || !strings.Contains(opened.Err.Error(), "prepare tmux config") {
		t.Fatalf("err = %v", opened.Err)
	}
}

func TestSSHReconnectInitialCommandsTmux(t *testing.T) {
	database, _ := sshTmuxTestDB(t)
	stubSSHTmux(t)

	cmds, err := sshReconnectInitialCommands(database, &internalssh.ConnectResult{}, &db.Host{}, "work")
	if err != nil {
		t.Fatal(err)
	}

	if len(cmds) != 1 || cmds[0] != "tmux -f '/cfg/tmux.conf' source-file '/cfg/tmux.conf' && exec tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestSSHReconnectInitialCommandsPlain(t *testing.T) {
	database, _ := sshTmuxTestDB(t)

	cmds, err := sshReconnectInitialCommands(database, &internalssh.ConnectResult{}, &db.Host{RemoteCommand: "uptime"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(cmds) != 1 || cmds[0] != "uptime" {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestApplySSHTmuxMenuReadyErrorSetsMenuError(t *testing.T) {
	a := App{tmuxMenu: tmuxmenu.NewSSH(1, "prod")}
	a.tmuxMenu.SetLoading(true)

	next, cmd := a.applySSHTmuxMenuReady(sshTmuxMenuReadyMsg{hostID: 1, hostLabel: "prod", err: errors.New("dial failed")})
	a = next

	if cmd != nil {
		t.Fatal("menu-open error must render inline, not toast")
	}
	if a.tmuxMenu == nil {
		t.Fatal("menu must stay open")
	}
	view := a.tmuxMenu.View()
	if !strings.Contains(view, "dial failed") {
		t.Fatalf("menu missing inline error:\n%s", view)
	}
	if strings.Contains(view, "Loading") {
		t.Fatalf("menu stuck in loading state:\n%s", view)
	}
}

func TestApplySSHTmuxMenuReadyErrorToastsWithoutMenu(t *testing.T) {
	a := App{}

	next, cmd := a.applySSHTmuxMenuReady(sshTmuxMenuReadyMsg{hostID: 1, hostLabel: "prod", err: errors.New("dial failed")})
	a = next

	if a.tmuxMenu != nil {
		t.Fatal("menu must stay closed")
	}
	if cmd == nil {
		t.Fatal("expected toast command for the error")
	}
}

func TestSSHReconnectAlias(t *testing.T) {
	if got := sshReconnectAlias("[T]prod-work", "prod", "work"); got != "[T]prod-work" {
		t.Fatalf("tmux tab title not preserved: %q", got)
	}
	if got := sshReconnectAlias("prod", "prod", ""); got != "prod" {
		t.Fatalf("plain reconnect alias = %q", got)
	}
}

func TestMigrateTmuxKeyBindings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		goos      string
		tmuxMenu  string
		batch     string
		wantTmux  string
		wantBatch string
	}{
		{name: "linux defaults", goos: "linux", tmuxMenu: "m", batch: "ctrl+shift+m", wantTmux: "ctrl+shift+m", wantBatch: "ctrl+shift+a"},
		{name: "windows defaults", goos: "windows", tmuxMenu: "m", batch: "alt+shift+m", wantTmux: "alt+shift+m", wantBatch: "alt+shift+a"},
		{name: "keeps custom", goos: "linux", tmuxMenu: "x", batch: "ctrl+shift+z", wantTmux: "x", wantBatch: "ctrl+shift+z"},
		{name: "keeps custom batch actions", goos: "linux", tmuxMenu: "m", batch: "ctrl+shift+z", wantTmux: "ctrl+shift+m", wantBatch: "ctrl+shift+z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := KeyBindingConfig{
				TmuxMenu:     []string{tc.tmuxMenu},
				BatchActions: []string{tc.batch},
			}
			migrateTmuxKeyBindings(&cfg, tc.goos)
			if len(cfg.TmuxMenu) != 1 || cfg.TmuxMenu[0] != tc.wantTmux {
				t.Fatalf("TmuxMenu = %#v", cfg.TmuxMenu)
			}
			if len(cfg.BatchActions) != 1 || cfg.BatchActions[0] != tc.wantBatch {
				t.Fatalf("BatchActions = %#v", cfg.BatchActions)
			}
		})
	}
}
