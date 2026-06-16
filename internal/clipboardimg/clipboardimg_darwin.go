//go:build darwin

package clipboardimg

import (
	"os"
	"os/exec"
	"path/filepath"
)

func Read() (*Image, error) {
	tmp := filepath.Join(os.TempDir(), "eterm-clipboard-image.png")
	_ = os.Remove(tmp)
	script := `try
set theData to the clipboard as «class PNGf»
set theFile to open for access POSIX file "` + tmp + `" with write permission
write theData to theFile
close access theFile
on error
try
close access POSIX file "` + tmp + `"
end try
return ""
end try`
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return nil, ErrNoImage
	}
	data, err := os.ReadFile(tmp)
	_ = os.Remove(tmp)
	if err != nil {
		return nil, ErrNoImage
	}
	return validate(data)
}
