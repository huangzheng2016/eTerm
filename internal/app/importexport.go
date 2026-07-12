package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/sshconfig"
	"github.com/huangzheng2016/eTerm/internal/types"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

func sshConfigPath() string {
	return sshconfig.MainConfigPath()
}

func parseSSHConfigForImport() ([]sshconfig.ParsedHost, error) {
	parsed, err := sshconfig.ParseSSHConfig(sshConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parsed, nil
}

func buildSSHImportPreview(database *gorm.DB, parsed []sshconfig.ParsedHost) types.ImportSSHConfigPreviewResultMsg {
	preview := buildSSHConfigImportPreview(database, parsed)
	keyPreview := previewSSHKeyImport(database, parsed)
	preview.KeysAdded = keyPreview.imported
	preview.KeysSkipped = keyPreview.skipped
	preview.KeysFailed = keyPreview.failed
	return preview
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

type sshKeyImportStats struct {
	imported int
	skipped  int
	failed   int
}

type sshKeyImportPath struct {
	path       string
	referenced bool
}

type sshKeyFileInfo struct {
	path        string
	name        string
	keyType     string
	publicKey   string
	fingerprint string
	publicPath  string
	certPath    string
}

func previewSSHKeyImport(database *gorm.DB, parsed []sshconfig.ParsedHost) sshKeyImportStats {
	var stats sshKeyImportStats
	for _, item := range discoverSSHKeyImportPaths(parsed) {
		info, err := readSSHKeyFileInfo(item.path)
		if err != nil {
			if item.referenced {
				stats.failed++
			}
			continue
		}
		if _, ok := findExistingSSHKeyForImport(database, info.path, info.fingerprint); ok {
			stats.skipped++
			continue
		}
		stats.imported++
	}
	return stats
}

func importSSHKeys(database *gorm.DB, parsed []sshconfig.ParsedHost) sshKeyImportStats {
	var stats sshKeyImportStats
	for _, item := range discoverSSHKeyImportPaths(parsed) {
		info, err := readSSHKeyFileInfo(item.path)
		if err != nil {
			if item.referenced {
				stats.failed++
			}
			continue
		}
		if _, ok := findExistingSSHKeyForImport(database, info.path, info.fingerprint); ok {
			stats.skipped++
			continue
		}
		name := uniqueSSHImportKeyName(database, info.name)
		key := db.SSHKey{
			Name:            name,
			Type:            info.keyType,
			PublicKeyData:   info.publicKey,
			PrivatePath:     info.path,
			PublicPath:      info.publicPath,
			Fingerprint:     info.fingerprint,
			StorageMode:     "file",
			CertificatePath: info.certPath,
		}
		if err := database.Create(&key).Error; err != nil {
			stats.failed++
			continue
		}
		stats.imported++
	}
	return stats
}

func discoverSSHKeyImportPaths(parsed []sshconfig.ParsedHost) []sshKeyImportPath {
	items := make(map[string]bool)
	for _, ph := range parsed {
		path := strings.TrimSpace(ph.IdentFile)
		if path == "" {
			continue
		}
		items[path] = true
	}

	sshDir := filepath.Dir(sshConfigPath())
	entries, err := os.ReadDir(sshDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(sshDir, entry.Name())
			if !shouldTrySSHPrivateKey(path) {
				continue
			}
			if _, ok := items[path]; !ok {
				items[path] = false
			}
		}
	}

	paths := make([]string, 0, len(items))
	for path := range items {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]sshKeyImportPath, 0, len(paths))
	for _, path := range paths {
		out = append(out, sshKeyImportPath{path: path, referenced: items[path]})
	}
	return out
}

func shouldTrySSHPrivateKey(path string) bool {
	name := filepath.Base(path)
	if strings.HasSuffix(name, ".pub") || strings.HasSuffix(name, "-cert.pub") {
		return false
	}
	switch name {
	case "config", "authorized_keys":
		return false
	}
	return !strings.HasPrefix(name, "known_hosts")
}

func readSSHKeyFileInfo(path string) (sshKeyFileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sshKeyFileInfo{}, err
	}
	defer security.ClearBytes(data)
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return sshKeyFileInfo{}, err
	}
	pub := signer.PublicKey()
	info := sshKeyFileInfo{
		path:        path,
		name:        filepath.Base(path),
		keyType:     pub.Type(),
		publicKey:   string(ssh.MarshalAuthorizedKey(pub)),
		fingerprint: ssh.FingerprintSHA256(pub),
	}
	if info.name == "" || info.name == "." || info.name == string(filepath.Separator) {
		info.name = "ssh-key"
	}
	pubPath := path + ".pub"
	if _, err := os.Stat(pubPath); err == nil {
		info.publicPath = pubPath
	}
	certPath := path + "-cert.pub"
	if _, err := os.Stat(certPath); err == nil {
		info.certPath = certPath
	}
	return info, nil
}

