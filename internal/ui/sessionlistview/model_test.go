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

func TestSessionListHidesEmptyTranscripts(t *testing.T) {
	database := sessionTestDB(t)
	rows := []db.ConnectionHistory{
		{Label: "empty", ConnectedAt: time.Now(), Transcript: " \n\t\r"},
		{Label: "visible", ConnectedAt: time.Now(), Transcript: "output"},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	msg := New(database).reload()().(loadedMsg)
	if msg.err != nil || len(msg.rows) != 1 || msg.rows[0].Label != "visible" {
		t.Fatalf("rows=%+v err=%v", msg.rows, msg.err)
	}
}

func TestSessionListShowEmptyShortcut(t *testing.T) {
	database := sessionTestDB(t)
	row := db.ConnectionHistory{Label: "empty", ConnectedAt: time.Now(), Transcript: "\n"}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	m := New(database)
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'h', Text: "h"}))
	m = updated.(*Model)
	msg := cmd().(loadedMsg)
	if !m.showEmpty || len(msg.rows) != 1 {
		t.Fatalf("showEmpty=%v rows=%+v", m.showEmpty, msg.rows)
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

func TestMouseWheelScrollsSessionDetail(t *testing.T) {
	m := New(nil)
	m.loaded = true
	m.detail = true
	m.rows = []db.ConnectionHistory{{Transcript: "0\n1\n2\n3\n4\n5\n6\n7\n8\n9"}}
	m.SetSize(80, 10)

	updated, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	m = updated.(*Model)
	if m.detailScroll != 3 {
		t.Fatalf("detailScroll=%d want 3", m.detailScroll)
	}
}

func TestEscapeDoesNotCloseSessionList(t *testing.T) {
	m := New(nil)
	m.loaded = true

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if updated.(*Model) != m || cmd != nil {
		t.Fatal("escape closed the session list")
	}
}
