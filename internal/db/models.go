package db

import (
	"time"

	"gorm.io/gorm"
)

type Host struct {
	gorm.Model
	SyncID      string `gorm:"uniqueIndex;size:36"`
	SyncRev     int64  `gorm:"default:0"`
	SyncDel     bool   `gorm:"default:false"`
	Alias       string `gorm:"index"`
	Hostname    string `gorm:"not null"`
	Port        int    `gorm:"default:22"`
	Username    string `gorm:"not null"`
	AuthMethod  string `gorm:"not null;default:'key'"`
	Password    string
	KeyID       *uint
	Key         SSHKey `gorm:"foreignKey:KeyID"`
	Passphrase  string
	JumpHostID  *uint
	JumpHost    *Host `gorm:"foreignKey:JumpHostID"`
	Tags        string
	Description string
	Group       string `gorm:"index;default:''"`
	// Proxy: empty = direct TCP; "http" = HTTP CONNECT; "socks5" = SOCKS5.
	ProxyType     string `gorm:"default:''"`
	ProxyHost     string
	ProxyPort     int `gorm:"default:0"`
	ProxyUser     string
	ProxyPassword string // encrypted (same as Password)
	// GSSAPI/Kerberos: "ccache" (default, uses kinit ticket) or "keytab".
	GSSAPISource string `gorm:"default:''"`
	GSSAPIKeytab string // keytab file path (keytab mode)
	KrbPrincipal string // e.g. "user@REALM" (keytab mode, required)
	// ProxyCommand: if set, takes priority over ProxyType/JumpHost.
	// Tokens: %h = hostname, %p = port, %% = literal %.
	ProxyCommand    string
	ForwardAgent    bool   `gorm:"default:false"`
	RemoteCommand   string `gorm:"type:text"`
	ExtraSSHOptions string `gorm:"type:text"`
	LastConnectedAt *time.Time
}

type SSHKey struct {
	gorm.Model
	SyncID          string `gorm:"uniqueIndex;size:36"`
	SyncRev         int64  `gorm:"default:0"`
	SyncDel         bool   `gorm:"default:false"`
	Name            string `gorm:"uniqueIndex;not null"`
	Type            string `gorm:"not null"`
	PrivateKeyData  string
	PublicKeyData   string
	PrivatePath     string
	PublicPath      string
	Fingerprint     string `gorm:"not null"`
	Bits            int
	Passphrase      string
	StorageMode     string `gorm:"default:'database'"`
	CertificatePath string
}

type HostFingerprint struct {
	gorm.Model
	Hostname    string `gorm:"uniqueIndex:idx_host_fp;not null"`
	Port        int    `gorm:"uniqueIndex:idx_host_fp;not null"`
	Algorithm   string `gorm:"not null"`
	Fingerprint string `gorm:"not null"`
	TrustedAt   time.Time
}

type AppSetting struct {
	gorm.Model
	Key   string `gorm:"uniqueIndex;not null"`
	Value string `gorm:"not null"`
}

type ConnectionHistory struct {
	gorm.Model
	HostID         uint   `gorm:"index"`
	Host           Host   `gorm:"foreignKey:HostID"`
	Label          string `gorm:"index"`
	Source         string `gorm:"index;default:'ssh'"`
	ConnectedAt    time.Time
	DisconnectedAt *time.Time
	Status         string `gorm:"default:'success'"`
	// Transcript is optional plain-text session capture (scrollback + screen), truncated at save time.
	Transcript string `gorm:"type:text"`
}

type Snippet struct {
	gorm.Model
	SyncID  string `gorm:"uniqueIndex;size:36"`
	SyncRev int64  `gorm:"default:0"`
	SyncDel bool   `gorm:"default:false"`
	Name    string `gorm:"uniqueIndex;not null"`
	Command string `gorm:"not null"`
	Tags    string
}

type PortForward struct {
	gorm.Model
	SyncID     string `gorm:"uniqueIndex;size:36"`
	SyncRev    int64  `gorm:"default:0"`
	SyncDel    bool   `gorm:"default:false"`
	HostID     uint   `gorm:"index"`
	Host       Host   `gorm:"foreignKey:HostID"`
	LocalPort  int    `gorm:"not null"`
	RemoteHost string `gorm:"not null;default:'localhost'"`
	RemotePort int    `gorm:"not null"`
	Direction  string `gorm:"not null;default:'local'"` // "local" or "remote"
}
