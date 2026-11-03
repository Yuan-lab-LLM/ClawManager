package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	gatewayDeploymentLeaseTTL       = 4 * time.Minute
	gatewayDeploymentLeaseHeartbeat = time.Minute
	gatewayDeploymentMetricsTTL     = 7 * 24 * time.Hour
)

var ErrGatewayDeploymentUnavailable = errors.New("gateway deployment is unavailable")

// GatewayDeploymentSnapshot is the shared, short-lived routing state for one
// physical model deployment. It deliberately contains no tenant or prompt data.
type GatewayDeploymentSnapshot struct {
	DeploymentID        int
	State               string
	EWMATTFTMs          float64
	EWMALatencyMs       float64
	EWMATokensPerSecond float64
	Inflight            int
	Samples             int64
	ConsecutiveFailures int
	CooldownUntilMs     int64
	LastSuccessMs       int64
	LastFailureMs       int64
	SnapshotAtMs        int64
}

type GatewayDeploymentAcquireInput struct {
	DeploymentID int
	RequestID    string
	MaxInflight  int
}

type GatewayDeploymentObservation struct {
	DeploymentID      int
	Success           bool
	PenalizeFailure   bool
	ImmediateCooldown bool
	TTFTMs            int
	LatencyMs         int
	TokensPerSecond   float64
}

type GatewayDeploymentLease struct {
	DeploymentID int
	requestID    string
	setKey       string
	mu           sync.Mutex
	released     bool
}

// GatewayDeploymentStore coordinates deployment load and health across gateway
// replicas. Metrics failures may fall back to static routing, but callers must
// fail closed on acquisition errors when a capacity limit is configured.
// Capacity/cooldown rejection uses ErrGatewayDeploymentUnavailable.
type GatewayDeploymentStore interface {
	Snapshots(context.Context, []int) (map[int]GatewayDeploymentSnapshot, error)
	Acquire(context.Context, GatewayDeploymentAcquireInput) (*GatewayDeploymentLease, error)
	Observe(context.Context, GatewayDeploymentObservation) error
	Release(context.Context, *GatewayDeploymentLease) error
}

type noopGatewayDeploymentStore struct{}

func NewNoopGatewayDeploymentStore() GatewayDeploymentStore { return noopGatewayDeploymentStore{} }
func (noopGatewayDeploymentStore) Snapshots(_ context.Context, ids []int) (map[int]GatewayDeploymentSnapshot, error) {
	items := make(map[int]GatewayDeploymentSnapshot, len(ids))
	for _, id := range ids {
		items[id] = GatewayDeploymentSnapshot{DeploymentID: id, State: "unknown", SnapshotAtMs: time.Now().UTC().UnixMilli()}
	}
	return items, nil
}
func (noopGatewayDeploymentStore) Acquire(_ context.Context, in GatewayDeploymentAcquireInput) (*GatewayDeploymentLease, error) {
	if in.MaxInflight > 0 {
		return nil, ErrGatewayDeploymentUnavailable
	}
	return &GatewayDeploymentLease{DeploymentID: in.DeploymentID}, nil
}
func (noopGatewayDeploymentStore) Observe(context.Context, GatewayDeploymentObservation) error {
	return nil
}
func (noopGatewayDeploymentStore) Release(context.Context, *GatewayDeploymentLease) error { return nil }

type redisGatewayDeploymentStore struct{ bus gatewayRedisCommander }

func NewRedisGatewayDeploymentStore(rawURL string) (GatewayDeploymentStore, error) {
	bus, err := newRedisBus(rawURL)
	if err != nil {
		return nil, err
	}
	return &redisGatewayDeploymentStore{bus: bus}, nil
}

func deploymentMetricsKey(id int) string {
	return fmt.Sprintf("clawmanager:gateway-deployment:v1:{%d}:metrics", id)
}

func deploymentInflightKey(id int) string {
	return fmt.Sprintf("clawmanager:gateway-deployment:v1:{%d}:inflight", id)
}

