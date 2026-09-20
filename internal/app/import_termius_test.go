package app

import (
	"path/filepath"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
	"gorm.io/gorm"
)

func appTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func TestBuildHostItemsConflict(t *testing.T) {
	tests := []struct {
		name         string
		alias        string
		existing     *db.Host
		wantBlocked  bool
		wantConflict bool
	}{
		{
			name:  "exact duplicate",
			alias: "prod",
			existing: &db.Host{
				SyncID: "h1", Alias: "prod", Hostname: "1.2.3.4", Port: 22, Username: "root", AuthMethod: "agent",
			},
			wantBlocked: true,
		},
		{
			name:  "name conflict",
			alias: "prod",
			existing: &db.Host{
				SyncID: "h2", Alias: "prod", Hostname: "9.9.9.9", Port: 22, Username: "admin", AuthMethod: "agent",
			},
			wantConflict: true,
		},
		{name: "no conflict", alias: "new-host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := appTestDB(t)
			if tt.existing != nil {
				database.Create(tt.existing)
			}
			items := buildHostItems(database, []parser.HostRecord{
				{Aliases: []string{tt.alias}, Host: "1.2.3.4", Port: 22, Username: "root"},
			})
			if len(items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(items))
			}
			if items[0].blocked != tt.wantBlocked {
				t.Errorf("blocked = %v, want %v", items[0].blocked, tt.wantBlocked)
			}
			if items[0].nameConflict != tt.wantConflict {
				t.Errorf("nameConflict = %v, want %v", items[0].nameConflict, tt.wantConflict)
			}
		})
	}
}

func TestBuildHostItems_DefaultAliasAndSort(t *testing.T) {
	database := appTestDB(t)
	hosts := []parser.HostRecord{
		{Aliases: []string{"zeta"}, Host: "1.2.3.4", Port: 22, Username: "root"},
		{Host: "2.3.4.5", Port: 22, Username: "root"},
		{Aliases: []string{"alpha"}, Host: "3.4.5.6", Port: 22, Username: "root"},
	}
	items := buildHostItems(database, hosts)
	got := []string{items[0].chosenAlias, items[1].chosenAlias, items[2].chosenAlias}
	want := []string{"<UNKNOWN>", "alpha", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected sorted aliases %v, got %v", want, got)
		}
	}
}

func TestImportHostListViewFitsWindowHeight(t *testing.T) {
	items := make([]importHostEntry, 40)
	for i := range items {
		items[i] = importHostEntry{
			rec: parser.HostRecord{
				Aliases:  []string{"host"},
				Host:     "192.168.1.1",
				Port:     22,
				Username: "root",
			},
			chosenAlias: "host",
		}
	}
	m := newImportHostList(items)
	m.setPageSize(24)

	if h := lipgloss.Height(m.View()); h > 24 {
		t.Fatalf("view height %d exceeds window height 24", h)
	}
}

func TestBuildKeyItemsConflict(t *testing.T) {
	tests := []struct {
		name           string
		alias          string
		existing       *db.SSHKey
		fingerprints   []string
		wantBlocked    bool
		wantConflict   bool
		wantExistingID bool
	}{
		{
			name:  "exact duplicate",
			alias: "deploy",
			existing: &db.SSHKey{
				SyncID: "k1", Name: "deploy", Type: "ssh-ed25519", Fingerprint: "SHA256:AAAA",
			},
			fingerprints: []string{"SHA256:AAAA"},
			wantBlocked:  true,
		},
		{
			name:  "existing fingerprint different name",
			alias: "termius-deploy",
			existing: &db.SSHKey{
				SyncID: "k1", Name: "local-deploy", Type: "ssh-ed25519", Fingerprint: "SHA256:AAAA",
			},
			fingerprints:   []string{"SHA256:AAAA"},
			wantBlocked:    true,
			wantExistingID: true,
		},
		{
			name:  "name conflict",
			alias: "deploy",
			existing: &db.SSHKey{
				SyncID: "k2", Name: "deploy", Type: "ssh-ed25519", Fingerprint: "SHA256:BBBB",
			},
			fingerprints: []string{"SHA256:CCCC"},
			wantConflict: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := appTestDB(t)
			database.Create(tt.existing)
			items := buildKeyItemsWithFP(database, []parser.KeyRecord{
				{Aliases: []string{tt.alias}, PrivateKey: ""},
			}, tt.fingerprints)
			if items[0].blocked != tt.wantBlocked {
				t.Errorf("blocked = %v, want %v", items[0].blocked, tt.wantBlocked)
			}
			if items[0].nameConflict != tt.wantConflict {
				t.Errorf("nameConflict = %v, want %v", items[0].nameConflict, tt.wantConflict)
			}
			if tt.wantExistingID && items[0].existingID != tt.existing.ID {
				t.Errorf("existingID = %d, want %d", items[0].existingID, tt.existing.ID)
			}
		})
	}
}

func TestRunTermiusImport_UsesExistingDuplicateKeyForHost(t *testing.T) {
	database := appTestDB(t)
	existing := db.SSHKey{
		SyncID:      "k1",
		Name:        "local-deploy",
		Type:        "ssh-ed25519",
		Fingerprint: "SHA256:AAAA",
	}
	database.Create(&existing)

	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("test-password"))

	hosts := []importHostEntry{
		{
			rec:         parser.HostRecord{Aliases: []string{"prod"}, Host: "1.2.3.4", Port: 22, Username: "root", KeyName: "termius-deploy"},
			selected:    true,
			chosenAlias: "prod",
		},
	}
	keys := []importKeyEntry{
		{
			rec:         parser.KeyRecord{Aliases: []string{"termius-deploy"}, PrivateKey: ""},
			blocked:     true,
			chosenAlias: "termius-deploy",
			fingerprint: "SHA256:AAAA",
			existingID:  existing.ID,
		},
	}
	msg := runTermiusImport(database, mk, hosts, keys)().(termiusImportResultMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}

	var host db.Host
	if err := database.Where("alias = ?", "prod").First(&host).Error; err != nil {
		t.Fatal(err)
	}
	if host.KeyID == nil || *host.KeyID != existing.ID {
		t.Fatalf("expected host to use key ID %d, got %v", existing.ID, host.KeyID)
	}
}

func TestComputeKeyFingerprint_InvalidKey(t *testing.T) {
	fp := computeKeyFingerprint("not a valid key")
	if fp != "" {
		t.Errorf("expected empty fingerprint for invalid key, got %q", fp)
	}
}
