package tmux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

const SettingConfigFile = "tmux_config_file"

const managedConfig = `set -g mouse on
set -g mode-keys vi
set -g set-clipboard on
set -as terminal-features ',*:clipboard'
bind -T copy-mode-vi MouseDragEnd1Pane send -X copy-selection-and-cancel
bind -T copy-mode MouseDragEnd1Pane send -X copy-selection-and-cancel
bind -T copy-mode-vi y send -X copy-selection-and-cancel
bind -T copy-mode y send -X copy-selection-and-cancel
set -g extended-keys on
set -g extended-keys-format csi-u
`

func ResolveConfig(database *gorm.DB, configDir, homeDir string) (string, error) {
	configured, err := db.GetSetting(database, SettingConfigFile)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if strings.HasPrefix(configured, "~/") {
			return filepath.Join(homeDir, configured[2:]), nil
		}
		return configured, nil
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "tmux.conf")
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 {
		if content, err := os.ReadFile(path); err == nil && string(content) == managedConfig {
			return path, nil
		}
	}
	tmp, err := os.CreateTemp(configDir, ".tmux.conf-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(managedConfig); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}