func (s *redisGatewayDeploymentStore) Snapshots(ctx context.Context, ids []int) (map[int]GatewayDeploymentSnapshot, error) {
	items := make(map[int]GatewayDeploymentSnapshot, len(ids))
	for _, id := range ids {
		script := `local clock = redis.call('TIME'); local now = clock[1] * 1000 + math.floor(clock[2] / 1000); redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now); local v = redis.call('HMGET', KEYS[1], 'state', 'ewma_ttft_ms', 'ewma_latency_ms', 'ewma_tps', 'samples', 'consecutive_failures', 'cooldown_until_ms', 'last_success_ms', 'last_failure_ms'); table.insert(v, redis.call('ZCARD', KEYS[2])); table.insert(v, now); return v`
		reply, err := s.bus.do(ctx, "EVAL", script, "2", deploymentMetricsKey(id), deploymentInflightKey(id))
		if err != nil {
			return nil, err
		}
		values, ok := reply.([]interface{})
		if !ok || len(values) != 11 {
			return nil, fmt.Errorf("unexpected deployment snapshot response")
		}
		items[id] = GatewayDeploymentSnapshot{
			DeploymentID:        id,
			State:               redisString(values[0], "unknown"),
			EWMATTFTMs:          redisFloat(values[1]),
			EWMALatencyMs:       redisFloat(values[2]),
			EWMATokensPerSecond: redisFloat(values[3]),
			Samples:             redisInt64(values[4]),
			ConsecutiveFailures: int(redisInt64(values[5])),
			CooldownUntilMs:     redisInt64(values[6]),
			LastSuccessMs:       redisInt64(values[7]),
			LastFailureMs:       redisInt64(values[8]),
			Inflight:            int(redisInt64(values[9])),
			SnapshotAtMs:        redisInt64(values[10]),
		}
	}
	return items, nil
}

func (s *redisGatewayDeploymentStore) Acquire(ctx context.Context, in GatewayDeploymentAcquireInput) (*GatewayDeploymentLease, error) {
	if in.DeploymentID <= 0 || strings.TrimSpace(in.RequestID) == "" {
		return nil, errors.New("deployment lease identity is required")
	}
	setKey := deploymentInflightKey(in.DeploymentID)
	script := `local clock = redis.call('TIME'); local now = clock[1] * 1000 + math.floor(clock[2] / 1000); local cooldown = tonumber(redis.call('HGET', KEYS[1], 'cooldown_until_ms') or '0'); local state = redis.call('HGET', KEYS[1], 'state') or 'unknown'; redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now); local count = redis.call('ZCARD', KEYS[2]); if state == 'cooldown' and cooldown > now then return -1 end; if state == 'cooldown' and cooldown <= now then state = 'half_open'; redis.call('HSET', KEYS[1], 'state', state) end; if state == 'half_open' and count >= 1 then return -1 end; local limit = tonumber(ARGV[2]); if limit > 0 and count >= limit then return -1 end; if not redis.call('ZSCORE', KEYS[2], ARGV[1]) then redis.call('ZADD', KEYS[2], now + tonumber(ARGV[3]), ARGV[1]) end; redis.call('PEXPIRE', KEYS[2], ARGV[4]); return count + 1`
	reply, err := s.bus.do(ctx, "EVAL", script, "2", deploymentMetricsKey(in.DeploymentID), setKey,
		in.RequestID, strconv.Itoa(in.MaxInflight), strconv.FormatInt(gatewayDeploymentLeaseTTL.Milliseconds(), 10),
		strconv.FormatInt((gatewayDeploymentLeaseTTL+time.Minute).Milliseconds(), 10))
	if err != nil {
		return nil, err
	}
	value, ok := reply.(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected deployment acquire response")
	}
	if value < 0 {
		return nil, ErrGatewayDeploymentUnavailable
	}
	lease := &GatewayDeploymentLease{DeploymentID: in.DeploymentID, requestID: in.RequestID, setKey: setKey}
	go s.keepLeaseAlive(ctx, lease)
	return lease, nil
}

func (s *redisGatewayDeploymentStore) keepLeaseAlive(ctx context.Context, lease *GatewayDeploymentLease) {
	ticker := time.NewTicker(gatewayDeploymentLeaseHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lease.mu.Lock()
			released := lease.released
			lease.mu.Unlock()
			if released {
				return
			}
			script := `if not redis.call('ZSCORE', KEYS[1], ARGV[1]) then return 0 end; local clock = redis.call('TIME'); local now = clock[1] * 1000 + math.floor(clock[2] / 1000); redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[1]); redis.call('PEXPIRE', KEYS[1], ARGV[3]); return 1`
			if _, err := s.bus.do(ctx, "EVAL", script, "1", lease.setKey, lease.requestID,
				strconv.FormatInt(gatewayDeploymentLeaseTTL.Milliseconds(), 10),
				strconv.FormatInt((gatewayDeploymentLeaseTTL+time.Minute).Milliseconds(), 10)); err != nil {
				return
			}
		}
	}
}

