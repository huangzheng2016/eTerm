package daemon

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"golang.org/x/crypto/ssh"
)

func startTestSSHServer(t *testing.T) (port int, fingerprint string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConn(conn, cfg)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ssh.FingerprintSHA256(signer.PublicKey())
}

func serveTestSSHConn(conn net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		sch, sreqs, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer sch.Close()
			for req := range sreqs {
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
			}
		}()
	}
}

func testHostRuntime(t *testing.T, port int) *runtimeConfig {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	secKey := mk.GetKey()
	enc, err := security.Encrypt([]byte("secret"), secKey.Bytes())
	secKey.Clear()
	if err != nil {
		t.Fatal(err)
	}
	host := db.Host{
		SyncID:     "h1",
		Hostname:   "127.0.0.1",
		Port:       port,
		Username:   "tester",
		AuthMethod: "password",
		Password:   enc,
	}
	if err := database.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	return &runtimeConfig{db: database, mk: mk}
}

func TestOpenHostRejectsUnknownFingerprint(t *testing.T) {
	port, _ := startTestSSHServer(t)
	rt := testHostRuntime(t, port)

	_, err := openHost(rt, "h1", 24, 80)
	if err == nil || !strings.Contains(err.Error(), "connect directly from the TUI") {
		t.Fatalf("err = %v, want TUI confirmation hint", err)
	}
	var n int64
	rt.db.Model(&db.HostFingerprint{}).Count(&n)
	if n != 0 {
		t.Fatalf("fingerprint was stored without user confirmation")
	}
}

func TestOpenHostAcceptsTrustedFingerprint(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	if err := rt.db.Create(&db.HostFingerprint{
		Hostname:    "127.0.0.1",
		Port:        port,
		Algorithm:   "ssh-ed25519",
		Fingerprint: fp,
		TrustedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	is, err := openHost(rt, "h1", 24, 80)
	if err != nil {
		t.Fatalf("trusted fingerprint rejected: %v", err)
	}
	_ = is.Close()
}

func TestOpenHostRejectsChangedFingerprint(t *testing.T) {
	port, _ := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	if err := rt.db.Create(&db.HostFingerprint{
		Hostname:    "127.0.0.1",
		Port:        port,
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		TrustedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err := openHost(rt, "h1", 24, 80)
	if err == nil || !strings.Contains(err.Error(), "HOST IDENTIFICATION HAS CHANGED") {
		t.Fatalf("err = %v, want host key changed warning", err)
	}
}
