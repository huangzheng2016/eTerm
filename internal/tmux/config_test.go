package tmux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

func TestResolveConfigCreatesManagedDefault(t *testing.T) {
	database := testConfigDB(t)
	dir := t.TempDir()
	path, err := ResolveConfig(database, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "tmux.conf") {
		t.Fatalf("path = %q", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != managedConfig {
		t.Fatalf("content = %q", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestResolveConfigRefreshesManagedDefault(t *testing.T) {
	database := testConfigDB(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveConfig(database, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(b) != managedConfig || info.Mode().Perm() != 0600 {
		t.Fatalf("content = %q mode = %o", b, info.Mode().Perm())
	}
}

func TestResolveConfigReusesMatchingManagedDefault(t *testing.T) {
	database := testConfigDB(t)
	dir := t.TempDir()
	path, err := ResolveConfig(database, dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveConfig(database, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("matching config was replaced")
	}
}

func TestResolveConfigReplacesSymlinkWithoutChangingTarget(t *testing.T) {
	database := testConfigDB(t)
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tmux.conf")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveConfig(database, dir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("mode = %v", info.Mode())
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "keep" {
		t.Fatalf("target = %q", b)
	}
}

func TestResolveConfigCustomPath(t *testing.T) {
	database := testConfigDB(t)
	if err := db.SetSetting(database, SettingConfigFile, "  ~/custom.conf  "); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	path, err := ResolveConfig(database, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(home, "custom.conf") {
		t.Fatalf("path = %q", path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("custom config was created: %v", err)
	}
}

func TestResolveConfigLeavesOtherCustomPathUnchanged(t *testing.T) {
	database := testConfigDB(t)
	if err := db.SetSetting(database, SettingConfigFile, "  relative/tmux.conf  "); err != nil {
		t.Fatal(err)
	}
	path, err := ResolveConfig(database, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if path != "relative/tmux.conf" {
		t.Fatalf("path = %q", path)
	}
}

func TestResolveConfigReturnsSettingReadError(t *testing.T) {
	database := testConfigDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveConfig(database, t.TempDir(), t.TempDir())
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func testConfigDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "eterm.db"))
	if err != nil {
		t.Fatal(err)
	}
	return database
}
