package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

func TestCreateLocalSessionHistoryStoresSourceWithoutHost(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	id := createLocalSessionHistory(database, "daemon-prod", "remote-tmux")
	if id == 0 {
		t.Fatal("history ID is zero")
	}
	var history db.ConnectionHistory
	if err := database.First(&history, id).Error; err != nil {
		t.Fatal(err)
	}
	if history.HostID != 0 || history.Label != "daemon-prod" || history.Source != "remote-tmux" {
		t.Fatalf("history = %+v", history)
	}
}
