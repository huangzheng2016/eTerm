package sshconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/eterm/eterm/internal/config"
	"github.com/eterm/eterm/internal/db"
	"gorm.io/gorm"
)

type exportedHost struct {
	Alias        string `json:"alias"`
	Hostname     string `json:"hostname"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	AuthMethod   string `json:"auth_method"`
	Group        string `json:"group,omitempty"`
	Tags         string `json:"tags,omitempty"`
	ProxyType    string `json:"proxy_type,omitempty"`
	ProxyHost    string `json:"proxy_host,omitempty"`
	ProxyPort    int    `json:"proxy_port,omitempty"`
	ProxyUser    string `json:"proxy_user,omitempty"`
	ProxyCommand string `json:"proxy_command,omitempty"`
	JumpAlias    string `json:"jump_alias,omitempty"`
	GSSAPISource string `json:"gssapi_source,omitempty"`
	KrbPrincipal string `json:"krb_principal,omitempty"`
	GSSAPIKeytab string `json:"gssapi_keytab,omitempty"`
}

// ExportConfig exports all hosts to a JSON file.
func ExportConfig(database *gorm.DB) (string, error) {
	var hosts []db.Host
	if err := database.Preload("JumpHost").Find(&hosts).Error; err != nil {
		return "", err
	}

	exported := make([]exportedHost, len(hosts))
	for i, h := range hosts {
		exported[i] = exportedHost{
			Alias:      h.Alias,
			Hostname:   h.Hostname,
			Port:       h.Port,
			Username:   h.Username,
			AuthMethod: h.AuthMethod,
			Group:      h.Group,
			Tags:       h.Tags,
			ProxyType:  h.ProxyType,
			ProxyHost:  h.ProxyHost,
			ProxyPort:    h.ProxyPort,
			ProxyUser:    h.ProxyUser,
			ProxyCommand: h.ProxyCommand,
			GSSAPISource: h.GSSAPISource,
			KrbPrincipal: h.KrbPrincipal,
			GSSAPIKeytab: h.GSSAPIKeytab,
		}
		if h.JumpHost != nil && h.JumpHost.ID > 0 {
			ja := strings.TrimSpace(h.JumpHost.Alias)
			if ja == "" {
				ja = h.JumpHost.Hostname
			}
			exported[i].JumpAlias = ja
		}
	}

	data, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return "", err
	}

	outPath := filepath.Join(config.ConfigDir(), "export.json")
	if err := os.WriteFile(outPath, data, 0600); err != nil {
		return "", err
	}

	return outPath, nil
}
