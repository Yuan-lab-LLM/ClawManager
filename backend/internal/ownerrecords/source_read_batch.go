package ownerrecords

import (
	"context"
	"github.com/upper/db/v4"
	"time"
)

type sourceReadBatchKey struct{}
type sourceDayRead struct {
	revision uint64
	buckets  map[int64][]sourceUsage
	count    int
	disabled bool
}
type sourceReadBatch struct {
	days            map[string]*sourceDayRead
	retained, loads int
}

// Numeric aggregate rows only, scoped to one serial phase. Production has no
// (record_type,business_date,occurred_at) index, so do not rescan all legacy
// aggregates for every ten-minute slice. No new physical index/table required.
func withSourceReadBatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, sourceReadBatchKey{}, &sourceReadBatch{days: map[string]*sourceDayRead{}})
}

func readSourceRows(ctx context.Context, tx db.Session, day, start, end time.Time, full bool) ([]sourceUsage, error) {
	q := `SELECT scope_id,owner_normalized,COALESCE(runtime_type,''),instance_id,occurred_at,prompt_tokens,completion_tokens,total_tokens,invocation_count FROM northbound_owner_records WHERE record_type='source_usage' AND business_date=?`
	args := []any{day.Format("2006-01-02")}
	if full {
		q += ` LIMIT 50001`
	} else {
		q += ` AND occurred_at>=? AND occurred_at<?`
		args = append(args, start.UTC(), end.UTC())
	}
	rows, e := tx.SQL().QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []sourceUsage
	for rows.Next() {
		var u sourceUsage
		if e = rows.Scan(&u.scope, &u.owner, &u.runtime, &u.instance, &u.at, &u.prompt, &u.completion, &u.tokens, &u.calls); e != nil {
			return nil, e
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func previousSourceRows(ctx context.Context, tx db.Session, day, start, end time.Time, window bool, revision uint64) ([]sourceUsage, *sourceDayRead, error) {
	batch, _ := ctx.Value(sourceReadBatchKey{}).(*sourceReadBatch)
	if !window || batch == nil {
		rows, e := readSourceRows(ctx, tx, day, start, end, false)
		return rows, nil, e
	}
	key := day.Format("2006-01-02")
	cached := batch.days[key]
	if cached != nil && cached.disabled {
		rows, e := readSourceRows(ctx, tx, day, start, end, false)
		return rows, nil, e
	}
	if cached == nil || cached.revision != revision {
		rows, e := readSourceRows(ctx, tx, day, start, end, true)
		if e != nil {
			return nil, nil, e
		}
		batch.loads++
		if len(rows) > 50000 {
			batch.days[key] = &sourceDayRead{disabled: true}
			rows, e = readSourceRows(ctx, tx, day, start, end, false)
			return rows, nil, e
		}
		if batch.retained+len(rows) > 50000 {
			clear(batch.days)
			batch.retained = 0
		} else if cached != nil {
			batch.retained -= cached.count
		}
		cached = &sourceDayRead{revision: revision, buckets: map[int64][]sourceUsage{}, count: len(rows)}
		for _, u := range rows {
			bucket := u.at.UTC().Truncate(summaryInterval).Unix()
			cached.buckets[bucket] = append(cached.buckets[bucket], u)
		}
		batch.days[key] = cached
		batch.retained += len(rows)
	}
	// Never mutate the cached slice before the SQL commit is confirmed.
	return append([]sourceUsage(nil), cached.buckets[start.UTC().Unix()]...), cached, nil
}

func commitSourceRead(ctx context.Context, cached *sourceDayRead, start time.Time, revision uint64, items []sourceUsage) {
	if cached == nil {
		return
	}
	batch, _ := ctx.Value(sourceReadBatchKey{}).(*sourceReadBatch)
	if batch == nil {
		return
	}
	old := len(cached.buckets[start.UTC().Unix()])
	delta := len(items) - old
	if batch.retained+delta > 50000 {
		clear(batch.days)
		batch.retained = 0
		return
	}
	cached.buckets[start.UTC().Unix()] = append([]sourceUsage(nil), items...)
	cached.revision = revision
	cached.count += delta
	batch.retained += delta
}
