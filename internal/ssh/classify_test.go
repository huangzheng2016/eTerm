package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"nil", nil, ErrKindUnknown},
		{"dns type", &net.DNSError{Err: "no such host", Name: "bogus.invalid", IsNotFound: true}, ErrKindDNS},
		{"dns string", fmt.Errorf("dial tcp: lookup bogus: %s", "no such host"), ErrKindDNS},
		{"refused errno", fmt.Errorf("dial tcp 1.2.3.4:22: %w", syscall.ECONNREFUSED), ErrKindRefused},
		{"refused string", errors.New("dial tcp 1.2.3.4:22: connect: connection refused"), ErrKindRefused},
		{"timeout deadline", fmt.Errorf("handshake: %w", os.ErrDeadlineExceeded), ErrKindTimeout},
		{"timeout string", errors.New("dial tcp 1.2.3.4:22: i/o timeout"), ErrKindTimeout},
		{"unreachable errno", fmt.Errorf("dial: %w", syscall.EHOSTUNREACH), ErrKindUnreachable},
		{"unreachable string", errors.New("dial tcp: connect: network is unreachable"), ErrKindUnreachable},
		{"locked", errors.New("failed to build auth methods: master key is locked"), ErrKindLocked},
		{"auth unable", errors.New("failed to ssh handshake 1.2.3.4:22: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"), ErrKindAuth},
		{"auth permission", errors.New("ssh: handshake failed: permission denied (publickey,password)"), ErrKindAuth},
		{"auth decrypt", errors.New("failed to decrypt password: cipher: message authentication failed"), ErrKindAuth},
		{"hostkey", errors.New("ssh: handshake failed: host key rejected by user for 1.2.3.4:22"), ErrKindHostKey},
		{"unknown", errors.New("something totally unexpected happened"), ErrKindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.err)
			if c.err == nil {
				if got != nil {
					t.Fatalf("Classify(nil) = %#v, want nil", got)
				}
				return
			}
			if got.Kind != c.want {
				t.Fatalf("Classify(%q).Kind = %v, want %v", c.err, got.Kind, c.want)
			}
			if got.Summary == "" {
				t.Errorf("Summary empty for %q", c.err)
			}
			if got.Err != c.err {
				t.Errorf("Err not preserved")
			}
		})
	}
}

func TestClassifyTimeoutBeforeRefused(t *testing.T) {
	// net.OpError carrying a timeout should classify as timeout, not unknown.
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}
	if got := Classify(opErr); got.Kind != ErrKindTimeout {
		t.Fatalf("timeout OpError = %v, want ErrKindTimeout", got.Kind)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestErrorKindShort(t *testing.T) {
	for k := ErrKindUnknown; k <= ErrKindLocked; k++ {
		if k.Short() == "" {
			t.Errorf("Short() empty for kind %d", k)
		}
	}
}

func TestConnectErrorUnwrap(t *testing.T) {
	base := syscall.ECONNREFUSED
	ce := Classify(fmt.Errorf("wrap: %w", base))
	if !errors.Is(ce, base) {
		t.Fatalf("errors.Is(ce, ECONNREFUSED) = false, want true")
	}
}
