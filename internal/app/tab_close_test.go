package app

import (
	"sync"
	"testing"
	"time"

	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
	"github.com/huangzheng2016/eTerm/internal/viewkeys"
)

type blockingCloser struct {
	started     chan struct{}
	release     chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func newBlockingCloser() *blockingCloser {
	return &blockingCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *blockingCloser) Close() error {
	c.startOnce.Do(func() { close(c.started) })
	<-c.release
	return nil
}

func (c *blockingCloser) Release() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func TestCloseCurrentTabRemovesTabBeforeSessionCleanup(t *testing.T) {
	closer := newBlockingCloser()
	t.Cleanup(closer.Release)

	is := &internalssh.InteractiveSession{}
	is.AddCloser(closer)
	tab := sshview.New(is, "ssh", 0, viewkeys.SSHKeys{})
	a := App{
		tabs: []Tab{
			{Type: HomeTab, Title: "home"},
			{Type: SSHTab, Title: "ssh", Model: tab},
		},
		activeTab: 1,
	}

	next, cmd := a.closeCurrentTabIfAllowed()
	if cmd == nil {
		t.Fatal("expected cleanup command")
	}
	if len(next.tabs) != 1 || next.tabs[0].Type != HomeTab {
		t.Fatalf("tabs = %#v, want only home tab", next.tabs)
	}
	if next.activeTab != 0 {
		t.Fatalf("activeTab = %d, want 0", next.activeTab)
	}
	select {
	case <-closer.started:
		t.Fatal("session cleanup started before command ran")
	default:
	}

	done := make(chan struct{})
	go func() {
		cmd()
		close(done)
	}()

	select {
	case <-closer.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	select {
	case <-done:
		t.Fatal("cleanup finished before release")
	default:
	}

	closer.Release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
}
