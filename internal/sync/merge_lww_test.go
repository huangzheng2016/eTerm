package sync

import (
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func hostRecord(t *testing.T, syncID, alias string, updatedAt time.Time) SyncRecord {
	t.Helper()
	payload, err := encryptPayload(HostDTO{
		SyncID:   syncID,
		Alias:    alias,
		Hostname: alias + ".example.com",
		Port:     22,
		Username: "root",
	}, "sync-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	return SyncRecord{
		SyncID:    syncID,
		Type:      TypeHost,
		Payload:   payload,
		UpdatedAt: updatedAt,
	}
}

func TestMergeSkipsOlderRecord(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	existing := db.Host{SyncID: "h1", Alias: "new-local", Hostname: "a.example.com", Port: 22}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	older := hostRecord(t, "h1", "old-remote", existing.UpdatedAt.Add(-time.Hour))
	res, err := MergeRecords(database, mk, "sync-passphrase", []SyncRecord{older})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged != 0 || res.Failed != 0 {
		t.Fatalf("got merged=%d failed=%d, want 0/0 (skipped)", res.Merged, res.Failed)
	}

	var got db.Host
	if err := database.First(&got, existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Alias != "new-local" {
		t.Fatalf("alias = %q, want new-local", got.Alias)
	}
}

func TestMergePreservesRemoteUpdatedAt(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	remoteTime := time.Now().Add(-time.Minute).UTC()
	rec := hostRecord(t, "h2", "remote-host", remoteTime)
	res, err := MergeRecords(database, mk, "sync-passphrase", []SyncRecord{rec})
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged != 1 {
		t.Fatalf("merged = %d, want 1", res.Merged)
	}

	var got db.Host
	if err := database.Where("sync_id = ?", "h2").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.Equal(remoteTime) {
		t.Fatalf("updated_at = %v, want %v", got.UpdatedAt, remoteTime)
	}

	// A merged record must not be collected as dirty again (no echo push).
	dirty, err := CollectDirty(database, mk, "sync-passphrase", "device-a", time.Now().Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != 0 {
		t.Fatalf("got %d dirty records, want 0", len(dirty))
	}
}

func TestMergeSkipsOlderTombstone(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	existing := db.Host{SyncID: "h3", Alias: "keep-me", Hostname: "b.example.com", Port: 22}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	_, err := MergeRecords(database, mk, "sync-passphrase", []SyncRecord{{
		SyncID:    "h3",
		Type:      TypeHost,
		Deleted:   true,
		UpdatedAt: existing.UpdatedAt.Add(-time.Hour),
	}})
	if err != nil {
		t.Fatal(err)
	}

	var got db.Host
	if err := database.First(&got, existing.ID).Error; err != nil {
		t.Fatalf("local record should survive older tombstone: %v", err)
	}
}

func TestMergeNewerTombstoneDeletes(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()

	existing := db.Host{SyncID: "h4", Alias: "delete-me", Hostname: "c.example.com", Port: 22}
	if err := database.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	_, err := MergeRecords(database, mk, "sync-passphrase", []SyncRecord{{
		SyncID:    "h4",
		Type:      TypeHost,
		Deleted:   true,
		UpdatedAt: existing.UpdatedAt.Add(time.Hour),
	}})
	if err != nil {
		t.Fatal(err)
	}

	var count int64
	database.Unscoped().Model(&db.Host{}).Where("sync_id = ?", "h4").Count(&count)
	if count != 0 {
		t.Fatalf("got %d rows, want 0", count)
	}
}

func TestCollectDirtyFailsWhenMasterKeyLocked(t *testing.T) {
	database := testDB(t)
	mk := testMasterKey()
	mk.Lock()

	host := db.Host{SyncID: "h5", Alias: "secret", Hostname: "d.example.com", Port: 22, Password: "encrypted"}
	if err := database.Create(&host).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := CollectDirty(database, mk, "sync-passphrase", "device-a", time.Time{}); err == nil {
		t.Fatal("expected error when master key is locked")
	}
}
