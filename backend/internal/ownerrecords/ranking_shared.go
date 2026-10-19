package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type sharedRanking struct {
	At     time.Time               `json:"at"`
	Owners map[string]TokenRanking `json:"owners"`
}

func (s *Store) readRanking(ctx context.Context) (sharedRanking, error) {
	var r sharedRanking
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='ranking_snapshot' AND record_key=?`, Key("ranking", "v4"))
	if e != nil {
		return r, e
	}
	var raw []byte
	if e = row.Scan(&raw); e == sql.ErrNoRows {
		return r, nil
	} else if e != nil {
		return r, e
	}
	e = json.Unmarshal(raw, &r)
	return r, e
}

// Only the elected worker computes the shared ranking. Portal requests read it.
func (s *Store) publishRanking(ctx context.Context, now time.Time) error {
	now = now.Truncate(summaryInterval)
	old, e := s.readRanking(ctx)
	if e != nil {
		return e
	}
	if !rankingRefreshDue(rankingSnapshot{old.At, old.Owners}, now) {
		return nil
	}
	rows, e := s.factRows(ctx, `SELECT owner_normalized,SUM(total_tokens),RANK() OVER (ORDER BY SUM(total_tokens) DESC) FROM northbound_owner_records WHERE record_type='invocation' AND owner_normalized<>'' AND occurred_at>=? AND occurred_at<? GROUP BY owner_normalized HAVING SUM(total_tokens)>0`, now.Add(-168*time.Hour).UTC(), now.UTC())
	if e != nil {
		return e
	}
	r := sharedRanking{At: now, Owners: map[string]TokenRanking{}}
	for rows.Next() {
		var who string
		var v TokenRanking
		var rank int
		if e = rows.Scan(&who, &v.Tokens, &rank); e != nil {
			rows.Close()
			return e
		}
		v.Rank = &rank
		r.Owners[who] = v
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,payload) VALUES('ranking_snapshot',?,0,'ready',?,?) ON DUPLICATE KEY UPDATE occurred_at=VALUES(occurred_at),payload=VALUES(payload),version=version+1`, Key("ranking", "v4"), now.UTC(), string(raw))
	return e
}

func (s *Store) sharedTokenRanking(ctx context.Context, owner string, now time.Time) (*TokenRanking, error) {
	// A primary-key read, with no raw-source queries or writes on page access.
	r, e := s.readRanking(ctx)
	if e != nil {
		return nil, e
	}
	if r.Owners == nil {
		return nil, nil
	}
	result := r.Owners[owner]
	result.Participants = len(r.Owners)
	result.Start = r.At.Add(-168 * time.Hour)
	result.End = r.At
	return &result, nil
}
