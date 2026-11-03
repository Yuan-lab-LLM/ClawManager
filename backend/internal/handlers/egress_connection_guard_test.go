package handlers

import (
	"clawreef/internal/egresspolicy"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type liveEgressRules struct {
	mu    sync.Mutex
	rules []egresspolicy.PrivateExceptionRule
}

func (s *liveEgressRules) SnapshotRules() []egresspolicy.PrivateExceptionRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]egresspolicy.PrivateExceptionRule(nil), s.rules...)
}

func (s *liveEgressRules) SnapshotRulesContext(ctx context.Context) []egresspolicy.PrivateExceptionRule {
	if ctx.Err() != nil {
		return nil
	}
	return s.SnapshotRules()
}

// The selected destination is synthetic; every connection is real TCP.
func liveEgressTCP(t *testing.T, h *EgressProxyHandler, ctx context.Context, selected string) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	h.baseDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != selected {
			t.Errorf("dial selected %q, want %q", address, selected)
		}
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	conn, err := h.proxyDialContext(ctx, "tcp", selected)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	// The per-attempt context has already been canceled; successful I/O must work.
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := io.ReadFull(conn, b[:]); err != nil {
		t.Fatalf("successful dial context cancellation closed connection: %v", err)
	}
	conn.SetDeadline(time.Time{})
	return conn, peer
}

func TestEgressEstablishedDestinationRevocation(t *testing.T) {
	for _, reason := range []string{"node reclassification", "inventory outage", "private exception removal"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			var revoked atomic.Bool
			var checks atomic.Int32
			selected := "8.8.4.4:443"
			if reason == "private exception removal" {
				selected = "10.80.0.8:443"
			}
			destination, _ := netip.ParseAddrPort(selected)
			h := NewEgressProxyHandler(nil, WithEgressHardDeny(func(ctx context.Context, ip netip.Addr) (bool, error) {
				checks.Add(1)
				if ip != destination.Addr() {
					t.Errorf("recheck changed pinned destination: %v", ip)
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
					t.Error("inventory check lacks two-second bound")
				}
				if revoked.Load() {
					if reason == "node reclassification" {
						return true, nil
					}
					if reason == "inventory outage" {
						return false, errors.New("inventory stale")
					}
				}
				return false, nil
			}))
			id := 17
			rules := &liveEgressRules{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: id, Prefix: netip.MustParsePrefix("10.80.0.8/32"), Port: 443}}}
			h.privateExceptions = rules
			ctx := context.WithValue(context.Background(), egressIdentityContextKey{}, egressRequestIdentity{InstanceID: &id})
			conn, peer := liveEgressTCP(t, h, ctx, selected)
			revoked.Store(true)
			rules.mu.Lock()
			rules.rules = nil
			rules.mu.Unlock()
			peer.SetReadDeadline(time.Now().Add(8 * time.Second))
			var b [1]byte
			if _, err := peer.Read(b[:]); !errors.Is(err, io.EOF) {
				t.Fatalf("revocation left established connection open: %v", err)
			}
			conn.Close()
			if reason != "private exception removal" && checks.Load() < 2 {
				t.Fatal("destination was not rechecked")
			}
		})
	}
}

func TestEgressEstablishedCloseCancelsAndJoinsInventoryCheck(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	entered, exited := make(chan struct{}), make(chan struct{})
	h := NewEgressProxyHandler(nil, WithEgressHardDeny(func(ctx context.Context, _ netip.Addr) (bool, error) {
		if calls.Add(1) == 1 {
			return false, nil
		}
		close(entered)
		<-ctx.Done()
		close(exited)
		return false, ctx.Err()
	}))
	var revoked atomic.Bool
	ctx := context.WithValue(context.Background(), egressRevokeContextKey{}, context.CancelFunc(func() { revoked.Store(true) }))
	conn, _ := liveEgressTCP(t, h, ctx, "1.1.1.1:443")
	select {
	case <-entered:
	case <-time.After(7 * time.Second):
		t.Fatal("no established destination watcher")
	}
	closed := make(chan struct{})
	go func() { conn.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join watcher")
	}
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before inventory callback exited")
	}
	if revoked.Load() {
		t.Fatal("normal Close revoked the owning request")
	}
}

func TestEgressEstablishedPrivateIdentityCopiedAndTCPHalfClose(t *testing.T) {
	t.Parallel()
	id := 17
	rules := &liveEgressRules{rules: []egresspolicy.PrivateExceptionRule{{ScopeType: egresspolicy.ScopeInstance, ScopeID: id, Prefix: netip.MustParsePrefix("10.80.0.8/32"), Port: 443}}}
	h := NewEgressProxyHandler(nil)
	h.privateExceptions = rules
	ctx := context.WithValue(context.Background(), egressIdentityContextKey{}, egressRequestIdentity{InstanceID: &id})
	conn, peer := liveEgressTCP(t, h, ctx, "10.80.0.8:443")
	id = 99
	time.Sleep(5500 * time.Millisecond)
	if _, err := peer.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := io.ReadFull(conn, b[:]); err != nil {
		t.Fatalf("verified identity was not copied: %v", err)
	}
	half, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("TCP half-close interface was lost")
	}
	if err := half.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(b[:]); !errors.Is(err, io.EOF) {
		t.Fatalf("half close not forwarded: %v", err)
	}
}
