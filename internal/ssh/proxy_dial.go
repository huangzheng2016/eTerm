package ssh

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	"golang.org/x/net/proxy"
)

// normalizeProxyType returns "" for direct connection, or "http" / "socks5".
func normalizeProxyType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return s
}

func decryptProxyPassword(h *db.Host, masterKey *security.MasterKeyManager) (string, error) {
	if strings.TrimSpace(h.ProxyPassword) == "" {
		return "", nil
	}
	sec := masterKey.GetKey()
	if sec == nil {
		return "", fmt.Errorf("master key is locked")
	}
	defer sec.Clear()
	b, err := security.Decrypt(h.ProxyPassword, sec.Bytes())
	if err != nil {
		return "", fmt.Errorf("decrypt proxy password: %w", err)
	}
	defer security.ClearBytes(b)
	return string(b), nil
}

// dialWithProxy opens a TCP connection to targetAddr ("host:port") using the host's proxy when configured.
func dialWithProxy(h *db.Host, targetAddr string, masterKey *security.MasterKeyManager) (net.Conn, error) {
	// ProxyCommand takes highest priority.
	if h.ProxyCommand != "" {
		host, portStr, _ := net.SplitHostPort(targetAddr)
		port, _ := strconv.Atoi(portStr)
		return dialProxyCommand(h.ProxyCommand, host, port)
	}

	pt := normalizeProxyType(h.ProxyType)
	if pt == "" {
		return net.DialTimeout("tcp", targetAddr, 30*time.Second)
	}
	host := strings.TrimSpace(h.ProxyHost)
	if host == "" || h.ProxyPort <= 0 {
		return nil, fmt.Errorf("proxy is enabled but proxy host/port are missing")
	}
	proxyAddr := net.JoinHostPort(host, strconv.Itoa(h.ProxyPort))
	plainPass, err := decryptProxyPassword(h, masterKey)
	if err != nil {
		return nil, err
	}

	switch pt {
	case "socks5":
		var auth *proxy.Auth
		if strings.TrimSpace(h.ProxyUser) != "" || plainPass != "" {
			auth = &proxy.Auth{User: h.ProxyUser, Password: plainPass}
		}
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 proxy: %w", err)
		}
		return dialer.Dial("tcp", targetAddr)
	case "http":
		return dialHTTPConnect(proxyAddr, targetAddr, h.ProxyUser, plainPass)
	default:
		return nil, fmt.Errorf("unknown proxy type: %q", h.ProxyType)
	}
}

// bufferedConn wraps a net.Conn so that Read drains the bufio.Reader first.
// This prevents data loss when a bufio.Reader has buffered bytes beyond a
// protocol header (HTTP CONNECT response, SOCKS5 handshake, etc.).
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (bc *bufferedConn) Read(p []byte) (int, error) {
	return bc.r.Read(p)
}

func dialHTTPConnect(proxyAddr, targetAddr, user, pass string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", proxyAddr, 30*time.Second)
	if err != nil {
		return nil, err
	}
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)
	if strings.TrimSpace(user) != "" || pass != "" {
		basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req += "Proxy-Authorization: Basic " + basic + "\r\n"
	}
	req += "\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		_ = c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	status, err := br.ReadString('\n')
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	// Parse "HTTP/1.x <code> <reason>" — only accept 200.
	parts := strings.SplitN(strings.TrimSpace(status), " ", 3)
	if len(parts) < 2 || parts[1] != "200" {
		_ = c.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &bufferedConn{Conn: c, r: br}, nil
}
