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
	oldInteractive := sshTmuxInteractive
	oldGate := sshTmuxFingerprintGate
	t.Cleanup(func() {
		sshTmuxConnect = oldConnect
		sshTmuxProbe = oldProbe
		sshTmuxEnsureConfig = oldEnsure
		sshTmuxListSessions = oldList
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
	if len(opened.initialCommands) != 1 || opened.initialCommands[0] != "tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
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
	if opened.tmuxSession != defaultSSHTmuxSessionName {
		t.Fatalf("tmuxSession = %q", opened.tmuxSession)
	}
	if len(opened.initialCommands) != 1 || !strings.Contains(opened.initialCommands[0], "new-session -A -s '"+defaultSSHTmuxSessionName+"'") {
		t.Fatalf("initialCommands = %#v", opened.initialCommands)
	}
}

func TestOpenSSHTmuxSessionConfigWarn(t *testing.T) {
	database, host := sshTmuxTestDB(t)
	stubSSHTmux(t)
	sshTmuxEnsureConfig = func(*ssh.Client, string) (string, error) {
		return "", errors.New("read-only home")
	}

	msg := openSSHTmuxSession(database, nil, types.TmuxOpenMsg{HostID: host.ID, Name: "work"}, "Open remote tmux", func(string) {}, 24, 80)

	opened, ok := msg.(openSSHUITabMsg)
	if !ok {
		t.Fatalf("got %T want openSSHUITabMsg", msg)
	}
	if !opened.configWarn {
		t.Fatal("configWarn = false, want true")
	}
	if len(opened.initialCommands) != 1 || !strings.HasPrefix(opened.initialCommands[0], "tmux attach-session") {
		t.Fatalf("initialCommands = %#v, want fallback without -f", opened.initialCommands)
	}
}

func TestSSHReconnectInitialCommandsTmux(t *testing.T) {
	database, _ := sshTmuxTestDB(t)
	stubSSHTmux(t)

	cmds := sshReconnectInitialCommands(database, &internalssh.ConnectResult{}, &db.Host{}, "work")

	if len(cmds) != 1 || cmds[0] != "tmux -f '/cfg/tmux.conf' attach-session -t 'work'" {
		t.Fatalf("cmds = %#v", cmds)
	}
}

func TestSSHReconnectInitialCommandsPlain(t *testing.T) {
	database, _ := sshTmuxTestDB(t)

	cmds := sshReconnectInitialCommands(database, &internalssh.ConnectResult{}, &db.Host{RemoteCommand: "uptime"}, "")

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

func TestMigrateTmuxKeyBindingsDefaults(t *testing.T) {
	cfg := KeyBindingConfig{
		TmuxMenu:     []string{"m"},
		BatchActions: []string{"ctrl+shift+m"},
	}
	migrateTmuxKeyBindings(&cfg, "linux")
	if len(cfg.TmuxMenu) != 1 || cfg.TmuxMenu[0] != "ctrl+shift+m" {
		t.Fatalf("TmuxMenu = %#v", cfg.TmuxMenu)
	}
	if len(cfg.BatchActions) != 1 || cfg.BatchActions[0] != "ctrl+shift+a" {
		t.Fatalf("BatchActions = %#v", cfg.BatchActions)
	}
}

func TestMigrateTmuxKeyBindingsWindows(t *testing.T) {
	cfg := KeyBindingConfig{
		TmuxMenu:     []string{"m"},
		BatchActions: []string{"alt+shift+m"},
	}
	migrateTmuxKeyBindings(&cfg, "windows")
	if len(cfg.TmuxMenu) != 1 || cfg.TmuxMenu[0] != "alt+shift+m" {
		t.Fatalf("TmuxMenu = %#v", cfg.TmuxMenu)
	}
	if len(cfg.BatchActions) != 1 || cfg.BatchActions[0] != "alt+shift+a" {
		t.Fatalf("BatchActions = %#v", cfg.BatchActions)
	}
}

func TestMigrateTmuxKeyBindingsKeepsCustom(t *testing.T) {
	cfg := KeyBindingConfig{
		TmuxMenu:     []string{"x"},
		BatchActions: []string{"ctrl+shift+z"},
	}
	migrateTmuxKeyBindings(&cfg, "linux")
	if cfg.TmuxMenu[0] != "x" || cfg.BatchActions[0] != "ctrl+shift+z" {
		t.Fatalf("custom bindings changed: %#v", cfg)
	}
}

func TestMigrateTmuxKeyBindingsKeepsCustomBatchActions(t *testing.T) {
	cfg := KeyBindingConfig{
		TmuxMenu:     []string{"m"},
		BatchActions: []string{"ctrl+shift+z"},
	}
	migrateTmuxKeyBindings(&cfg, "linux")
	if len(cfg.TmuxMenu) != 1 || cfg.TmuxMenu[0] != "ctrl+shift+m" {
		t.Fatalf("TmuxMenu = %#v", cfg.TmuxMenu)
	}
	if cfg.BatchActions[0] != "ctrl+shift+z" {
		t.Fatalf("custom BatchActions changed: %#v", cfg.BatchActions)
	}
}
