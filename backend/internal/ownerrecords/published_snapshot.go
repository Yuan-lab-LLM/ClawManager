package ownerrecords

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/upper/db/v4"
	"time"
)

type ownerSnapshot struct {
	Reports   map[string]*Report `json:"reports"`
	Dashboard *Dashboard         `json:"dashboard"`
	Growth    *Growth            `json:"growth"`
	State     SnapshotState      `json:"snapshot"`
}

func (s *Store) buildOwnerSnapshot(ctx context.Context, owner string, now time.Time, save bool) (*ownerSnapshot, error) {
	epoch, e := s.analyticsEpoch(ctx)
	if e != nil {
		return nil, e
	}
	effective, e := s.policy(ctx, growthRuleV2, now, false)
	if e != nil {
		return nil, e
	}
	v3, e := s.policy(ctx, growthRuleV3, now, false)
	if e != nil {
		return nil, e
	}
	result := &ownerSnapshot{}
	e = s.Session.TxContext(ctx, func(tx db.Session) error {
		// Serialize publishing with immediate login awards, never with instance work.
		if save {
			row, e := tx.SQL().QueryRowContext(ctx, `SELECT version FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=? FOR UPDATE`, Key("growth", owner))
			if e != nil {
				return e
			}
			var version uint64
			if e = row.Scan(&version); e != nil && e != sql.ErrNoRows {
				return e
			}
		}
		reader := &Store{Session: tx}
		reader.perf.epoch = epoch
		var e error
		result.Dashboard, e = reader.Dashboard(ctx, owner, nil, "30d", now.Truncate(summaryInterval))
		if e != nil {
			return e
		}
		// Text authorization and expiry must be checked at read time, not frozen
		// into a long-lived numeric snapshot after instance ownership changes.
		result.Dashboard.RecentQuestions = nil
		result.Growth, e = reader.growthSnapshot(ctx, owner, now, effective, v3, nil)
		if e != nil {
			return e
		}
		result.Reports = map[string]*Report{}
		loc, _ := time.LoadLocation("Asia/Shanghai")
		for _, kind := range []string{"week", "month"} {
			report, e := reader.Report(ctx, owner, kind, now.In(loc).Format("2006-01-02"), now.Truncate(summaryInterval), false)
			if e != nil {
				return e
			}
			result.Reports[kind] = report
		}
		result.State = SnapshotState{NextRefreshAt: nextSummaryBatch(now).Add(20 * time.Second), State: "ready"}
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT status FROM northbound_owner_records WHERE record_type='analytics_epoch' AND record_key=?`, Key("analytics-epoch", "v4"))
		if e != nil {
			return e
		}
		var state string
		if e = row.Scan(&state); e != nil && e != sql.ErrNoRows {
			return e
		}
		if state != "ready" {
			result.State.State = "initializing"
		}
		asof := now.Truncate(summaryInterval)
		result.State.AsOf = &asof
		if !save {
			return nil
		}
		// Metadata time alone does not cause a new owner write on repeated visits.
		result.Growth.AsOf = time.Time{}
		result.Dashboard.Insights.AsOf = time.Time{}
		stable, e := json.Marshal(struct {
			D *Dashboard
			G *Growth
		}{result.Dashboard, result.Growth})
		if e != nil {
			return e
		}
		sum := sha256.Sum256(stable)
		hash := hex.EncodeToString(sum[:])
		key := Key("owner-published", owner)
		row, e = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(source_key,''),version FROM northbound_owner_records WHERE record_type='owner_snapshot' AND record_key=? FOR UPDATE`, key)
		if e != nil {
			return e
		}
		var previous string
		var revision uint64
		e = row.Scan(&previous, &revision)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		result.Growth.AsOf = now
		result.Dashboard.Insights.AsOf = asof
		if e == nil && previous == hash {
			return nil
		}
		result.State.Version = revision + 1
		raw, e := json.Marshal(result)
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,source_key,version,occurred_at,payload) VALUES('owner_snapshot',?,0,?,'published',?,?,?,?) ON DUPLICATE KEY UPDATE source_key=VALUES(source_key),version=VALUES(version),occurred_at=VALUES(occurred_at),payload=VALUES(payload)`, key, owner, hash, result.State.Version, now.UTC(), string(raw))
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: !save})
	return result, e
}

func (s *Store) publishedOwner(ctx context.Context, owner string, now time.Time) (*ownerSnapshot, error) {
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT payload,(SELECT status FROM northbound_owner_records WHERE record_type='analytics_epoch' AND record_key=?),COALESCE((SELECT status FROM northbound_owner_records WHERE record_type='summary_batch' AND record_key=?),'ready') FROM northbound_owner_records WHERE record_type='owner_snapshot' AND record_key=?`, Key("analytics-epoch", "v4"), Key("summary-batch", "v4"), Key("owner-published", owner))
	if e != nil {
		return nil, e
	}
	var raw []byte
	var liveState string
	var batchStatus string
	e = row.Scan(&raw, &liveState, &batchStatus)
	var snapshot ownerSnapshot
	if e == nil {
		e = json.Unmarshal(raw, &snapshot)
	}
	if e != nil && e != sql.ErrNoRows {
		return nil, e
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	if e == sql.ErrNoRows || snapshot.Growth == nil || snapshot.Growth.AsOf.In(loc).Format("2006-01-02") != now.In(loc).Format("2006-01-02") {
		fresh, e := s.buildOwnerSnapshot(ctx, owner, now, false)
		if e != nil {
			return nil, e
		}
		snapshot = *fresh
	}
	snapshot.State.NextRefreshAt = nextSummaryBatch(now).Add(20 * time.Second)
	if liveState == "ready" {
		snapshot.State.State = "ready"
		if batchStatus != "ready" {
			snapshot.State.State = "updating"
		}
	}
	if snapshot.State.State == "initializing" {
		snapshot.State.NextRefreshAt = now.Add(30 * time.Second)
	}
	snapshot.Growth.Ranking, e = s.TokenRanking(ctx, owner, now)
	if e != nil {
		return nil, e
	}
	snapshot.Dashboard.RecentQuestions, e = s.recentQuestions(ctx, owner, nil, now)
	if e != nil {
		return nil, e
	}
	snapshot.Dashboard.Growth = snapshot.Growth
	snapshot.Dashboard.Snapshot = &snapshot.State
	snapshot.Growth.Snapshot = &snapshot.State
	return &snapshot, nil
}

func snapshotRange(d *Dashboard, period string, now time.Time) *Dashboard {
	copy := *d
	copy.Range = period
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(loc)
	end := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	start := end.AddDate(0, 0, -7)
	if period == "30d" {
		start = end.AddDate(0, 0, -30)
	} else if period == "month" {
		start = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	}
	copy.Trend = []Day{}
	for _, day := range d.Insights.Days {
		if day.Date >= start.Format("2006-01-02") && day.Date < end.Format("2006-01-02") {
			copy.Trend = append(copy.Trend, Day{Date: day.Date, Tokens: day.Tokens})
		}
	}
	return &copy
}
