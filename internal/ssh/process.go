package ssh

import (
	"io"
	"sync"
	"time"
)

var ProcessCloseKillTimeout = 2 * time.Second

type processExitCloser struct {
	exited  <-chan struct{}
	kill    func() error
	timeout time.Duration
	once    sync.Once
	err     error
}

func NewProcessExitCloser(exited <-chan struct{}, kill func() error, timeout time.Duration) io.Closer {
	return &processExitCloser{exited: exited, kill: kill, timeout: timeout}
}

func (c *processExitCloser) Close() error {
	c.once.Do(func() {
		timer := time.NewTimer(c.timeout)
		defer timer.Stop()
		select {
		case <-c.exited:
		case <-timer.C:
			if c.kill != nil {
				c.err = c.kill()
			}
		}
	})
	return c.err
}
