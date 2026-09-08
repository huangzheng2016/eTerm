package ssh

import (
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

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

func (i *InteractiveSession) SetClosers(c []io.Closer) {
	i.closers = c
}

func (i *InteractiveSession) AddCloser(c io.Closer) {
	i.closers = append(i.closers, c)
}

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

func NewInteractiveSession(client *ssh.Client, rows, cols int, forwardAgent bool) (*InteractiveSession, error) {
	rows, cols = NormalizePTYSize(rows, cols)

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
