package ownerrecords

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"log"
	"os"
	"time"
)

type syncContextKey struct{}
type SyncStage struct {
	Name          string               `json:"name"`
	State         string               `json:"state"`
	Reason        string               `json:"reason,omitempty"`
	Elapsed       int64                `json:"elapsed_ms"`
	Selected      int                  `json:"selected,omitempty"`
	Processed     int                  `json:"processed,omitempty"`
	Failed        int                  `json:"failed,omitempty"`
	SelectedKnown bool                 `json:"selected_known,omitempty"`
	Selection     []SyncSelectionGroup `json:"selection,omitempty"`
}
type SyncSelectionGroup struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
	Elapsed  int64  `json:"elapsed_ms"`
	Selected int    `json:"selected"`
}
type SyncEvent struct {
	Lane     string   `json:"lane"`
	Identity Identity `json:"-"`
	State    string   `json:"state"`
	Reason   string   `json:"reason,omitempty"`
	Elapsed  int64    `json:"elapsed_ms"`
}
type syncRun struct {
	Current         string      `json:"current_stage,omitempty"`
	Worker          string      `json:"worker,omitempty"`
	Progress        time.Time   `json:"last_progress_at,omitzero"`
	Started         time.Time   `json:"started_at"`
	Finished        time.Time   `json:"finished_at,omitzero"`
	Stages          []SyncStage `json:"stages"`
	Omitted         int         `json:"omitted_events"`
	Events          []SyncEvent `json:"-"`
	counts          map[string]*SyncStage
	diagnosticSpent time.Duration
}

func newSyncRun(ctx context.Context, now time.Time) (context.Context, *syncRun) {
	worker, _ := os.Hostname()
	r := &syncRun{Started: now.UTC(), Worker: worker, Stages: []SyncStage{}, counts: map[string]*SyncStage{}}
	return context.WithValue(ctx, syncContextKey{}, r), r
}
func syncReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errSyncYield):
		return "budget_yield"
	case errors.Is(err, errReaderBusy):
		return "reader_busy"
	case errors.Is(err, errScanChanged):
		return "instance_changed"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "budget"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case errors.Is(err, errHistoryRestricted):
		return "restricted"
	case errors.Is(err, errHistoryCorrupt):
		return "invalid_format"
	case errors.Is(err, errIncompleteTail):
		return "appending"
	default:
		var databaseError *mysqlDriver.MySQLError
		if errors.As(err, &databaseError) && databaseError.Number >= 1000 && databaseError.Number <= 9999 {
			return fmt.Sprintf("mysql_%d", databaseError.Number)
		}
		return "read_or_store"
	}
}
func (r *syncRun) stage(name string, started time.Time, err error) {
	p := SyncStage{Name: name, State: "success", Reason: syncReason(err), Elapsed: time.Since(started).Milliseconds()}
	if c := r.counts[name]; c != nil {
		p.Selected = c.Selected
		p.Processed = c.Processed
		p.Failed = c.Failed
		p.SelectedKnown = c.SelectedKnown
		p.Selection = c.Selection
	}
	if err != nil {
		p.State = "failed"
		if errors.Is(err, errSyncYield) {
			p.State = "partial"
		}
		if p.SelectedKnown && p.Processed > 0 {
			p.State = "partial"
		}
		var selection *scanSelectionError
		if errors.As(err, &selection) && selection.partial {
			p.State = "partial"
		}
	} else if p.Failed > 0 || p.Selected > p.Processed {
		p.State = "partial"
	}
	r.Stages = append(r.Stages, p)
}
func recordSyncSelected(ctx context.Context, lane string, n int) {
	if r, ok := ctx.Value(syncContextKey{}).(*syncRun); ok {
		if r.counts[lane] == nil {
			r.counts[lane] = &SyncStage{}
		}
		r.counts[lane].Selected += n
		r.counts[lane].SelectedKnown = true
	}
}
func recordSyncSelection(ctx context.Context, lane string, groups []SyncSelectionGroup) {
	if r, ok := ctx.Value(syncContextKey{}).(*syncRun); ok {
		if r.counts[lane] == nil {
			r.counts[lane] = &SyncStage{}
		}
		r.counts[lane].Selection = groups
	}
}
func recordSyncItem(ctx context.Context, lane string, i Identity, state, reason string, started time.Time, event bool) {
	r, ok := ctx.Value(syncContextKey{}).(*syncRun)
	if !ok {
		return
	}
	if r.counts[lane] == nil {
		r.counts[lane] = &SyncStage{}
	}
	r.counts[lane].Processed++
	if state == "retry" || state == "restricted" {
		r.counts[lane].Failed++
	}
	if !event {
		return
	}
	if len(r.Events) >= 50 {
		r.Omitted++
		return
	}
	r.Events = append(r.Events, SyncEvent{lane, i, state, reason, time.Since(started).Milliseconds()})
}

