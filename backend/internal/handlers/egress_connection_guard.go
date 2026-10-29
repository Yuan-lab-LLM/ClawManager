package handlers

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// egressGuardedConn owns its lifetime: the dialer's per-attempt context is
// canceled immediately after success, and must never own this watcher.
// HTTP transport / CONNECT close the connection when their own lifetime ends.
type egressGuardedConn struct {
	net.Conn
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
	closeErr error
}

func newEgressGuardedConn(conn net.Conn, check func(context.Context) error, revoke context.CancelFunc) *egressGuardedConn {
	ctx, cancel := context.WithCancel(context.Background())
	guarded := &egressGuardedConn{Conn: conn, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(guarded.done)
		ticker := time.NewTicker(egressRecheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if check(ctx) != nil {
					// Interrupt the owning HTTP request's blocked downstream I/O.
					// Normal Close cancels our own context and must not revoke it.
					if ctx.Err() == nil && revoke != nil {
						revoke()
					}
					// The watcher closes raw I/O, but never attempts to join itself.
					guarded.shutdown()
					return
				}
			}
		}
	}()
	return guarded
}

func (c *egressGuardedConn) shutdown() {
	c.once.Do(func() { c.cancel(); c.closeErr = c.Conn.Close() })
}

func (c *egressGuardedConn) Close() error {
	c.shutdown()
	<-c.done
	return c.closeErr
}

func (c *egressGuardedConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return errors.New("connection does not support CloseWrite")
}

func (c *egressGuardedConn) CloseRead() error {
	if half, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return half.CloseRead()
	}
	return errors.New("connection does not support CloseRead")
}
