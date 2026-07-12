package sessionlistview

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

func sessionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestGlobalSessionSearchMatchesLabelAndTranscript(t *testing.T) {
	database := sessionTestDB(t)
	now := time.Now()
	rows := []db.ConnectionHistory{
		{Label: "daemon-prod", Source: "remote", ConnectedAt: now, Status: "success", Transcript: "deploy completed"},
		{Label: "local-work", Source: "tmux", ConnectedAt: now, Status: "success", Transcript: "go test ./..."},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	m := New(database)
	m.search.SetValue("deploy")
	msg := m.reload()().(loadedMsg)
	if msg.err != nil || len(msg.rows) != 1 || msg.rows[0].Label != "daemon-prod" {
		t.Fatalf("transcript search = %+v err=%v", msg.rows, msg.err)
	}
	m.search.SetValue("local-work")
	msg = m.reload()().(loadedMsg)
	if msg.err != nil || len(msg.rows) != 1 || msg.rows[0].Source != "tmux" {
		t.Fatalf("label search = %+v err=%v", msg.rows, msg.err)
	}
}

func TestSessionCardEnterOpensReadableTranscript(t *testing.T) {
	m := New(nil)
	m.loaded = true
	m.rows = []db.ConnectionHistory{{Label: "remote-shell", Transcript: "line one\nline two"}}
	m.SetSize(80, 20)
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(*Model)
	if !m.detail || m.selectedTranscript() != "line one\nline two" {
		t.Fatalf("detail=%v transcript=%q", m.detail, m.selectedTranscript())
	}
	if got := m.View().Content; got == "" {
		t.Fatal("detail view is empty")
	}
}
