package syncd

import (
	"testing"
	"time"
)

func TestHostMetasFiltersTenantDeletedAndSorts(t *testing.T) {
	engine := testEngine(t)
	now := time.Now()
	if _, err := engine.Push("tenant-a", []SyncEntry{
		{SyncID: "b", Type: "host", Meta: `{"sync_id":"b","alias":"beta","hostname":"b.example","port":22,"username":"root"}`, DeviceID: "d", UpdatedAt: now},
		{SyncID: "a", Type: "host", Meta: `{"sync_id":"a","alias":"alpha","hostname":"a.example","port":22,"username":"root"}`, DeviceID: "d", UpdatedAt: now},
		{SyncID: "deleted", Type: "host", Deleted: true, Meta: `{"sync_id":"deleted","alias":"gone"}`, DeviceID: "d", UpdatedAt: now},
		{SyncID: "old", Type: "host", DeviceID: "d", UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Push("tenant-b", []SyncEntry{
		{SyncID: "other", Type: "host", Meta: `{"sync_id":"other","alias":"aardvark"}`, DeviceID: "d", UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	hosts, err := engine.HostMetas("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2", len(hosts))
	}
	if hosts[0].Alias != "alpha" || hosts[1].Alias != "beta" {
		t.Fatalf("got %#v, want alpha then beta", hosts)
	}
}
