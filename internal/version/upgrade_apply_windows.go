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

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func ScheduleDeferredReplace(newBinAbs, targetAbs string) error {
	dir := filepath.Dir(newBinAbs)
	f, err := os.CreateTemp(dir, "eterm-replace-*.ps1")
	if err != nil {
		return err
	}
	script := f.Name()
	body := fmt.Sprintf(`param([switch]$Elevated)
Start-Sleep -Seconds 3
try {
  Move-Item -LiteralPath %s -Destination %s -Force -ErrorAction Stop
  Remove-Item -LiteralPath $PSCommandPath -Force
} catch {
  if (-not $Elevated) {
    Start-Process powershell.exe -Verb RunAs -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File',('"' + $PSCommandPath + '"'),'-Elevated'
  }
}
`, psQuote(filepath.Clean(newBinAbs)), psQuote(filepath.Clean(targetAbs)))
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		_ = os.Remove(script)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(script)
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(script)
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
