package sync

import (
	"encoding/json"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

func mergeSSHKeys(database *gorm.DB, mk *security.MasterKeyManager, passphrase string, records []SyncRecord) MergeResult {
	var res MergeResult
	for _, r := range records {
		var existing db.SSHKey
		found := database.Unscoped().Where("sync_id = ?", r.SyncID).First(&existing).Error == nil

		if r.Deleted {
			if found {
				database.Unscoped().Delete(&existing)
			}
			res.Merged++
			continue
		}

		plain, err := decryptPayload(r.Payload, passphrase)
		if err != nil {
			res.Failed++
			continue
		}
		var dto SSHKeyDTO
		if json.Unmarshal(plain, &dto) != nil {
			res.Failed++
			continue
		}

		ea := &encAccum{mk: mk}
		key := db.SSHKey{
			SyncID:         dto.SyncID,
			Name:           dto.Name,
			Type:           dto.Type,
			PrivateKeyData: ea.enc(dto.PrivateKey),
			PublicKeyData:  dto.PublicKey,
			Fingerprint:    dto.Fingerprint,
			Bits:           dto.Bits,
			Passphrase:     ea.enc(dto.Passphrase),
			StorageMode:    "database",
			CertificatePath: dto.CertificatePath,
		}
		if ea.err != nil {
			res.Failed++
			continue
		}

		if found {
			key.Model = existing.Model
			database.Save(&key)
		} else {
			database.Create(&key)
		}
		res.Merged++
	}
	return res
}
