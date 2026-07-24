package sync

import (
	"fmt"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"gorm.io/gorm"
)

// Tunnel is an SSH local port forward to a remote etermsyncd HTTP listener.
type Tunnel struct {
	conn    *internalssh.ConnectResult
	fwd     *internalssh.PortForwardCloser
	baseURL string
}

// OpenTunnel connects to the sync SSH host and forwards a random local port
// to 127.0.0.1:remotePort on the remote, where etermsyncd must be listening.
func OpenTunnel(database *gorm.DB, mk *security.MasterKeyManager, hostID uint, remotePort int) (*Tunnel, error) {
	var host db.Host
	if err := database.Preload("Key").First(&host, hostID).Error; err != nil {
		return nil, fmt.Errorf("load sync host: %w", err)
	}
	if internalssh.NeedsFingerprint(database, host.Hostname, host.Port) {
		return nil, fmt.Errorf("sync host %s:%d fingerprint not trusted; connect to it once first", host.Hostname, host.Port)
	}
	var hostKey *db.SSHKey
	if host.KeyID != nil {
		hostKey = &host.Key
	}
	var jumpHost *db.Host
	var jumpKey *db.SSHKey
	if host.JumpHostID != nil {
		var jh db.Host
		if database.Preload("Key").First(&jh, *host.JumpHostID).Error == nil {
			jumpHost = &jh
			if jh.KeyID != nil {
				jumpKey = &jh.Key
			}
		}
	}
	result, err := internalssh.Connect(internalssh.ConnectConfig{
		Host:      &host,
		Key:       hostKey,
		JumpHost:  jumpHost,
		JumpKey:   jumpKey,
		MasterKey: mk,
		DB:        database,
		FingerprintCallback: func(string, int, string, string) bool {
			return false
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ssh connect: %w", err)
	}
	fwd, err := internalssh.StartLocalForward(result.Client, 0, "127.0.0.1", remotePort)
	if err != nil {
		result.Close()
		return nil, fmt.Errorf("local forward: %w", err)
	}
	return &Tunnel{
		conn:    result,
		fwd:     fwd,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", fwd.LocalPort()),
	}, nil
}

func (t *Tunnel) BaseURL() string { return t.baseURL }

func (t *Tunnel) Close() error {
	_ = t.fwd.Close()
	t.conn.Close()
	return nil
}
