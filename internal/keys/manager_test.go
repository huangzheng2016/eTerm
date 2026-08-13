package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&db.SSHKey{}); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCreateKeyFileRejectsPathSeparatorName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := CreateKey(testDB(t), nil, "../evil", "ed25519", 0, "", "file")
	if err == nil || !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("expected path separator error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "evil")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written outside .ssh, stat err=%v", statErr)
	}
}

func TestCreateKeyFileValidName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	k, err := CreateKey(testDB(t), nil, "etest-key", "ed25519", 0, "", "file")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".ssh", "etest-key")
	if k.PrivatePath != want {
		t.Fatalf("PrivatePath = %q, want %q", k.PrivatePath, want)
	}
	if _, err := os.Stat(k.PrivatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(k.PublicPath); err != nil {
		t.Fatal(err)
	}
}

func TestImportPrivateKeyRecordRejectsPathSeparatorName(t *testing.T) {
	pem, _, _, err := GenerateED25519()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err = importPrivateKeyRecord(testDB(t), nil, "../evil", pem, "file", "", "")
	if err == nil || !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("expected path separator error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "evil")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written outside .ssh, stat err=%v", statErr)
	}
}
