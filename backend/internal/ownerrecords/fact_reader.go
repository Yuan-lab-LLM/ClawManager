package ownerrecords

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Source selection is shared by every projection. Legacy facts are retained for
// rollback, but never added to the replacement rollups.
func (s *Store) factSQL(ctx context.Context, query string) (string, error) {
	epoch, err := s.analyticsEpoch(ctx)
	if err != nil || epoch == nil {
		return query, err
	}
	query = strings.NewReplacer("'invocation'", "'source_usage'", "'session'", "'source_session'", "'question'", "'question_v4'").Replace(query)
	query = strings.Replace(query, "SELECT record_type,", "SELECT CASE record_type WHEN 'source_usage' THEN 'invocation' WHEN 'source_session' THEN 'session' WHEN 'question_v4' THEN 'question' ELSE record_type END,", 1)
	return query, nil
}
func (s *Store) factRows(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, err := s.factSQL(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.Session.SQL().QueryContext(ctx, query, args...)
}
func (s *Store) factRow(ctx context.Context, query string, args ...any) (*sql.Row, error) {
	query, err := s.factSQL(ctx, query)
	if err != nil {
		return nil, err
	}
	return s.Session.SQL().QueryRowContext(ctx, query, args...)
}
func (s *Store) analyticsEpoch(ctx context.Context) (*analyticsEpoch, error) {
	if s.Session == nil {
		return nil, nil
	}
	s.perf.epochMu.Lock()
	defer s.perf.epochMu.Unlock()
	if s.perf.epoch != nil {
		return s.perf.epoch, nil
	}
	if time.Since(s.perf.epochChecked) < time.Second*10 {
		return nil, nil
	}
	epoch, err := s.readAnalyticsEpoch(ctx)
	if err == nil {
		s.perf.epoch = epoch
		s.perf.epochChecked = time.Now()
	}
	return epoch, err
}
