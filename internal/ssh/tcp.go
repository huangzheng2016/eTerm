package ssh

import "net"

// setNoDelay disables Nagle's algorithm on the underlying TCP connection.
// It works for *net.TCPConn and any wrapper that implements SetNoDelay.
func setNoDelay(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		return
	}
	if nd, ok := conn.(interface{ SetNoDelay(bool) error }); ok {
		_ = nd.SetNoDelay(true)
	}
}
