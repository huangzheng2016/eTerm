package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

func testDaemonDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func TestLoadRuntimeRejectsSSHSyncModeWithoutHost(t *testing.T) {
	database := testDaemonDB(t)
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	salt, verifier := mk.Setup([]byte("pw"))
	_ = db.SetSetting(database, "encryption_salt", base64.StdEncoding.EncodeToString(salt))
	_ = db.SetSetting(database, "encryption_verifier", base64.StdEncoding.EncodeToString(verifier))
	_ = db.SetSetting(database, "sync_enabled", "true")
	_ = db.SetSetting(database, "sync_mode", "ssh")

	_, err := loadRuntime(database, Config{Password: "pw"})
	if err == nil || !strings.Contains(err.Error(), "no SSH host") {
		t.Fatalf("got %v, want no SSH host error", err)
	}
}

func TestLoadRuntimeNameResolution(t *testing.T) {
	setup := func(t *testing.T) *gorm.DB {
		database := testDaemonDB(t)
		mk := security.NewMasterKeyManager(nil, nil, time.Minute)
		salt, verifier := mk.Setup([]byte("pw"))
		_ = db.SetSetting(database, "encryption_salt", base64.StdEncoding.EncodeToString(salt))
		_ = db.SetSetting(database, "encryption_verifier", base64.StdEncoding.EncodeToString(verifier))
		_ = db.SetSetting(database, "sync_enabled", "true")
		_ = db.SetSetting(database, "sync_server_url", "http://localhost:8080")
		k := mk.GetKey()
		enc, err := security.Encrypt([]byte("pass"), k.Bytes())
		k.Clear()
		if err != nil {
			t.Fatal(err)
		}
		_ = db.SetSetting(database, "sync_passphrase", enc)
		return database
	}

	t.Run("defaults to hostname", func(t *testing.T) {
		rt, err := loadRuntime(setup(t), Config{Password: "pw"})
		if err != nil {
			t.Fatal(err)
		}
		host, _ := os.Hostname()
		if rt.name != host {
			t.Fatalf("name = %q, want hostname %q", rt.name, host)
		}
	})

	t.Run("saved setting wins over hostname", func(t *testing.T) {
		database := setup(t)
		_ = db.SetSetting(database, "daemon_peer_name", "saved")
		rt, err := loadRuntime(database, Config{Password: "pw"})
		if err != nil {
			t.Fatal(err)
		}
		if rt.name != "saved" {
			t.Fatalf("name = %q, want saved", rt.name)
		}
	})

	t.Run("flag wins and persists", func(t *testing.T) {
		database := setup(t)
		_ = db.SetSetting(database, "daemon_peer_name", "saved")
		rt, err := loadRuntime(database, Config{Password: "pw", Name: "flag"})
		if err != nil {
			t.Fatal(err)
		}
		if rt.name != "flag" {
			t.Fatalf("name = %q, want flag", rt.name)
		}
		if got, _ := db.GetSetting(database, "daemon_peer_name"); got != "flag" {
			t.Fatalf("persisted name = %q, want flag", got)
		}
	})
}

func TestHandleOpenPeerRename(t *testing.T) {
	database := testDaemonDB(t)
	rt := &runtimeConfig{db: database, peerID: "peer-1", tenantID: "tenant"}
	sender, out := newTestSender()
	mgr := newSessionManager()
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetPeerRename, Name: "new-name"})

	handleOpen(rt, relay.Frame{Type: relay.FrameOpen, StreamID: 9, Payload: payload}, mgr, sender, context.Background(), context.Background())

	_ = waitDaemonFrame(t, out, relay.FrameOpenOK)
	hello := waitDaemonFrame(t, out, relay.FrameHello)
	var hp relay.HelloPayload
	if err := json.Unmarshal(hello.Payload, &hp); err != nil {
		t.Fatal(err)
	}
	if hp.Name != "new-name" || hp.PeerID != "peer-1" {
		t.Fatalf("hello = %+v", hp)
	}
	if got, _ := db.GetSetting(database, peerNameSettingKey); got != "new-name" {
		t.Fatalf("persisted = %q, want new-name", got)
	}
}

func TestHandleOpenPeerRenameRejectsEmptyName(t *testing.T) {
	database := testDaemonDB(t)
	rt := &runtimeConfig{db: database, peerID: "peer-1", tenantID: "tenant"}
	sender, out := newTestSender()
	mgr := newSessionManager()
	payload, _ := json.Marshal(relay.OpenRequest{Target: relay.TargetPeerRename, Name: "  "})

	handleOpen(rt, relay.Frame{Type: relay.FrameOpen, StreamID: 9, Payload: payload}, mgr, sender, context.Background(), context.Background())

	f := waitDaemonFrame(t, out, relay.FrameOpenErr)
	if !strings.Contains(string(f.Payload), "empty peer name") {
		t.Fatalf("payload = %q", f.Payload)
	}
	if got, _ := db.GetSetting(database, peerNameSettingKey); got != "" {
		t.Fatalf("persisted = %q, want empty", got)
	}
}

func TestQueueInputDropsOldestWhenFull(t *testing.T) {
	sr := &streamRelay{input: make(chan []byte, 2), stop: make(chan struct{})}
	sr.queueInput([]byte("a"))
	sr.queueInput([]byte("b"))
	sr.queueInput([]byte("c"))
	if got := <-sr.input; string(got) != "b" {
		t.Fatalf("first queued = %q, want b (oldest dropped)", got)
	}
	if got := <-sr.input; string(got) != "c" {
		t.Fatalf("second queued = %q, want c", got)
	}
}

func writeFakeTmux(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubTmuxProbeDirs(t *testing.T, dirs []string) {
	t.Helper()
	old := tmuxProbeDirs
	tmuxProbeDirs = dirs
	t.Cleanup(func() { tmuxProbeDirs = old })
}

func TestResolveTmuxBinaryFromPATH(t *testing.T) {
	dir := t.TempDir()
	want := writeFakeTmux(t, dir, 0o755)
	t.Setenv("PATH", dir)
	stubTmuxProbeDirs(t, nil)

	if got := resolveTmuxBinary(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if os.Getenv("PATH") != dir {
		t.Fatalf("PATH modified: %q", os.Getenv("PATH"))
	}
}

func TestResolveTmuxBinaryFallsBackToProbeDirs(t *testing.T) {
	noExec := t.TempDir()
	writeFakeTmux(t, noExec, 0o644)
	fallback := t.TempDir()
	want := writeFakeTmux(t, fallback, 0o755)
	t.Setenv("PATH", noExec)
	stubTmuxProbeDirs(t, []string{noExec, fallback})

	if got := resolveTmuxBinary(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasPrefix(os.Getenv("PATH"), fallback+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want %s prepended", os.Getenv("PATH"), fallback)
	}
}

func TestResolveTmuxBinaryNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	stubTmuxProbeDirs(t, []string{t.TempDir()})

	if got := resolveTmuxBinary(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
