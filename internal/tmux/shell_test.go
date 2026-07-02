package tmux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func TestParseSessions(t *testing.T) {
	got := parseSessions([]byte("work\t1710000000\t1\nlogs\t1710000010\t0\n"))
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Name != "work" || got[0].CreatedUnix != 1710000000 || !got[0].Attached {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Name != "logs" || got[1].CreatedUnix != 1710000010 || got[1].Attached {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestNoServerOutput(t *testing.T) {
	if !isNoServerOutput([]byte("no server running on /tmp/tmux-501/default\n")) {
		t.Fatal("expected tmux no server output")
	}
}

func TestCommandErrorForMissingBinary(t *testing.T) {
	err := tmuxCommandError("list-sessions", exec.ErrNotFound, nil)
	if err == nil || err.Error() != "tmux not found in PATH" {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultSessionNameUsesShortTmuxPrefix(t *testing.T) {
	name := defaultSessionName()
	if !regexp.MustCompile(`^tmux-[a-z0-9]{6}$`).MatchString(name) {
		t.Fatalf("name = %q", name)
	}
}

func TestSessionCommandsDisableStatus(t *testing.T) {
	name := "tmux-abc123"
	if got := newSessionDetachedArgs(name); !sameStrings(got, []string{"new-session", "-d", "-s", name}) {
		t.Fatalf("new args = %#v", got)
	}
	if got := statusOffArgs(name); !sameStrings(got, []string{"set-option", "-t", name, "status", "off"}) {
		t.Fatalf("status args = %#v", got)
	}
	if got := attachSessionArgs(name); !sameStrings(got, []string{"attach-session", "-t", name}) {
		t.Fatalf("attach args = %#v", got)
	}
}

func TestNewSessionCleansUpWhenAttachFails(t *testing.T) {
	oldRun := runTmuxCmd
	oldAttach := attachTmuxSession
	oldKill := killTmuxSession
	t.Cleanup(func() {
		runTmuxCmd = oldRun
		attachTmuxSession = oldAttach
		killTmuxSession = oldKill
	})
	created := ""
	killed := ""
	runTmuxCmd = func(ctx context.Context, op string, args []string) error {
		if op == "new-session" && len(args) == 4 {
			created = args[3]
		}
		return nil
	}
	attachTmuxSession = func(ctx context.Context, name string, rows, cols int) (*internalssh.InteractiveSession, error) {
		return nil, errors.New("attach failed")
	}
	killTmuxSession = func(ctx context.Context, name string) error {
		killed = name
		return nil
	}

	_, _, err := NewSession(context.Background(), 24, 80)
	if err == nil || err.Error() != "attach failed" {
		t.Fatalf("err = %v", err)
	}
	if created == "" || killed != created {
		t.Fatalf("created = %q killed = %q", created, killed)
	}
}

func TestPtyCommandCloseKillsStubbornProcessAfterTimeout(t *testing.T) {
	oldTimeout := internalssh.ProcessCloseKillTimeout
	internalssh.ProcessCloseKillTimeout = 20 * time.Millisecond
	t.Cleanup(func() { internalssh.ProcessCloseKillTimeout = oldTimeout })
	cmd := exec.Command(stubbornShell(t))

	is, err := ptyCommand(cmd, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := is.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-is.Done:
	case <-time.After(time.Second):
		t.Fatal("process did not exit after close timeout")
	}
}

func stubbornShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stubborn-sh")
	script := "#!/bin/sh\ntrap '' HUP TERM INT\nwhile :; do :; done\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
