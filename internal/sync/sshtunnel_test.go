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

func parseTunnelTestHost(s string) (user, host string, port int) {
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

// Requires ETERM_SYNC_TEST_HOST (e.g. root@mock.example.com:2222) and an
// etermsyncd HTTP listener on the remote (ETERM_SYNC_TEST_PORT, default 18443).
func TestOpenTunnelPing(t *testing.T) {
	hostEnv := os.Getenv("ETERM_SYNC_TEST_HOST")
	if hostEnv == "" {
		t.Skip("set ETERM_SYNC_TEST_HOST to run SSH tunnel integration test")
	}
	remotePort, _ := strconv.Atoi(os.Getenv("ETERM_SYNC_TEST_PORT"))
	if remotePort <= 0 {
		remotePort = 18443
	}
	apiKey := os.Getenv("ETERM_SYNC_TEST_API_KEY")

	user, hostname, port := parseTunnelTestHost(hostEnv)
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}

	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.SetupNoPassword()

	h := db.Host{
		Username: user,
		Hostname: hostname,
		Port:     port,
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

	algo, fp, err := internalssh.ProbeHostKey(hostname, port, 5*time.Second)
	if err != nil {
		t.Fatalf("probe host key: %v", err)
	}
	if err := database.Create(&db.HostFingerprint{
		Hostname: hostname, Port: port, Algorithm: algo, Fingerprint: fp, TrustedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("trust fingerprint: %v", err)
	}

	if err := database.Create(&h).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}

	tunnel, err := OpenTunnel(database, mk, h.ID, remotePort)
	if err != nil {
		t.Fatalf("open tunnel: %v", err)
	}
	defer tunnel.Close()

	tr := NewHTTPTransportWithOptions(tunnel.BaseURL(), apiKey, "", false)
	defer tr.Close()
	if err := tr.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
