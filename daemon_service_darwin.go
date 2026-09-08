//go:build darwin

package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/huangzheng2016/eTerm/internal/config"
)

const daemonLaunchdLabel = "com.huangzheng2016.eterm.daemon"

func daemonLaunchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", daemonLaunchdLabel+".plist"), nil
}

func daemonLaunchdPlist(programArgs []string, logPath string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + daemonLaunchdLabel + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range programArgs {
		b.WriteString("\t\t<string>" + plistEscape(arg) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + plistEscape(logPath) + "</string>\n")
	b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + plistEscape(logPath) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func plistEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func daemonLaunchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func daemonLaunchdLoaded() bool {
	return exec.Command("launchctl", "print", daemonLaunchdDomain()+"/"+daemonLaunchdLabel).Run() == nil
}

func daemonServiceEnable(opts daemonOptions) error {
	if err := daemonServiceCheckNoPassword(opts); err != nil {
		return err
	}
	if err := config.EnsureConfigDir(); err != nil {
		return err
	}
	args, err := daemonServiceProgramArguments(opts)
	if err != nil {
		return err
	}
	plistPath, err := daemonLaunchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(daemonLaunchdPlist(args, daemonServiceLogPath())), 0644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", daemonLaunchdDomain(), plistPath).Run()
	if err := exec.Command("launchctl", "bootstrap", daemonLaunchdDomain(), plistPath).Run(); err != nil {
		if loadErr := exec.Command("launchctl", "load", "-w", plistPath).Run(); loadErr != nil {
			return fmt.Errorf("launchctl bootstrap failed: %v; fallback load -w failed: %v", err, loadErr)
		}
	}
	return nil
}

func daemonServiceDisable() error {
	plistPath, err := daemonLaunchdPlistPath()
	if err != nil {
		return err
	}
	err = exec.Command("launchctl", "bootout", daemonLaunchdDomain(), plistPath).Run()
	if err != nil {
		err = exec.Command("launchctl", "unload", "-w", plistPath).Run()
	}
	if err != nil && daemonLaunchdLoaded() {
		return fmt.Errorf("launchctl bootout failed: %v", err)
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func daemonServiceStatus() (bool, string) {
	if daemonLaunchdLoaded() {
		return true, "enabled (launchd " + daemonLaunchdLabel + ")"
	}
	plistPath, err := daemonLaunchdPlistPath()
	if err == nil {
		if _, statErr := os.Stat(plistPath); statErr == nil {
			return true, "installed but not loaded (" + plistPath + ")"
		}
	}
	return false, "not installed"
}
