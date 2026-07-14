package sessionhistview

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/huangzheng2016/eTerm/internal/db"
)

func TestHistoryListHidesEmptyTranscripts(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.Host{}, &db.ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	host := db.Host{Alias: "host"}
	if err := database.Create(&host).Error; err != nil {
		t.Fatal(err)
	}
	rows := []db.ConnectionHistory{
		{HostID: host.ID, ConnectedAt: time.Now(), Transcript: "\n\t "},
		{HostID: host.ID, ConnectedAt: time.Now(), Transcript: "output"},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	msg := New(database, host.ID).reload()().(loadedMsg)
	if len(msg.rows) != 1 || msg.rows[0].Transcript != "output" {
		t.Fatalf("rows=%+v", msg.rows)
	}
}

func TestMouseClickSelectsHistoryRow(t *testing.T) {
	m := &Model{
		width:     90,
		height:    20,
		loaded:    true,
		focusList: true,
		rows: []db.ConnectionHistory{
			{ConnectedAt: time.Unix(3, 0), Status: "success", Transcript: "third"},
			{ConnectedAt: time.Unix(2, 0), Status: "success", Transcript: "second"},
		},
	}

	next, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 3, Button: tea.MouseLeft}))
	got := next.(*Model)

	if got.sel != 1 {
		t.Fatalf("got selected row %d want 1", got.sel)
	}
	if !got.focusList {
		t.Fatalf("expected list focus")
	}
}

func TestMouseWheelScrollsFocusedTranscript(t *testing.T) {
	m := &Model{
		width:     90,
		height:    8,
		loaded:    true,
		focusList: false,
		rows: []db.ConnectionHistory{
			{ConnectedAt: time.Unix(1, 0), Status: "success", Transcript: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no"},
		},
	}

	next, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{X: 40, Y: 4, Button: tea.MouseWheelDown}))
	got := next.(*Model)

	if got.scroll != 6 {
		t.Fatalf("got scroll %d want 6", got.scroll)
	}
}

func TestMouseDragCopiesSelectedHistoryText(t *testing.T) {
	m := &Model{
		width: 90, height: 10, loaded: true, focusList: false,
		rows: []db.ConnectionHistory{{Transcript: "hello world\nsecond line"}},
	}
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 34, Y: 3, Button: tea.MouseLeft}))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 38, Y: 3, Button: tea.MouseLeft}))
	_, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 38, Y: 3, Button: tea.MouseLeft}))
	if cmd == nil || cmd() == nil {
		t.Fatal("expected clipboard command")
	}
}

func TestPageDownFocusesAndPagesTranscript(t *testing.T) {
	m := &Model{
		width: 90, height: 8, loaded: true, focusList: true,
		rows: []db.ConnectionHistory{{Transcript: "0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11"}},
	}

	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}))
	got := next.(*Model)
	if got.focusList || got.scroll != 6 {
		t.Fatalf("focusList=%v scroll=%d want false, 6", got.focusList, got.scroll)
	}
}

func TestDisplayTranscriptPrefersANSI(t *testing.T) {
	m := &Model{rows: []db.ConnectionHistory{{Transcript: "plain", ANSITranscript: "\x1b[31mred\x1b[0m"}}}
	if got := m.selectedDisplayTranscript(); got != "\x1b[31mred\x1b[0m" {
		t.Fatalf("display transcript = %q", got)
	}
}
