package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	"github.com/eterm/eterm/internal/sshconfig"
	"github.com/eterm/eterm/internal/types"
	"gorm.io/gorm"
)

func importSSHConfig(database *gorm.DB, mk *security.MasterKeyManager) types.ImportSSHConfigResultMsg {
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".ssh", "config")

	parsed, err := sshconfig.ParseSSHConfig(configPath)
	if err != nil {
		return types.ImportSSHConfigResultMsg{Err: err}
	}

	created := make(map[string]uint)

	imported := 0
	skipped := 0
	for _, ph := range parsed {
		// Skip if alias already exists
		var count int64
		database.Model(&db.Host{}).Where("alias = ? OR (hostname = ? AND port = ? AND username = ?)",
			ph.Alias, ph.Hostname, ph.Port, ph.Username).Count(&count)
		if count > 0 {
			skipped++
			continue
		}

		host := db.Host{
			Alias:        ph.Alias,
			Hostname:     ph.Hostname,
			Port:         ph.Port,
			Username:     ph.Username,
			AuthMethod:   "agent", // default for imported hosts
			ProxyCommand: ph.ProxyCommand,
		}

		// If identity file specified, try to match existing key
		if ph.IdentFile != "" {
			var key db.SSHKey
			if err := database.Where("private_path = ?", ph.IdentFile).First(&key).Error; err == nil {
				host.AuthMethod = "key"
				host.KeyID = &key.ID
			}
		}

		if err := database.Create(&host).Error; err != nil {
			continue
		}
		imported++
		created[ph.Alias] = host.ID
	}

	unresolved := 0
	for _, ph := range parsed {
		if strings.TrimSpace(ph.ProxyJump) == "" {
			continue
		}
		// Resolve for newly created hosts and existing hosts without a jump host.
		var hostID uint
		if id, ok := created[ph.Alias]; ok {
			hostID = id
		} else {
			// Existing host — check if it already has a jump host set.
			var existing db.Host
			if err := database.Where("alias = ?", ph.Alias).First(&existing).Error; err != nil {
				continue
			}
			if existing.JumpHostID != nil {
				continue // already configured, don't overwrite
			}
			hostID = existing.ID
		}
		jid := db.ResolveJumpHostID(database, ph.ProxyJump)
		if jid == nil {
			unresolved++
			continue
		}
		if *jid == hostID {
			unresolved++
			continue
		}
		if db.JumpChainPointsBackToHost(database, hostID, *jid) {
			unresolved++
			continue
		}
		if err := database.Model(&db.Host{}).Where("id = ?", hostID).Update("jump_host_id", jid).Error; err != nil {
			unresolved++
		}
	}

	return types.ImportSSHConfigResultMsg{
		Imported:             imported,
		Skipped:              skipped,
		UnresolvedProxyJumps: unresolved,
	}
}

func exportConfig(database *gorm.DB) types.ExportConfigResultMsg {
	path, err := sshconfig.ExportConfig(database)
	if err != nil {
		return types.ExportConfigResultMsg{Err: err}
	}
	return types.ExportConfigResultMsg{Path: path}
}
