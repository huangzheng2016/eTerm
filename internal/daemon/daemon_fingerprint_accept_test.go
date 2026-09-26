package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
)

func openHostFrame(t *testing.T, rt *runtimeConfig, sender *frameSender, mgr *sessionManager, streamID uint32, req relay.OpenRequest) {
	t.Helper()
	payload, _ := json.Marshal(req)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	handleOpen(rt, relay.Frame{Type: relay.FrameOpen, StreamID: streamID, Payload: payload}, mgr, sender, ctx, ctx)
}

func TestHandleOpenHostFingerprintUnconfirmedStructuredPayload(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	if err := rt.db.Model(&db.Host{}).Where("sync_id = ?", "h1").Update("alias", "web-1").Error; err != nil {
		t.Fatal(err)
	}
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 7, relay.OpenRequest{Target: relay.TargetHost, HostSyncID: "h1", Rows: 24, Cols: 80})

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	var p relay.OpenErrPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("payload is not structured JSON: %v (%q)", err, f.Payload)
	}
	if p.Code != relay.CodeFingerprintUnconfirmed {
		t.Fatalf("code = %q, want %q", p.Code, relay.CodeFingerprintUnconfirmed)
	}
	if !strings.Contains(p.Message, "connect directly from the TUI") {
		t.Fatalf("message = %q, want legacy TUI hint", p.Message)
	}
	if p.HostSyncID != "h1" || p.Alias != "web-1" || p.Hostname != "127.0.0.1" || p.Port != port {
		t.Fatalf("host fields = %+v", p)
	}
	if p.Fingerprint != fp || p.Alg != "ssh-ed25519" {
		t.Fatalf("key fields = %+v", p)
	}
	var n int64
	rt.db.Model(&db.HostFingerprint{}).Count(&n)
	if n != 0 {
		t.Fatalf("fingerprint was stored without confirmation")
	}
}

func TestHandleOpenHostFingerprintAccept(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 8, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "h1",
		Hostname: "127.0.0.1", Port: port,
		Fingerprint: fp, Alg: "ssh-ed25519",
	})
	waitDaemonFrame(t, out, relay.FrameOpenOK)
	waitDaemonFrame(t, out, relay.FrameClose)

	var stored db.HostFingerprint
	if err := rt.db.Where("hostname = ? AND port = ?", "127.0.0.1", port).First(&stored).Error; err != nil {
		t.Fatalf("fingerprint not stored: %v", err)
	}
	if stored.Fingerprint != fp || stored.Algorithm != "ssh-ed25519" {
		t.Fatalf("stored = %+v", stored)
	}

	openHostFrame(t, rt, sender, mgr, 9, relay.OpenRequest{Target: relay.TargetHost, HostSyncID: "h1", Rows: 24, Cols: 80})
	waitDaemonFrame(t, out, relay.FrameOpenOK)
}

func TestHandleOpenHostFingerprintAcceptRejectsWrongFingerprint(t *testing.T) {
	port, _ := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 10, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "h1",
		Hostname: "127.0.0.1", Port: port,
		Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Alg: "ssh-ed25519",
	})
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "does not match") {
		t.Fatalf("payload = %q", f.Payload)
	}
	var n int64
	rt.db.Model(&db.HostFingerprint{}).Count(&n)
	if n != 0 {
		t.Fatalf("wrong fingerprint was stored")
	}
}

func TestHandleOpenHostFingerprintAcceptRejectsUnknownSyncID(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 11, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "nope",
		Hostname: "127.0.0.1", Port: port,
		Fingerprint: fp, Alg: "ssh-ed25519",
	})
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "unknown host_sync_id") {
		t.Fatalf("payload = %q", f.Payload)
	}
}

func TestHandleOpenHostFingerprintAcceptRejectsForeignPeer(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 12, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "h1",
		Hostname: "127.0.0.1", Port: port + 1,
		Fingerprint: fp, Alg: "ssh-ed25519",
	})
	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "does not match the host or its jump host") {
		t.Fatalf("payload = %q", f.Payload)
	}
	var n int64
	rt.db.Model(&db.HostFingerprint{}).Count(&n)
	if n != 0 {
		t.Fatalf("foreign peer fingerprint was stored")
	}
}

