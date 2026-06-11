package ssh

import (
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
)

type ErrorKind int

const (
	ErrKindUnknown ErrorKind = iota
	ErrKindAuth
	ErrKindRefused
	ErrKindTimeout
	ErrKindDNS
	ErrKindHostKey
	ErrKindUnreachable
	ErrKindLocked
)

// Short returns a one-word label for compact UI (e.g. batch result rows).
func (k ErrorKind) Short() string {
	switch k {
	case ErrKindAuth:
		return "auth"
	case ErrKindRefused:
		return "refused"
	case ErrKindTimeout:
		return "timeout"
	case ErrKindDNS:
		return "dns"
	case ErrKindHostKey:
		return "host-key"
	case ErrKindUnreachable:
		return "unreachable"
	case ErrKindLocked:
		return "locked"
	default:
		return "error"
	}
}

// ConnectError wraps a connection failure with a user-facing classification.
type ConnectError struct {
	Kind    ErrorKind
	Summary string
	Hint    string
	Err     error
}

func (e *ConnectError) Error() string { return e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// Classify maps a low-level connection error to a ConnectError. Returns nil for nil.
func Classify(err error) *ConnectError {
	if err == nil {
		return nil
	}
	kind := classifyKind(err)
	summary, hint := describe(kind, err)
	return &ConnectError{Kind: kind, Summary: summary, Hint: hint, Err: err}
}

func classifyKind(err error) ErrorKind {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrKindDNS
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrKindTimeout
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return ErrKindTimeout
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return ErrKindRefused
	}
	if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return ErrKindUnreachable
	}

	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "no such host"), strings.Contains(s, "name resolution"):
		return ErrKindDNS
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "deadline exceeded"):
		return ErrKindTimeout
	case strings.Contains(s, "connection refused"):
		return ErrKindRefused
	case strings.Contains(s, "no route to host"), strings.Contains(s, "network is unreachable"):
		return ErrKindUnreachable
	case strings.Contains(s, "master key is locked"):
		return ErrKindLocked
	case strings.Contains(s, "host key"), strings.Contains(s, "knownhosts"):
		return ErrKindHostKey
	case strings.Contains(s, "unable to authenticate"),
		strings.Contains(s, "no supported methods remain"),
		strings.Contains(s, "permission denied"),
		strings.Contains(s, "decrypt password"),
		strings.Contains(s, "load private key"),
		strings.Contains(s, "ssh-agent"):
		return ErrKindAuth
	default:
		return ErrKindUnknown
	}
}

func describe(kind ErrorKind, err error) (summary, hint string) {
	switch kind {
	case ErrKindAuth:
		return "Authentication failed", "Check the username, password, SSH key, or agent for this host."
	case ErrKindRefused:
		return "Connection refused", "The host is reachable but nothing is listening on the SSH port. Check the port and that sshd is running."
	case ErrKindTimeout:
		return "Connection timed out", "No response from the host. Check the network, firewall, or whether the host is online."
	case ErrKindDNS:
		return "Host not found", "The hostname could not be resolved. Check the address for typos or your DNS."
	case ErrKindHostKey:
		return "Host key problem", "The server's host key was rejected or changed. Verify the fingerprint before trusting it."
	case ErrKindUnreachable:
		return "Network unreachable", "No route to the host. Check your network connection, routing, or VPN."
	case ErrKindLocked:
		return "Vault is locked", "The master key is locked. Unlock the vault and try again."
	default:
		return firstLine(err.Error()), "See the raw error below for details."
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	const max = 100
	if len(s) > max {
		s = s[:max-3] + "..."
	}
	if s == "" {
		return "Connection failed"
	}
	return s
}
