package app

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
	"gorm.io/gorm"
)

func TestBatchConnectHostReportsMissingHost(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.SSHKey{}, &db.HostFingerprint{}, &db.ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("test-password"))
	a := NewApp(database, mk)

	msg := a.batchConnectHostCmd(999, "")()

	if _, ok := msg.(types.ErrorMsg); !ok {
		t.Fatalf("got %#v, want types.ErrorMsg", msg)
	}
}
