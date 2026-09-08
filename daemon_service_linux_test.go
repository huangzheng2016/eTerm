//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func stubRunSystemctl(t *testing.T, fn func(args ...string) (string, error)) *[]string {
	t.Helper()
	var calls []string
	orig := runSystemctl
	runSystemctl = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return fn(args...)
	}
	t.Cleanup(func() { runSystemctl = orig })
	return &calls
}

func newNoPasswordDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "eterm.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(database, "no_password", "true"); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func writeDaemonServiceUnit(t *testing.T, dir string) string {
	t.Helper()
	unitPath := filepath.Join(dir, "systemd", "user", daemonServiceUnitName)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath, []byte("unit"), 0644); err != nil {
		t.Fatal(err)
	}
	return unitPath
}

func testExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func readDaemonServiceUnit(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "systemd", "user", daemonServiceUnitName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDaemonServiceUnitContents(t *testing.T) {
	unit := daemonServiceUnit([]string{"/usr/local/bin/eterm", "daemon", "run", "-c", "/tmp/e.db", "-name", "box", "-pprof", "127.0.0.1:6061"})
	want := "[Unit]\nDescription=eTerm sync daemon\n\n[Service]\nExecStart=/usr/local/bin/eterm daemon run -c /tmp/e.db -name box -pprof 127.0.0.1:6061\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n"
	if unit != want {
		t.Fatalf("unit = %q, want %q", unit, want)
	}
}

func TestDaemonServiceUnitPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path, err := daemonServiceUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "systemd", "user", "eterm-daemon.service")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestDaemonServiceEnableWritesUnitAndCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	dbPath := newNoPasswordDB(t, dir)
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	if err := daemonServiceEnable(daemonOptions{DBPath: dbPath}); err != nil {
		t.Fatal(err)
	}
	want := "[Unit]\nDescription=eTerm sync daemon\n\n[Service]\nExecStart=" + testExecutable(t) + " daemon run -c " + dbPath + "\nRestart=on-failure\nRestartSec=2\n\n[Install]\nWantedBy=default.target\n"
	if got := readDaemonServiceUnit(t, dir); got != want {
		t.Fatalf("unit = %q, want %q", got, want)
	}
	got := strings.Join(*calls, "; ")
	want = "--user daemon-reload; --user enable eterm-daemon.service; --user restart eterm-daemon.service"
	if got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
}

func TestDaemonServiceEnableIdempotentLoadsLatestConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	dbPath := newNoPasswordDB(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	if err := daemonServiceEnable(daemonOptions{DBPath: dbPath}); err != nil {
		t.Fatal(err)
	}
	if err := daemonServiceEnable(daemonOptions{DBPath: dbPath, Name: "box"}); err != nil {
		t.Fatal(err)
	}
	unit := readDaemonServiceUnit(t, dir)
	if !strings.Contains(unit, "ExecStart="+testExecutable(t)+" daemon run -c "+dbPath+" -name box\n") {
		t.Fatalf("unit = %q", unit)
	}
}

func TestDaemonServiceEnableRejectsPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	err := daemonServiceEnable(daemonOptions{Password: "secret"})
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("systemctl called: %v", *calls)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "systemd", "user", daemonServiceUnitName)); !os.IsNotExist(statErr) {
		t.Fatal("unit file was written despite password")
	}
}

func TestDaemonServiceEnableRejectsMissingDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	err := daemonServiceEnable(daemonOptions{DBPath: filepath.Join(dir, "missing.db")})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("systemctl called: %v", *calls)
	}
}

func TestDaemonServiceEnableRejectsPasswordProtectedDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	dbPath := filepath.Join(dir, "eterm.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := database.DB(); err == nil {
		sqlDB.Close()
	}
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	err = daemonServiceEnable(daemonOptions{DBPath: dbPath})
	if err == nil || !strings.Contains(err.Error(), "no-password") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("systemctl called: %v", *calls)
	}
}

func TestDaemonServiceEnableSystemctlNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	dbPath := newNoPasswordDB(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		return "", &exec.Error{Name: "systemctl", Err: exec.ErrNotFound}
	})
	err := daemonServiceEnable(daemonOptions{DBPath: dbPath})
	if err == nil || !strings.Contains(err.Error(), "systemctl not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestDaemonServiceEnableUserBusFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	dbPath := newNoPasswordDB(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		return "Failed to connect to bus: No such file or directory", errors.New("exit status 1")
	})
	err := daemonServiceEnable(daemonOptions{DBPath: dbPath})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "systemctl --user daemon-reload") || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("err = %v", err)
	}
}

func TestDaemonServiceDisableRemovesUnitAndCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	unitPath := writeDaemonServiceUnit(t, dir)
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	if err := daemonServiceDisable(); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(*calls, "; ")
	want := "--user disable --now eterm-daemon.service; --user daemon-reload"
	if got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatal("unit file was not removed")
	}
}

func TestDaemonServiceDisableNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	calls := stubRunSystemctl(t, func(args ...string) (string, error) { return "", nil })
	if err := daemonServiceDisable(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("systemctl called: %v", *calls)
	}
}

func TestDaemonServiceStatusNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	enabled, detail := daemonServiceStatus()
	if enabled || detail != "not installed" {
		t.Fatalf("enabled=%v detail=%q", enabled, detail)
	}
}

func TestDaemonServiceStatusEnabledActive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeDaemonServiceUnit(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		switch args[1] {
		case "is-enabled":
			return "enabled", nil
		case "is-active":
			return "active", nil
		}
		return "", nil
	})
	enabled, detail := daemonServiceStatus()
	if !enabled || detail != "enabled, active" {
		t.Fatalf("enabled=%v detail=%q", enabled, detail)
	}
}

func TestDaemonServiceStatusDisabledInactive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeDaemonServiceUnit(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		switch args[1] {
		case "is-enabled":
			return "disabled", errors.New("exit status 1")
		case "is-active":
			return "inactive", errors.New("exit status 3")
		}
		return "", nil
	})
	enabled, detail := daemonServiceStatus()
	if enabled || detail != "disabled, inactive" {
		t.Fatalf("enabled=%v detail=%q", enabled, detail)
	}
}

func TestDaemonServiceStatusSystemctlNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeDaemonServiceUnit(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		return "", &exec.Error{Name: "systemctl", Err: exec.ErrNotFound}
	})
	enabled, detail := daemonServiceStatus()
	if !enabled || detail != "installed, systemctl not found" {
		t.Fatalf("enabled=%v detail=%q", enabled, detail)
	}
}

func TestDaemonServiceStatusNoUserBus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeDaemonServiceUnit(t, dir)
	stubRunSystemctl(t, func(args ...string) (string, error) {
		return "Failed to connect to bus: No such file or directory", errors.New("exit status 1")
	})
	enabled, detail := daemonServiceStatus()
	if !enabled || !strings.Contains(detail, "unavailable") {
		t.Fatalf("enabled=%v detail=%q", enabled, detail)
	}
}
