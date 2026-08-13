package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonCommandDefaultsToStart(t *testing.T) {
	cmd, opts, err := parseDaemonArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "start" {
		t.Fatalf("cmd = %q, want start", cmd)
	}
	if opts.DBPath != "" || opts.Password != "" || opts.Name != "" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestDaemonCommandParsesSubcommandAndFlags(t *testing.T) {
	cmd, opts, err := parseDaemonArgs([]string{"start", "-c", "test.db", "-password", "pw", "-name", "box", "-pprof", "127.0.0.1:6061"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "start" {
		t.Fatalf("cmd = %q, want start", cmd)
	}
	if opts.DBPath != "test.db" || opts.Password != "pw" || opts.Name != "box" || opts.PProfAddr != "127.0.0.1:6061" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestDaemonStatusReportsStoppedForMissingPid(t *testing.T) {
	ctl := daemonController{
		pidPath: filepath.Join(t.TempDir(), "daemon.pid"),
		isAlive: func(int) bool {
			t.Fatal("isAlive should not be called")
			return false
		},
	}
	var out strings.Builder
	code := ctl.status(&out)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if strings.TrimSpace(out.String()) != "stopped" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestDaemonStatusReportsRunningPid(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctl := daemonController{
		pidPath: pidPath,
		isAlive: func(pid int) bool {
			return pid == 123
		},
	}
	var out strings.Builder
	code := ctl.status(&out)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if strings.TrimSpace(out.String()) != "running pid=123" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestDaemonStopEscalatesToKill(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	killed := false
	ctl := daemonController{
		pidPath: pidPath,
		isAlive: func(pid int) bool {
			return !killed
		},
		terminate: func(pid int) error { return nil },
		kill: func(pid int) error {
			killed = true
			return nil
		},
	}
	var out strings.Builder
	code := ctl.stop(&out)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !killed {
		t.Fatal("kill was not called after terminate did not stop the process")
	}
	if strings.TrimSpace(out.String()) != "stopped" {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("pid file was not removed")
	}
}

func TestDaemonStopFailsWhenKillDoesNotHelp(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctl := daemonController{
		pidPath:   pidPath,
		isAlive:   func(pid int) bool { return true },
		terminate: func(pid int) error { return nil },
		kill:      func(pid int) error { return nil },
	}
	var out strings.Builder
	code := ctl.stop(&out)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "still running") {
		t.Fatalf("output = %q", out.String())
	}
}
