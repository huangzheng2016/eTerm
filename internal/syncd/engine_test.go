package syncd

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(database)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestPushAssignsUniqueRevisionsUnderConcurrency(t *testing.T) {
	engine := testEngine(t)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := engine.Push("", []SyncEntry{{
				SyncID:    fmt.Sprintf("sync-%02d", i),
				Type:      "host",
				Payload:   "{}",
				DeviceID:  "device-a",
				UpdatedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
			}})
			if err != nil {
				t.Errorf("push %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	entries, rev, err := engine.Pull("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("got %d entries, want 20", len(entries))
	}
	if rev != 20 {
		t.Fatalf("got max revision %d, want 20", rev)
	}
	seen := map[int64]bool{}
	for _, entry := range entries {
		if seen[entry.Revision] {
			t.Fatalf("duplicate revision %d", entry.Revision)
		}
		seen[entry.Revision] = true
	}
}

func TestEngineTenantIsolation(t *testing.T) {
	engine := testEngine(t)
	now := time.Now()

	if _, err := engine.Push("tenant-a", []SyncEntry{{
		SyncID:    "shared-id",
		Type:      "host",
		Payload:   "a",
		DeviceID:  "device-a",
		UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Push("tenant-b", []SyncEntry{{
		SyncID:    "shared-id",
		Type:      "host",
		Payload:   "b",
		DeviceID:  "device-b",
		UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}

	entries, _, err := engine.Pull("tenant-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Payload != "a" || entries[0].Tenant != "tenant-a" {
		t.Fatalf("got %#v, want tenant-a payload", entries[0])
	}
}

func TestNewEngineDropsLegacySyncIDUniqueIndex(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`CREATE TABLE sync_entries (
		id integer primary key autoincrement,
		sync_id text not null,
		type text not null,
		payload text,
		device_id text not null,
		deleted numeric default false,
		revision integer not null,
		updated_at datetime not null
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`CREATE UNIQUE INDEX idx_sync_entries_sync_id ON sync_entries(sync_id)`).Error; err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := engine.Push("tenant-a", []SyncEntry{{SyncID: "same", Type: "host", DeviceID: "a", UpdatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Push("tenant-b", []SyncEntry{{SyncID: "same", Type: "host", DeviceID: "b", UpdatedAt: now}}); err != nil {
		t.Fatal(err)
	}
}

func TestBlobCreateGetDelete(t *testing.T) {
	engine := testEngine(t)
	data := []byte("image-bytes")
	blob, err := engine.CreateBlob("tenant-a", "image/png", "a.png", data)
	if err != nil {
		t.Fatal(err)
	}
	if d := blob.ExpiresAt.Sub(blob.CreatedAt); d != 30*time.Minute {
		t.Fatalf("ttl = %v", d)
	}
	got, err := engine.GetBlob(blob.ID, blob.DownloadToken)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != string(data) || got.Mime != "image/png" {
		t.Fatalf("got %#v", got)
	}
	if _, err := engine.GetBlob(blob.ID, "bad-token"); err != ErrBlobNotFound {
		t.Fatalf("bad token err = %v", err)
	}
	if err := engine.DeleteBlob("tenant-a", blob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.GetBlob(blob.ID, blob.DownloadToken); err != ErrBlobNotFound {
		t.Fatalf("deleted blob err = %v", err)
	}
}

func TestBlobRejectsOver10MiB(t *testing.T) {
	engine := testEngine(t)
	data := make([]byte, MaxBlobBytes+1)
	if _, err := engine.CreateBlob("", "image/png", "big.png", data); err != ErrBlobTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestBlobExpiry(t *testing.T) {
	engine := testEngine(t)
	blob, err := engine.CreateBlob("", "image/png", "a.png", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&BlobEntry{}).Where("id = ?", blob.ID).Update("expires_at", time.Now().UTC().Add(-time.Minute))
	if _, err := engine.GetBlob(blob.ID, blob.DownloadToken); err != ErrBlobNotFound {
		t.Fatalf("expired blob err = %v", err)
	}
}
