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
		switch goarch {
		case "amd64", "arm64", "386":
			return fmt.Sprintf("eterm_linux_%s.tar.gz", goarch), fmt.Sprintf("eterm_linux_%s", goarch), true
		default:
			return "", "", false
		}
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
		if goarch != "amd64" && goarch != "arm64" {
			return "", "", false
		}
		return fmt.Sprintf("eterm_windows_%s.zip", goarch), fmt.Sprintf("eterm_windows_%s.exe", goarch), true
	default:
		return "", "", false
	}
}
