package ssh

import (
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// PortForwardCloser can stop a running port forward.
type PortForwardCloser struct {
	listener net.Listener
	done     chan struct{}
	once     sync.Once
}

func (p *PortForwardCloser) Close() error {
	p.once.Do(func() { close(p.done) })
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

// StartLocalForward listens on localPort and forwards connections to remoteHost:remotePort via the SSH client.
func StartLocalForward(client *ssh.Client, localPort int, remoteHost string, remotePort int) (*PortForwardCloser, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on local port %d: %w", localPort, err)
	}

	pfc := &PortForwardCloser{listener: listener, done: make(chan struct{})}
	remoteAddr := fmt.Sprintf("%s:%d", remoteHost, remotePort)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-pfc.done:
					return
				default:
				}
				return
			}
			go func(local net.Conn) {
				defer local.Close()
				setNoDelay(local)
				remote, err := client.Dial("tcp", remoteAddr)
				if err != nil {
					return
				}
				defer remote.Close()
				copyBidi(local, remote, pfc.done)
			}(conn)
		}
	}()

	return pfc, nil
}

// StartRemoteForward requests the SSH server to listen on remotePort and forwards to localHost:localPort.
func StartRemoteForward(client *ssh.Client, remotePort int, localHost string, localPort int) (*PortForwardCloser, error) {
	listener, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", remotePort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on remote port %d: %w", remotePort, err)
	}

	pfc := &PortForwardCloser{listener: listener, done: make(chan struct{})}
	localAddr := net.JoinHostPort(localHost, fmt.Sprintf("%d", localPort))

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-pfc.done:
					return
				default:
				}
				return
			}
			go func(remote net.Conn) {
				defer remote.Close()
				local, err := net.Dial("tcp", localAddr)
				if err != nil {
					return
				}
				defer local.Close()
				setNoDelay(local)
				copyBidi(remote, local, pfc.done)
			}(conn)
		}
	}()

	return pfc, nil
}

func copyBidi(a, b net.Conn, done <-chan struct{}) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
	}
	go cp(a, b)
	go cp(b, a)

	// Wait for either copy to finish or done signal
	ch := make(chan struct{})
	go func() { wg.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-done:
	}
}
