package daemon

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
)

func TestLoadRuntimeRejectsSSHSyncMode(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	salt, verifier := mk.Setup([]byte("pw"))
	_ = db.SetSetting(database, "encryption_salt", base64.StdEncoding.EncodeToString(salt))
	_ = db.SetSetting(database, "encryption_verifier", base64.StdEncoding.EncodeToString(verifier))
	_ = db.SetSetting(database, "sync_enabled", "true")
	_ = db.SetSetting(database, "sync_mode", "ssh")

	_, err = loadRuntime(database, Config{Password: "pw"})
	if err == nil || !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("got %v, want HTTP mode error", err)
	}
}
