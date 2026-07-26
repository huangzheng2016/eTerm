package app

import (
	"bytes"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
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

func TestReplayAlwaysSavesSearchableTranscript(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.AppSetting{}, &db.ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(database, saveSessionTranscriptKey, "false"); err != nil {
		t.Fatal(err)
	}
	history := db.ConnectionHistory{Label: "replay"}
	if err := database.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	done <- nil
	m := sshview.New(&internalssh.InteractiveSession{Stdout: bytes.NewReader([]byte("searchable output")), Done: done}, "replay", 0, viewkeys.SSHKeys{})
	m.SetHistoryID(history.ID)
	m.EnableReplayRecording()
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)
	finalizeSSHSession(database, m)
	if err := database.First(&history, history.ID).Error; err != nil {
		t.Fatal(err)
	}
	if history.Transcript == "" || len(history.ReplayData) == 0 {
		t.Fatalf("transcript=%q replay=%d", history.Transcript, len(history.ReplayData))
	}
}
