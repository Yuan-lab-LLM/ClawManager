package ownerrecords

import (
	"context"
	"fmt"
	"github.com/upper/db/v4"
	"math/bits"
	"time"
)

type activityStream struct {
	at     time.Time
	seq    uint64
	active bool
}
type activityWindow struct {
	owner string
	start time.Time
	mask  uint64
}

// Approximate foreground duration: no SQL or client-supplied durations.
func (s *Store) BufferActivity(owner string, instance uint64, p ActivityPulse, now time.Time) error {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	if !streamPattern.MatchString(p.Stream) || p.Sequence == 0 || p.Sequence > 1000000000 || instance == 0 {
		return fmt.Errorf("invalid activity pulse")
	}
	s.perf.low.activityMu.Lock()
	defer s.perf.low.activityMu.Unlock()
	if s.perf.low.streams == nil {
		s.perf.low.streams = map[string]activityStream{}
	}
	if s.perf.low.windows == nil {
		s.perf.low.windows = map[string]activityWindow{}
	}
	key := Key(owner, fmt.Sprint(instance), p.Stream)
	prev, exists := s.perf.low.streams[key]
	if exists && p.Sequence <= prev.seq {
		return nil
	}
	if len(s.perf.low.streams) >= 4096 {
		for k, v := range s.perf.low.streams {
			if now.Sub(v.at) > 10*time.Minute {
				delete(s.perf.low.streams, k)
			}
		}
	}
	if !exists && len(s.perf.low.streams) >= 4096 {
		return nil
	}
	s.perf.low.streams[key] = activityStream{now, p.Sequence, p.Active}
	gap := now.Sub(prev.at)
	if !exists || !prev.active || !p.Active || gap < 45*time.Second || gap > 90*time.Second {
		return nil
	}
	minute := now.UTC().Truncate(time.Minute).Add(-time.Minute)
	start := minute.Truncate(5 * time.Minute)
	windowKey := Key("activity-window", owner, fmt.Sprint(start.Unix()))
	w, ok := s.perf.low.windows[windowKey]
	if !ok && len(s.perf.low.windows) >= 2048 {
		return nil
	}
	w.owner = owner
	w.start = start
	w.mask |= 1 << uint(minute.Sub(start)/time.Minute)
	s.perf.low.windows[windowKey] = w
	return nil
}
func (s *Store) flushActivity(ctx context.Context) error {
	s.perf.low.activityMu.Lock()
	windows := s.perf.low.windows
	s.perf.low.windows = nil
	s.perf.low.activityMu.Unlock()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	n := 0
	for key, w := range windows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n++
		if n > 500 {
			return nil
		}
		err := s.Session.TxContext(ctx, func(tx db.Session) error {
			row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=?`, Key("growth", w.owner))
			if err != nil {
				return err
			}
			var found int
			if err = row.Scan(&found); err != nil {
				return err
			}
			if found == 0 {
				return nil
			}
			row, err = tx.SQL().QueryRowContext(ctx, `SELECT id FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=? FOR UPDATE`, Key("growth", w.owner))
			if err != nil {
				return err
			}
			var id uint64
			if err = row.Scan(&id); err != nil {
				return err
			}
			day := w.start.In(loc).Format("2006-01-02")
			row, err = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(active_work_seconds),0),COALESCE(MAX(CASE WHEN record_type='activity_window' AND record_key=? THEN version ELSE 0 END),0) FROM northbound_owner_records WHERE record_type IN ('activity_tick','activity_window') AND owner_normalized=? AND business_date=?`, key, w.owner, day)
			if err != nil {
				return err
			}
			var seconds, old uint64
			if err = row.Scan(&seconds, &old); err != nil {
				return err
			}
			fresh := w.mask &^ old
			available := uint64(0)
			if seconds < 8*3600 {
				available = (8*3600 - seconds) / 60
			}
			for uint64(bits.OnesCount64(fresh)) > available {
				fresh &= fresh - 1
			}
			if fresh == 0 {
				return nil
			}
			merged := old | fresh
			_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,interval_start,interval_end,occurred_at,version,active_work_seconds) VALUES ('activity_window',?,0,?,'confirmed',?,?,?,?,?,?) ON DUPLICATE KEY UPDATE version=VALUES(version),active_work_seconds=VALUES(active_work_seconds)`, key, w.owner, day, w.start, w.start.Add(5*time.Minute), w.start, merged, bits.OnesCount64(merged)*60)
			if err != nil {
				return err
			}
			return markUsageDay(ctx, tx, w.owner, w.start)
		}, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// Disposable buffers are per serving replica; database uniqueness merges them.
func (s *Store) RunBuffered(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	observations := time.NewTimer(time.Until(nextSummaryBatch(time.Now())))
	defer observations.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-observations.C:
			if s.Available() {
				pass, cancel := context.WithTimeout(ctx, 3*time.Second)
				s.drainObservations(pass)
				cancel()
			}
			observations.Reset(time.Until(nextSummaryBatch(time.Now())))
		case <-ticker.C:
			if !s.Available() {
				continue
			}
			pass, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = s.flushActivity(pass)
			cancel()
		}
	}
}
