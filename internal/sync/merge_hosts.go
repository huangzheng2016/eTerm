package sync

import (
	"encoding/json"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

func mergeHosts(database *gorm.DB, mk *security.MasterKeyManager, passphrase string, records []SyncRecord) (MergeResult, error) {
	var res MergeResult
	type pendingJump struct {
		hostID     uint
		jumpSyncID string
	}
	var jumps []pendingJump

	for _, r := range records {
		var existing db.Host
		found := database.Unscoped().Where("sync_id = ?", r.SyncID).First(&existing).Error == nil

		if r.Deleted {
			if found {
				if err := database.Unscoped().Delete(&existing).Error; err != nil {
					res.Failed++
					return res, err
				}
			}
			res.Merged++
			continue
		}

		plain, err := decryptPayload(r.Payload, passphrase)
		if err != nil {
			res.Failed++
			continue
		}
		var dto HostDTO
		if json.Unmarshal(plain, &dto) != nil {
			res.Failed++
			continue
		}

		ea := &encAccum{mk: mk}
		host := db.Host{
			SyncID:          dto.SyncID,
			Alias:           dto.Alias,
			Hostname:        dto.Hostname,
			Port:            dto.Port,
			Username:        dto.Username,
			AuthMethod:      dto.AuthMethod,
			Password:        ea.enc(dto.Password),
			Passphrase:      ea.enc(dto.Passphrase),
			Tags:            dto.Tags,
			Description:     dto.Description,
			Group:           dto.Group,
			ProxyType:       dto.ProxyType,
			ProxyHost:       dto.ProxyHost,
			ProxyPort:       dto.ProxyPort,
			ProxyUser:       dto.ProxyUser,
			ProxyPassword:   ea.enc(dto.ProxyPassword),
			GSSAPISource:    dto.GSSAPISource,
			GSSAPIKeytab:    dto.GSSAPIKeytab,
			KrbPrincipal:    dto.KrbPrincipal,
			ProxyCommand:    dto.ProxyCommand,
			ForwardAgent:    dto.ForwardAgent,
			RemoteCommand:   dto.RemoteCommand,
			ExtraSSHOptions: dto.ExtraSSHOptions,
			KeyID:           resolveLocalID(database, "ssh_keys", dto.KeySyncID),
		}
		if ea.err != nil {
			res.Failed++
			continue
		}

		if found {
			host.Model = existing.Model
			host.DeletedAt = gorm.DeletedAt{}
			if err := database.Save(&host).Error; err != nil {
				res.Failed++
				return res, err
			}
		} else {
			if err := database.Create(&host).Error; err != nil {
				res.Failed++
				return res, err
			}
		}

		if dto.JumpSyncID != "" {
			jumps = append(jumps, pendingJump{hostID: host.ID, jumpSyncID: dto.JumpSyncID})
		}
		res.Merged++
	}

	for _, j := range jumps {
		if jid := resolveLocalID(database, "hosts", j.jumpSyncID); jid != nil {
			if err := database.Model(&db.Host{}).Where("id = ?", j.hostID).Update("jump_host_id", *jid).Error; err != nil {
				res.Failed++
				return res, err
			}
		}
	}
	return res, nil
}
