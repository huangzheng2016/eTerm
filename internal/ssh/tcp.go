package ssh

import "net"

func setNoDelay(conn net.Conn) {
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		return
	}
	if nd, ok := conn.(interface{ SetNoDelay(bool) error }); ok {
		_ = nd.SetNoDelay(true)
	}
}
