package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
)

// Store is Core-only. The northbound Gateway must not receive this dependency.
type Store struct {
	Session      db.Session
	HistoryRoot  string
	Availability func() bool
	perf         performanceState
}

func (s *Store) Available() bool { return s != nil && (s.Availability == nil || s.Availability()) }

type Identity struct {
	Scope         uint64
	Owner         string
	Instance      uint64
	Runtime, Mode string
}
type Fact struct {
	Type, Source, SourceID, SessionID, QuestionID string
	At                                            time.Time
	SubmittedAt                                   time.Time
	Prompt, Completion, Tokens                    uint64
}

func (f Fact) validate() error {
	switch f.Type {
	case "invocation", "session", "question":
	default:
		return fmt.Errorf("unsupported usage fact")
	}
	if f.Source == "" || len(f.Source) > 32 || f.SourceID == "" || len(f.SourceID) > 255 || f.At.IsZero() {
		return fmt.Errorf("missing or oversized fact identity")
	}
	if len(f.SessionID) > 255 || len(f.QuestionID) > 255 {
		return fmt.Errorf("oversized conversation identity")
	}
	if f.Type != "invocation" && f.SessionID == "" {
		return fmt.Errorf("stable session identity required")
	}
	if f.Type == "question" && f.QuestionID == "" {
		return fmt.Errorf("stable question identity required")
	}
	if f.Type != "invocation" && (f.Prompt != 0 || f.Completion != 0 || f.Tokens != 0) {
		return fmt.Errorf("tokens belong only to invocations")
	}
	return nil
}

