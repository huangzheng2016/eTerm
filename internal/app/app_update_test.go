package app

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func tickTestApp(t *testing.T) App {
	t.Helper()
	gdb, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	a := NewApp(gdb, mk).SetNoUpdateCheck(true)
	a.viewState = MainView
	a.tmuxRestorePath = filepath.Join(t.TempDir(), "tmux_restore.json")
	return a
}

func batchCmdCount(t *testing.T, cmd tea.Cmd) int {
	t.Helper()
	if cmd == nil {
		return 0
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		return len(batch)
	}
	return 1
}

func TestArmSyncTickDedup(t *testing.T) {
	a := tickTestApp(t)
	_ = db.SetSetting(a.db, "sync_enabled", "true")

	a, cmd1 := a.armSyncTick()
	if cmd1 == nil || !a.syncTickPending {
		t.Fatal("first arm should schedule the tick")
	}
	if _, cmd2 := a.armSyncTick(); cmd2 != nil {
		t.Fatal("second arm while pending should be a no-op")
	}
}

func TestSyncSettingsSavedArmsTick(t *testing.T) {
	a := tickTestApp(t)
	_ = db.SetSetting(a.db, "sync_enabled", "true")

	next, _ := a.Update(types.SuccessMsg{Message: "Sync settings saved"})
	if !next.(App).syncTickPending {
		t.Fatal("sync tick should be armed after enabling sync in settings")
	}
}

func TestSyncSettingsSavedDisabledDoesNotArm(t *testing.T) {
	a := tickTestApp(t)

	next, _ := a.Update(types.SuccessMsg{Message: "Sync settings saved"})
	if next.(App).syncTickPending {
		t.Fatal("sync tick should stay disarmed while sync is disabled")
	}
}

func TestSyncTickRearmsWhileLocked(t *testing.T) {
	a := tickTestApp(t)
	_ = db.SetSetting(a.db, "sync_enabled", "true")
	a.masterKey.Lock()
	a.syncTickPending = true

	next, cmd := a.Update(types.SyncTickMsg{})
	updated := next.(App)
	if !updated.syncTickPending || cmd == nil {
		t.Fatal("tick chain should re-arm while locked instead of dying")
	}
	if updated.syncing {
		t.Fatal("sync should not start while locked")
	}
}

func TestSyncTickWhileSyncingHandsChainToResult(t *testing.T) {
	a := tickTestApp(t)
	_ = db.SetSetting(a.db, "sync_enabled", "true")
	a.syncTickPending = true
	a.syncing = true

	next, cmd := a.Update(types.SyncTickMsg{})
	a = next.(App)
	if a.syncTickPending || cmd != nil {
		t.Fatal("tick while syncing should be consumed without re-arm")
	}

	next, _ = a.Update(types.SyncResultMsg{})
	if !next.(App).syncTickPending {
		t.Fatal("sync result should re-arm the chain")
	}
}

func TestUnlockArmsChainsOnce(t *testing.T) {
	a := tickTestApp(t)
	_ = db.SetSetting(a.db, "sync_enabled", "true")

	next, cmd1 := a.Update(types.MasterKeyUnlockedMsg{})
	a = next.(App)
	if !a.syncTickPending || !a.autoLockOn {
		t.Fatalf("unlock should arm both chains: sync=%v autoLock=%v", a.syncTickPending, a.autoLockOn)
	}
	first := batchCmdCount(t, cmd1)

	a.aiView = nil
	next, cmd2 := a.Update(types.MasterKeyUnlockedMsg{})
	a = next.(App)
	second := batchCmdCount(t, cmd2)
	if second != first-2 {
		t.Fatalf("second unlock re-armed chains: first batch %d cmds, second %d", first, second)
	}
}

func TestAutoLockTickStopsWhileLocked(t *testing.T) {
	a := tickTestApp(t)
	a.autoLockOn = true
	a.viewState = LoginView

	next, cmd := a.Update(types.AutoLockTickMsg{})
	updated := next.(App)
	if updated.autoLockOn || cmd != nil {
		t.Fatal("autoLock chain should stop while locked")
	}
}

func TestAutoLockTickStopsInNoPasswordMode(t *testing.T) {
	a := tickTestApp(t)
	a.autoLockOn = true
	a.noPasswordMode = true

	next, cmd := a.Update(types.AutoLockTickMsg{})
	updated := next.(App)
	if updated.autoLockOn || cmd != nil {
		t.Fatal("autoLock chain should stop in no-password mode")
	}
}

func TestAutoLockTickRearmsInMainView(t *testing.T) {
	a := tickTestApp(t)
	a.autoLockOn = true

	next, cmd := a.Update(types.AutoLockTickMsg{})
	updated := next.(App)
	if !updated.autoLockOn || cmd == nil {
		t.Fatal("autoLock chain should re-arm in main view")
	}
}
