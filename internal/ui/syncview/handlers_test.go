package syncview

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestSaveReportsDatabaseError(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
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
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(database, security.NewMasterKeyManager(nil, nil, time.Minute))
	m.enableIdx = 1
	m.modeIdx = 1
	m.inputs[inServerURL].SetValue("https://sync.example.com")
	m.inputs[inPassphrase].SetValue("secret")

	msg := m.save()()

	if _, ok := msg.(types.ErrorMsg); !ok {
		t.Fatalf("got %T want types.ErrorMsg", msg)
	}
}
