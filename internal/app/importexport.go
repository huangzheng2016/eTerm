package app

import (
	"fmt"
	"strings"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/eTerm/internal/types"
	"gorm.io/gorm"
)

func sshConfigPath() string {
	return sshconfig.MainConfigPath()
}

func buildSSHConfigImportPreview(database *gorm.DB, parsed []sshconfig.ParsedHost) types.ImportSSHConfigPreviewResultMsg {
	var preview types.ImportSSHConfigPreviewResultMsg
	for _, ph := range parsed {
		existing, ok := findHostByParsed(database, ph)
		if !ok {
			preview.Added++
			continue
		}
		incoming := hostFromParsed(database, ph)
		if importComparableHost(existing) == importComparableHost(incoming) {
			preview.Skipped++
		} else {
			preview.Changed++
		}
	}
	return preview
}

type comparableImportHost struct {
	Alias           string
	Hostname        string
	Port            int
	Username        string
	AuthMethod      string
	KeyID           uint
	HasKeyID        bool
	Group           string
	Tags            string
	Description     string
	ProxyType       string
	ProxyHost       string
	ProxyPort       int
	ProxyUser       string
	ProxyCommand    string
	GSSAPISource    string
	GSSAPIKeytab    string
	KrbPrincipal    string
	ForwardAgent    bool
	RemoteCommand   string
	ExtraSSHOptions string
}

func importComparableHost(h db.Host) comparableImportHost {
	var keyID uint
	hasKeyID := false
	if h.KeyID != nil {
		keyID = *h.KeyID
		hasKeyID = true
	}
	return comparableImportHost{
		Alias:           h.Alias,
		Hostname:        h.Hostname,
		Port:            h.Port,
		Username:        h.Username,
		AuthMethod:      h.AuthMethod,
		KeyID:           keyID,
		HasKeyID:        hasKeyID,
		Group:           h.Group,
		Tags:            h.Tags,
		Description:     h.Description,
		ProxyType:       h.ProxyType,
		ProxyHost:       h.ProxyHost,
		ProxyPort:       h.ProxyPort,
		ProxyUser:       h.ProxyUser,
		ProxyCommand:    h.ProxyCommand,
		GSSAPISource:    h.GSSAPISource,
		GSSAPIKeytab:    h.GSSAPIKeytab,
		KrbPrincipal:    h.KrbPrincipal,
		ForwardAgent:    h.ForwardAgent,
		RemoteCommand:   h.RemoteCommand,
		ExtraSSHOptions: h.ExtraSSHOptions,
	}
}

// CountImportConflicts returns how many parsed host blocks match an existing DB row.
func CountImportConflicts(database *gorm.DB) (int, error) {
	parsed, err := sshconfig.ParseSSHConfig(sshConfigPath())
	if err != nil {
		return 0, err
	}
	if len(parsed) == 0 {
		return 0, nil
	}

	// Batch: collect all aliases for a single IN query
	aliases := make([]string, len(parsed))
	for i, ph := range parsed {
		aliases[i] = ph.Alias
	}
	var matchedAliases []string
	database.Model(&db.Host{}).Where("alias IN ?", aliases).Pluck("alias", &matchedAliases)
	aliasSet := make(map[string]bool, len(matchedAliases))
	for _, a := range matchedAliases {
		aliasSet[a] = true
	}

	n := 0
	for _, ph := range parsed {
		if aliasSet[ph.Alias] {
			n++
			continue
		}
		// Fallback: check by endpoint (less common, still per-row but only for non-alias matches)
		var count int64
		database.Model(&db.Host{}).Where("hostname = ? AND port = ? AND username = ?",
			ph.Hostname, ph.Port, ph.Username).Count(&count)
		if count > 0 {
			n++
		}
	}
	return n, nil
}

// findHostByParsed loads an existing host matching the SSH config block (alias or same endpoint).
func findHostByParsed(database *gorm.DB, ph sshconfig.ParsedHost) (db.Host, bool) {
	var h db.Host
	if err := database.Where("alias = ?", ph.Alias).First(&h).Error; err == nil {
		return h, true
	}
	if err := database.Where("hostname = ? AND port = ? AND username = ?", ph.Hostname, ph.Port, ph.Username).First(&h).Error; err == nil {
		return h, true
	}
	return h, false
}

