package app

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/termius_exporter/pkg/exporter"
	"github.com/huangzheng2016/termius_exporter/pkg/parser"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// Internal message types for the Termius import flow.
type termiusLoadMsg struct{}
type sshConfigLoadMsg struct{}

type termiusExportResultMsg struct {
	hosts     []parser.HostRecord
	keys      []parser.KeyRecord
	sshParsed []sshconfig.ParsedHost
	sshSource bool
	err       error
}

type termiusHostsReadyMsg struct {
	hostItems []importHostEntry
	// allKeys is read from a.importHostList.allKeys in app_update.go
}

type termiusImportRunMsg struct {
	hosts []importHostEntry
	keys  []importKeyEntry
}

type termiusImportResultMsg struct {
	imported int
	skipped  int
	err      error
}

// importHostEntry is one row in the host list overlay.
type importHostEntry struct {
	rec          parser.HostRecord
	sshParsed    *sshconfig.ParsedHost
	selected     bool
	blocked      bool // exact duplicate - not selectable
	nameConflict bool // same alias, different content - must rename before import
	chosenAlias  string
	exportID     uint
	existing     bool
}

// importKeyEntry is one row in the key list overlay.
type importKeyEntry struct {
	rec          parser.KeyRecord
	sshInfo      *sshKeyFileInfo
	selected     bool
	blocked      bool // exact duplicate - not selectable
	locked       bool // required by a selected host - cannot deselect
	nameConflict bool // same name, different fingerprint - must rename
	chosenAlias  string
	fingerprint  string
	existingID   uint
	exportID     uint
}

const unknownImportAlias = "<UNKNOWN>"

func loadTermiusData() tea.Cmd {
	return func() tea.Msg {
		hosts, keys, err := exporter.Export("")
		return termiusExportResultMsg{hosts: hosts, keys: keys, err: err}
	}
}

func loadSSHConfigData() tea.Cmd {
	return func() tea.Msg {
		parsed, err := parseSSHConfigForImport()
		if err != nil {
			return termiusExportResultMsg{err: err}
		}
		hosts := make([]parser.HostRecord, 0, len(parsed))
		for i := range parsed {
			ph := &parsed[i]
			hosts = append(hosts, parser.HostRecord{Aliases: []string{ph.Alias}, Host: ph.Hostname, Port: ph.Port, Username: ph.Username, KeyName: ph.IdentFile})
		}
		keys := make([]parser.KeyRecord, 0)
		for _, path := range discoverSSHKeyImportPaths(parsed) {
			info, readErr := readSSHKeyFileInfo(path.path)
			if readErr != nil {
				continue
			}
			data, readErr := os.ReadFile(info.path)
			if readErr != nil {
				continue
			}
			keys = append(keys, parser.KeyRecord{Aliases: []string{info.path}, PrivateKey: string(data)})
			security.ClearBytes(data)
		}
		msg := termiusExportResultMsg{hosts: hosts, keys: keys, sshSource: true}
		msg.sshParsed = parsed
		return msg
	}
}

// buildHostItems checks each HostRecord against the DB and returns importHostEntry rows.
func buildHostItems(database *gorm.DB, hosts []parser.HostRecord) []importHostEntry {
	items := make([]importHostEntry, 0, len(hosts))
	for _, h := range hosts {
		alias := unknownImportAlias
		if len(h.Aliases) > 0 {
			alias = h.Aliases[0]
		}
		var existing db.Host
		blocked := false
		nameConflict := false
		if err := database.Where("alias = ?", alias).First(&existing).Error; err == nil {
			if existing.Hostname == h.Host && existing.Port == h.Port && existing.Username == h.Username {
				blocked = true
			} else {
				nameConflict = true
			}
		}
		items = append(items, importHostEntry{
			rec:          h,
			blocked:      blocked,
			nameConflict: nameConflict,
			chosenAlias:  alias,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].chosenAlias) < strings.ToLower(items[j].chosenAlias)
	})
	return items
}

func buildSSHHostItems(database *gorm.DB, parsed []sshconfig.ParsedHost, hosts []parser.HostRecord) []importHostEntry {
	items := buildHostItems(database, hosts)
	for i := range items {
		for j := range parsed {
			if parsed[j].Alias == items[i].chosenAlias {
				items[i].sshParsed = &parsed[j]
				var existing db.Host
				if err := database.Where("alias = ?", parsed[j].Alias).First(&existing).Error; err == nil {
					incoming := hostFromParsed(database, parsed[j])
					items[i].blocked = importComparableHost(existing) == importComparableHost(incoming)
					items[i].nameConflict = !items[i].blocked
				}
				break
			}
		}
	}
	return items
}

// buildKeyItems checks each KeyRecord against the DB and returns importKeyEntry rows.
func buildKeyItems(database *gorm.DB, keys []parser.KeyRecord) []importKeyEntry {
	fps := make([]string, len(keys))
	for i, k := range keys {
		fps[i] = computeKeyFingerprint(k.PrivateKey)
	}
	return buildKeyItemsWithFP(database, keys, fps)
}

func buildSSHKeyItems(database *gorm.DB, keys []parser.KeyRecord) []importKeyEntry {
	items := buildKeyItems(database, keys)
	for i := range items {
		info, err := readSSHKeyFileInfo(keys[i].Aliases[0])
		if err == nil {
			items[i].sshInfo = &info
			items[i].chosenAlias = info.name
			var existing db.SSHKey
			if findErr := database.Where("name = ?", info.name).First(&existing).Error; findErr == nil && existing.Fingerprint != info.fingerprint {
				items[i].nameConflict = true
				items[i].blocked = false
				items[i].existingID = 0
			}
		}
	}
	return items
}

