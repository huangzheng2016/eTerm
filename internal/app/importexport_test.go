package app

import (
	"path/filepath"
	"testing"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/sshconfig"
)

func TestHostFromParsedImportsGSSAPIAuthentication(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "import.db"))
	if err != nil {
		t.Fatal(err)
	}

	key := db.SSHKey{
		SyncID:      "key-gssapi",
		Name:        "id_gssapi",
		Type:        "ed25519",
		Fingerprint: "fp-gssapi",
		PrivatePath: "/tmp/id_gssapi",
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	host := hostFromParsed(database, sshconfig.ParsedHost{
		Alias:                "kerberos",
		Hostname:             "kerberos.example.com",
		Port:                 22,
		Username:             "alice",
		IdentFile:            "/tmp/id_gssapi",
		GSSAPIAuthentication: true,
	})

	if host.AuthMethod != "gssapi" {
		t.Fatalf("got auth %q", host.AuthMethod)
	}
	if host.GSSAPISource != "ccache" {
		t.Fatalf("got source %q", host.GSSAPISource)
	}
	if host.KeyID != nil {
		t.Fatalf("expected key to be cleared, got %v", *host.KeyID)
	}
}

func TestHostFromParsedPrefersGSSAPIWhenListedFirst(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "prefer-gssapi.db"))
	if err != nil {
		t.Fatal(err)
	}

	key := db.SSHKey{
		SyncID:      "key-prefer-gssapi",
		Name:        "id_prefer_gssapi",
		Type:        "ed25519",
		Fingerprint: "fp-prefer-gssapi",
		PrivatePath: "/tmp/id_prefer_gssapi",
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	host := hostFromParsed(database, sshconfig.ParsedHost{
		Alias:                    "kerberos",
		Hostname:                 "kerberos.example.com",
		Port:                     22,
		Username:                 "alice",
		IdentFile:                "/tmp/id_prefer_gssapi",
		PreferredAuthentications: []string{"gssapi-with-mic", "publickey"},
	})

	if host.AuthMethod != "gssapi" {
		t.Fatalf("got auth %q", host.AuthMethod)
	}
}

func TestHostFromParsedPrefersPublicKeyWhenListedFirst(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "prefer-key.db"))
	if err != nil {
		t.Fatal(err)
	}

	key := db.SSHKey{
		SyncID:      "key-prefer-publickey",
		Name:        "id_prefer_publickey",
		Type:        "ed25519",
		Fingerprint: "fp-prefer-publickey",
		PrivatePath: "/tmp/id_prefer_publickey",
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	host := hostFromParsed(database, sshconfig.ParsedHost{
		Alias:                    "kerberos",
		Hostname:                 "kerberos.example.com",
		Port:                     22,
		Username:                 "alice",
		IdentFile:                "/tmp/id_prefer_publickey",
		PreferredAuthentications: []string{"publickey", "gssapi-with-mic"},
	})

	if host.AuthMethod != "key" {
		t.Fatalf("got auth %q", host.AuthMethod)
	}
	if host.KeyID == nil || *host.KeyID != key.ID {
		t.Fatalf("got key id %#v want %d", host.KeyID, key.ID)
	}
}

func TestHostFromParsedUsesIdentityFileWithoutGSSAPI(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}

	key := db.SSHKey{
		SyncID:      "key-identity",
		Name:        "id_identity",
		Type:        "ed25519",
		Fingerprint: "fp-identity",
		PrivatePath: "/tmp/id_identity",
	}
	if err := database.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	host := hostFromParsed(database, sshconfig.ParsedHost{
		Alias:     "plain",
		Hostname:  "plain.example.com",
		Port:      22,
		Username:  "alice",
		IdentFile: "/tmp/id_identity",
	})

	if host.AuthMethod != "key" {
		t.Fatalf("got auth %q", host.AuthMethod)
	}
	if host.KeyID == nil || *host.KeyID != key.ID {
		t.Fatalf("got key id %#v want %d", host.KeyID, key.ID)
	}
}
