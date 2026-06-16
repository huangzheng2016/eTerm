package wskeepalive

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct {
	pings  atomic.Int32
	closed atomic.Bool
	err    error
}

func (f *fakeConn) Ping(context.Context) error {
	f.pings.Add(1)
	return f.err
}

func (f *fakeConn) CloseNow() error {
	f.closed.Store(true)
	return nil
}

func TestStartPingsUntilContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeConn{}

	Start(ctx, c, time.Millisecond, time.Second)
	time.Sleep(5 * time.Millisecond)
	cancel()

	if c.pings.Load() == 0 {
		t.Fatal("expected ping")
	}
	if c.closed.Load() {
		t.Fatal("connection should not close on successful ping")
	}
}

func TestStartClosesOnPingError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeConn{err: errors.New("ping failed")}

	Start(ctx, c, time.Millisecond, time.Second)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.closed.Load() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("connection was not closed after ping error")
}