func TestHandleOpenHostFingerprintAcceptUpdatesChangedRecord(t *testing.T) {
	port, fp := startTestSSHServer(t)
	rt := testHostRuntime(t, port)
	if err := rt.db.Create(&db.HostFingerprint{
		Hostname:    "127.0.0.1",
		Port:        port,
		Algorithm:   "ssh-ed25519",
		Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		TrustedAt:   time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 13, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "h1",
		Hostname: "127.0.0.1", Port: port,
		Fingerprint: fp, Alg: "ssh-ed25519",
	})
	waitDaemonFrame(t, out, relay.FrameOpenOK)

	var stored db.HostFingerprint
	if err := rt.db.Where("hostname = ? AND port = ?", "127.0.0.1", port).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Fingerprint != fp {
		t.Fatalf("fingerprint = %q, want %q", stored.Fingerprint, fp)
	}
	openHostFrame(t, rt, sender, mgr, 14, relay.OpenRequest{Target: relay.TargetHost, HostSyncID: "h1", Rows: 24, Cols: 80})
	waitDaemonFrame(t, out, relay.FrameOpenOK)
}

func TestHandleOpenHostFingerprintJumpHostIdentity(t *testing.T) {
	targetPort, _ := startTestSSHServer(t)
	jumpPort, jumpFP := startTestSSHServer(t)
	rt := testHostRuntime(t, targetPort)
	secKey := rt.mk.GetKey()
	enc, err := security.Encrypt([]byte("secret"), secKey.Bytes())
	secKey.Clear()
	if err != nil {
		t.Fatal(err)
	}
	jump := db.Host{
		SyncID:     "jump1",
		Hostname:   "127.0.0.1",
		Port:       jumpPort,
		Username:   "tester",
		AuthMethod: "password",
		Password:   enc,
	}
	if err := rt.db.Create(&jump).Error; err != nil {
		t.Fatal(err)
	}
	if err := rt.db.Model(&db.Host{}).Where("sync_id = ?", "h1").Update("jump_host_id", jump.ID).Error; err != nil {
		t.Fatal(err)
	}
	sender, out := newTestSender()
	mgr := newSessionManager()

	openHostFrame(t, rt, sender, mgr, 15, relay.OpenRequest{Target: relay.TargetHost, HostSyncID: "h1", Rows: 24, Cols: 80})

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	var p relay.OpenErrPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("payload is not structured JSON: %v (%q)", err, f.Payload)
	}
	if p.Code != relay.CodeFingerprintUnconfirmed {
		t.Fatalf("code = %q", p.Code)
	}
	if p.HostSyncID != "h1" {
		t.Fatalf("host_sync_id = %q, want target h1 as display reference", p.HostSyncID)
	}
	if p.Hostname != "127.0.0.1" || p.Port != jumpPort || p.Fingerprint != jumpFP || p.Alg != "ssh-ed25519" {
		t.Fatalf("peer fields = %+v, want jump host identity", p)
	}

	openHostFrame(t, rt, sender, mgr, 16, relay.OpenRequest{
		Target: relay.TargetHostFingerprintAccept, HostSyncID: "h1",
		Hostname: "127.0.0.1", Port: jumpPort,
		Fingerprint: jumpFP, Alg: "ssh-ed25519",
	})
	waitDaemonFrame(t, out, relay.FrameOpenOK)

	var stored db.HostFingerprint
	if err := rt.db.Where("hostname = ? AND port = ?", "127.0.0.1", jumpPort).First(&stored).Error; err != nil {
		t.Fatalf("jump fingerprint not stored: %v", err)
	}

	_, err = openHost(rt, "h1", 24, 80)
	if err == nil {
		t.Fatal("openHost through mock jump should fail at direct-tcpip dial")
	}
	var fpErr *fingerprintUnconfirmedError
	if errors.As(err, &fpErr) {
		t.Fatalf("jump host still rejected on fingerprint: %v", err)
	}
}
