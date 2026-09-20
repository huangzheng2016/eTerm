package syncview

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestSaveReportsDatabaseError(t *testing.T) {
	database := newSyncDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	m := New(database, security.NewMasterKeyManager(nil, nil, time.Minute))
	msg := m.save()()

	if _, ok := msg.(types.ErrorMsg); !ok {
		t.Fatalf("got %T want types.ErrorMsg", msg)
	}
}

func TestSaveReportsMissingMasterKeyForSecrets(t *testing.T) {
	m := New(newSyncDB(t), security.NewMasterKeyManager(nil, nil, time.Minute))
	enableHTTPSync(m)
	m.pendPass = "secret"
	m.passDirty = true

	msg := m.save()()

	if _, ok := msg.(types.ErrorMsg); !ok {
		t.Fatalf("got %T want types.ErrorMsg", msg)
	}
}

func TestCtrlYSavesThenStartsSync(t *testing.T) {
	database := newSyncDB(t)
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("pw"))
	m := New(database, mk)
	enableHTTPSync(m)
	m.pendPass = "secret"
	m.passDirty = true

	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	if _, ok := msg.(types.SyncStartMsg); !ok {
		t.Fatalf("got %T want SyncStartMsg", msg)
	}
	mode, _ := db.GetSetting(database, "sync_mode")
	if mode != "http" {
		t.Fatalf("sync_mode = %q, want http", mode)
	}
}
