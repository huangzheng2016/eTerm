//go:build windows

package clipboardblob

import (
	"os/exec"
	"strings"
)

func clipboardFilePath() (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", `Add-Type -AssemblyName System.Windows.Forms; $files=[Windows.Forms.Clipboard]::GetFileDropList(); if($files -and $files.Count -gt 0){$files[0]}`).Output()
	if err != nil {
		return "", ErrNoBlob
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrNoBlob
	}
	return path, nil
}
