package db

import "gorm.io/gorm"

func DeleteHostForSync(database *gorm.DB, id uint) error {
	return markDeletedAndSoftDelete(database, &Host{}, id)
}

func DeleteSSHKeyForSync(database *gorm.DB, id uint) error {
	return markDeletedAndSoftDelete(database, &SSHKey{}, id)
}

func DeleteSnippetForSync(database *gorm.DB, id uint) error {
	return markDeletedAndSoftDelete(database, &Snippet{}, id)
}

func DeletePortForwardForSync(database *gorm.DB, id uint) error {
	return markDeletedAndSoftDelete(database, &PortForward{}, id)
}

func markDeletedAndSoftDelete(database *gorm.DB, model interface{}, id uint) error {
	return database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(model).Where("id = ?", id).Update("sync_del", true).Error; err != nil {
			return err
		}
		return tx.Delete(model, id).Error
	})
}
