package sync

import (
	"encoding/json"

	"github.com/huangzheng2016/eTerm/internal/db"
	"gorm.io/gorm"
)

func mergePortForwards(database *gorm.DB, passphrase string, records []SyncRecord) (MergeResult, error) {
	var res MergeResult
	for _, r := range records {
		var existing db.PortForward
		found := database.Unscoped().Where("sync_id = ?", r.SyncID).First(&existing).Error == nil

		if r.Deleted {
			if found && !r.UpdatedAt.Before(existing.UpdatedAt) {
				if err := database.Unscoped().Delete(&existing).Error; err != nil {
					res.Failed++
					return res, err
				}
			}
			res.Merged++
			continue
		}

		if found && !r.UpdatedAt.After(existing.UpdatedAt) {
			continue
		}

		plain, err := decryptPayload(r.Payload, passphrase)
		if err != nil {
			res.Failed++
			continue
		}
		var dto PortForwardDTO
		if json.Unmarshal(plain, &dto) != nil {
			res.Failed++
			continue
		}

		fwd := db.PortForward{
			SyncID:     dto.SyncID,
			LocalPort:  dto.LocalPort,
			RemoteHost: dto.RemoteHost,
			RemotePort: dto.RemotePort,
			Direction:  dto.Direction,
		}
		if hid := resolveLocalID(database, "hosts", dto.HostSyncID); hid != nil {
			fwd.HostID = *hid
		} else if dto.HostSyncID != "" && found {
			fwd.HostID = existing.HostID
		}

		if found {
			fwd.Model = existing.Model
			fwd.DeletedAt = gorm.DeletedAt{}
			if err := database.Save(&fwd).Error; err != nil {
				res.Failed++
				return res, err
			}
		} else {
			if err := database.Create(&fwd).Error; err != nil {
				res.Failed++
				return res, err
			}
		}
		if err := database.Model(&fwd).UpdateColumn("updated_at", r.UpdatedAt).Error; err != nil {
			res.Failed++
			return res, err
		}
		res.Merged++
	}
	return res, nil
}

func mergeSnippets(database *gorm.DB, passphrase string, records []SyncRecord) (MergeResult, error) {
	var res MergeResult
	for _, r := range records {
		var existing db.Snippet
		found := database.Unscoped().Where("sync_id = ?", r.SyncID).First(&existing).Error == nil

		if r.Deleted {
			if found && !r.UpdatedAt.Before(existing.UpdatedAt) {
				if err := database.Unscoped().Delete(&existing).Error; err != nil {
					res.Failed++
					return res, err
				}
			}
			res.Merged++
			continue
		}

		if found && !r.UpdatedAt.After(existing.UpdatedAt) {
			continue
		}

		plain, err := decryptPayload(r.Payload, passphrase)
		if err != nil {
			res.Failed++
			continue
		}
		var dto SnippetDTO
		if json.Unmarshal(plain, &dto) != nil {
			res.Failed++
			continue
		}

		snip := db.Snippet{
			SyncID:  dto.SyncID,
			Name:    dto.Name,
			Command: dto.Command,
			Tags:    dto.Tags,
		}

		if found {
			snip.Model = existing.Model
			snip.DeletedAt = gorm.DeletedAt{}
			if err := database.Save(&snip).Error; err != nil {
				res.Failed++
				return res, err
			}
		} else {
			if err := database.Create(&snip).Error; err != nil {
				res.Failed++
				return res, err
			}
		}
		if err := database.Model(&snip).UpdateColumn("updated_at", r.UpdatedAt).Error; err != nil {
			res.Failed++
			return res, err
		}
		res.Merged++
	}
	return res, nil
}
