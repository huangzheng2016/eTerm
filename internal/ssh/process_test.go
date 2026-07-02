package ssh

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessExitCloserKillsAfterTimeout(t *testing.T) {
	var killed atomic.Int32
	exited := make(chan struct{})
	closer := NewProcessExitCloser(exited, func() error {
		killed.Add(1)
		return nil
	}, 10*time.Millisecond)

	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if killed.Load() != 1 {
		t.Fatalf("killed = %d", killed.Load())
	}
}

func TestProcessExitCloserDoesNotKillExitedProcess(t *testing.T) {
	var killed atomic.Int32
	exited := make(chan struct{})
	close(exited)
	closer := NewProcessExitCloser(exited, func() error {
		killed.Add(1)
		return nil
	}, 10*time.Millisecond)

	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if killed.Load() != 0 {
		t.Fatalf("killed = %d", killed.Load())
	}
}
