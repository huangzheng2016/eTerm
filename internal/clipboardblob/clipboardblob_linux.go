//go:build linux

package clipboardblob

import "os/exec"

func clipboardFilePath() (string, error) {
	if out, err := exec.Command("wl-paste", "--type", "text/uri-list", "--no-newline").Output(); err == nil {
		if path, err := filePathFromURIList(string(out)); err == nil {
			return path, nil
		}
	}
	if out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o").Output(); err == nil {
		if path, err := filePathFromURIList(string(out)); err == nil {
			return path, nil
		}
	}
	return "", ErrNoBlob
}
