package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestSyncStartDisabledDoesNotSetInFlight(t *testing.T) {
	gdb, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	a := NewApp(gdb, mk)
	a.viewState = MainView

	next, cmd := a.Update(types.SyncStartMsg{})
	updated := next.(App)

	if updated.syncing {
		t.Fatal("syncing should stay false when sync is disabled")
	}
	if cmd == nil {
		t.Fatal("manual disabled sync should return result command")
	}
	msg := cmd()
	if _, ok := msg.(types.SyncResultMsg); !ok {
		t.Fatalf("got %T want SyncResultMsg", msg)
	}
}

func TestSyncTickDisabledDoesNotSetInFlight(t *testing.T) {
	gdb, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	a := NewApp(gdb, mk)
	a.viewState = MainView

	next, cmd := a.Update(types.SyncTickMsg{})
	updated := next.(App)

	if updated.syncing {
		t.Fatal("syncing should stay false when sync is disabled")
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("tick disabled sync should be silent, got %T", msg)
		}
	}
}
