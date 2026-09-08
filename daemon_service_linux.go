//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const daemonServiceUnitName = "eterm-daemon.service"

var runSystemctl = func(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func daemonServiceUnitPath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", daemonServiceUnitName), nil
}

func systemdQuoteArg(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}

func daemonServiceUnit(programArgs []string) string {
	quoted := make([]string, len(programArgs))
	for i, arg := range programArgs {
		quoted[i] = systemdQuoteArg(arg)
	}
	return "[Unit]\n" +
		"Description=eTerm sync daemon\n" +
		"\n" +
		"[Service]\n" +
		"ExecStart=" + strings.Join(quoted, " ") + "\n" +
		"Restart=on-failure\n" +
		"RestartSec=2\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

func runSystemctlUser(args ...string) error {
	out, err := runSystemctl(append([]string{"--user"}, args...)...)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("systemctl not found: daemon service requires systemd")
		}
		return fmt.Errorf("systemctl --user %s failed: %v: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

func daemonServiceEnable(opts daemonOptions) error {
	if err := daemonServiceCheckNoPassword(opts); err != nil {
		return err
	}
	args, err := daemonServiceProgramArguments(opts)
	if err != nil {
		return err
	}
	unitPath, err := daemonServiceUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(unitPath, []byte(daemonServiceUnit(args)), 0644); err != nil {
		return err
	}
	if err := runSystemctlUser("daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctlUser("enable", daemonServiceUnitName); err != nil {
		return err
	}
	return runSystemctlUser("restart", daemonServiceUnitName)
}

func daemonServiceDisable() error {
	unitPath, err := daemonServiceUnitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(unitPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := runSystemctlUser("disable", "--now", daemonServiceUnitName); err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil {
		return err
	}
	return runSystemctlUser("daemon-reload")
}

func daemonServiceStatus() (enabled bool, detail string) {
	unitPath, err := daemonServiceUnitPath()
	if err != nil {
		return false, "not installed"
	}
	if _, err := os.Stat(unitPath); err != nil {
		return false, "not installed"
	}
	state, err := runSystemctl("--user", "is-enabled", daemonServiceUnitName)
	if errors.Is(err, exec.ErrNotFound) {
		return true, "installed, systemctl not found"
	}
	if err != nil && state != "disabled" {
		return true, "installed, systemctl --user unavailable (no systemd user bus)"
	}
	active, _ := runSystemctl("--user", "is-active", daemonServiceUnitName)
	if active == "" {
		active = "unknown"
	}
	return state == "enabled", state + ", " + active
}
