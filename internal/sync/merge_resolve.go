package sync

import "gorm.io/gorm"

func resolveLocalID(database *gorm.DB, table, syncID string) *uint {
	if syncID == "" {
		return nil
	}
	var id uint
	err := database.Table(table).Select("id").Where("sync_id = ?", syncID).Row().Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}
