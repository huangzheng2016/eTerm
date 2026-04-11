package config

import (
	"os"
	"path/filepath"
)

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "eterm")
}

func DBPath() string {
	return filepath.Join(ConfigDir(), "eterm.db")
}

func EnsureConfigDir() error {
	return os.MkdirAll(ConfigDir(), 0700)
}
