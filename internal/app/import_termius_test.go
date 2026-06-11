package app

import (
	"path/filepath"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
)

func TestBuildHostItems_ExactDuplicate(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	existing := db.Host{
		SyncID:     "h1",
		Alias:      "prod",
		Hostname:   "1.2.3.4",
		Port:       22,
		Username:   "root",
		AuthMethod: "agent",
	}
	database.Create(&existing)

	hosts := []parser.HostRecord{
		{Aliases: []string{"prod"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !items[0].blocked {
		t.Error("expected exact duplicate to be blocked")
	}
	if items[0].nameConflict {
		t.Error("exact duplicate should not be nameConflict")
	}
}

func TestBuildHostItems_NameConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	existing := db.Host{
		SyncID:     "h2",
		Alias:      "prod",
		Hostname:   "9.9.9.9",
		Port:       22,
		Username:   "admin",
		AuthMethod: "agent",
	}
	database.Create(&existing)

	hosts := []parser.HostRecord{
		{Aliases: []string{"prod"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if items[0].blocked {
		t.Error("name conflict should not be blocked")
	}
	if !items[0].nameConflict {
		t.Error("expected nameConflict=true")
	}
}

func TestBuildHostItems_NoConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	hosts := []parser.HostRecord{
		{Aliases: []string{"new-host"}, Host: "1.2.3.4", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	if items[0].blocked || items[0].nameConflict {
		t.Error("expected no conflict for new host")
	}
}

func TestBuildKeyItems_ExactDuplicate(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.Create(&db.SSHKey{
		SyncID:      "k1",
		Name:        "deploy",
		Type:        "ssh-ed25519",
		Fingerprint: "SHA256:AAAA",
	})
	keys := []parser.KeyRecord{
		{Aliases: []string{"deploy"}, PrivateKey: ""},
	}
	items := buildKeyItemsWithFP(database, keys, []string{"SHA256:AAAA"})
	if !items[0].blocked {
		t.Error("expected exact duplicate key to be blocked")
	}
}

func TestBuildKeyItems_NameConflict(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.Create(&db.SSHKey{
		SyncID:      "k2",
		Name:        "deploy",
		Type:        "ssh-ed25519",
		Fingerprint: "SHA256:BBBB",
	})
	keys := []parser.KeyRecord{
		{Aliases: []string{"deploy"}, PrivateKey: ""},
	}
	items := buildKeyItemsWithFP(database, keys, []string{"SHA256:CCCC"})
	if items[0].blocked {
		t.Error("name conflict should not be blocked")
	}
	if !items[0].nameConflict {
		t.Error("expected nameConflict=true")
	}
}

func TestComputeKeyFingerprint_InvalidKey(t *testing.T) {
	fp := computeKeyFingerprint("not a valid key")
	if fp != "" {
		t.Errorf("expected empty fingerprint for invalid key, got %q", fp)
	}
}
