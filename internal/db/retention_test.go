package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func retentionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	return database
}

func historyIDs(t *testing.T, gdb *gorm.DB) []uint {
	t.Helper()
	var rows []ConnectionHistory
	if err := gdb.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	ids := make([]uint, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestPruneKeepsNewestRowsWithinLimit(t *testing.T) {
	database := retentionTestDB(t)
	base := time.Now()
	for i := 0; i < 5; i++ {
		h := ConnectionHistory{Label: "s", ConnectedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := database.Create(&h).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneConnectionHistories(database, 3, 90); err != nil {
		t.Fatal(err)
	}
	var rows []ConnectionHistory
	if err := database.Order("connected_at").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("kept %d rows, want 3", len(rows))
	}
	for i, r := range rows {
		want := base.Add(time.Duration(i+2) * time.Minute)
		if !r.ConnectedAt.Equal(want) {
			t.Fatalf("row %d connected_at = %v, want %v", i, r.ConnectedAt, want)
		}
	}
	var unscoped int64
	if err := database.Unscoped().Model(&ConnectionHistory{}).Count(&unscoped).Error; err != nil {
		t.Fatal(err)
	}
	if unscoped != 3 {
		t.Fatalf("unscoped count = %d, want 3 (rows soft-deleted, not pruned)", unscoped)
	}
}

func TestPruneDeletesRowsOlderThanMaxAge(t *testing.T) {
	database := retentionTestDB(t)
	now := time.Now()
	rows := []ConnectionHistory{
		{Label: "old", ConnectedAt: now.AddDate(0, 0, -31)},
		{Label: "edge-old", ConnectedAt: now.AddDate(0, 0, -30).Add(-time.Hour)},
		{Label: "edge-new", ConnectedAt: now.AddDate(0, 0, -30).Add(time.Hour)},
		{Label: "new", ConnectedAt: now.AddDate(0, 0, -29)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := PruneConnectionHistories(database, 500, 30); err != nil {
		t.Fatal(err)
	}
	var labels []string
	if err := database.Model(&ConnectionHistory{}).Order("connected_at").Pluck("label", &labels).Error; err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0] != "edge-new" || labels[1] != "new" {
		t.Fatalf("kept labels = %v", labels)
	}
}

func TestPruneAppliesCountAndAgeTogether(t *testing.T) {
	database := retentionTestDB(t)
	now := time.Now()
	old := ConnectionHistory{Label: "old-but-few", ConnectedAt: now.AddDate(0, 0, -100)}
	if err := database.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		h := ConnectionHistory{Label: "recent", ConnectedAt: now.Add(time.Duration(i) * time.Minute)}
		if err := database.Create(&h).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := PruneConnectionHistories(database, 3, 90); err != nil {
		t.Fatal(err)
	}
	ids := historyIDs(t, database)
	if len(ids) != 3 {
		t.Fatalf("kept %d rows, want 3", len(ids))
	}
	var count int64
	if err := database.Model(&ConnectionHistory{}).Where("label = ?", "old-but-few").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("old row kept although older than max age")
	}
}

func TestPruneDefaultsKeepEverything(t *testing.T) {
	database := retentionTestDB(t)
	now := time.Now()
	rows := []ConnectionHistory{
		{Label: "a", ConnectedAt: now.AddDate(0, 0, -10)},
		{Label: "b", ConnectedAt: now},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := PruneConnectionHistories(database, HistoryMaxRows, HistoryMaxAgeDays); err != nil {
		t.Fatal(err)
	}
	if ids := historyIDs(t, database); len(ids) != 2 {
		t.Fatalf("kept %d rows, want 2", len(ids))
	}
}

func TestHistoryMetaColumnsSkipsBlobs(t *testing.T) {
	database := retentionTestDB(t)
	rows := []ConnectionHistory{
		{Label: "plain", ConnectedAt: time.Now(), Transcript: "output"},
		{Label: "replay", ConnectedAt: time.Now(), ReplayData: []byte{1, 2, 3}, ReplayDuration: 5},
		{Label: "empty", ConnectedAt: time.Now(), Transcript: " \n\t\r"},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var got []ConnectionHistory
	if err := database.Select(HistoryMetaColumns).Order("id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d", len(got))
	}
	if got[0].HasTranscript != true || got[0].HasReplay != false {
		t.Fatalf("plain row flags = %+v", got[0])
	}
	if got[1].HasReplay != true || got[1].HasTranscript != false || got[1].ReplayDuration != 5 {
		t.Fatalf("replay row flags = %+v", got[1])
	}
	if got[2].HasTranscript != false || got[2].HasReplay != false {
		t.Fatalf("empty row flags = %+v", got[2])
	}
	for i, r := range got {
		if r.Transcript != "" || r.ANSITranscript != "" || len(r.ReplayData) != 0 {
			t.Fatalf("row %d loaded blob columns: %+v", i, r)
		}
	}
	var full ConnectionHistory
	if err := database.First(&full, got[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if full.HasTranscript || full.HasReplay {
		t.Fatalf("plain select populated computed flags: %+v", full)
	}
	if full.Transcript != "output" {
		t.Fatalf("full row transcript = %q", full.Transcript)
	}
}

func TestInitDBPrunesOnStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etest.db")
	database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&ConnectionHistory{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		h := ConnectionHistory{Label: "s", ConnectedAt: now.Add(time.Duration(i) * time.Minute)}
		if err := database.Create(&h).Error; err != nil {
			t.Fatal(err)
		}
	}
	stale := ConnectionHistory{Label: "stale", ConnectedAt: now.AddDate(0, 0, -120)}
	if err := database.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	_ = sqlDB.Close()

	opened, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := opened.Model(&ConnectionHistory{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("rows after InitDB = %d, want 5 (stale pruned)", count)
	}
}
