package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/keys"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
)

func TestHostFromParsedAuthMethod(t *testing.T) {
	tests := []struct {
		name   string
		parsed sshconfig.ParsedHost
		check  func(t *testing.T, host db.Host, keyID uint)
	}{
		{
			name: "gssapi authentication",
			parsed: sshconfig.ParsedHost{
				Alias: "kerberos", Hostname: "kerberos.example.com", Port: 22, Username: "alice",
				IdentFile: "/tmp/id_test", GSSAPIAuthentication: true,
			},
			check: func(t *testing.T, host db.Host, keyID uint) {
				if host.AuthMethod != "gssapi" {
					t.Fatalf("got auth %q", host.AuthMethod)
				}
				if host.GSSAPISource != "ccache" {
					t.Fatalf("got source %q", host.GSSAPISource)
				}
				if host.KeyID != nil {
					t.Fatalf("expected key to be cleared, got %v", *host.KeyID)
				}
			},
		},
		{
			name: "prefers gssapi when listed first",
			parsed: sshconfig.ParsedHost{
				Alias: "kerberos", Hostname: "kerberos.example.com", Port: 22, Username: "alice",
				IdentFile: "/tmp/id_test", PreferredAuthentications: []string{"gssapi-with-mic", "publickey"},
			},
			check: func(t *testing.T, host db.Host, keyID uint) {
				if host.AuthMethod != "gssapi" {
					t.Fatalf("got auth %q", host.AuthMethod)
				}
			},
		},
		{
			name: "prefers publickey when listed first",
			parsed: sshconfig.ParsedHost{
				Alias: "kerberos", Hostname: "kerberos.example.com", Port: 22, Username: "alice",
				IdentFile: "/tmp/id_test", PreferredAuthentications: []string{"publickey", "gssapi-with-mic"},
			},
			check: func(t *testing.T, host db.Host, keyID uint) {
				if host.AuthMethod != "key" {
					t.Fatalf("got auth %q", host.AuthMethod)
				}
				if host.KeyID == nil || *host.KeyID != keyID {
					t.Fatalf("got key id %#v want %d", host.KeyID, keyID)
				}
			},
		},
		{
			name: "identity file without gssapi",
			parsed: sshconfig.ParsedHost{
				Alias: "plain", Hostname: "plain.example.com", Port: 22, Username: "alice",
				IdentFile: "/tmp/id_test",
			},
			check: func(t *testing.T, host db.Host, keyID uint) {
				if host.AuthMethod != "key" {
					t.Fatalf("got auth %q", host.AuthMethod)
				}
				if host.KeyID == nil || *host.KeyID != keyID {
					t.Fatalf("got key id %#v want %d", host.KeyID, keyID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := appTestDB(t)
			key := db.SSHKey{
				SyncID:      "key-test",
				Name:        "id_test",
				Type:        "ed25519",
				Fingerprint: "fp-test",
				PrivatePath: "/tmp/id_test",
			}
			if err := database.Create(&key).Error; err != nil {
				t.Fatal(err)
			}

			host := hostFromParsed(database, tt.parsed)
			tt.check(t, host, key.ID)
		})
	}
}

func TestBuildSSHConfigImportPreviewCountsAddedChangedSkipped(t *testing.T) {
	database := appTestDB(t)

	same := db.Host{Alias: "same", Hostname: "same.example.com", Port: 22, Username: "root", AuthMethod: "agent"}
	changed := db.Host{Alias: "changed", Hostname: "old.example.com", Port: 22, Username: "root", AuthMethod: "agent"}
	if err := database.Create(&same).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&changed).Error; err != nil {
		t.Fatal(err)
	}

	preview := buildSSHConfigImportPreview(database, []sshconfig.ParsedHost{
		{Alias: "same", Hostname: "same.example.com", Port: 22, Username: "root"},
		{Alias: "changed", Hostname: "new.example.com", Port: 22, Username: "root"},
		{Alias: "new", Hostname: "new.example.com", Port: 2222, Username: "deploy"},
	})

	if preview.Added != 1 || preview.Changed != 1 || preview.Skipped != 1 {
		t.Fatalf("got added=%d changed=%d skipped=%d", preview.Added, preview.Changed, preview.Skipped)
	}
}

func TestImportSSHConfigImportsIdentityFileKeyAndHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	fp := writeTestPrivateKey(t, keyPath)
	config := []byte(`
Host prod
  HostName prod.example.com
  User deploy
  IdentityFile ~/.ssh/id_ed25519
`)
	if err := os.WriteFile(filepath.Join(sshDir, "config"), config, 0600); err != nil {
		t.Fatal(err)
	}
	database := appTestDB(t)

	msg := importSSHConfig(database, "skip")
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if msg.Imported != 1 || msg.KeysImported != 1 {
		t.Fatalf("got hosts=%d keys=%d", msg.Imported, msg.KeysImported)
	}

	var key db.SSHKey
	if err := database.Where("private_path = ?", keyPath).First(&key).Error; err != nil {
		t.Fatal(err)
	}
	if key.StorageMode != "file" || key.Fingerprint != fp {
		t.Fatalf("got mode=%q fp=%q", key.StorageMode, key.Fingerprint)
	}
	var host db.Host
	if err := database.Where("alias = ?", "prod").First(&host).Error; err != nil {
		t.Fatal(err)
	}
	if host.AuthMethod != "key" || host.KeyID == nil || *host.KeyID != key.ID {
		t.Fatalf("got auth=%q key=%v want key id %d", host.AuthMethod, host.KeyID, key.ID)
	}
}

func TestImportSSHConfigImportsStandaloneSSHKeyWithoutConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	writeTestPrivateKey(t, keyPath)
	database := appTestDB(t)

	msg := importSSHConfig(database, "skip")
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if msg.Imported != 0 || msg.KeysImported != 1 {
		t.Fatalf("got hosts=%d keys=%d", msg.Imported, msg.KeysImported)
	}
}

func writeTestPrivateKey(t *testing.T, path string) string {
	t.Helper()
	privateKey, publicKey, fingerprint, err := keys.GenerateED25519()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, privateKey, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", []byte(publicKey), 0644); err != nil {
		t.Fatal(err)
	}
	return fingerprint
}
