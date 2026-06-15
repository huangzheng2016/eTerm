package sshview

import (
	"errors"
	"io"
	"os/exec"

	"golang.org/x/crypto/ssh"
)

// shouldOfferReconnect is true when the session ended abnormally (e.g. network drop)
// rather than a normal shell exit (EOF) or remote exit status.
func shouldOfferReconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return false
	}
	var localExit *exec.ExitError
	if errors.As(err, &localExit) {
		return false
	}
	return true
}
