package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"
)

func (s *Store) queueReportPreviews(ctx context.Context, owner string, now time.Time) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	for _, kind := range []string{"week", "month"} {
		start, _, err := reportWindow(kind, now.In(loc).Format("2006-01-02"), now)
		if err != nil {
			return err
		}
		_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,period_type,period_start,version,expires_at) VALUES('report_preview_job',?,0,?,'pending',?,?,1,?) ON DUPLICATE KEY UPDATE status='pending',version=version+1`, Key("report-preview-job", owner, kind, start.Format("2006-01-02")), owner, kind, start.Format("2006-01-02"), now.UTC())
		if err != nil {
			return err
		}
	}
	return nil
}

// Alternating lanes prevent current previews and closed reports starving each other.
func (s *Store) processReports(ctx context.Context, now time.Time) error {
	work, cancel := context.WithTimeout(ctx, syncReportBudget)
	defer cancel()
	if _, ok := work.Value(syncContextKey{}).(*syncRun); !ok {
		work, _ = newSyncRun(work, now)
	}
	first := (now.Unix()/600)%2 == 0
	var failures []error
	for batch := 0; batch < 256; batch++ {
		if !scanHasBudget(work) {
			return errors.Join(append(failures, errSyncYield)...)
		}
		r := work.Value(syncContextKey{}).(*syncRun)
		before := 0
		if p := r.counts["reports"]; p != nil {
			before = p.Selected
		}
		for _, preview := range []bool{first, !first} {
			if e := s.runReportJobs(work, now, preview); e != nil {
				failures = append(failures, e)
			}
		}
		after := 0
		if p := r.counts["reports"]; p != nil {
			after = p.Selected
		}
		if after == before {
			return errors.Join(failures...)
		}
		first = !first
	}
	return errors.Join(append(failures, errSyncYield)...)
}

func (s *Store) runReportJobs(ctx context.Context, now time.Time, preview bool) error {
	started, completed := time.Now(), 0
	defer func() {
		log.Printf("owner report phase preview=%t completed=%d elapsed_ms=%d budget_exhausted=%t", preview, completed, time.Since(started).Milliseconds(), ctx.Err() != nil)
	}()
	typ, limit := "report_dirty", 5
	if preview {
		typ, limit = "report_preview_job", 2
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 300*time.Millisecond {
		return nil
	}
	rows, err := s.Session.SQL().QueryContext(ctx, `SELECT record_key,owner_normalized,period_type,period_start,version,COALESCE(JSON_EXTRACT(payload,'$.failures'),0) FROM northbound_owner_records WHERE record_type=? AND status='pending' AND expires_at<=? AND (JSON_EXTRACT(payload,'$.retry_at') IS NULL OR JSON_EXTRACT(payload,'$.retry_at')<=?) ORDER BY expires_at,id LIMIT ?`, typ, now.UTC(), now.Unix(), limit)
	if err != nil {
		return err
	}
	type job struct {
		key, owner, kind string
		date             time.Time
		version          uint64
		failures         int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.key, &j.owner, &j.kind, &j.date, &j.version, &j.failures); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	recordSyncSelected(ctx, "reports", len(jobs))
	for _, j := range jobs {
		deadline, ok := ctx.Deadline()
		if ok && time.Until(deadline) < 300*time.Millisecond {
			break
		}
		budget := syncOwnerBudget
		if ok && time.Until(deadline)-150*time.Millisecond < budget {
			budget = time.Until(deadline) - 150*time.Millisecond
		}
		work, cancel := context.WithTimeout(ctx, budget)
		jobStarted := time.Now()
		_, end, e := reportWindow(j.kind, j.date.Format("2006-01-02"), now)
		obsolete := e != nil || preview && !now.Before(end)
		if !obsolete {
			var r *Report
			r, e = s.Report(work, j.owner, j.kind, j.date.Format("2006-01-02"), now, !preview)
			if e == nil && preview {
				var raw []byte
				raw, e = json.Marshal(r)
				if e == nil {
					// Publish only the selected source version; late data remains queued.
					_, e = s.Session.SQL().ExecContext(work, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,occurred_at,payload,version) SELECT 'report_preview',?,0,?,'ready',?,?,? FROM northbound_owner_records WHERE record_type=? AND record_key=? AND version=? ON DUPLICATE KEY UPDATE payload=VALUES(payload),occurred_at=VALUES(occurred_at),version=VALUES(version)`, Key("report-preview", j.owner, j.kind, j.date.Format("2006-01-02")), j.owner, now.UTC(), string(raw), j.version, typ, j.key, j.version)
				}
			}
		}
		cancel()
		jobErr := e
		if obsolete || e == nil {
			completed++
			status := "done"
			if obsolete {
				status = "outside_report_range"
			}
			_, e = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status=?,payload=JSON_OBJECT() WHERE record_type=? AND record_key=? AND version=?`, status, typ, j.key, j.version)
		} else {
			failures = append(failures, e)
			n := j.failures + 1
			shift := n - 1
			if shift > 4 {
				shift = 4
			}
			retry := now.Add(time.Duration(1<<shift) * summaryInterval)
			reason := "report_read_or_store"
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
				reason = "report_budget"
			}
			_, e = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET payload=JSON_OBJECT('failures',?,'retry_at',?,'reason',?) WHERE record_type=? AND record_key=? AND version=?`, n, retry.Unix(), reason, typ, j.key, j.version)
		}
		if e != nil {
			failures = append(failures, e)
		}
		if e != nil {
			jobErr = e
		}
		jobState := "ready"
		if jobErr != nil {
			jobState = "retry"
		}
		recordSyncItem(ctx, "reports", Identity{Owner: j.owner}, jobState, syncReason(jobErr), jobStarted, (jobErr != nil && j.failures == 0) || (jobErr == nil && j.failures > 0))
	}
	return errors.Join(failures...)
}

func (s *Store) reportPreview(ctx context.Context, owner, kind, date string, now time.Time) (*Report, error) {
	start, end, err := reportWindow(kind, date, now)
	if err != nil {
		return nil, err
	}
	if !now.Before(end) {
		return nil, sql.ErrNoRows
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='report_preview' AND record_key=?`, Key("report-preview", owner, kind, start.Format("2006-01-02")))
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = row.Scan(&raw); err != nil {
		return nil, err
	}
	var r Report
	err = json.Unmarshal(raw, &r)
	return &r, err
}
