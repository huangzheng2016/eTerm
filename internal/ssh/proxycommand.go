package ssh

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// proxyCommandConn wraps a subprocess whose stdin/stdout act as a network connection.
type proxyCommandConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (c *proxyCommandConn) Read(b []byte) (int, error)  { return c.stdout.Read(b) }
func (c *proxyCommandConn) Write(b []byte) (int, error)  { return c.stdin.Write(b) }
func (c *proxyCommandConn) LocalAddr() net.Addr           { return dummyAddr("proxycommand") }
func (c *proxyCommandConn) RemoteAddr() net.Addr          { return dummyAddr("proxycommand") }
func (c *proxyCommandConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyCommandConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyCommandConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *proxyCommandConn) Close() error {
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	return nil
}

type dummyAddr string

func (a dummyAddr) Network() string { return "proxycommand" }
func (a dummyAddr) String() string  { return string(a) }

// expandProxyCommand replaces %h, %p, and %% tokens in the command string.
func expandProxyCommand(command, host string, port int) string {
	r := strings.NewReplacer(
		"%h", host,
		"%p", strconv.Itoa(port),
		"%%", "%",
	)
	return r.Replace(command)
}

// dialProxyCommand starts a subprocess and returns a net.Conn backed by its stdin/stdout.
func dialProxyCommand(command, host string, port int) (net.Conn, error) {
	expanded := expandProxyCommand(command, host, port)
	cmd := exec.Command("sh", "-c", expanded)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("proxycommand stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("proxycommand stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("proxycommand start: %w", err)
	}

	return &proxyCommandConn{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}
