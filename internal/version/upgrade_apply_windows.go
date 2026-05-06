//go:build windows

package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func batQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func ScheduleDeferredReplace(newBinAbs, targetAbs string) error {
	dir := filepath.Dir(newBinAbs)
	f, err := os.CreateTemp(dir, "eterm-replace-*.bat")
	if err != nil {
		return err
	}
	bat := f.Name()

	newQ := filepath.Clean(newBinAbs)
	oldQ := filepath.Clean(targetAbs)
	selfQ := filepath.Clean(bat)

	body := fmt.Sprintf(
		"ping 127.0.0.1 -n 4 > nul\r\nmove /Y %s %s\r\ndel /F /Q %s\r\n",
		batQuote(newQ),
		batQuote(oldQ),
		batQuote(selfQ),
	)

	if _, err := f.WriteString("@echo off\r\n"); err != nil {
		f.Close()
		_ = os.Remove(bat)
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		_ = os.Remove(bat)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(bat)
		return err
	}

	cmd := exec.Command("cmd", "/C", bat)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(bat)
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
