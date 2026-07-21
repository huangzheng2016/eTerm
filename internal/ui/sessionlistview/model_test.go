package sessionlistview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebarez/sqlite"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/types"
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
	m.rows = []db.ConnectionHistory{{Transcript: "0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11"}}
	m.SetSize(80, 10)

	updated, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	m = updated.(*Model)
	if m.detailScroll != 6 {
		t.Fatalf("detailScroll=%d want 6", m.detailScroll)
	}
}

func TestMouseDragCopiesSelectedSessionText(t *testing.T) {
	m := New(nil)
	m.loaded = true
	m.detail = true
	m.rows = []db.ConnectionHistory{{Transcript: "hello world\nsecond line"}}
	m.SetSize(80, 10)
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 6, Y: 3, Button: tea.MouseLeft}))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: 3, Button: tea.MouseLeft}))
	_, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 10, Y: 3, Button: tea.MouseLeft}))
	if cmd == nil {
		t.Fatal("expected clipboard command")
	}
	msgs := cmd().(tea.BatchMsg)
	foundSuccess := false
	for _, next := range msgs {
		if msg, ok := next().(types.SuccessMsg); ok && msg.Message == "Copied 5 chars" {
			foundSuccess = true
		}
	}
	if !foundSuccess {
		t.Fatal("missing copy success message")
	}
}

func TestSessionCardsUseThreeContentLines(t *testing.T) {
	end := time.Date(2026, 7, 21, 13, 20, 0, 0, time.Local)
	start := end.Add(-time.Hour)
	m := New(nil)
	m.loaded = true
	m.rows = []db.ConnectionHistory{{
		Label:          "dev-k1",
		Source:         "ssh",
		Status:         "success",
		ConnectedAt:    start,
		DisconnectedAt: &end,
		Transcript:     "output",
	}}
	m.SetSize(80, 20)
	view := m.View().Content
	for _, want := range []string{"dev-k1", "2026-07-21 12:20 · 1h0m0s", "ssh · success · transcript"} {
		if !strings.Contains(view, want) {
			t.Fatalf("card missing %q: %q", want, view)
		}
	}
	if m.grid.CardH != 5 {
		t.Fatalf("card height = %d, want 5", m.grid.CardH)
	}
}

func TestSessionCardMouseUsesFiveLineRows(t *testing.T) {
	m := New(nil)
	m.loaded = true
	m.rows = []db.ConnectionHistory{{Label: "first"}, {Label: "second"}}
	m.SetSize(40, 20)

	updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 6, Button: tea.MouseLeft}))
	m = updated.(*Model)
	if m.cursor != 1 || m.detail {
		t.Fatalf("cursor=%d detail=%v", m.cursor, m.detail)
	}

	updated, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 0, Button: tea.MouseLeft}))
	m = updated.(*Model)
	if m.cursor != 1 || m.detail {
		t.Fatalf("header click changed cursor=%d detail=%v", m.cursor, m.detail)
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
