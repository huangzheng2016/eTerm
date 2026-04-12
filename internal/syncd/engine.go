package syncd

import (
	"time"

	"gorm.io/gorm"
)

type SyncEntry struct {
	ID        uint      `gorm:"primaryKey"`
	SyncID    string    `gorm:"uniqueIndex;size:36;not null"`
	Type      string    `gorm:"not null"`
	Payload   string    `gorm:"type:text"`
	DeviceID  string    `gorm:"not null"`
	Deleted   bool      `gorm:"default:false"`
	Revision  int64     `gorm:"index;not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type Engine struct {
	DB *gorm.DB
}

func NewEngine(database *gorm.DB) (*Engine, error) {
	if err := database.AutoMigrate(&SyncEntry{}); err != nil {
		return nil, err
	}
	return &Engine{DB: database}, nil
}

func (e *Engine) Ping() error {
	return nil
}

func (e *Engine) Pull(sinceRev int64) ([]SyncEntry, int64, error) {
	var entries []SyncEntry
	if err := e.DB.Where("revision > ?", sinceRev).Order("revision").Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	var maxRev int64
	e.DB.Model(&SyncEntry{}).Select("COALESCE(MAX(revision), 0)").Row().Scan(&maxRev)
	return entries, maxRev, nil
}

func (e *Engine) Push(entries []SyncEntry) (int64, error) {
	var maxRev int64
	e.DB.Model(&SyncEntry{}).Select("COALESCE(MAX(revision), 0)").Row().Scan(&maxRev)

	for _, entry := range entries {
		var existing SyncEntry
		found := e.DB.Where("sync_id = ?", entry.SyncID).First(&existing).Error == nil

		if found {
			// LWW: skip if incoming is not newer
			if !entry.UpdatedAt.After(existing.UpdatedAt) {
				continue
			}
			maxRev++
			existing.Payload = entry.Payload
			existing.DeviceID = entry.DeviceID
			existing.Deleted = entry.Deleted
			existing.Revision = maxRev
			existing.UpdatedAt = entry.UpdatedAt
			existing.Type = entry.Type
			if err := e.DB.Save(&existing).Error; err != nil {
				return 0, err
			}
		} else {
			maxRev++
			entry.Revision = maxRev
			if err := e.DB.Create(&entry).Error; err != nil {
				return 0, err
			}
		}
	}
	return maxRev, nil
}