func findExistingSSHKeyForImport(database *gorm.DB, path, fingerprint string) (db.SSHKey, bool) {
	var key db.SSHKey
	if strings.TrimSpace(path) != "" {
		if err := database.Where("private_path = ?", path).First(&key).Error; err == nil {
			return key, true
		}
	}
	if strings.TrimSpace(fingerprint) != "" {
		if err := database.Where("fingerprint = ?", fingerprint).First(&key).Error; err == nil {
			return key, true
		}
	}
	return db.SSHKey{}, false
}

func uniqueSSHImportKeyName(database *gorm.DB, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "ssh-key"
	}
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i+1)
		}
		var count int64
		database.Model(&db.SSHKey{}).Where("name = ?", name).Count(&count)
		if count == 0 {
			database.Unscoped().Where("name = ? AND deleted_at IS NOT NULL", name).Delete(&db.SSHKey{})
			return name
		}
	}
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
	parsed, err := parseSSHConfigForImport()
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
	keyID := resolveSSHKeyIDForParsed(database, ph)
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

func resolveSSHKeyIDForParsed(database *gorm.DB, ph sshconfig.ParsedHost) *uint {
	if ph.KeyName != "" {
		var key db.SSHKey
		if err := database.Where("name = ?", ph.KeyName).First(&key).Error; err == nil {
			id := key.ID
			return &id
		}
	}
	if ph.IdentFile != "" {
		var key db.SSHKey
		if err := database.Where("private_path = ?", ph.IdentFile).First(&key).Error; err == nil {
			id := key.ID
			return &id
		}
		if info, err := readSSHKeyFileInfo(ph.IdentFile); err == nil {
			if key, ok := findExistingSSHKeyForImport(database, "", info.fingerprint); ok {
				id := key.ID
				return &id
			}
		}
	}
	return nil
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
	parsed, err := parseSSHConfigForImport()
	if err != nil {
		return types.ImportSSHConfigResultMsg{Err: err}
	}

	keyStats := importSSHKeys(database, parsed)
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
						KeysImported:         keyStats.imported,
						KeysSkipped:          keyStats.skipped,
						KeysFailed:           keyStats.failed,
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
				KeysImported:         keyStats.imported,
				KeysSkipped:          keyStats.skipped,
				KeysFailed:           keyStats.failed,
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
		KeysImported:         keyStats.imported,
		KeysSkipped:          keyStats.skipped,
		KeysFailed:           keyStats.failed,
	}
}

func runSSHListImport(database *gorm.DB, hosts []importHostEntry, keys []importKeyEntry) tea.Cmd {
	return func() tea.Msg {
		imported, skipped := 0, 0
		keyByPath := make(map[string]uint)
		for _, item := range keys {
			if item.blocked && item.existingID != 0 {
				keyByPath[item.rec.Aliases[0]] = item.existingID
				continue
			}
			if (!item.selected && !item.locked) || item.sshInfo == nil {
				continue
			}
			info := item.sshInfo
			key := db.SSHKey{Name: item.chosenAlias, Type: info.keyType, PublicKeyData: info.publicKey, PrivatePath: info.path, PublicPath: info.publicPath, Fingerprint: info.fingerprint, StorageMode: "file", CertificatePath: info.certPath}
			database.Unscoped().Where("name = ? AND deleted_at IS NOT NULL", key.Name).Delete(&db.SSHKey{})
			if err := database.Create(&key).Error; err != nil {
				skipped++
				continue
			}
			keyByPath[info.path] = key.ID
			imported++
		}

		created := make(map[string]uint)
		for _, item := range hosts {
			if item.blocked || !item.selected || item.sshParsed == nil {
				continue
			}
			ph := *item.sshParsed
			ph.Alias = item.chosenAlias
			host := hostFromParsed(database, ph)
			if id, ok := keyByPath[ph.IdentFile]; ok {
				host.KeyID = &id
				host.AuthMethod = "key"
			}
			database.Unscoped().Where("alias = ? AND deleted_at IS NOT NULL", host.Alias).Delete(&db.Host{})
			if err := database.Create(&host).Error; err != nil {
				skipped++
				continue
			}
			created[item.sshParsed.Alias] = host.ID
			imported++
		}
		for _, item := range hosts {
			if item.sshParsed == nil || item.sshParsed.ProxyJump == "" {
				continue
			}
			hostID := created[item.sshParsed.Alias]
			jumpID := created[item.sshParsed.ProxyJump]
			if hostID != 0 && jumpID != 0 && hostID != jumpID {
				_ = database.Model(&db.Host{}).Where("id = ?", hostID).Update("jump_host_id", jumpID).Error
			}
		}
		return termiusImportResultMsg{imported: imported, skipped: skipped}
	}
}
