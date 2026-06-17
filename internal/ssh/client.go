package ssh

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

type ConnectConfig struct {
	Host                *db.Host
	Key                 *db.SSHKey
	JumpHost            *db.Host
	JumpKey             *db.SSHKey
	MasterKey           *security.MasterKeyManager
	DB                  *gorm.DB
	FingerprintCallback FingerprintCallback
}

// ConnectResult holds the SSH client and resources that must be closed when done.
type ConnectResult struct {
	Client  *ssh.Client
	Closers []io.Closer // agent conns, jump client, etc.
}

func Connect(cfg ConnectConfig) (*ConnectResult, error) {
	authMethods, authClosers, err := BuildAuthMethods(cfg.Host, cfg.Key, cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to build auth methods: %w", err)
	}
	closers := authClosers

	hostname := cfg.Host.Hostname
	port := cfg.Host.Port

	clientConfig := &ssh.ClientConfig{
		User: cfg.Host.Username,
		Auth: authMethods,
		HostKeyCallback: func(h string, remote net.Addr, key ssh.PublicKey) error {
			return VerifyHostKey(cfg.DB, hostname, port, remote, key, cfg.FingerprintCallback)
		},
		Timeout: 30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", hostname, port)

	if cfg.JumpHost != nil {
		jumpAuth, jumpClosers, err := BuildAuthMethods(cfg.JumpHost, cfg.JumpKey, cfg.MasterKey)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("failed to build jump host auth methods: %w", err)
		}
		closers = append(closers, jumpClosers...)

		jumpHostname := cfg.JumpHost.Hostname
		jumpPort := cfg.JumpHost.Port

		jumpConfig := &ssh.ClientConfig{
			User: cfg.JumpHost.Username,
			Auth: jumpAuth,
			HostKeyCallback: func(h string, remote net.Addr, key ssh.PublicKey) error {
				return VerifyHostKey(cfg.DB, jumpHostname, jumpPort, remote, key, cfg.FingerprintCallback)
			},
			Timeout: 30 * time.Second,
		}

		jumpAddr := fmt.Sprintf("%s:%d", jumpHostname, jumpPort)
		jumpConn, err := dialWithProxy(cfg.Host, jumpAddr, cfg.MasterKey)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("failed to connect to jump host: %w", err)
		}
		setNoDelay(jumpConn)
		jumpNcc, jumpChans, jumpReqs, err := ssh.NewClientConn(jumpConn, jumpAddr, jumpConfig)
		if err != nil {
			_ = jumpConn.Close()
			closeAll(closers)
			return nil, fmt.Errorf("failed to ssh handshake jump host: %w", err)
		}
		jumpClient := ssh.NewClient(jumpNcc, jumpChans, jumpReqs)

		conn, err := jumpClient.Dial("tcp", addr)
		if err != nil {
			jumpClient.Close()
			closeAll(closers)
			return nil, fmt.Errorf("failed to dial through jump host: %w", err)
		}

		ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
		if err != nil {
			conn.Close()
			jumpClient.Close()
			closeAll(closers)
			return nil, fmt.Errorf("failed to create client connection through jump host: %w", err)
		}

		// jumpClient must be closed when the session ends
		closers = append(closers, jumpClient)
		return &ConnectResult{
			Client:  ssh.NewClient(ncc, chans, reqs),
			Closers: closers,
		}, nil
	}

	tcpConn, err := dialWithProxy(cfg.Host, addr, cfg.MasterKey)
	if err != nil {
		closeAll(closers)
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	setNoDelay(tcpConn)
	ncc, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, clientConfig)
	if err != nil {
		_ = tcpConn.Close()
		closeAll(closers)
		return nil, fmt.Errorf("failed to ssh handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(ncc, chans, reqs)

	return &ConnectResult{
		Client:  client,
		Closers: closers,
	}, nil
}

func closeAll(closers []io.Closer) {
	for _, c := range closers {
		if c != nil {
			_ = c.Close()
		}
	}
}

// Close releases the SSH client and associated resources (jump host, agent, etc.).
func (r *ConnectResult) Close() {
	if r == nil {
		return
	}
	if r.Client != nil {
		_ = r.Client.Close()
	}
	closeAll(r.Closers)
	r.Client = nil
	r.Closers = nil
}
