package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"regexp"
	"time"
)

var streamPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{16,64}$`)

type ActivityPulse struct {
	Stream   string `json:"stream"`
	Sequence uint64 `json:"sequence"`
	Active   bool   `json:"active"`
}

// Server time, short consecutive intervals and owner-global 10s buckets prevent
// client-supplied durations, replay, offline catch-up and overlapping tabs.
func (s *Store) ActivityPulse(ctx context.Context, owner string, instance uint64, p ActivityPulse, now time.Time) error {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	if !streamPattern.MatchString(p.Stream) || p.Sequence == 0 || p.Sequence > 1000000000 || instance == 0 {
		return fmt.Errorf("invalid activity pulse")
	}
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM northbound_owner_records WHERE record_type='growth_profile' AND record_key=? FOR UPDATE`, Key("growth", owner))
		if err != nil {
			return err
		}
		var profile uint64
		if err = row.Scan(&profile); err == sql.ErrNoRows {
			return nil
		} else if err != nil {
			return err
		}
		key := Key("activity_cursor", owner, p.Stream, fmt.Sprint(instance))
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT version,occurred_at,status FROM northbound_owner_records WHERE record_type='activity_cursor' AND record_key=?`, key)
		if err != nil {
			return err
		}
		var sequence uint64
		var last time.Time
		var previousStatus string
		err = row.Scan(&sequence, &last, &previousStatus)
		fresh := err == sql.ErrNoRows
		if err != nil && !fresh {
			return err
		}
		if !fresh && p.Sequence <= sequence {
			return nil
		}
		if !p.Active && (fresh || previousStatus == "idle") {
			return nil
		}
		if fresh {
			var streams int
			row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='activity_cursor' AND owner_normalized=? AND expires_at>?`, owner, now.UTC())
			if err != nil {
				return err
			}
			if err = row.Scan(&streams); err != nil {
				return err
			}
			if streams >= 64 {
				return fmt.Errorf("too many active activity streams")
			}
		}
		status := "idle"
		if p.Active {
			status = "active"
		}
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status,version,occurred_at,expires_at) VALUES ('activity_cursor',?,0,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE version=VALUES(version),status=VALUES(status),occurred_at=VALUES(occurred_at),expires_at=VALUES(expires_at)`, key, owner, instance, status, p.Sequence, now.UTC(), now.Add(24*time.Hour).UTC())
		if err != nil {
			return err
		}
		gap := now.Sub(last)
		if fresh || !p.Active || previousStatus != "active" || gap < 10*time.Second || gap > 20*time.Second {
			return nil
		}
		loc, _ := time.LoadLocation("Asia/Shanghai")
		bucket := now.UTC().Truncate(10 * time.Second).Add(-10 * time.Second)
		day := bucket.In(loc).Format("2006-01-02")
		var seconds uint64
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(active_work_seconds),0) FROM northbound_owner_records WHERE record_type='activity_tick' AND owner_normalized=? AND business_date=?`, owner, day)
		if err != nil {
			return err
		}
		if err = row.Scan(&seconds); err != nil {
			return err
		}
		if seconds >= 8*3600 {
			return nil
		}
		payload, _ := json.Marshal(map[string]string{"source": "foreground-interaction", "rule": growthRule})
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status,active_work_seconds,business_date,occurred_at,interval_start,interval_end,payload) VALUES ('activity_tick',?,0,?,?,'confirmed',10,?,?,?,?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, Key("activity_tick", owner, fmt.Sprint(bucket.Unix())), owner, instance, day, bucket, bucket, bucket.Add(10*time.Second), string(payload))
		if err == nil {
			err = markUsageDay(ctx, tx, owner, bucket)
		}
		return err
	}, nil)
}
