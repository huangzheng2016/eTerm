package ssh

import (
	"fmt"
	"net"
	"time"

	"github.com/huangzheng2016/eTerm/internal/db"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

type FingerprintCallback func(hostname string, port int, algorithm string, fingerprint string) bool

// ProbeHostKey connects to a host just to retrieve its public key, then disconnects.
func ProbeHostKey(hostname string, port int, timeout time.Duration) (algorithm, fingerprint string, err error) {
	addr := net.JoinHostPort(hostname, fmt.Sprintf("%d", port))
	var hostKey ssh.PublicKey

	config := &ssh.ClientConfig{
		User: "probe",
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(h string, remote net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return fmt.Errorf("probe complete") // abort after getting key
		},
		Timeout: timeout,
	}

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return "", "", fmt.Errorf("failed to connect to %s: %w", addr, err)
	}
	defer conn.Close()

	c, _, _, err := ssh.NewClientConn(conn, addr, config)
	if c != nil {
		c.Close()
	}
	// We expect the "probe complete" error from our callback
	if hostKey == nil {
		return "", "", fmt.Errorf("failed to retrieve host key from %s: %v", addr, err)
	}

	return hostKey.Type(), ssh.FingerprintSHA256(hostKey), nil
}

// NeedsFingerprint checks if a host fingerprint is already stored in the database.
func NeedsFingerprint(database *gorm.DB, hostname string, port int) bool {
	var existing db.HostFingerprint
	result := database.Where("hostname = ? AND port = ?", hostname, port).First(&existing)
	return result.Error == gorm.ErrRecordNotFound
}

// LiveHostKeyDiffersFromStored reports whether the live server key differs from the DB record.
// When no row exists, returns (false, "", "", zero, nil) so callers still use NeedsFingerprint first.
func LiveHostKeyDiffersFromStored(database *gorm.DB, hostname string, port int, timeout time.Duration) (differs bool, newAlgo, newFP string, stored db.HostFingerprint, err error) {
	var existing db.HostFingerprint
	result := database.Where("hostname = ? AND port = ?", hostname, port).First(&existing)
	if result.Error == gorm.ErrRecordNotFound {
		return false, "", "", existing, nil
	}
	if result.Error != nil {
		return false, "", "", existing, fmt.Errorf("query host fingerprint: %w", result.Error)
	}
	newAlgo, newFP, err = ProbeHostKey(hostname, port, timeout)
	if err != nil {
		return false, "", "", existing, err
	}
	if newFP != existing.Fingerprint {
		return true, newAlgo, newFP, existing, nil
	}
	return false, newAlgo, newFP, existing, nil
}

func VerifyHostKey(database *gorm.DB, hostname string, port int, remote net.Addr, key ssh.PublicKey, callback FingerprintCallback) error {
	fingerprint := ssh.FingerprintSHA256(key)
	algorithm := key.Type()

	var existing db.HostFingerprint
	result := database.Where("hostname = ? AND port = ?", hostname, port).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		if !callback(hostname, port, algorithm, fingerprint) {
			return fmt.Errorf("host key rejected by user for %s:%d", hostname, port)
		}

		record := db.HostFingerprint{
			Hostname:    hostname,
			Port:        port,
			Algorithm:   algorithm,
			Fingerprint: fingerprint,
			TrustedAt:   time.Now(),
		}
		if err := database.Create(&record).Error; err != nil {
			return fmt.Errorf("failed to save host fingerprint: %w", err)
		}

		return nil
	}

	if result.Error != nil {
		return fmt.Errorf("failed to query host fingerprint: %w", result.Error)
	}

	if existing.Fingerprint != fingerprint {
		return fmt.Errorf(
			"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED for %s:%d!\n"+
				"Previous fingerprint: %s\n"+
				"Current fingerprint: %s\n"+
				"This could indicate a man-in-the-middle attack",
			hostname, port, existing.Fingerprint, fingerprint,
		)
	}

	return nil
}
