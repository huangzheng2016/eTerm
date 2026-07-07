//go:build darwin

package clipboardblob

import (
	"os/exec"
	"strings"
)

func clipboardFilePath() (string, error) {
	out, err := exec.Command("osascript", "-e", `try
set f to the clipboard as «class furl»
return POSIX path of f
on error
return ""
end try`).Output()
	if err != nil {
		return "", ErrNoBlob
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrNoBlob
	}
	return path, nil
}
