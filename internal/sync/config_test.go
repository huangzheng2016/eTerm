package sync

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
)

func TestLoadConfigDefaultsRemotePort(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)

	cfg := LoadConfig(database, mk)
	if cfg.RemotePort != 18443 {
		t.Fatalf("remote port = %d, want 18443", cfg.RemotePort)
	}
	if cfg.Mode != "http" {
		t.Fatalf("mode = %q, want http", cfg.Mode)
	}
}
