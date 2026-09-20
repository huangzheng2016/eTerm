package app

import (
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestBatchConnectHostReportsMissingHost(t *testing.T) {
	database := appTestMemoryDB(t, &db.Host{}, &db.SSHKey{}, &db.HostFingerprint{}, &db.ConnectionHistory{})
	mk := security.NewMasterKeyManager(nil, nil, time.Minute)
	mk.Setup([]byte("test-password"))
	a := NewApp(database, mk)

	msg := a.batchConnectHostCmd(999, "")()

	if _, ok := msg.(types.ErrorMsg); !ok {
		t.Fatalf("got %#v, want types.ErrorMsg", msg)
	}
}
