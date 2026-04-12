package app

import (
	"time"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/ui/sshview"
	"gorm.io/gorm"
)

const saveSessionTranscriptKey = "save_session_transcript"

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
