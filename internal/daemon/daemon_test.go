package daemon

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
)

func TestLoadRuntimeRejectsSSHSyncModeWithoutHost(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	salt, verifier := mk.Setup([]byte("pw"))
	_ = db.SetSetting(database, "encryption_salt", base64.StdEncoding.EncodeToString(salt))
	_ = db.SetSetting(database, "encryption_verifier", base64.StdEncoding.EncodeToString(verifier))
	_ = db.SetSetting(database, "sync_enabled", "true")
	_ = db.SetSetting(database, "sync_mode", "ssh")

	_, err = loadRuntime(database, Config{Password: "pw"})
	if err == nil || !strings.Contains(err.Error(), "no SSH host") {
		t.Fatalf("got %v, want no SSH host error", err)
	}
}

func TestQueueInputDropsOldestWhenFull(t *testing.T) {
	sr := &streamRelay{input: make(chan []byte, 2), stop: make(chan struct{})}
	sr.queueInput([]byte("a"))
	sr.queueInput([]byte("b"))
	sr.queueInput([]byte("c"))
	if got := <-sr.input; string(got) != "b" {
		t.Fatalf("first queued = %q, want b (oldest dropped)", got)
	}
	if got := <-sr.input; string(got) != "c" {
		t.Fatalf("second queued = %q, want c", got)
	}
}

func writeFakeTmux(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubTmuxProbeDirs(t *testing.T, dirs []string) {
	t.Helper()
	old := tmuxProbeDirs
	tmuxProbeDirs = dirs
	t.Cleanup(func() { tmuxProbeDirs = old })
}

func TestResolveTmuxBinaryFromPATH(t *testing.T) {
	dir := t.TempDir()
	want := writeFakeTmux(t, dir, 0o755)
	t.Setenv("PATH", dir)
	stubTmuxProbeDirs(t, nil)

	if got := resolveTmuxBinary(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if os.Getenv("PATH") != dir {
		t.Fatalf("PATH modified: %q", os.Getenv("PATH"))
	}
}

func TestResolveTmuxBinaryFallsBackToProbeDirs(t *testing.T) {
	noExec := t.TempDir()
	writeFakeTmux(t, noExec, 0o644)
	fallback := t.TempDir()
	want := writeFakeTmux(t, fallback, 0o755)
	t.Setenv("PATH", noExec)
	stubTmuxProbeDirs(t, []string{noExec, fallback})

	if got := resolveTmuxBinary(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), fallback+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want %s prepended", os.Getenv("PATH"), fallback)
	}
}

func TestResolveTmuxBinaryNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	stubTmuxProbeDirs(t, []string{t.TempDir()})

	if got := resolveTmuxBinary(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
