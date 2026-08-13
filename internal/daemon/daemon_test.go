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

func TestLoadRuntimeRejectsSSHSyncModeWithoutHost(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "no SSH host") {
		t.Fatalf("got %v, want no SSH host error", err)
	}
}

func TestQueueInputDropsOldestWhenFull(t *testing.T) {
	sr := &streamRelay{input: make(chan []byte, 2), stop: make(chan struct{})}
	sr.queueInput([]byte("a"))
	sr.queueInput([]byte("b"))
	sr.queueInput([]byte("c"))
	if got := <-sr.input; string(got) != "b" {
		t.Fatalf("first queued = %q, want b (oldest dropped)", got)
	}
	if got := <-sr.input; string(got) != "c" {
		t.Fatalf("second queued = %q, want c", got)
	}
}
