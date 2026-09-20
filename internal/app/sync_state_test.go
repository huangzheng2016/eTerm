package app

import (
	"testing"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestSyncStartDisabledDoesNotSetInFlight(t *testing.T) {
	a := tickTestApp(t)

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
	a := tickTestApp(t)

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

func TestSyncStartWhileInFlightShowsToastCommand(t *testing.T) {
	a := tickTestApp(t)
	a.syncing = true

	next, cmd := a.Update(types.SyncStartMsg{})
	updated := next.(App)

	if !updated.syncing {
		t.Fatal("syncing should stay true")
	}
	if cmd == nil {
		t.Fatal("expected toast command")
	}
}
