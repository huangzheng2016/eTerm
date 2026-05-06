//go:build !windows

package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func ScheduleDeferredReplace(newBinAbs, targetAbs string) error {
	dir := filepath.Dir(newBinAbs)
	f, err := os.CreateTemp(dir, "eterm-replace-*.sh")
	if err != nil {
		return err
	}
	scriptPath := f.Name()

	body := fmt.Sprintf(`#!/bin/sh
sleep 3
chmod 0755 %s
mv -f %s %s
chmod 0755 %s
rm -f %s
`, strconv.Quote(newBinAbs), strconv.Quote(newBinAbs), strconv.Quote(targetAbs), strconv.Quote(targetAbs), strconv.Quote(scriptPath))

	if _, err := f.WriteString(body); err != nil {
		f.Close()
		_ = os.Remove(scriptPath)
		return err
	}
	if err := f.Chmod(0o755); err != nil {
		f.Close()
		_ = os.Remove(scriptPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}

	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
