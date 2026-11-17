package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"

	"clawreef/internal/historymaintenance"
	"github.com/upper/db/v4"
)

var ErrHistoryPaginationRequired = errors.New("history exceeds synchronous detail limits; use the bounded history archive cursor/export API")
var historyReaders sync.Map // keyed by the actual SQL pool, never shared across databases

type monthlySQL struct{ db.SQL }

func (q monthlySQL) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.SQL.QueryContext(ctx, query, args...)
}
func (q monthlySQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.SQL.ExecContext(ctx, query, args...)
}
func ConfigureHistoryReader(sess db.Session, reader *historymaintenance.Service) {
	if pool, ok := sess.Driver().(*sql.DB); ok {
		if reader == nil {
			historyReaders.Delete(pool)
		} else {
			historyReaders.Store(pool, reader)
		}
	}
}
func historyReader(sess db.Session) *historymaintenance.Service {
	if pool, ok := sess.Driver().(*sql.DB); ok {
		if value, found := historyReaders.Load(pool); found {
			return value.(*historymaintenance.Service)
		}
	}
	if pool, ok := sess.Driver().(*sql.DB); ok {
		return historymaintenance.New(pool, nil, nil)
	}
	return nil
}
func archivedByID[T any](sess db.Session, kind string, id int) (*T, error) {
	reader := historyReader(sess)
	if reader == nil {
		return nil, nil
	}
	record, err := reader.Get(context.Background(), kind, int64(id), 0, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var item T
	if err = json.Unmarshal(record.Payload, &item); err != nil {
		return nil, err
	}
	return &item, nil
}
func monthlyByID[T any](ctx context.Context, sess db.Session, kind string, id int) (*T, bool, error) {
	reader := historyReader(sess)
	if reader == nil {
		return nil, false, nil
	}
	record, handled, err := reader.MonthlyGet(ctx, kind, int64(id), 0, true)
	if err != nil || !handled {
		return nil, handled, err
	}
	var item T
	if err = json.Unmarshal(record.Payload, &item); err != nil {
		return nil, true, err
	}
	if err = hydrateObservation(ctx, &item); err != nil {
		return nil, true, err
	}
	return &item, true, nil
}
func routedLogDetails[T any](sess db.Session, kind, trace, session string, owner, limit int, recent bool) ([]T, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	reader := historyReader(sess)
	if reader == nil {
		return nil, errors.New("SQL history reader unavailable")
	}
	records, err := reader.LogDetails(context.Background(), kind, trace, session, int64(owner), limit, recent)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(records))
	size := 0
	for _, record := range records {
		var item T
		if err = json.Unmarshal(record.Payload, &item); err != nil {
			return nil, err
		}
		if err = hydrateObservation(context.Background(), &item); err != nil {
			return nil, err
		}
		raw, e := json.Marshal(item)
		if e != nil {
			return nil, e
		}
		size += len(raw)
		if size > historymaintenance.MaxPageBytes {
			return nil, ErrHistoryPaginationRequired
		}
		out = append(out, item)
	}
	return out, nil
}
func routedRecentLogs[T any](sess db.Session, kind string, owner, limit int) ([]T, error) {
	return routedLogDetails[T](sess, kind, "", "", owner, limit, true)
}
