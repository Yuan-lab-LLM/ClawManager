package ownerrecords

import (
	"context"
	"github.com/upper/db/v4"
	"strconv"
	"strings"
	"time"
)

func (s *Store) applyNewQuestion(ctx context.Context, i Identity, f Fact, start time.Time) (bool, error) {
	if f.SubmittedAt.IsZero() || f.SubmittedAt.Before(start) || f.SubmittedAt.After(f.At) {
		return false, nil
	}
	owner := strings.ToLower(strings.TrimSpace(i.Owner))
	key := Key("question-v4", strconv.FormatUint(i.Instance, 10), f.SessionID, f.QuestionID)
	if f.Source == "openclaw_chat" || f.Source == "native_openclaw" {
		key = Key("question-v4", strconv.FormatUint(i.Instance, 10), "openclaw", f.QuestionID)
	}
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM northbound_owner_records WHERE record_type='question_v4' AND record_key=?)`, key)
	if e != nil {
		return false, e
	}
	var exists bool
	if e = row.Scan(&exists); e != nil {
		return false, e
	}
	if exists {
		return false, nil
	}
	applied := false
	e = s.Session.TxContext(ctx, func(tx db.Session) error {
		result, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,runtime_type,source_kind,session_key,question_key,status,occurred_at,interval_start) VALUES('question_v4',?,?,?,?,?,?,?,?,'confirmed',?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, key, i.Scope, owner, i.Instance, i.Runtime, f.Source, f.SessionID, f.QuestionID, f.At.UTC(), f.SubmittedAt.UTC())
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return nil
		}
		applied = true
		return markUsageDay(ctx, tx, owner, f.At)
	}, nil)
	return applied, e
}
