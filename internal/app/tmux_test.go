package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func missingTmuxTestApp(t *testing.T) App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))
	return remoteHTTPTestApp(t)
}

func TestLoadTmuxSessionsReportsMissingBinary(t *testing.T) {
	a := missingTmuxTestApp(t)

	msg := a.loadTmuxSessions()()

	loaded, ok := msg.(types.TmuxSessionsLoadedMsg)
	if !ok {
		t.Fatalf("got %T want TmuxSessionsLoadedMsg", msg)
	}
	if loaded.Err == nil || !strings.Contains(loaded.Err.Error(), "tmux not found in PATH") {
		t.Fatalf("err = %v", loaded.Err)
	}
}

func TestOpenTmuxNewReportsMissingBinary(t *testing.T) {
	a := missingTmuxTestApp(t)

	_, cmd := a.openTmux(types.TmuxOpenMsg{New: true})
	msg := cmd()

	errMsg, ok := msg.(types.ErrorMsg)
	if !ok {
		t.Fatalf("got %T want ErrorMsg", msg)
	}
	if !strings.Contains(errMsg.Err.Error(), "tmux not found in PATH") {
		t.Fatalf("err = %v", errMsg.Err)
	}
}
