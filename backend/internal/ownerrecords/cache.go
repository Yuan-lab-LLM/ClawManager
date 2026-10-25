package ownerrecords

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type rankingSnapshot struct {
	at     time.Time
	owners map[string]TokenRanking
}
type readEntry struct {
	at   time.Time
	data *Dashboard
}
type readFlight struct {
	done chan struct{}
	data *Dashboard
	err  error
}
type performanceState struct {
	epochMu       sync.Mutex
	epoch         *analyticsEpoch
	epochChecked  time.Time
	cacheRevision atomic.Uint64
	low           lowCostState
	rankMu        sync.Mutex
	rank          rankingSnapshot
	rankBuilds    uint64
	readMu        sync.Mutex
	reads         map[string]readEntry
	flights       map[string]*readFlight
	bootstrapDone bool // only the elected collector accesses this flag
	policyMu      sync.Mutex
	policies      map[string]time.Time
	defsMu        sync.Mutex
	definitions   map[string]bool
}

// Immutable rule boundaries may be cached indefinitely. GET never initializes
// them: missing policy is temporarily presented as tomorrow until the worker/visit initializes it.
func (s *Store) policy(ctx context.Context, rule string, now time.Time, initialize bool) (time.Time, error) {
	s.perf.policyMu.Lock()
	defer s.perf.policyMu.Unlock()
	if at, ok := s.perf.policies[rule]; ok {
		return at, nil
	}
	key := Key("growth-policy", rule)
	read := func() (time.Time, error) {
		row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='growth_policy' AND record_key=?`, key)
		if err != nil {
			return time.Time{}, err
		}
		var at time.Time
		err = row.Scan(&at)
		return at, err
	}
	at, err := read()
	if err == sql.ErrNoRows {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		local := now.In(loc)
		at = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc).UTC()
		if !initialize {
			return at, nil
		}
		_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,rule_version) VALUES ('growth_policy',?,0,'scheduled',?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, key, at, rule)
		if err == nil {
			at, err = read()
		}
	}
	if err == nil {
		if s.perf.policies == nil {
			s.perf.policies = map[string]time.Time{}
		}
		s.perf.policies[rule] = at
	}
	return at, err
}

// A bounded cache, scoped by trusted owner/account, coalesces repeated reads.
// Scope/northbound callers are kept on the authoritative uncached path.
func (s *Store) CachedDashboard(ctx context.Context, owner string, scope *uint64, period string, now time.Time) (*Dashboard, error) {
	if period != "7d" && period != "30d" && period != "month" {
		return nil, fmt.Errorf("range must be 7d, 30d or month")
	}
	if scope != nil {
		return s.Dashboard(ctx, owner, scope, period, now)
	}
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		snapshot, e := s.publishedOwner(ctx, owner, now)
		if e != nil {
			return nil, e
		}
		return snapshotRange(snapshot.Dashboard, period, now), nil
	}
	key := Key(owner, period)
	s.perf.readMu.Lock()
	if e, ok := s.perf.reads[key]; ok && !now.Before(e.at) && now.Sub(e.at) < 10*time.Minute {
		s.perf.readMu.Unlock()
		return e.data, nil
	}
	if f, ok := s.perf.flights[key]; ok {
		s.perf.readMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
			return f.data, f.err
		}
	}
	if s.perf.flights == nil {
		s.perf.flights = map[string]*readFlight{}
	}
	f := &readFlight{done: make(chan struct{})}
	s.perf.flights[key] = f
	revision := s.perf.cacheRevision.Load()
	s.perf.readMu.Unlock()
	release, err := s.readBudget(ctx)
	var d *Dashboard
	if err == nil {
		d, err = s.Dashboard(ctx, owner, nil, period, now)
		release()
	}
	s.perf.readMu.Lock()
	defer s.perf.readMu.Unlock()
	f.data, f.err = d, err
	delete(s.perf.flights, key)
	close(f.done)
	if err != nil {
		return nil, err
	}
	if s.perf.reads == nil || len(s.perf.reads) >= 2048 {
		s.perf.reads = map[string]readEntry{}
	}
	if revision == s.perf.cacheRevision.Load() {
		s.perf.reads[key] = readEntry{now, d}
	}
	return d, nil
}
