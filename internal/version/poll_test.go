package version

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func TestPollLatestRelease_Disabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.db")
	gdb, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	tag, url, err := PollLatestRelease(gdb, true)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "" || url != "" {
		t.Fatalf("got tag=%q url=%q", tag, url)
	}
}

func TestPollLatestRelease_Throttled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.db")
	gdb, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(gdb, LastSuccessfulUpdateCheckKey, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	tag, url, err := PollLatestRelease(gdb, false)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "" || url != "" {
		t.Fatalf("expected throttle skip, got tag=%q url=%q", tag, url)
	}
}
