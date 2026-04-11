package ssh

import (
	"io"

	"golang.org/x/crypto/ssh"
)

type SSHSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	ptyCols int
	ptyRows int
}

func NewSSHSession(client *ssh.Client) (*SSHSession, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	return &SSHSession{
		client:  client,
		session: session,
	}, nil
}

func (s *SSHSession) SetStdin(r io.Reader) {
	s.stdin = r
}

func (s *SSHSession) SetStdout(w io.Writer) {
	s.stdout = w
}

func (s *SSHSession) SetStderr(w io.Writer) {
	s.stderr = w
}

// SetPtySize sets the PTY size in terminal cells (cols x rows). Call before Run.
// If unset, defaults to 80x24.
func (s *SSHSession) SetPtySize(cols, rows int) {
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}
	s.ptyCols, s.ptyRows = cols, rows
}

func (s *SSHSession) Run() error {
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	cols, rows := s.ptyCols, s.ptyRows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}

	// RequestPty(term, height, width, …) — height is rows, width is columns.
	if err := s.session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		s.session.Close()
		return err
	}

	s.session.Stdin = s.stdin
	s.session.Stdout = s.stdout
	s.session.Stderr = s.stderr

	if err := s.session.Shell(); err != nil {
		s.session.Close()
		return err
	}

	return s.session.Wait()
}
