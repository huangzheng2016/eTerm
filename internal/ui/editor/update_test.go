package editor

import (
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestPasteMsgUpdatesFocusedInput(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	masterKey := security.NewMasterKeyManager(nil, nil, time.Minute)
	masterKey.Setup([]byte("pw"))

	model := New(database, masterKey, nil)
	updated, _ := model.Update(tea.PasteMsg{Content: "root@10.0.0.1"})
	got := updated.(Model).inputs[0].Value()

	if got != "root@10.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestSaveExistingHostWithEmptySyncIDAssignsUniqueSyncID(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	masterKey := security.NewMasterKeyManager(nil, nil, time.Minute)
	masterKey.Setup([]byte("pw"))

	first := db.Host{Alias: "first", Hostname: "1.1.1.1", Port: 22, Username: "root", AuthMethod: "agent"}
	second := db.Host{Alias: "second", Hostname: "2.2.2.2", Port: 22, Username: "root", AuthMethod: "agent"}
	if err := database.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.Host{}).Where("id = ?", first.ID).Update("sync_id", "").Error; err != nil {
		t.Fatal(err)
	}
	first.SyncID = ""

	model := New(database, masterKey, &first)
	msg := model.save()()
	if errMsg, ok := msg.(types.ErrorMsg); ok {
		t.Fatal(errMsg.Err)
	}
	if _, ok := msg.(types.HostSavedMsg); !ok {
		t.Fatalf("got %T", msg)
	}

	var saved db.Host
	if err := database.First(&saved, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.SyncID == "" {
		t.Fatal("expected sync_id to be assigned")
	}
}
