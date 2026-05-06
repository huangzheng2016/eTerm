package app

import (
	"fmt"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/types"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"
)

const hostKeyProbeTimeout = 10 * time.Second

// hostFingerprintDialBlock returns a message to interrupt the dial (trust dialog or probe error), or nil to continue.
func hostFingerprintDialBlock(database *gorm.DB, hostID uint, hostname string, port int, connType string, streamID uint64, forwardRuleID uint) tea.Msg {
	if internalssh.NeedsFingerprint(database, hostname, port) {
		algo, fp, err := internalssh.ProbeHostKey(hostname, port, hostKeyProbeTimeout)
		if err != nil {
			return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
		}
		return types.FingerprintConfirmMsg{
			HostID: hostID, Hostname: hostname, Port: port,
			Algorithm: algo, Fingerprint: fp,
			ConnType: connType, StreamID: streamID, ForwardRuleID: forwardRuleID,
		}
	}
	changed, algo, fp, prev, err := internalssh.LiveHostKeyDiffersFromStored(database, hostname, port, hostKeyProbeTimeout)
	if err != nil {
		return types.ErrorMsg{Err: fmt.Errorf("failed to probe host key: %w", err)}
	}
	if !changed {
		return nil
	}
	return types.FingerprintConfirmMsg{
		HostID: hostID, Hostname: hostname, Port: port,
		Algorithm:           algo,
		Fingerprint:         fp,
		PreviousAlgorithm:   prev.Algorithm,
		PreviousFingerprint: prev.Fingerprint,
		ConnType:            connType,
		StreamID:            streamID,
		ForwardRuleID:       forwardRuleID,
	}
}

func fingerprintConfirmTitle(msg types.FingerprintConfirmMsg) string {
	if msg.PreviousFingerprint != "" {
		return "Host Key Changed"
	}
	return "Unknown Host Key"
}

func fingerprintConfirmBody(msg types.FingerprintConfirmMsg) string {
	if msg.PreviousFingerprint != "" {
		return fmt.Sprintf(
			"Host: %s:%d\n\nREMOTE HOST IDENTIFICATION HAS CHANGED.\n"+
				"Possible server reinstall or man-in-the-middle.\n\n"+
				"Previously trusted (%s):\n  %s\n\n"+
				"Server now presents (%s):\n  %s\n\n"+
				"Yes: update saved fingerprint and connect\n"+
				"No: cancel",
			msg.Hostname, msg.Port,
			msg.PreviousAlgorithm, msg.PreviousFingerprint,
			msg.Algorithm, msg.Fingerprint,
		)
	}
	return fmt.Sprintf(
		"Host: %s:%d\nAlgorithm: %s\nFingerprint:\n  %s\n\nTrust this host?",
		msg.Hostname, msg.Port, msg.Algorithm, msg.Fingerprint,
	)
}
