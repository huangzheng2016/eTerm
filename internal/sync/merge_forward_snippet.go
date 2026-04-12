package sync

import (
	"encoding/json"

	"github.com/eterm/eterm/internal/db"
	"gorm.io/gorm"
)

func mergePortForwards(database *gorm.DB, passphrase string, records []SyncRecord) MergeResult {
	var res MergeResult
	for _, r := range records {
		var existing db.PortForward
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
		}

		if found {
			fwd.Model = existing.Model
			database.Save(&fwd)
		} else {
			database.Create(&fwd)
		}
		res.Merged++
	}
	return res
}

func mergeSnippets(database *gorm.DB, passphrase string, records []SyncRecord) MergeResult {
	var res MergeResult
	for _, r := range records {
		var existing db.Snippet
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
			database.Save(&snip)
		} else {
			database.Create(&snip)
		}
		res.Merged++
	}
	return res
}
