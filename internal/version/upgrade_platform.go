package version

import (
	"fmt"
	"runtime"
)

var ErrUnsupportedPlatform = fmt.Errorf("no prebuilt release for this OS/CPU (try building from source)")

// ReleaseArchiveNames returns CI artifact basename (without extension) and archive filename.
func ReleaseArchiveNames() (archive string, inner string, ok bool) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch goos {
	case "linux":
		if goarch != "amd64" {
			return "", "", false
		}
		return "eterm_linux_amd64.tar.gz", "eterm_linux_amd64", true
	case "darwin":
		switch goarch {
		case "amd64":
			return "eterm_darwin_amd64.tar.gz", "eterm_darwin_amd64", true
		case "arm64":
			return "eterm_darwin_arm64.tar.gz", "eterm_darwin_arm64", true
		default:
			return "", "", false
		}
	case "windows":
		if goarch != "amd64" {
			return "", "", false
		}
		return "eterm_windows_amd64.zip", "eterm_windows_amd64.exe", true
	default:
		return "", "", false
	}
}
