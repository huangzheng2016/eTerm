package sync

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runSyncd(t *testing.T, dbPath string) (*json.Encoder, *json.Decoder, func()) {
	t.Helper()
	cmd := exec.Command("go", "run", "../../cmd/etermsyncd", "--stdio", "--db", dbPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start syncd: %v", err)
	}

	cleanup := func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}
	return json.NewEncoder(stdin), json.NewDecoder(stdout), cleanup
}

func TestStdioPing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	enc, dec, cleanup := runSyncd(t, dbPath)
	defer cleanup()

	if err := enc.Encode(stdioRequest{Method: "ping"}); err != nil {
		t.Fatalf("encode ping: %v", err)
	}
	var resp stdioResponse
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ping failed: %s", resp.Error)
	}
}

func TestStdioPushPull(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	enc, dec, cleanup := runSyncd(t, dbPath)
	defer cleanup()

	rec := SyncRecord{
		SyncID:    "test-host-1",
		Type:      TypeHost,
		DeviceID:  "test-device",
		Payload:   "{}",
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := enc.Encode(stdioRequest{Method: "push", Records: []SyncRecord{rec}}); err != nil {
		t.Fatalf("encode push: %v", err)
	}
	var pushResp stdioResponse
	if err := dec.Decode(&pushResp); err != nil {
		t.Fatalf("decode push: %v", err)
	}
	if pushResp.Revision != 1 {
		t.Fatalf("push revision = %d, want 1", pushResp.Revision)
	}

	if err := enc.Encode(stdioRequest{Method: "pull", Since: 0}); err != nil {
		t.Fatalf("encode pull: %v", err)
	}
	var pullResp stdioResponse
	if err := dec.Decode(&pullResp); err != nil {
		t.Fatalf("decode pull: %v", err)
	}
	if len(pullResp.Records) != 1 {
		t.Fatalf("pulled %d records, want 1", len(pullResp.Records))
	}
	if pullResp.Records[0].SyncID != rec.SyncID {
		t.Fatalf("pulled sync_id = %q, want %q", pullResp.Records[0].SyncID, rec.SyncID)
	}
}
