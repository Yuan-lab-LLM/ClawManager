package ownerrecords

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/upper/db/v4"
)

type sourceUsage struct {
	scope                             uint64
	instance                          uint64
	at                                time.Time
	owner, runtime                    string
	prompt, completion, tokens, calls uint64
}

func (u sourceUsage) key() string {
	return Key("source-usage-v4", strconv.FormatUint(u.scope, 10), u.owner, strconv.FormatUint(u.instance, 10), u.at.UTC().Format(time.RFC3339))
}

// Read numeric source columns only. Captured bindings take precedence over a
// current instance owner; deleted/unattributable calls are not guessed.
const sourceIdentityJoin = ` FROM model_invocations m
LEFT JOIN northbound_owner_records b ON b.record_type='binding' AND b.instance_id=m.instance_id
LEFT JOIN instances i ON i.id=m.instance_id
WHERE m.created_at>=? AND m.created_at<? AND m.status IN ('completed','failed')
AND COALESCE(b.owner_normalized,LOWER(TRIM(i.owner)),'')<>''
AND COALESCE(b.scope_id,i.user_id,0)>0
AND m.prompt_tokens>=0 AND m.completion_tokens>=0 AND m.total_tokens>=0`

// aggregateSourceDay is a bounded replacement, never an additive replay or a
// copy of invocation bodies. The source read and absolute writes share one
// transaction. An older bootstrap window cannot overwrite a newer live window.
// This is staged independently until all projection readers use source_usage.
func (s *Store) aggregateSourceDay(ctx context.Context, start, end time.Time) (int, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := start.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if !start.Equal(day) || !end.After(start) || end.After(day.AddDate(0, 0, 1)) {
		return 0, fmt.Errorf("one bounded Beijing source day required")
	}
	changed := 0
	epoch, epochErr := s.analyticsEpoch(ctx)
	if epochErr != nil {
		return 0, epochErr
	}
	err := s.Session.TxContext(ctx, func(tx db.Session) error {
		cursorKey := Key("source-day-cursor", day.Format("2006-01-02"))
		if _, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'initializing',?) ON DUPLICATE KEY UPDATE record_key=record_key`, cursorKey, start.UTC()); err != nil {
			return err
		}
		cursor, err := tx.SQL().QueryRowContext(ctx, `SELECT interval_end FROM northbound_owner_records WHERE record_type='source_day_cursor' AND record_key=? FOR UPDATE`, cursorKey)
		if err != nil {
			return err
		}
		var publishedUntil time.Time
		if err = cursor.Scan(&publishedUntil); err != nil {
			return err
		}
		if publishedUntil.After(end) {
			return nil
		}
		rows, err := tx.SQL().QueryContext(ctx, `SELECT COALESCE(b.scope_id,i.user_id),COALESCE(b.owner_normalized,LOWER(TRIM(i.owner))),COALESCE(b.runtime_type,i.type,''),m.instance_id,TIMESTAMPADD(SECOND,FLOOR(TIMESTAMPDIFF(SECOND,'1970-01-01',m.created_at)/600)*600,'1970-01-01'),SUM(m.prompt_tokens),SUM(m.completion_tokens),SUM(m.total_tokens),COUNT(*)`+sourceIdentityJoin+` GROUP BY 1,2,3,4,5`, start.UTC(), end.UTC())
		if err != nil {
			return err
		}
		items := []sourceUsage{}
		for rows.Next() {
			var u sourceUsage
			var bucket string
			if err = rows.Scan(&u.scope, &u.owner, &u.runtime, &u.instance, &bucket, &u.prompt, &u.completion, &u.tokens, &u.calls); err != nil {
				rows.Close()
				return err
			}
			if u.at, err = time.ParseInLocation("2006-01-02 15:04:05.999999", bucket, time.UTC); err != nil {
				rows.Close()
				return err
			}
			items = append(items, u)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Keep absolute zeroes when an earlier terminal source row is corrected to
		// a nonterminal state. Retain the aggregate record; do not delete history.
		present := map[string]bool{}
		groupKey := func(u sourceUsage) string { return u.key() }
		for _, u := range items {
			present[groupKey(u)] = true
		}
		old := map[string]sourceUsage{}
		previousRows, e := tx.SQL().QueryContext(ctx, `SELECT scope_id,owner_normalized,COALESCE(runtime_type,''),instance_id,occurred_at,prompt_tokens,completion_tokens,total_tokens,invocation_count FROM northbound_owner_records WHERE record_type='source_usage' AND business_date=?`, day.Format("2006-01-02"))
		if e != nil {
			return e
		}
		for previousRows.Next() {
			var u sourceUsage
			if e = previousRows.Scan(&u.scope, &u.owner, &u.runtime, &u.instance, &u.at, &u.prompt, &u.completion, &u.tokens, &u.calls); e != nil {
				previousRows.Close()
				return e
			}
			old[groupKey(u)] = u
			if !present[groupKey(u)] {
				u.prompt = 0
				u.completion = 0
				u.tokens = 0
				u.calls = 0
				items = append(items, u)
			}
		}
		e = previousRows.Err()
		previousRows.Close()
		if e != nil {
			return e
		}
		owners := map[string]bool{}
		values := []string{}
		arguments := []any{}
		flush := func() error {
			if len(values) == 0 {
				return nil
			}
			_, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,runtime_type,instance_id,status,business_date,occurred_at,interval_start,interval_end,prompt_tokens,completion_tokens,total_tokens,invocation_count,version) VALUES `+strings.Join(values, ",")+` ON DUPLICATE KEY UPDATE prompt_tokens=VALUES(prompt_tokens),completion_tokens=VALUES(completion_tokens),total_tokens=VALUES(total_tokens),invocation_count=VALUES(invocation_count),interval_end=VALUES(interval_end),version=version+1`, arguments...)
			values = nil
			arguments = nil
			return e
		}
		for _, u := range items {
			key := u.key()
			if prior, ok := old[key]; ok && prior.prompt == u.prompt && prior.completion == u.completion && prior.tokens == u.tokens && prior.calls == u.calls {
				continue
			}
			values = append(values, `('source_usage',?,?,?,?,?,'ready',?,?,?,?,?,?,?,?,1)`)
			arguments = append(arguments, key, u.scope, u.owner, u.runtime, u.instance, day.Format("2006-01-02"), u.at.UTC(), start.UTC(), end.UTC(), u.prompt, u.completion, u.tokens, u.calls)
			if len(values) >= 100 {
				if e = flush(); e != nil {
					return e
				}
			}
			owners[u.owner] = true
			changed++
		}
		if e = flush(); e != nil {
			return e
		}
		// A single stable identity per successful session, not one row per call.
		// Earliest observation wins across days, so totals are never SUM(DISTINCT/day).
		rows, err = tx.SQL().QueryContext(ctx, `SELECT COALESCE(b.scope_id,i.user_id),COALESCE(b.owner_normalized,LOWER(TRIM(i.owner))),m.instance_id,m.session_id,MIN(m.created_at)`+sourceIdentityJoin+` AND m.status='completed' AND COALESCE(m.session_id,'')<>'' GROUP BY 1,2,3,4`, start.UTC(), end.UTC())
		if err != nil {
			return err
		}
		type session struct {
			scope, instance uint64
			owner, id       string
			at              time.Time
		}
		sessions := []session{}
		for rows.Next() {
			var v session
			if err = rows.Scan(&v.scope, &v.owner, &v.instance, &v.id, &v.at); err != nil {
				rows.Close()
				return err
			}
			sessions = append(sessions, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		sessionKeys := []string{}
		valid := sessions[:0]
		for _, v := range sessions {
			if strings.HasPrefix(v.id, "sess_trc_") || strings.HasPrefix(v.id, "trc_") {
				continue
			}
			valid = append(valid, v)
			key := Key("source-session", strconv.FormatUint(v.instance, 10), v.id)
			sessionKeys = append(sessionKeys, key, Key("session-presence", key, day.Format("2006-01-02")))
		}
		known := map[string]time.Time{}
		for begin := 0; begin < len(sessionKeys); begin += 500 {
			last := begin + 500
			if last > len(sessionKeys) {
				last = len(sessionKeys)
			}
			bind := []any{}
			for _, key := range sessionKeys[begin:last] {
				bind = append(bind, key)
			}
			rows, e := tx.SQL().QueryContext(ctx, `SELECT record_key,occurred_at FROM northbound_owner_records WHERE record_type IN ('source_session','session_presence') AND record_key IN (`+strings.TrimRight(strings.Repeat("?,", len(bind)), ",")+`)`, bind...)
			if e != nil {
				return e
			}
			for rows.Next() {
				var key string
				var at time.Time
				if e = rows.Scan(&key, &at); e != nil {
					rows.Close()
					return e
				}
				known[key] = at
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		sessionValues := []string{}
		sessionArgs := []any{}
		oldDates := map[string]struct {
			owner string
			at    time.Time
		}{}
		flushSessions := func() error {
			if len(sessionValues) == 0 {
				return nil
			}
			_, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,session_key,status,occurred_at,version) VALUES `+strings.Join(sessionValues, ",")+` ON DUPLICATE KEY UPDATE occurred_at=LEAST(occurred_at,VALUES(occurred_at)),version=version+1`, sessionArgs...)
			sessionValues = nil
			sessionArgs = nil
			return e
		}
		for _, v := range valid {
			key := Key("source-session", strconv.FormatUint(v.instance, 10), v.id)
			presence := Key("session-presence", key, day.Format("2006-01-02"))
			if _, ok := known[presence]; !ok {
				sessionValues = append(sessionValues, `(?,?,?,?,?,?,'ready',?,1)`)
				sessionArgs = append(sessionArgs, "session_presence", presence, v.scope, v.owner, v.instance, v.id, start.UTC())
				owners[v.owner] = true
			}
			previous, ok := known[key]
			if !ok || v.at.Before(previous) {
				sessionValues = append(sessionValues, `(?,?,?,?,?,?,'ready',?,1)`)
				sessionArgs = append(sessionArgs, "source_session", key, v.scope, v.owner, v.instance, v.id, v.at.UTC())
				owners[v.owner] = true
				changed++
				if ok {
					oldDates[Key(v.owner, previous.In(loc).Format("2006-01-02"))] = struct {
						owner string
						at    time.Time
					}{v.owner, previous}
				}
			}
			if len(sessionValues) >= 100 {
				if e = flushSessions(); e != nil {
					return e
				}
			}
		}
		if e = flushSessions(); e != nil {
			return e
		}
		for _, v := range oldDates {
			at := v.at
			if epoch != nil && at.Before(epoch.Start) {
				at = epoch.Start
			}
			if e = markUsageDay(ctx, tx, v.owner, at); e != nil {
				return e
			}
		}
		markAt := start
		if epoch != nil && markAt.Before(epoch.Start) {
			markAt = epoch.Start
		}
		pendingValues := []string{}
		pendingArgs := []any{}
		flushPending := func() error {
			if len(pendingValues) == 0 {
				return nil
			}
			_, e := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,business_date,status,version,occurred_at) VALUES `+strings.Join(pendingValues, ",")+` ON DUPLICATE KEY UPDATE version=version+1,status='pending'`, pendingArgs...)
			pendingValues = nil
			pendingArgs = nil
			return e
		}
		for owner := range owners {
			date := markAt.In(loc).Format("2006-01-02")
			pendingValues = append(pendingValues, `('usage_dirty',?,0,?,?,'pending',1,?)`)
			pendingArgs = append(pendingArgs, Key("usage-dirty", owner, date), owner, date, markAt.UTC())
			if len(pendingValues) >= 100 {
				if err = flushPending(); err != nil {
					return err
				}
			}
		}
		if err = flushPending(); err != nil {
			return err
		}
		if publishedUntil.Before(end) {
			_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET interval_end=?,status='ready' WHERE record_type='source_day_cursor' AND record_key=?`, end.UTC(), cursorKey)
			if err != nil {
				return err
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	return changed, err
}
