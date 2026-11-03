package ownerrecords

import (
	"context"
	"database/sql"
	"time"

	"github.com/upper/db/v4"
)

type analyticsEpoch struct {
	Start     time.Time
	RuleStart time.Time
}

// A durable boundary, never a per-process or per-owner timestamp. This is only
// called by the initializer; reads never create schema, rules or progress rows.
func (s *Store) prepareAnalyticsEpoch(ctx context.Context, now time.Time) (analyticsEpoch, error) {
	var result analyticsEpoch
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		key := Key("analytics-epoch", "v4")
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,rule_version) VALUES ('analytics_epoch',?,0,'initializing',?,'growth-v4') ON DUPLICATE KEY UPDATE record_key=record_key`, key, now.UTC())
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='analytics_epoch' AND record_key=? FOR UPDATE`, key)
		if err != nil {
			return err
		}
		if err = row.Scan(&result.Start); err != nil {
			return err
		}
		// Activate immediately; daily legacy awards are credited by category,
		// so switching versions never issues the same daily reward twice.
		result.RuleStart = result.Start
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,rule_version) VALUES ('growth_policy',?,0,'scheduled',?,'growth-v4') ON DUPLICATE KEY UPDATE record_key=record_key`, Key("growth-policy", growthRuleV4), result.RuleStart)
		if err != nil {
			return err
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='growth_policy' AND record_key=?`, Key("growth-policy", growthRuleV4))
		if err != nil {
			return err
		}
		return row.Scan(&result.RuleStart)
	}, nil)
	if err == nil {
		s.perf.epochMu.Lock()
		s.perf.epoch = &result
		s.perf.epochMu.Unlock()
	}
	return result, err
}

func (s *Store) readAnalyticsEpoch(ctx context.Context) (*analyticsEpoch, error) {
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT e.occurred_at,p.occurred_at FROM northbound_owner_records e JOIN northbound_owner_records p ON p.record_type='growth_policy' AND p.record_key=? WHERE e.record_type='analytics_epoch' AND e.record_key=?`, Key("growth-policy", growthRuleV4), Key("analytics-epoch", "v4"))
	if err != nil {
		return nil, err
	}
	var result analyticsEpoch
	if err = row.Scan(&result.Start, &result.RuleStart); err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}