func (s *redisGatewayDeploymentStore) Observe(ctx context.Context, in GatewayDeploymentObservation) error {
	if in.DeploymentID <= 0 {
		return nil
	}
	success := "0"
	if in.Success {
		success = "1"
	}
	penalize := "0"
	if in.PenalizeFailure {
		penalize = "1"
	}
	immediateCooldown := "0"
	if in.ImmediateCooldown && in.PenalizeFailure && !in.Success {
		immediateCooldown = "1"
	}
	script := `
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local success = ARGV[1] == '1'
local penalize = ARGV[2] == '1'
local ttft = tonumber(ARGV[3])
local latency = tonumber(ARGV[4])
local tps = tonumber(ARGV[5])
local samples = tonumber(redis.call('HGET', KEYS[1], 'samples') or '0')
local cooldownUntil = tonumber(redis.call('HGET', KEYS[1], 'cooldown_until_ms') or '0')
local alpha = samples < 5 and 0.5 or 0.2
local function ewma(field, sample)
  if sample <= 0 then return end
  local old = tonumber(redis.call('HGET', KEYS[1], field) or '0')
  local value = old <= 0 and sample or (alpha * sample + (1-alpha) * old)
  redis.call('HSET', KEYS[1], field, tostring(value))
end
if success then
  ewma('ewma_ttft_ms', ttft)
  ewma('ewma_latency_ms', latency)
  ewma('ewma_tps', tps)
  -- Calls already in flight must not clear a newly established cooldown.
  redis.call('HSET', KEYS[1], 'last_success_ms', now)
  if cooldownUntil <= now then
    redis.call('HSET', KEYS[1], 'state', 'healthy', 'consecutive_failures', '0', 'cooldown_until_ms', '0')
  end
  if ttft > 0 or latency > 0 or tps > 0 then
    redis.call('HINCRBY', KEYS[1], 'samples', 1)
  end
  redis.call('HINCRBY', KEYS[1], 'successes', 1)
elseif penalize then
  local failures = redis.call('HINCRBY', KEYS[1], 'consecutive_failures', 1)
  redis.call('HSET', KEYS[1], 'last_failure_ms', now)
  redis.call('HINCRBY', KEYS[1], 'failures', 1)
  if failures >= 3 or ARGV[8] == '1' then
    redis.call('HSET', KEYS[1], 'state', 'cooldown', 'cooldown_until_ms', math.max(cooldownUntil, now + tonumber(ARGV[6])))
  elseif cooldownUntil <= now then
    redis.call('HSET', KEYS[1], 'state', 'degraded')
  end
end
redis.call('PEXPIRE', KEYS[1], ARGV[7])
return 1`
	_, err := s.bus.do(ctx, "EVAL", script, "1", deploymentMetricsKey(in.DeploymentID), success, penalize,
		strconv.Itoa(in.TTFTMs), strconv.Itoa(in.LatencyMs), strconv.FormatFloat(in.TokensPerSecond, 'f', 4, 64),
		strconv.FormatInt((30*time.Second).Milliseconds(), 10),
		strconv.FormatInt(gatewayDeploymentMetricsTTL.Milliseconds(), 10), immediateCooldown)
	return err
}

func (s *redisGatewayDeploymentStore) Release(ctx context.Context, lease *GatewayDeploymentLease) error {
	if lease == nil || lease.setKey == "" || lease.requestID == "" {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.released {
		return nil
	}
	lease.released = true
	_, err := s.bus.do(ctx, "ZREM", lease.setKey, lease.requestID)
	return err
}

func redisString(value interface{}, fallback string) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

func redisFloat(value interface{}) float64 {
	text, ok := value.(string)
	if !ok {
		return 0
	}
	parsed, _ := strconv.ParseFloat(text, 64)
	return parsed
}

func redisInt64(value interface{}) int64 {
	switch item := value.(type) {
	case int64:
		return item
	case string:
		parsed, _ := strconv.ParseInt(item, 10, 64)
		return parsed
	default:
		return 0
	}
}
