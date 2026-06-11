package sync

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.SSHKey{}, &db.Snippet{}, &db.PortForward{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func testMasterKey() *security.MasterKeyManager {
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("test-password"))
	return mk
}

func TestCollectDirtyIncludesDeletedHostTombstone(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	host := db.Host{
		SyncID:     "host-delete",
		Alias:      "delete-me",
		Hostname:   "delete.example.com",
		Port:       22,
		Username:   "root",
		AuthMethod: "agent",
		SyncDel:    true,
	}
	if err := database.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Delete(&host).Error; err != nil {
		t.Fatal(err)
	}

	records, err := CollectDirty(database, mk, "sync-passphrase", "device-a", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].SyncID != "host-delete" || !records[0].Deleted {
		t.Fatalf("got %#v, want deleted host tombstone", records[0])
	}
}

func TestMergeRecordsReportsCreateError(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	existing := db.SSHKey{
		SyncID:         "existing-key",
		Name:           "duplicate",
		Type:           "ed25519",
		PublicKeyData:  "ssh-ed25519 existing",
		Fingerprint:    "fp-existing",
		PrivateKeyData: "existing",
		StorageMode:    "database",
	}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := encryptPayload(SSHKeyDTO{
		SyncID:      "incoming-key",
		Name:        "duplicate",
		Type:        "ed25519",
		PublicKey:   "ssh-ed25519 incoming",
		Fingerprint: "fp-incoming",
	}, "sync-passphrase")
	if err != nil {
		t.Fatal(err)
	}

	res := MergeRecords(database, mk, "sync-passphrase", []SyncRecord{{
		SyncID:    "incoming-key",
		Type:      TypeSSHKey,
		Payload:   payload,
		UpdatedAt: time.Now(),
	}})

	if res.Failed != 1 || res.Merged != 0 {
		t.Fatalf("got merged=%d failed=%d, want merged=0 failed=1", res.Merged, res.Failed)
	}
}
