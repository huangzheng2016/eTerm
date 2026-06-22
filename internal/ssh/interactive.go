package ssh

import (
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// InteractiveSession is a non-blocking interactive shell: caller reads Stdout and writes Stdin.
// Call Wait in a goroutine or read from Done; close Stdin and Session to terminate.
type InteractiveSession struct {
	Client        *ssh.Client
	Session       *ssh.Session
	Stdin         io.WriteCloser
	Stdout        io.Reader
	Done          <-chan error
	Resize        func(rows, cols int) error
	closers       []io.Closer
	stopKeepalive chan struct{}
}

// SetClosers attaches resources (agent conns, jump clients) to be closed with the session.
func (i *InteractiveSession) SetClosers(c []io.Closer) {
	i.closers = c
}

// AddCloser appends a single closer (e.g. port forward) to be cleaned up with the session.
func (i *InteractiveSession) AddCloser(c io.Closer) {
	i.closers = append(i.closers, c)
}

// Close releases local resources (remote shell may still exit).
func (i *InteractiveSession) Close() error {
	if i.stopKeepalive != nil {
		select {
		case <-i.stopKeepalive:
		default:
			close(i.stopKeepalive)
		}
	}
	if i.Stdin != nil {
		_ = i.Stdin.Close()
	}
	if i.Session != nil {
		_ = i.Session.Close()
	}
	var err error
	if i.Client != nil {
		err = i.Client.Close()
	}
	for _, c := range i.closers {
		if c != nil {
			_ = c.Close()
		}
	}
	return err
}

// NewInteractiveSession opens a PTY shell without blocking on Wait().
// rows/cols are PTY dimensions (height x width in cells).
func NewInteractiveSession(client *ssh.Client, rows, cols int, forwardAgent bool) (*InteractiveSession, error) {
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}

	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	if forwardAgent {
		if err := EnableAgentForwarding(client, sess); err != nil {
			_ = sess.Close()
			return nil, err
		}
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	setTerminalEnv(sess)
	if err := sess.RequestPty(terminalTerm, rows, cols, modes); err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		return nil, err
	}

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	stopKA := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					_ = sess.Close()
					return
				}
			case <-stopKA:
				return
			}
		}
	}()

	return &InteractiveSession{
		Client:        client,
		Session:       sess,
		Stdin:         stdin,
		Stdout:        stdout,
		Done:          done,
		Resize:        sess.WindowChange,
		stopKeepalive: stopKA,
	}, nil
}
