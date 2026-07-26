package app

import (
	"strings"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"gorm.io/gorm"
)

const saveSessionTranscriptKey = "save_session_transcript"
const sessionCaptureModeKey = "session_capture_mode"

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

func replayRecordingEnabled(gdb *gorm.DB) bool {
	s, err := db.GetSetting(gdb, sessionCaptureModeKey)
	return err != nil || s != "transcript"
}

func configureSessionCapture(gdb *gorm.DB, m *sshview.Model) {
	if replayRecordingEnabled(gdb) {
		m.EnableReplayRecording()
	}
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
	if saveSessionTranscriptEnabled(gdb) || m.ReplayRecordingEnabled() {
		t := m.PlainTranscript(sshview.MaxTranscriptBytes)
		if strings.TrimSpace(t) != "" {
			vals["transcript"] = t
			ansi := m.ANSITranscript(sshview.MaxTranscriptBytes)
			if ansi != "" {
				vals["ansi_transcript"] = ansi
			}
		}
	}
	if data, duration, stopped := m.ReplayRecording(); len(data) > 0 {
		vals["replay_data"] = data
		vals["replay_duration"] = duration.Milliseconds()
		vals["replay_stopped"] = stopped
	}
	_ = gdb.Model(&db.ConnectionHistory{}).Where("id = ?", hid).Updates(vals).Error
}
