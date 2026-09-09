//go:build darwin

package clipboardblob

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"golang.design/x/clipboard"
)

func clipboardFilePath() (string, error) {
	if err := clipboard.Init(); err != nil {
		return "", ErrNoBlob
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := clipboard.Read(ctx, clipboard.Register("public.file-url"))
	if err != nil {
		return "", ErrNoBlob
	}
	data := strings.TrimRight(string(raw), "\x00")
	if data == "" {
		return "", ErrNoBlob
	}
	path, err := filePathFromURIList(data)
	if err != nil {
		return "", err
	}
	// Finder may advertise a file-id reference (file:///.file/id=...) instead
	// of the real path; only AppleScript resolves it.
	if strings.HasPrefix(path, "/.file/") {
		return resolveFileRefPath()
	}
	return path, nil
}

func resolveFileRefPath() (string, error) {
	out, err := exec.Command("osascript", "-e", `POSIX path of (the clipboard as «class furl»)`).Output()
	if err != nil {
		return "", ErrNoBlob
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrNoBlob
	}
	return path, nil
}
