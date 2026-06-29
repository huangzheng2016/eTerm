package localtmux

import (
	"os/exec"
	"strings"
	"testing"
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

func TestIsNoServerOutput(t *testing.T) {
	if !isNoServerOutput([]byte("no server running on /tmp/tmux-501/default\n")) {
		t.Fatal("expected tmux no server output")
	}
}

func TestTmuxCommandErrorForMissingBinary(t *testing.T) {
	err := tmuxCommandError("list-sessions", exec.ErrNotFound, nil)
	if err == nil || err.Error() != "tmux not found in PATH" {
		t.Fatalf("err = %v", err)
	}
}

func TestTmuxSessionCommandsDisableStatus(t *testing.T) {
	if got := strings.Join(newSessionDetachedArgs("work"), " "); got != "new-session -d -s work" {
		t.Fatalf("new args = %q", got)
	}
	if got := strings.Join(statusOffArgs("work"), " "); got != "set-option -t work status off" {
		t.Fatalf("status args = %q", got)
	}
	if got := strings.Join(attachSessionArgs("work"), " "); got != "attach-session -t work" {
		t.Fatalf("attach args = %q", got)
	}
}
