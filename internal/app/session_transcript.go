package app

import (
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

const saveSessionTranscriptKey = "save_session_transcript"

func createLocalSessionHistory(gdb *gorm.DB, label, source string) uint {
	history := db.ConnectionHistory{Label: label, Source: source, ConnectedAt: time.Now(), Status: "success"}
	if err := gdb.Create(&history).Error; err != nil {
		return 0
	}
	return history.ID
}

func saveSessionTranscriptEnabled(gdb *gorm.DB) bool {
	s, err := db.GetSetting(gdb, saveSessionTranscriptKey)
	if err != nil {
		return true
	}
	return s != "false"
}

func finalizeSSHSession(gdb *gorm.DB, m *sshview.Model) {
	if m == nil {
		return
	}
	hid := m.HistoryID()
	if hid == 0 {
		return
	}
	now := time.Now()
	vals := map[string]interface{}{"disconnected_at": &now}
	if saveSessionTranscriptEnabled(gdb) {
		t := m.PlainTranscript(sshview.MaxTranscriptBytes)
		if t != "" {
			vals["transcript"] = t
		}
	}
	_ = gdb.Model(&db.ConnectionHistory{}).Where("id = ?", hid).Updates(vals).Error
}