func hostFromParsed(database *gorm.DB, ph sshconfig.ParsedHost) db.Host {
	var keyID *uint
	if ph.KeyName != "" {
		var key db.SSHKey
		if err := database.Where("name = ?", ph.KeyName).First(&key).Error; err == nil {
			id := key.ID
			keyID = &id
		}
	}
	if keyID == nil && ph.IdentFile != "" {
		var key db.SSHKey
		if err := database.Where("private_path = ?", ph.IdentFile).First(&key).Error; err == nil {
			id := key.ID
			keyID = &id
		}
	}

	authMethod, gssapiSource := importedAuthFromParsed(ph, keyID != nil)
	if authMethod != "key" {
		keyID = nil
	}

	host := db.Host{
		Alias:           ph.Alias,
		Hostname:        ph.Hostname,
		Port:            ph.Port,
		Username:        ph.Username,
		AuthMethod:      authMethod,
		KeyID:           keyID,
		Group:           ph.Group,
		Tags:            ph.Tags,
		Description:     ph.Description,
		ProxyType:       ph.ProxyType,
		ProxyHost:       ph.ProxyHost,
		ProxyPort:       ph.ProxyPort,
		ProxyUser:       ph.ProxyUser,
		ProxyCommand:    ph.ProxyCommand,
		GSSAPISource:    gssapiSource,
		GSSAPIKeytab:    ph.GSSAPIKeytab,
		KrbPrincipal:    ph.KrbPrincipal,
		ForwardAgent:    ph.ForwardAgent,
		RemoteCommand:   ph.RemoteCommand,
		ExtraSSHOptions: ph.ExtraSSHOptions,
	}
	if ph.Username == "" {
		host.Username = "root"
	}
	return host
}

func importedAuthFromParsed(ph sshconfig.ParsedHost, hasKey bool) (string, string) {
	if ph.AuthMethod != "" {
		switch ph.AuthMethod {
		case "gssapi":
			src := ph.GSSAPISource
			if src == "" {
				src = "ccache"
			}
			return "gssapi", src
		case "key":
			if hasKey {
				return "key", ""
			}
		case "password", "interactive", "agent":
			return ph.AuthMethod, ""
		}
	}
	for _, pref := range ph.PreferredAuthentications {
		switch strings.ToLower(strings.TrimSpace(pref)) {
		case "gssapi-with-mic":
			return "gssapi", "ccache"
		case "publickey":
			if hasKey {
				return "key", ""
			}
		case "keyboard-interactive":
			return "interactive", ""
		case "password":
			return "password", ""
		}
	}
	if ph.GSSAPIAuthentication {
		return "gssapi", "ccache"
	}
	if hasKey {
		return "key", ""
	}
	return "agent", ""
}

func importSSHConfig(database *gorm.DB, strategy string) types.ImportSSHConfigResultMsg {
	parsed, err := sshconfig.ParseSSHConfig(sshConfigPath())
	if err != nil {
		return types.ImportSSHConfigResultMsg{Err: err}
	}

	created := make(map[string]uint)
	imported := 0
	skipped := 0
	overwritten := 0
	unresolved := 0

	for _, ph := range parsed {
		var count int64
		database.Model(&db.Host{}).Where("alias = ? OR (hostname = ? AND port = ? AND username = ?)",
			ph.Alias, ph.Hostname, ph.Port, ph.Username).Count(&count)
		if count > 0 {
			if strategy == "overwrite" {
				h, ok := findHostByParsed(database, ph)
				if !ok {
					skipped++
					continue
				}
				nh := hostFromParsed(database, ph)
				updates := map[string]interface{}{
					"alias":             nh.Alias,
					"hostname":          nh.Hostname,
					"port":              nh.Port,
					"username":          nh.Username,
					"auth_method":       nh.AuthMethod,
					"key_id":            nh.KeyID,
					"group":             nh.Group,
					"tags":              nh.Tags,
					"description":       nh.Description,
					"proxy_type":        nh.ProxyType,
					"proxy_host":        nh.ProxyHost,
					"proxy_port":        nh.ProxyPort,
					"proxy_user":        nh.ProxyUser,
					"proxy_command":     nh.ProxyCommand,
					"gssapi_source":     nh.GSSAPISource,
					"gssapi_keytab":     nh.GSSAPIKeytab,
					"krb_principal":     nh.KrbPrincipal,
					"forward_agent":     nh.ForwardAgent,
					"remote_command":    nh.RemoteCommand,
					"extra_ssh_options": nh.ExtraSSHOptions,
				}
				if err := database.Model(&db.Host{}).Where("id = ?", h.ID).Updates(updates).Error; err != nil {
					return types.ImportSSHConfigResultMsg{
						Imported:             imported,
						Skipped:              skipped,
						Overwritten:          overwritten,
						UnresolvedProxyJumps: unresolved,
						Err:                  fmt.Errorf("overwrite %q: %w", ph.Alias, err),
					}
				}
				overwritten++
				created[ph.Alias] = h.ID
				continue
			}
			skipped++
			continue
		}

		host := hostFromParsed(database, ph)
		if err := database.Create(&host).Error; err != nil {
			return types.ImportSSHConfigResultMsg{
				Imported:             imported,
				Skipped:              skipped,
				Overwritten:          overwritten,
				UnresolvedProxyJumps: unresolved,
				Err:                  fmt.Errorf("import %q: %w", ph.Alias, err),
			}
		}
		imported++
		created[ph.Alias] = host.ID
	}

	for _, ph := range parsed {
		if strings.TrimSpace(ph.ProxyJump) == "" {
			continue
		}
		var hostID uint
		if id, ok := created[ph.Alias]; ok {
			hostID = id
		} else {
			existing, ok := findHostByParsed(database, ph)
			if !ok {
				continue
			}
			if existing.JumpHostID != nil {
				continue
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
		Overwritten:          overwritten,
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
