package sync

import (
	"strconv"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

type Config struct {
	Enabled     bool
	Mode        string // "http", "ssh"
	SSHHostID   uint
	RemotePort  int
	ServerURL   string
	InsecureTLS bool
	APIKey      string // plaintext
	Passphrase  string // plaintext
	Interval    int    // seconds
	DeviceID    string
	LastRev     int64
}

func (c Config) TenantID() string {
	return TenantIDFromPassphrase(c.Passphrase)
}

func LoadConfig(database *gorm.DB, mk *security.MasterKeyManager) Config {
	get := func(key, def string) string {
		v, err := db.GetSetting(database, key)
		if err != nil || v == "" {
			return def
		}
		return v
	}
	decrypt := func(key string) string {
		v, err := db.GetSetting(database, key)
		if err != nil || v == "" {
			return ""
		}
		k := mk.GetKey()
		if k == nil {
			return ""
		}
		defer k.Clear()
		plain, err := security.Decrypt(v, k.Bytes())
		if err != nil {
			return ""
		}
		return string(plain)
	}

	interval, _ := strconv.Atoi(get("sync_interval", "300"))
	lastRev, _ := strconv.ParseInt(get("sync_last_rev", "0"), 10, 64)
	hostID, _ := strconv.ParseUint(get("sync_ssh_host_id", "0"), 10, 64)
	remotePort, _ := strconv.Atoi(get("sync_remote_port", "18443"))
	if remotePort <= 0 || remotePort > 65535 {
		remotePort = 18443
	}

	mode := get("sync_mode", "http")
	if mode == "https" {
		mode = "http"
	}
	return Config{
		Enabled:     get("sync_enabled", "false") == "true",
		Mode:        mode,
		SSHHostID:   uint(hostID),
		RemotePort:  remotePort,
		ServerURL:   get("sync_server_url", ""),
		InsecureTLS: get("sync_insecure_tls", "false") == "true",
		APIKey:      decrypt("sync_api_key"),
		Passphrase:  decrypt("sync_passphrase"),
		Interval:    interval,
		DeviceID:    get("sync_device_id", ""),
		LastRev:     lastRev,
	}
}
