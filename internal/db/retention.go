package db

import (
	"time"

	"gorm.io/gorm"
)

const (
	HistoryMaxRows    = 500
	HistoryMaxAgeDays = 90
	HistoryListLimit  = 500
)

const HistoryMetaColumns = "id, host_id, label, source, connected_at, disconnected_at, status, replay_duration, replay_stopped, " +
	"coalesce(length(replay_data), 0) > 0 AS has_replay, " +
	"length(trim(coalesce(transcript, ''), char(9) || char(10) || char(13) || ' ')) > 0 AS has_transcript"

const HistoryNonEmptyFilter = "length(trim(transcript, char(9) || char(10) || char(13) || ' ')) > 0 OR length(replay_data) > 0"

func PruneConnectionHistories(gdb *gorm.DB, maxRows int, maxAgeDays int) error {
	keep := gdb.Model(&ConnectionHistory{}).Select("id").Order("connected_at DESC, id DESC").Limit(maxRows)
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	if err := gdb.Unscoped().Where("connected_at < ? OR id NOT IN (?)", cutoff, keep).Delete(&ConnectionHistory{}).Error; err != nil {
		return err
	}
	return gdb.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
}
