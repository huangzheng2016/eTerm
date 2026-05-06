package version

import (
	"os"
	"path/filepath"
)

const SettingUpgradeDismissedTag = "upgrade_dismissed_tag"

func RunningExecutablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Clean(p))
}
