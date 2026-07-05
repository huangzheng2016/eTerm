package ssh

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"golang.org/x/crypto/ssh"
)

// StartDynamicForward listens on 127.0.0.1:localPort and serves SOCKS5 CONNECT (RFC 1928)
// over the SSH connection (outbound dials use client.Dial).
func StartDynamicForward(client *ssh.Client, localPort int) (*PortForwardCloser, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on local port %d: %w", localPort, err)
	}

	pfc := &PortForwardCloser{listener: listener, done: make(chan struct{})}

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
			go handleSOCKSConn(client, conn, pfc.done)
		}
	}()

	return pfc, nil
}

func handleSOCKSConn(client *ssh.Client, c net.Conn, done <-chan struct{}) {
	defer c.Close()
	setNoDelay(c)
	br := bufio.NewReader(c)

	// Negotiation: VER, NMETHODS, METHODS...
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return
	}
	if hdr[0] != 5 {
		return
	}
	n := int(hdr[1])
	methods := make([]byte, n)
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil { // no auth
		return
	}

	// Request: VER CMD RSV ATYP ...
	var reqHdr [4]byte
	if _, err := io.ReadFull(br, reqHdr[:]); err != nil {
		return
	}
	if reqHdr[0] != 5 || reqHdr[1] != 1 { // CONNECT only
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	atyp := reqHdr[3]
	var host string
	switch atyp {
	case 1:
		var ip [4]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return
		}
		host = net.IP(ip[:]).String()
	case 3:
		lb, err := br.ReadByte()
		if err != nil {
			return
		}
		name := make([]byte, lb)
		if _, err := io.ReadFull(br, name); err != nil {
			return
		}
		host = string(name)
	case 4:
		var ip [16]byte
		if _, err := io.ReadFull(br, ip[:]); err != nil {
			return
		}
		host = net.IP(ip[:]).String()
	default:
		_, _ = c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0}) // address not supported
		return
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return
	}
	port := int(portBuf[0])<<8 | int(portBuf[1])
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	remote, err := client.Dial("tcp", addr)
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // connection refused / failed
		return
	}
	defer remote.Close()

	// success, bind 0.0.0.0:0
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	// Wrap so copyBidi reads any bytes already buffered by br.
	bc := &bufferedConn{Conn: c, r: br}
	copyBidi(bc, remote, done)
}