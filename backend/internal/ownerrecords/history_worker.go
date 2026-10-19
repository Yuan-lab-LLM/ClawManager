package ownerrecords

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Native histories are read-only input. Failures do not touch instance state or
// decrement accumulated statistics. Scan progress is independent of invocation
// accounting, and unique evidence keys make restart/replay safe.
func (s *Store) ScanHistories(ctx context.Context, after uint64) (uint64, error) {
	if s.HistoryRoot == "" {
		return 0, nil
	}
	// Keep last successful excerpts across quiet days and deployments.
	_, err := s.Session.SQL().ExecContext(ctx, `DELETE r FROM northbound_owner_records r LEFT JOIN instances i ON i.id=r.instance_id AND i.user_id=r.scope_id AND LOWER(TRIM(i.owner))=r.owner_normalized WHERE r.record_type='recent_question' AND i.id IS NULL`)
	if err != nil {
		return after, err
	}
	epoch, err := s.analyticsEpoch(ctx)
	if err != nil {
		return after, err
	}
	query := `SELECT id,COALESCE(workspace_path,''),user_id,LOWER(TRIM(owner)),type FROM instances WHERE id>? AND owner IS NOT NULL AND TRIM(owner)<>'' AND LOWER(TRIM(owner)) NOT IN ('factory-reset-staging@clawmanager.local','admin@clawmanager.local') AND instance_mode='lite' AND type IN ('openclaw','hermes','opencode','deepseek-harness') AND (EXISTS (SELECT 1 FROM model_invocations m WHERE m.instance_id=instances.id AND m.created_at>=UTC_TIMESTAMP()-INTERVAL 1 DAY) OR NOT EXISTS (SELECT 1 FROM northbound_owner_records h WHERE h.record_type='history_scan' AND h.instance_id=instances.id)) ORDER BY id LIMIT 20`
	args := []any{after}
	if epoch != nil {
		query = strings.Replace(query, "m.created_at>=UTC_TIMESTAMP()-INTERVAL 1 DAY", "m.created_at>=GREATEST(?,UTC_TIMESTAMP()-INTERVAL 1 DAY)", 1)
		query = strings.Replace(query, " OR NOT EXISTS (SELECT 1 FROM northbound_owner_records h WHERE h.record_type='history_scan' AND h.instance_id=instances.id)", "", 1)
		args = append(args, epoch.Start.UTC())
	}
	rows, err := s.Session.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return after, err
	}
	type item struct {
		id      uint64
		path    string
		current Identity
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.path, &i.current.Scope, &i.current.Owner, &i.current.Runtime); err != nil {
			rows.Close()
			return after, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	for _, i := range items {
		if ctx.Err() != nil {
			return after, ctx.Err()
		}
		i.current.Instance = i.id
		after = i.id
		i.current.Mode = "lite"
		key := Key("history_scan", strconv.FormatUint(i.id, 10))
		before, fingerprintErr := historyFingerprint(ctx, s.HistoryRoot, i.path, i.current)
		if fingerprintErr == nil {
			row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.fingerprint')),''),occurred_at FROM northbound_owner_records WHERE record_type='history_scan' AND record_key=? AND status='ready'`, key)
			var fingerprint string
			var checked time.Time
			if e == nil && row.Scan(&fingerprint, &checked) == nil && fingerprint == before {
				after = i.id
				continue
			}
		}
		identity, err := s.Bind(ctx, i.id)
		if err != nil {
			return after, err
		}
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var facts []NativeQuestion
		var readErr error
		if epoch == nil {
			facts, readErr = ReadNativeHistory(readCtx, s.HistoryRoot, i.path, *identity)
		} else {
			facts, readErr = readNativeHistory(readCtx, s.HistoryRoot, i.path, *identity, false, epoch.Start)
		}
		cancel()
		state := "ready"
		if readErr != nil {
			state = "unavailable"
		} else {
			for _, q := range facts {
				if q.At <= 0 || q.At > float64(time.Now().Add(5*time.Minute).Unix()) {
					state = "partial"
					continue
				}
				at := time.UnixMilli(int64(q.At * 1000))
				if _, err = s.Apply(ctx, *identity, Fact{Type: "question", Source: "native_" + identity.Runtime, SourceID: Key(q.Session, q.ID), SessionID: q.Session, QuestionID: q.ID, At: at, SubmittedAt: time.UnixMilli(int64(q.Submitted * 1000))}); err != nil {
					return after, err
				}
			}
		}
		fingerprint := ""
		if fingerprintErr == nil && state == "ready" {
			if afterRead, e := historyFingerprint(ctx, s.HistoryRoot, i.path, i.current); e == nil && afterRead == before {
				fingerprint = before
			}
		}
		_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records (record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,status,occurred_at,payload) VALUES ('history_scan',?,?,?,?,?,?,?,?,JSON_OBJECT('rule','native-v1','fingerprint',?,'reason',?)) ON DUPLICATE KEY UPDATE status=VALUES(status),occurred_at=VALUES(occurred_at),payload=VALUES(payload)`, key, identity.Scope, identity.Owner, identity.Owner, i.id, identity.Runtime, state, time.Now().UTC(), fingerprint, func() string {
			if readErr != nil {
				return fmt.Sprintf("%T", readErr)
			}
			return ""
		}())
		if err != nil {
			return after, err
		}
		after = i.id
	}
	return after, nil
}
