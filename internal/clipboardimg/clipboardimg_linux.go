//go:build linux

package clipboardimg

import "os/exec"

func Read() (*Image, error) {
	if data, err := exec.Command("wl-paste", "--type", "image/png", "--no-newline").Output(); err == nil && len(data) > 0 {
		return validate(data)
	}
	if data, err := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output(); err == nil && len(data) > 0 {
		return validate(data)
	}
	return nil, ErrNoImage
}
