package ownerrecords

import (
	"context"
	"github.com/upper/db/v4"
	"time"
)

func (s *Store) registerRead(owner, key string) {
	s.perf.low.mu.Lock()
	defer s.perf.low.mu.Unlock()
	if s.perf.low.ownerKeys == nil {
		s.perf.low.ownerKeys = map[string]map[string]bool{}
	}
	if len(s.perf.low.ownerKeys) >= 4096 {
		s.perf.low.ownerKeys = map[string]map[string]bool{}
	}
	if s.perf.low.ownerKeys[owner] == nil || len(s.perf.low.ownerKeys[owner]) >= 128 {
		s.perf.low.ownerKeys[owner] = map[string]bool{}
	}
	s.perf.low.ownerKeys[owner][key] = true
}

// No database writes and no global cache flush. A revision fence prevents an
// in-flight pre-commit read from repopulating the cache after invalidation.
func (s *Store) InvalidateOwner(owner string) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return
	}
	s.perf.low.mu.Lock()
	s.perf.cacheRevision.Add(1)
	for key := range s.perf.low.ownerKeys[owner] {
		delete(s.perf.low.growth, key)
		delete(s.perf.low.reports, key)
	}
	delete(s.perf.low.ownerKeys, owner)
	s.perf.low.mu.Unlock()
	s.perf.readMu.Lock()
	for _, period := range []string{"7d", "30d", "month"} {
		delete(s.perf.reads, Key(owner, period))
	}
	s.perf.readMu.Unlock()
}

// Daily visits are the sole foreground exception. Coalesce repeated requests;
// remember success only after the login fact AND its one daily reward commit.
// Failure leaves the visit retryable. Usage/history/cumulative tasks stay batched.
func (s *Store) VisitToday(ctx context.Context, owner string, now time.Time) error {
	now = now.Truncate(time.Microsecond)
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := now.In(loc).Format("2006-01-02")
	key := Key("visit-now", owner, day)
	for {
		s.perf.low.mu.Lock()
		if _, ok := s.perf.low.visits[key]; ok {
			s.perf.low.mu.Unlock()
			return nil
		}
		if wait, ok := s.perf.low.growthBusy[key]; ok {
			s.perf.low.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-wait:
				continue
			}
		}
		if s.perf.low.growthBusy == nil {
			s.perf.low.growthBusy = map[string]chan struct{}{}
		}
		done := make(chan struct{})
		s.perf.low.growthBusy[key] = done
		s.perf.low.mu.Unlock()
		release, e := s.readBudget(ctx)
		if e == nil {
			e = s.recordVisitToday(ctx, owner, day, now)
			release()
		}
		s.perf.low.mu.Lock()
		delete(s.perf.low.growthBusy, key)
		close(done)
		if e == nil {
			if s.perf.low.visits == nil {
				s.perf.low.visits = map[string]time.Time{}
			}
			for k, at := range s.perf.low.visits {
				if now.Sub(at) >= 24*time.Hour {
					delete(s.perf.low.visits, k)
				}
			}
			if len(s.perf.low.visits) < 2048 {
				s.perf.low.visits[key] = now
			}
		}
		s.perf.low.mu.Unlock()
		return e
	}
}

func (s *Store) recordVisitToday(ctx context.Context, owner, day string, now time.Time) error {
	epoch, epochErr := s.analyticsEpoch(ctx)
	if epochErr != nil {
		return epochErr
	}
	effective, err := s.growthEffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	v3, err := s.growthV3EffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	err = s.Session.TxContext(ctx, func(tx db.Session) error {
		_, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,xp_value,occurred_at,rule_version) VALUES ('growth_profile',?,0,?,'active',0,?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, Key("growth", owner), owner, now.UTC(), growthRule)
		if e != nil {
			return e
		}
		result, e := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,occurred_at) VALUES ('login_day',?,0,?,'confirmed',?,?)`, Key("login", owner, day), owner, day, now.UTC())
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n > 0 {
			return markUsageDay(ctx, tx, owner, now)
		}
		if epoch != nil {
			result, e = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET rule_version=? WHERE record_type='growth_profile' AND record_key=? AND COALESCE(rule_version,'')<>?`, growthRuleV4, Key("growth", owner), growthRuleV4)
			if e != nil {
				return e
			}
			n, e = result.RowsAffected()
			if e != nil {
				return e
			}
			if n > 0 {
				return markUsageDay(ctx, tx, owner, now)
			}
		}
		return nil
	}, nil)
	if err != nil {
		return err
	}
	err = s.reconcileGrowthSelected(ctx, owner, now, now, false, true, effective, v3)
	if err == nil {
		if epoch, e := s.analyticsEpoch(ctx); e != nil {
			return e
		} else if epoch != nil {
			_, err = s.buildOwnerSnapshot(ctx, owner, now, true)
		}
	}
	if err == nil {
		s.InvalidateOwner(owner)
	}
	return err
}