// Bind captures authoritative instance identity. It never accepts an owner
// supplied by a browser or by a model request. Existing snapshots are immutable.
func (s *Store) Bind(ctx context.Context, instanceID uint64) (*Identity, error) {
	if instanceID == 0 {
		return nil, fmt.Errorf("instance required")
	}
	key := Key("binding", strconv.FormatUint(instanceID, 10))
	read := func() (*Identity, error) {
		row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT scope_id,owner_normalized,instance_id,COALESCE(runtime_type,''),COALESCE(instance_mode,'') FROM northbound_owner_records WHERE record_type='binding' AND record_key=?`, key)
		if err != nil {
			return nil, err
		}
		var i Identity
		if err = row.Scan(&i.Scope, &i.Owner, &i.Instance, &i.Runtime, &i.Mode); err != nil {
			return nil, err
		}
		return &i, nil
	}
	if i, err := read(); err == nil {
		return i, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	_, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records
 (record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,instance_mode,status,payload)
 SELECT 'binding',?,user_id,owner,LOWER(TRIM(owner)),id,type,instance_mode,'captured',JSON_OBJECT('source','instances')
 FROM instances WHERE id=? AND owner IS NOT NULL AND TRIM(owner)<>''
 ON DUPLICATE KEY UPDATE record_key=record_key`, key, instanceID)
	if err != nil {
		return nil, err
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT scope_id,owner_normalized,instance_id,COALESCE(runtime_type,''),COALESCE(instance_mode,'') FROM northbound_owner_records WHERE record_type='binding' AND record_key=?`, key)
	if err != nil {
		return nil, err
	}
	var identity Identity
	if err = row.Scan(&identity.Scope, &identity.Owner, &identity.Instance, &identity.Runtime, &identity.Mode); err != nil {
		return nil, err
	}
	return &identity, nil
}

// Apply inserts an immutable dedupe fact and updates its owner aggregate in one
// transaction. Replays have no effect, including replays with another owner.
func (s *Store) Apply(ctx context.Context, identity Identity, fact Fact) (bool, error) {
	if identity.Scope == 0 || identity.Instance == 0 || strings.TrimSpace(identity.Owner) == "" {
		return false, fmt.Errorf("authoritative owner identity required")
	}
	if err := fact.validate(); err != nil {
		return false, err
	}
	if fact.Type == "question" {
		epoch, e := s.analyticsEpoch(ctx)
		if e != nil {
			return false, e
		}
		if epoch != nil {
			return s.applyNewQuestion(ctx, identity, fact, epoch.Start)
		}
	}
	owner := strings.ToLower(strings.TrimSpace(identity.Owner))
	factKey := Key(fact.Type, fact.Source, fact.SourceID)
	if fact.Type == "session" {
		factKey = Key("session", strconv.FormatUint(identity.Instance, 10), fact.SessionID)
	}
	if fact.Type == "question" {
		factKey = Key("question", strconv.FormatUint(identity.Instance, 10), fact.SessionID, fact.QuestionID)
		if fact.Source == "openclaw_chat" || fact.Source == "native_openclaw" {
			factKey = Key("question", strconv.FormatUint(identity.Instance, 10), "openclaw", fact.QuestionID)
		}
	}
	profileKey := Key("profile", strconv.FormatUint(identity.Scope, 10), owner)
	// Immutable facts never need another write. The unique constraint below still
	// arbitrates concurrent first arrivals; sessions retain earlier-date repair.
	if fact.Type != "session" {
		row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type=? AND record_key=?`, fact.Type, factKey)
		if err != nil {
			return false, err
		}
		var existing int
		if err = row.Scan(&existing); err != nil {
			return false, err
		}
		if existing > 0 {
			return false, nil
		}
	}
	applied := false
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		// A profile lock serializes owner aggregate updates across App replicas.
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records
   (record_type,record_key,scope_id,owner,owner_normalized,status,prompt_tokens,completion_tokens,total_tokens,invocation_count,question_count,session_count)
   VALUES ('profile',?,?,?,?,'active',0,0,0,0,0,0) ON DUPLICATE KEY UPDATE record_key=record_key`, profileKey, identity.Scope, owner, owner)
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM northbound_owner_records WHERE record_type='profile' AND record_key=? FOR UPDATE`, profileKey)
		if err != nil {
			return err
		}
		var profileID uint64
		if err = row.Scan(&profileID); err != nil {
			return err
		}
		if fact.Type == "question" && (fact.Source == "native_openclaw" || fact.Source == "openclaw_chat") {
			row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='question' AND instance_id=? AND question_key=? AND source_kind IN ('native_openclaw','openclaw_chat')`, identity.Instance, fact.QuestionID)
			if err != nil {
				return err
			}
			var existing int
			if err = row.Scan(&existing); err != nil {
				return err
			}
			if existing > 0 {
				return nil
			}
		}
		evidence, _ := json.Marshal(map[string]string{"source": fact.Source, "source_id": fact.SourceID})
		// Do not use INSERT IGNORE: invalid or overflowing measurements must fail.
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records
   (record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,instance_mode,source_kind,source_key,session_key,question_key,status,occurred_at,prompt_tokens,completion_tokens,total_tokens,payload)
   VALUES (?,?,?,?,?,?,?,?,?,?,?,?, 'applied',?,?,?,?,?)`, fact.Type, factKey, identity.Scope, owner, owner, identity.Instance, identity.Runtime, identity.Mode, fact.Source, fact.SourceID, fact.SessionID, fact.QuestionID, fact.At.UTC(), fact.Prompt, fact.Completion, fact.Tokens, string(evidence))
		if err != nil {
			var duplicate *mysqlDriver.MySQLError
			if errors.As(err, &duplicate) && duplicate.Number == 1062 {
				if fact.Type == "session" {
					row, e := tx.SQL().QueryRowContext(ctx, `SELECT occurred_at FROM northbound_owner_records WHERE record_type='session' AND record_key=? FOR UPDATE`, factKey)
					if e != nil {
						return e
					}
					var old time.Time
					if e = row.Scan(&old); e != nil {
						return e
					}
					if !fact.At.Before(old) {
						return nil
					}
					if _, e = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET occurred_at=? WHERE record_type='session' AND record_key=?`, fact.At.UTC(), factKey); e != nil {
						return e
					}
					if e = markUsageDay(ctx, tx, owner, old); e != nil {
						return e
					}
					return markUsageDay(ctx, tx, owner, fact.At)
				}
				return nil
			}
			return err
		}
		var calls, questions, sessions uint64
		switch fact.Type {
		case "invocation":
			calls = 1
		case "question":
			questions = 1
		case "session":
			sessions = 1
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET
   prompt_tokens=prompt_tokens+?,completion_tokens=completion_tokens+?,total_tokens=total_tokens+?,
   invocation_count=invocation_count+?,question_count=question_count+?,session_count=session_count+?,version=version+1,
   last_activity_at=CASE WHEN ?='question' THEN GREATEST(COALESCE(last_activity_at,?),?) ELSE last_activity_at END
   WHERE id=?`, fact.Prompt, fact.Completion, fact.Tokens, calls, questions, sessions, fact.Type, fact.At.UTC(), fact.At.UTC(), profileID)
		if err == nil {
			applied = true
			err = markUsageDay(ctx, tx, owner, fact.At)
		}
		return err
	}, nil)
	if err != nil {
		return false, err
	}
	return applied, nil
}

