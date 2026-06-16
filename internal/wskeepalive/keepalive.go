package wskeepalive

import (
	"context"
	"time"
)

type Conn interface {
	Ping(context.Context) error
	CloseNow() error
}

func Start(ctx context.Context, c Conn, interval, timeout time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pingCtx, cancel := context.WithTimeout(ctx, timeout)
				err := c.Ping(pingCtx)
				cancel()
				if err != nil {
					_ = c.CloseNow()
					return
				}
			}
		}
	}()
}