// buildKeyItemsWithFP is the testable version that accepts pre-computed fingerprints.
func buildKeyItemsWithFP(database *gorm.DB, keys []parser.KeyRecord, fps []string) []importKeyEntry {
	items := make([]importKeyEntry, 0, len(keys))
	for i, k := range keys {
		alias := ""
		if len(k.Aliases) > 0 {
			alias = k.Aliases[0]
		}
		fp := fps[i]
		var existing db.SSHKey
		blocked := false
		nameConflict := false
		existingID := uint(0)
		if err := database.Where("name = ?", alias).First(&existing).Error; err == nil {
			if existing.Fingerprint == fp && fp != "" {
				blocked = true
				existingID = existing.ID
			} else {
				nameConflict = true
			}
		}
		if !blocked && !nameConflict && fp != "" {
			if err := database.Where("fingerprint = ?", fp).First(&existing).Error; err == nil {
				blocked = true
				existingID = existing.ID
			}
		}
		items = append(items, importKeyEntry{
			rec:          k,
			blocked:      blocked,
			nameConflict: nameConflict,
			chosenAlias:  alias,
			fingerprint:  fp,
			existingID:   existingID,
		})
	}
	return items
}

// lockRequiredKeys marks keys that are referenced by selected hosts as locked=true.
func lockRequiredKeys(hosts []importHostEntry, keys []importKeyEntry) []importKeyEntry {
	needed := make(map[string]bool)
	for _, h := range hosts {
		if h.selected && h.rec.KeyName != "" {
			needed[h.rec.KeyName] = true
		}
	}
	result := make([]importKeyEntry, len(keys))
	copy(result, keys)
	for i, k := range result {
		for _, alias := range k.rec.Aliases {
			if needed[alias] {
				result[i].locked = true
				result[i].selected = true
				break
			}
		}
	}
	return result
}

func computeKeyFingerprint(privateKeyPEM string) string {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return ""
	}
	h := sha256.Sum256(signer.PublicKey().Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
}

func runTermiusImport(database *gorm.DB, masterKey *security.MasterKeyManager, hosts []importHostEntry, keys []importKeyEntry) tea.Cmd {
	return func() tea.Msg {
		imported := 0
		skipped := 0
		keyAliasToID := make(map[string]uint)

		secKey := masterKey.GetKey()
		if secKey == nil {
			return termiusImportResultMsg{err: fmt.Errorf("master key is locked")}
		}
		defer secKey.Clear()

		for _, ki := range keys {
			if ki.blocked && ki.existingID != 0 {
				for _, a := range ki.rec.Aliases {
					keyAliasToID[a] = ki.existingID
				}
				keyAliasToID[ki.chosenAlias] = ki.existingID
				continue
			}
			if ki.blocked || (!ki.selected && !ki.locked) {
				continue
			}
			signer, err := ssh.ParsePrivateKey([]byte(ki.rec.PrivateKey))
			if err != nil {
				skipped++
				continue
			}
			fp := ki.fingerprint
			if fp == "" {
				h := sha256.Sum256(signer.PublicKey().Marshal())
				fp = "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
			}
			encrypted, err := security.Encrypt([]byte(ki.rec.PrivateKey), secKey.Bytes())
			if err != nil {
				skipped++
				continue
			}
			// Remove any soft-deleted record with the same name before re-creating.
			database.Unscoped().Where("name = ? AND deleted_at IS NOT NULL", ki.chosenAlias).Delete(&db.SSHKey{})
			k := db.SSHKey{
				SyncID:         fmt.Sprintf("termius-%s", ki.chosenAlias),
				Name:           ki.chosenAlias,
				Type:           signer.PublicKey().Type(),
				PrivateKeyData: encrypted,
				PublicKeyData:  string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
				Fingerprint:    fp,
				StorageMode:    "database",
			}
			if err := database.Create(&k).Error; err != nil {
				skipped++
				continue
			}
			for _, a := range ki.rec.Aliases {
				keyAliasToID[a] = k.ID
			}
			keyAliasToID[ki.chosenAlias] = k.ID
			imported++
		}

		for _, hi := range hosts {
			if hi.blocked || !hi.selected {
				continue
			}
			var keyID *uint
			if hi.rec.KeyName != "" {
				if id, ok := keyAliasToID[hi.rec.KeyName]; ok {
					keyID = &id
				} else {
					var existingKey db.SSHKey
					if err := database.Where("name = ?", hi.rec.KeyName).First(&existingKey).Error; err == nil {
						id := existingKey.ID
						keyID = &id
					}
				}
			}
			authMethod := "agent"
			if keyID != nil {
				authMethod = "key"
			} else if hi.rec.KeyID > 0 || hi.rec.KeyName != "" {
				// Key referenced but not found/imported — mark as key auth so the
				// user sees the correct method.
				authMethod = "key"
			} else if hi.rec.Password != "" {
				authMethod = "password"
			}
			var encPassword string
			if hi.rec.Password != "" {
				if enc, err := security.Encrypt([]byte(hi.rec.Password), secKey.Bytes()); err == nil {
					encPassword = enc
				}
			}
			// Remove any soft-deleted host with the same alias before re-creating.
			database.Unscoped().Where("alias = ? AND deleted_at IS NOT NULL", hi.chosenAlias).Delete(&db.Host{})
			h := db.Host{
				SyncID:     fmt.Sprintf("termius-%s", hi.chosenAlias),
				Alias:      hi.chosenAlias,
				Hostname:   hi.rec.Host,
				Port:       hi.rec.Port,
				Username:   hi.rec.Username,
				AuthMethod: authMethod,
				KeyID:      keyID,
				Password:   encPassword,
			}
			if err := database.Create(&h).Error; err != nil {
				skipped++
				continue
			}
			imported++
		}
		return termiusImportResultMsg{imported: imported, skipped: skipped}
	}
}