// Backfill processes a bounded batch of existing terminal model calls. It scans
// by missing immutable source keys, not a high-water ID that can skip late commits.
// Missing historical ownership is deliberately skipped, never guessed.
func (s *Store) Backfill(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		return 0, fmt.Errorf("batch limit must be 1..500")
	}
	rows, err := s.Session.SQL().QueryContext(ctx, `SELECT m.id,m.instance_id,COALESCE(m.session_id,''),m.status,m.created_at,m.prompt_tokens,m.completion_tokens,m.total_tokens
 FROM model_invocations m WHERE m.status IN ('completed','failed') AND m.instance_id IS NOT NULL
 AND (EXISTS (SELECT 1 FROM instances i WHERE i.id=m.instance_id AND i.owner IS NOT NULL AND TRIM(i.owner)<>'')
 OR EXISTS (SELECT 1 FROM northbound_owner_records b WHERE b.record_type='binding' AND b.instance_id=m.instance_id))
 AND NOT EXISTS (SELECT 1 FROM northbound_owner_records r WHERE r.record_type='invocation' AND r.source_kind='model_invocations' AND r.source_key=CAST(m.id AS CHAR))
 ORDER BY m.id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	type call struct {
		id, instance               uint64
		session, status            string
		at                         time.Time
		prompt, completion, tokens int64
	}
	items := []call{}
	for rows.Next() {
		var c call
		if err = rows.Scan(&c.id, &c.instance, &c.session, &c.status, &c.at, &c.prompt, &c.completion, &c.tokens); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range items {
		if c.prompt < 0 || c.completion < 0 || c.tokens < 0 {
			return count, fmt.Errorf("negative usage in source record %d", c.id)
		}
		identity, err := s.Bind(ctx, c.instance)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return count, err
		}
		// Session first: a crash before invocation insertion is recoverable on replay.
		// Explicit trace fallback sessions are excluded; unknown historical quality
		// remains a coverage limitation, not proof of complete runtime history.
		if c.status == "completed" && c.session != "" && !strings.HasPrefix(c.session, "sess_trc_") && !strings.HasPrefix(c.session, "trc_") {
			if _, err = s.Apply(ctx, *identity, Fact{Type: "session", Source: "model_invocations", SourceID: strconv.FormatUint(c.id, 10), SessionID: c.session, At: c.at}); err != nil {
				return count, err
			}
		}
		inserted, err := s.Apply(ctx, *identity, Fact{Type: "invocation", Source: "model_invocations", SourceID: strconv.FormatUint(c.id, 10), SessionID: c.session, At: c.at, Prompt: uint64(c.prompt), Completion: uint64(c.completion), Tokens: uint64(c.tokens)})
		if err != nil {
			return count, err
		}
		if inserted {
			count++
		}
	}
	return count, nil
}
