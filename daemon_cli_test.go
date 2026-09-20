package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func writeDaemonPid(t *testing.T, dir, content string) string {
	t.Helper()
	pidPath := filepath.Join(dir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return pidPath
}

func holdDaemonLock(t *testing.T, path string) {
	t.Helper()
	lock, err := acquireDaemonLock(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
}

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

func TestDaemonCommandParsesEnableAndDisable(t *testing.T) {
	for _, want := range []string{"enable", "disable"} {
		cmd, _, err := parseDaemonArgs([]string{want})
		if err != nil {
			t.Fatal(err)
		}
		if cmd != want {
			t.Fatalf("cmd = %q, want %q", cmd, want)
		}
	}
}

func TestDaemonEnableParsesDBPath(t *testing.T) {
	cmd, opts, err := parseDaemonArgs([]string{"enable", "-c", "test.db"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "enable" || opts.DBPath != "test.db" {
		t.Fatalf("cmd = %q, opts = %#v", cmd, opts)
	}
}

func TestDaemonCommandParsesRename(t *testing.T) {
	cmd, opts, err := parseDaemonArgs([]string{"rename", "my-box"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "rename" {
		t.Fatalf("cmd = %q, want rename", cmd)
	}
	if len(opts.Positionals) != 1 || opts.Positionals[0] != "my-box" {
		t.Fatalf("positionals = %#v", opts.Positionals)
	}
}

func TestDaemonRenameWritesSetting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	var out strings.Builder
	if code := renameDaemonPeer(&out, daemonOptions{DBPath: dbPath, Positionals: []string{"new-name"}}); code != 0 {
		t.Fatalf("code = %d, out = %s", code, out.String())
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetSetting(database, "daemon_peer_name"); got != "new-name" {
		t.Fatalf("persisted name = %q, want new-name", got)
	}
}

func TestDaemonRenameRequiresName(t *testing.T) {
	var out strings.Builder
	if code := renameDaemonPeer(&out, daemonOptions{}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestDaemonServiceProgramArguments(t *testing.T) {
	args, err := daemonServiceProgramArguments(daemonOptions{DBPath: "test.db", Name: "box", PProfAddr: "127.0.0.1:6061"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 9 {
		t.Fatalf("args = %#v", args)
	}
	if !filepath.IsAbs(args[0]) {
		t.Fatalf("args[0] = %q, want absolute path", args[0])
	}
	want := []string{"daemon", "run", "-c", "test.db", "-name", "box", "-pprof", "127.0.0.1:6061"}
	for i, w := range want {
		if args[i+1] != w {
			t.Fatalf("args = %#v, want suffix %#v", args, want)
		}
	}
}

func TestDaemonServiceProgramArgumentsDefaults(t *testing.T) {
	args, err := daemonServiceProgramArguments(daemonOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[1] != "daemon" || args[2] != "run" {
		t.Fatalf("args = %#v", args)
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
	pidPath := writeDaemonPid(t, t.TempDir(), "123\n")
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
	pidPath := writeDaemonPid(t, t.TempDir(), "123\n")
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
	pidPath := writeDaemonPid(t, t.TempDir(), "123\n")
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

func TestDaemonLockSecondInstanceFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	holdDaemonLock(t, path)

	if _, err := acquireDaemonLock(path); !errors.Is(err, errDaemonAlreadyRunning) {
		t.Fatalf("err = %v, want %v", err, errDaemonAlreadyRunning)
	}
	pid, held := daemonLockHolder(path)
	if !held {
		t.Fatal("lock holder not detected")
	}
	if pid != os.Getpid() {
		t.Fatalf("holder pid = %d, want %d", pid, os.Getpid())
	}
}

func TestDaemonLockReleasedOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := acquireDaemonLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if _, held := daemonLockHolder(path); held {
		t.Fatal("lock still held after close")
	}
	lock2, err := acquireDaemonLock(path)
	if err != nil {
		t.Fatalf("lock not re-acquired after close: %v", err)
	}
	lock2.Close()
}

func TestDaemonStartReportsLockHolder(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "daemon.lock")
	holdDaemonLock(t, lockPath)

	ctl := daemonController{
		pidPath:  filepath.Join(dir, "daemon.pid"),
		lockPath: lockPath,
		isAlive:  func(int) bool { return false },
	}
	var out strings.Builder
	code := ctl.start(&out, daemonOptions{})
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	want := "running pid=" + strconv.Itoa(os.Getpid())
	if strings.TrimSpace(out.String()) != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestDaemonEnableGuardReportsLockHolder(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	holdDaemonLock(t, lockPath)

	err := daemonEnableGuard(lockPath)
	if err == nil {
		t.Fatal("expected error when lock is held")
	}
	want := "pid=" + strconv.Itoa(os.Getpid())
	if !strings.Contains(err.Error(), "eterm daemon already running") || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q", err)
	}
	if !strings.Contains(err.Error(), "daemon.log") {
		t.Fatalf("err missing log path clue: %q", err)
	}
}

func TestDaemonEnableGuardPassesWhenLockFree(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	if err := daemonEnableGuard(lockPath); err != nil {
		t.Fatal(err)
	}
}
