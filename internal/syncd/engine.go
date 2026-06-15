package syncd

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"
)

type SyncEntry struct {
	ID        uint      `gorm:"primaryKey"`
	Tenant    string    `gorm:"uniqueIndex:idx_tenant_sync_id;index;not null;default:''"`
	SyncID    string    `gorm:"uniqueIndex:idx_tenant_sync_id;size:36;not null"`
	Type      string    `gorm:"not null"`
	Meta      string    `gorm:"type:text"`
	Payload   string    `gorm:"type:text"`
	DeviceID  string    `gorm:"not null"`
	Deleted   bool      `gorm:"default:false"`
	Revision  int64     `gorm:"index;not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type Engine struct {
	DB *gorm.DB
	mu sync.Mutex
}

type HostMeta struct {
	SyncID   string `json:"sync_id"`
	Alias    string `json:"alias"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Tags     string `json:"tags"`
	Group    string `json:"group"`
}

func NewEngine(database *gorm.DB) (*Engine, error) {
	if database.Migrator().HasTable(&SyncEntry{}) && database.Migrator().HasIndex(&SyncEntry{}, "idx_sync_entries_sync_id") {
		if err := database.Migrator().DropIndex(&SyncEntry{}, "idx_sync_entries_sync_id"); err != nil {
			return nil, err
		}
	}
	if err := database.AutoMigrate(&SyncEntry{}); err != nil {
		return nil, err
	}
	return &Engine{DB: database}, nil
}

func (e *Engine) Ping() error {
	return nil
}

func (e *Engine) Pull(tenant string, sinceRev int64) ([]SyncEntry, int64, error) {
	var entries []SyncEntry
	if err := e.DB.Where("tenant = ? AND revision > ?", tenant, sinceRev).Order("revision").Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	var maxRev int64
	e.DB.Model(&SyncEntry{}).Where("tenant = ?", tenant).Select("COALESCE(MAX(revision), 0)").Row().Scan(&maxRev)
	return entries, maxRev, nil
}

func (e *Engine) Push(tenant string, entries []SyncEntry) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var maxRev int64
	e.DB.Model(&SyncEntry{}).Where("tenant = ?", tenant).Select("COALESCE(MAX(revision), 0)").Row().Scan(&maxRev)

	for _, entry := range entries {
		entry.Tenant = tenant
		var existing SyncEntry
		found := e.DB.Where("tenant = ? AND sync_id = ?", tenant, entry.SyncID).First(&existing).Error == nil

		if found {
			// LWW: skip if incoming is not newer
			if !entry.UpdatedAt.After(existing.UpdatedAt) {
				continue
			}
			maxRev++
			existing.Payload = entry.Payload
			existing.Meta = entry.Meta
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

func (e *Engine) HostMetas(tenant string) ([]HostMeta, error) {
	var entries []SyncEntry
	if err := e.DB.Where("tenant = ? AND type = ? AND deleted = ?", tenant, "host", false).Find(&entries).Error; err != nil {
		return nil, err
	}
	out := make([]HostMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.Meta == "" {
			continue
		}
		var meta HostMeta
		if err := json.Unmarshal([]byte(entry.Meta), &meta); err != nil {
			continue
		}
		if meta.SyncID == "" {
			meta.SyncID = entry.SyncID
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool {
		li := out[i].Alias
		if li == "" {
			li = out[i].Hostname
		}
		lj := out[j].Alias
		if lj == "" {
			lj = out[j].Hostname
		}
		if li == lj {
			return out[i].SyncID < out[j].SyncID
		}
		return li < lj
	})
	return out, nil
}
