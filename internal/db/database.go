package db

import (
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite" // modernc/sqlite — works with CGO_ENABLED=0 (releases, cross-builds)
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := os.Chmod(dbPath, 0600); err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Host{}, &SSHKey{}, &HostFingerprint{}, &AppSetting{}, &ConnectionHistory{}, &Snippet{}, &PortForward{}); err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	return db, nil
}

func GetSetting(db *gorm.DB, key string) (string, error) {
	var setting AppSetting
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		return "", err
	}
	return setting.Value, nil
}

func SetSetting(db *gorm.DB, key, value string) error {
	var setting AppSetting
	result := db.Where("key = ?", key).First(&setting)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return db.Create(&AppSetting{Key: key, Value: value}).Error
		}
		return result.Error
	}
	setting.Value = value
	return db.Save(&setting).Error
}
