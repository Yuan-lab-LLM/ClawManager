package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrGatewayKeyRPMLimitExceeded = errors.New("gateway key RPM limit exceeded")
var ErrGatewayKeyTPMLimitExceeded = errors.New("gateway key TPM limit exceeded")
var ErrGatewayKeyConcurrencyExceeded = errors.New("gateway key concurrency limit exceeded")

type GatewayRateLimitInput struct {
	KeyID                                       string
	RPMLimit, TPMLimit, ConcurrentRequestsLimit int
	EstimatedTokens                             int
	RequestID                                   string
}
type GatewayRateLimitLease struct {
	concurrentKey  string
	concurrentSet  string
	tokenKey       string
	tokenWindow    string
	reservedTokens int
	finalized      bool
	mu             sync.Mutex
}
type GatewayRateLimitStore interface {
	Acquire(context.Context, GatewayRateLimitInput) (*GatewayRateLimitLease, error)
	Finalize(context.Context, *GatewayRateLimitLease, int) error
	Release(context.Context, *GatewayRateLimitLease) error
}
type noopGatewayRateLimitStore struct{}

func NewNoopGatewayRateLimitStore() GatewayRateLimitStore { return noopGatewayRateLimitStore{} }
func (noopGatewayRateLimitStore) Acquire(context.Context, GatewayRateLimitInput) (*GatewayRateLimitLease, error) {
	return &GatewayRateLimitLease{}, nil
}
func (noopGatewayRateLimitStore) Finalize(context.Context, *GatewayRateLimitLease, int) error {
	return nil
}
func (noopGatewayRateLimitStore) Release(context.Context, *GatewayRateLimitLease) error { return nil }

type gatewayRedisCommander interface {
	do(context.Context, ...string) (interface{}, error)
}

const gatewayConcurrencyLeaseTTL = 4 * time.Minute
const gatewayConcurrencyHeartbeatInterval = time.Minute

type redisGatewayRateLimitStore struct{ bus gatewayRedisCommander }

func NewRedisGatewayRateLimitStore(rawURL string) (GatewayRateLimitStore, error) {
	bus, err := newRedisBus(rawURL)
	if err != nil {
		return nil, err
	}
	return &redisGatewayRateLimitStore{bus: bus}, nil
}
func (s *redisGatewayRateLimitStore) Acquire(ctx context.Context, in GatewayRateLimitInput) (*GatewayRateLimitLease, error) {
	if in.RPMLimit <= 0 && in.TPMLimit <= 0 && in.ConcurrentRequestsLimit <= 0 {
		return &GatewayRateLimitLease{}, nil
	}
	if strings.TrimSpace(in.KeyID) == "" || (in.ConcurrentRequestsLimit > 0 && strings.TrimSpace(in.RequestID) == "") {
		return nil, errors.New("gateway rate-limit lease identity is required")
	}
	prefix := "clawmanager:gateway-rate-limit:v1:{" + in.KeyID + "}"
	windowKey := prefix + ":minute"
	setKey := prefix + ":concurrent"
	estimated := in.EstimatedTokens
	if estimated < 1 {
		estimated = 1
	}
	// Check every active constraint before mutating any counter. This prevents
	// a request rejected by TPM/concurrency from still consuming RPM, and keeps
	// admission consistent across gateway replicas.
	script := `local clock = redis.call('TIME'); local now = clock[1] * 1000 + math.floor(clock[2] / 1000); local minute = tostring(math.floor(tonumber(clock[1]) / 60)); local rpm_limit = tonumber(ARGV[1]); local tpm_limit = tonumber(ARGV[2]); local requested = tonumber(ARGV[3]); local concurrent_limit = tonumber(ARGV[4]); local same_window = redis.call('HGET', KEYS[1], 'window') == minute; local rpm_current = same_window and tonumber(redis.call('HGET', KEYS[1], 'rpm') or '0') or 0; local tpm_current = same_window and tonumber(redis.call('HGET', KEYS[1], 'tpm') or '0') or 0; redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now); local exists = redis.call('ZSCORE', KEYS[2], ARGV[5]); local inflight = redis.call('ZCARD', KEYS[2]); if rpm_limit > 0 and rpm_current + 1 > rpm_limit then return {-1, minute} end; if tpm_limit > 0 and tpm_current + requested > tpm_limit then return {-2, minute} end; if concurrent_limit > 0 and not exists and inflight >= concurrent_limit then return {-3, minute} end; if rpm_limit > 0 or tpm_limit > 0 then local next_rpm = rpm_current; local next_tpm = tpm_current; if rpm_limit > 0 then next_rpm = next_rpm + 1 end; if tpm_limit > 0 then next_tpm = next_tpm + requested end; redis.call('HSET', KEYS[1], 'window', minute, 'rpm', next_rpm, 'tpm', next_tpm); redis.call('PEXPIRE', KEYS[1], 70000) end; if concurrent_limit > 0 and not exists then redis.call('ZADD', KEYS[2], now + tonumber(ARGV[6]), ARGV[5]); redis.call('PEXPIRE', KEYS[2], ARGV[7]) end; return {1, minute}`
	reply, err := s.bus.do(ctx, "EVAL", script, "2", windowKey, setKey,
		fmt.Sprintf("%d", in.RPMLimit), fmt.Sprintf("%d", in.TPMLimit), fmt.Sprintf("%d", estimated),
		fmt.Sprintf("%d", in.ConcurrentRequestsLimit), in.RequestID,
		fmt.Sprintf("%d", gatewayConcurrencyLeaseTTL.Milliseconds()),
		fmt.Sprintf("%d", (gatewayConcurrencyLeaseTTL+time.Minute).Milliseconds()))
	if err != nil {
		return nil, err
	}
	values, ok := reply.([]interface{})
	if !ok || len(values) != 2 {
		return nil, errors.New("unexpected gateway rate-limit response")
	}
	code := redisInt64(values[0])
	window := redisString(values[1], "")
	switch code {
	case -1:
		return nil, ErrGatewayKeyRPMLimitExceeded
	case -2:
		return nil, ErrGatewayKeyTPMLimitExceeded
	case -3:
		return nil, ErrGatewayKeyConcurrencyExceeded
	case 1:
	default:
		return nil, errors.New("unexpected gateway rate-limit response")
	}
	lease := &GatewayRateLimitLease{}
	if in.TPMLimit > 0 {
		lease.tokenKey = windowKey
		lease.tokenWindow = window
		lease.reservedTokens = estimated
	}
	if in.ConcurrentRequestsLimit > 0 {
		lease.concurrentKey = in.RequestID
		lease.concurrentSet = setKey
		go s.keepConcurrencyLeaseAlive(ctx, lease)
	}
	return lease, nil
}

