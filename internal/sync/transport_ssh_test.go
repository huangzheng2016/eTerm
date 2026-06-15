package sync

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
)

func parseSyncTestHost(s string) (user, host string, port int) {
	user = "root"
	port = 22
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user = s[:i]
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		host = s[:i]
		if p, err := strconv.Atoi(s[i+1:]); err == nil {
			port = p
		}
	} else {
		host = s
	}
	return
}

func TestSSHTransportPing(t *testing.T) {
	hostEnv := os.Getenv("ETERM_SYNC_TEST_HOST")
	if hostEnv == "" {
		t.Skip("set ETERM_SYNC_TEST_HOST to run SSH sync integration test, e.g. root@mock.example.com:2222")
	}

	user, hostname, port := parseSyncTestHost(hostEnv)
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.SetupNoPassword()

	h := db.Host{
		Username:   user,
		Hostname:   hostname,
		Port:       port,
	}

	keyPath := os.Getenv("ETERM_SYNC_TEST_KEY")
	var sshKey *db.SSHKey
	if keyPath != "" {
		h.AuthMethod = "key"
		sshKey = &db.SSHKey{
			Name:        "test-key",
			Type:        "ssh-key",
			StorageMode: "file",
			PrivatePath: keyPath,
		}
		if err := database.Create(sshKey).Error; err != nil {
			t.Fatalf("create key: %v", err)
		}
		h.KeyID = &sshKey.ID
	} else {
		if os.Getenv("SSH_AUTH_SOCK") == "" {
			t.Skip("SSH_AUTH_SOCK not set; set ETERM_SYNC_TEST_KEY to use a private key file")
		}
		h.AuthMethod = "agent"
	}

	if err := database.Create(&h).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}

	result, err := internalssh.Connect(internalssh.ConnectConfig{
		Host:      &h,
		Key:       sshKey,
		MasterKey: mk,
		DB:        database,
		FingerprintCallback: func(string, int, string, string) bool {
			return true
		},
	})
	if err != nil {
		t.Fatalf("ssh connect: %v", err)
	}
	defer result.Close()

	tr, err := NewSSHTransport(result.Client, result.Closers, "etermsyncd", "~/.config/etermsyncd/sync.db")
	if err != nil {
		t.Fatalf("new ssh transport: %v", err)
	}
	defer tr.Close()

	if err := tr.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