// Diagnostic writes are deliberately outside accounting transactions and phase
// deadlines. No goroutines or catch-up queue. A shared finite allowance reserves
// the last write even when intermediate checkpoints encounter a slow database.
func (s *Store) saveSyncRun(parent context.Context, r *syncRun) {
	s.checkpointSyncRun(parent, r, true)
}

func (s *Store) checkpointSyncRun(parent context.Context, r *syncRun, final bool) {
	r.Progress = time.Now().UTC()
	if final {
		r.Finished = r.Progress
		r.Current = ""
	}
	if parent.Err() != nil {
		return
	}
	remaining := diagnosticWriteAllowance(r.diagnosticSpent, final)
	if remaining <= 0 {
		return
	}
	started := time.Now()
	defer func() { r.diagnosticSpent += time.Since(started) }()
	ctx, cancel := context.WithTimeout(parent, remaining)
	defer cancel()
	raw, _ := json.Marshal(r)
	state := "success"
	for _, p := range r.Stages {
		if p.State != "success" {
			state = "partial"
		}
	}
	if len(r.Stages) == 1 && r.Stages[0].State == "failed" {
		state = "failed"
	}
	if !final {
		state = "updating"
	}
	key := Key("sync-batch", r.Started.Format(time.RFC3339Nano))
	if len(raw) > 16384 {
		log.Print("owner diagnostics omitted: size limit")
		return
	}
	// One atomic insert: a saved batch cannot falsely imply all its events were
	// saved when a second write failed. None of these rows are business facts.
	query := `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,runtime_type,status,occurred_at,interval_end,payload) VALUES ('sync_batch',?,0,NULL,NULL,NULL,?,?,?,?)`
	var finished any
	if final {
		finished = r.Finished
	}
	args := []interface{}{key, state, r.Started, finished, string(raw)}
	if final && len(r.Events) > 0 {
		for n, e := range r.Events {
			query += ",('sync_event',?,?,?,?,?,?,?,NULL,?)"
			body, _ := json.Marshal(e)
			args = append(args, Key(key, fmt.Sprint(n)), e.Identity.Scope, e.Identity.Owner, e.Identity.Instance, e.Identity.Runtime, e.State, r.Finished, string(body))
		}
	}
	query += ` ON DUPLICATE KEY UPDATE status=VALUES(status),interval_end=VALUES(interval_end),payload=VALUES(payload)`
	_, err := s.Session.SQL().ExecContext(ctx, query, args...)
	if err != nil {
		log.Printf("owner diagnostics incomplete: %s", syncReason(err))
		return
	}
	if !final {
		return
	}
	s.retainSyncDiagnostics(ctx, r)
}

func diagnosticWriteAllowance(spent time.Duration, final bool) time.Duration {
	remaining := syncDiagnosticBudget - spent
	if !final {
		remaining -= syncDiagnosticFinalReserve
	}
	if remaining > syncDiagnosticWriteBudget {
		remaining = syncDiagnosticWriteBudget
	}
	return remaining
}

func (s *Store) retainSyncDiagnostics(ctx context.Context, r *syncRun) {
	// Only new diagnostic rows are eligible; expires_at on business rows is NOT a TTL.
	// Bounded retention pass. Never delete accounting facts or current schedules.
	for _, typ := range []string{"sync_batch", "sync_event"} {
		if ctx.Err() != nil {
			break
		}
		_, e := s.Session.SQL().ExecContext(ctx, `DELETE FROM northbound_owner_records WHERE record_type=? AND occurred_at<? ORDER BY id LIMIT 100`, typ, r.Finished.AddDate(0, 0, -30))
		if e != nil {
			log.Printf("owner diagnostics retention deferred: %s", syncReason(e))
			break
		}
		// Fixed-size retention lookup; only the new types. An error or exhausted
		// budget defers maintenance without affecting the published batch.
		var boundary uint64
		row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT id FROM northbound_owner_records WHERE record_type=? ORDER BY id DESC LIMIT 1 OFFSET 20000`, typ)
		if e == nil {
			e = row.Scan(&boundary)
		}
		if e == nil {
			_, e = s.Session.SQL().ExecContext(ctx, `DELETE FROM northbound_owner_records WHERE record_type=? AND id<=? ORDER BY id LIMIT 100`, typ, boundary)
		}
	}
}
