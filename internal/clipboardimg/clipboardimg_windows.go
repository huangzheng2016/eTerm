//go:build windows

package clipboardimg

import (
	"os"
	"os/exec"
	"path/filepath"
)

func Read() (*Image, error) {
	tmp := filepath.Join(os.TempDir(), "eterm-clipboard-image.png")
	_ = os.Remove(tmp)
	script := `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $img=[Windows.Forms.Clipboard]::GetImage(); if($img){$img.Save('` + tmp + `',[Drawing.Imaging.ImageFormat]::Png)}`
	if err := exec.Command("powershell", "-NoProfile", "-Command", script).Run(); err != nil {
		return nil, ErrNoImage
	}
	data, err := os.ReadFile(tmp)
	_ = os.Remove(tmp)
	if err != nil {
		return nil, ErrNoImage
	}
	return validate(data)
}
