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
	cmd, opts, err := parseDaemonArgs([]string{"start", "-c", "test.db", "-password", "pw", "-name", "box"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "start" {
		t.Fatalf("cmd = %q, want start", cmd)
	}
	if opts.DBPath != "test.db" || opts.Password != "pw" || opts.Name != "box" {
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
