package db

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/glebarez/sqlite" // modernc/sqlite — works with CGO_ENABLED=0 (releases, cross-builds)
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func InitDB(dbPath string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, err
	}

	query := "?_journal_mode=WAL&_busy_timeout=5000"
	if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
		query = "?_journal_mode=MEMORY&_busy_timeout=5000"
	}
	db, err := gorm.Open(sqlite.Open(dbPath+query), &gorm.Config{
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

	backfillSyncIDs(db)

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	return db, nil
}

func backfillSyncIDs(db *gorm.DB) {
	uuidExpr := `lower(hex(randomblob(4))||'-'||hex(randomblob(2))||'-4'||substr(hex(randomblob(2)),2)||'-'||substr('89ab',abs(random())%4+1,1)||substr(hex(randomblob(2)),2)||'-'||hex(randomblob(6)))`
	for _, t := range []string{"hosts", "ssh_keys", "snippets", "port_forwards"} {
		db.Exec("UPDATE " + t + " SET sync_id = " + uuidExpr + " WHERE sync_id = '' OR sync_id IS NULL")
	}
}

func GetSetting(db *gorm.DB, key string) (string, error) {
	var setting AppSetting
	if err := db.Where("key = ?", key).First(&setting).Error; err != nil {
		return "", err
	}
	return setting.Value, nil
}

func SetSetting(db *gorm.DB, key, value string) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&AppSetting{Key: key, Value: value}).Error
}
