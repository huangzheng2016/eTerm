package sync

import "time"

type RecordType string

const (
	TypeHost    RecordType = "host"
	TypeSSHKey  RecordType = "ssh_key"
	TypeSnippet RecordType = "snippet"
	TypePortFwd RecordType = "port_forward"
)

// SyncRecord is the wire format envelope. Payload is age-encrypted, base64-encoded JSON.
type SyncRecord struct {
	SyncID    string     `json:"sync_id"`
	Type      RecordType `json:"type"`
	Deleted   bool       `json:"deleted"`
	DeviceID  string     `json:"device_id"`
	Meta      string     `json:"meta,omitempty"`
	Payload   string     `json:"payload"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type HostMeta struct {
	SyncID   string `json:"sync_id"`
	Alias    string `json:"alias"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Tags     string `json:"tags"`
	Group    string `json:"group"`
}

type HostDTO struct {
	SyncID          string `json:"sync_id"`
	Alias           string `json:"alias"`
	Hostname        string `json:"hostname"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	AuthMethod      string `json:"auth_method"`
	Password        string `json:"password"`
	KeySyncID       string `json:"key_sync_id"`
	Passphrase      string `json:"passphrase"`
	JumpSyncID      string `json:"jump_sync_id"`
	Tags            string `json:"tags"`
	Description     string `json:"description"`
	Group           string `json:"group"`
	ProxyType       string `json:"proxy_type"`
	ProxyHost       string `json:"proxy_host"`
	ProxyPort       int    `json:"proxy_port"`
	ProxyUser       string `json:"proxy_user"`
	ProxyPassword   string `json:"proxy_password"`
	GSSAPISource    string `json:"gssapi_source"`
	GSSAPIKeytab    string `json:"gssapi_keytab"`
	KrbPrincipal    string `json:"krb_principal"`
	ProxyCommand    string `json:"proxy_command"`
	ForwardAgent    bool   `json:"forward_agent"`
	RemoteCommand   string `json:"remote_command"`
	ExtraSSHOptions string `json:"extra_ssh_options"`
}

type SSHKeyDTO struct {
	SyncID          string `json:"sync_id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	PrivateKey      string `json:"private_key"`
	PublicKey       string `json:"public_key"`
	Fingerprint     string `json:"fingerprint"`
	Bits            int    `json:"bits"`
	Passphrase      string `json:"passphrase"`
	CertificatePath string `json:"certificate_path"`
}

type SnippetDTO struct {
	SyncID  string `json:"sync_id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Tags    string `json:"tags"`
}

type PortForwardDTO struct {
	SyncID     string `json:"sync_id"`
	HostSyncID string `json:"host_sync_id"`
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
	Direction  string `json:"direction"`
}