func (s *redisGatewayRateLimitStore) keepConcurrencyLeaseAlive(ctx context.Context, lease *GatewayRateLimitLease) {
	if lease == nil || lease.concurrentKey == "" || lease.concurrentSet == "" {
		return
	}
	ticker := time.NewTicker(gatewayConcurrencyHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if renewed, err := s.renewConcurrencyLease(ctx, lease, now.UTC()); err != nil || !renewed {
				return
			}
		}
	}
}

func (s *redisGatewayRateLimitStore) renewConcurrencyLease(ctx context.Context, lease *GatewayRateLimitLease, _ time.Time) (bool, error) {
	if lease == nil || lease.concurrentKey == "" || lease.concurrentSet == "" {
		return false, nil
	}
	script := `if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end; local clock = redis.call('TIME'); local now = clock[1] * 1000 + math.floor(clock[2] / 1000); redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[1]); redis.call('PEXPIRE', KEYS[1], ARGV[3]); return 1`
	reply, err := s.bus.do(ctx, "EVAL", script, "1", lease.concurrentSet, lease.concurrentKey, fmt.Sprintf("%d", gatewayConcurrencyLeaseTTL.Milliseconds()), fmt.Sprintf("%d", (gatewayConcurrencyLeaseTTL+time.Minute).Milliseconds()))
	if err != nil {
		return false, err
	}
	value, ok := reply.(int64)
	return ok && value == 1, nil
}

func (s *redisGatewayRateLimitStore) Finalize(ctx context.Context, lease *GatewayRateLimitLease, actualTokens int) error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.finalized {
		return nil
	}
	// Once the provider completed, a transient Redis failure must keep the
	// conservative reservation instead of causing Release to refund it.
	lease.finalized = true
	if actualTokens < 0 {
		actualTokens = 0
	}
	if lease.tokenKey != "" {
		delta := actualTokens - lease.reservedTokens
		if delta != 0 {
			script := `if redis.call('HGET', KEYS[1], 'window') ~= ARGV[1] then return 0 end; local next = tonumber(redis.call('HGET', KEYS[1], 'tpm') or '0') + tonumber(ARGV[2]); if next < 0 then next = 0 end; redis.call('HSET', KEYS[1], 'tpm', next); return next`
			if _, err := s.bus.do(ctx, "EVAL", script, "1", lease.tokenKey, lease.tokenWindow, fmt.Sprintf("%d", delta)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *redisGatewayRateLimitStore) Release(ctx context.Context, lease *GatewayRateLimitLease) error {
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if !lease.finalized {
		if err := s.refundTokens(ctx, lease); err != nil {
			return err
		}
		lease.finalized = true
	}
	if lease.concurrentKey != "" && lease.concurrentSet != "" {
		_, err := s.bus.do(ctx, "ZREM", lease.concurrentSet, lease.concurrentKey)
		return err
	}
	return nil
}

func (s *redisGatewayRateLimitStore) refundTokens(ctx context.Context, lease *GatewayRateLimitLease) error {
	if lease == nil || lease.tokenKey == "" || lease.reservedTokens <= 0 {
		return nil
	}
	script := `if redis.call('HGET', KEYS[1], 'window') ~= ARGV[1] then return 0 end; local next = tonumber(redis.call('HGET', KEYS[1], 'tpm') or '0') - tonumber(ARGV[2]); if next < 0 then next = 0 end; redis.call('HSET', KEYS[1], 'tpm', next); return next`
	_, err := s.bus.do(ctx, "EVAL", script, "1", lease.tokenKey, lease.tokenWindow, fmt.Sprintf("%d", lease.reservedTokens))
	return err
}
