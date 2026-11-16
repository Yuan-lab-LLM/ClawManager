package ownerrecords

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const scanSelectionGroupBudget = 10 * time.Second

var errIncompleteTail = errors.New("history append in progress")
var errHistoryRestricted = errors.New("history format or size outside supported bounds")

// Schedule metadata is separate from successful evidence. Failed reads never
// overwrite the last usable excerpt or confirm an accounting cursor.
type scanItem struct {
	identity          Identity
	workspace         string
	failures          uint64
	generation        string
	previousState     string
	previousReason    string
	unchanged         bool
	absenceChecked    bool
	emptyHistory      bool
	runtimeGeneration uint64
}

// A group failure is operational metadata, not a failure of an instance whose
// history has never been opened. Keep a successful sibling group usable.
type scanSelectionError struct {
	err     error
	partial bool
}

func (e *scanSelectionError) Error() string { return "scan selection incomplete" }
func (e *scanSelectionError) Unwrap() error { return e.err }

type scanSelection struct {
	items  []scanItem
	groups []SyncSelectionGroup
	err    error
}

func collectScanGroups(ctx context.Context, read func(context.Context, bool, int) ([]scanItem, error)) scanSelection {
	return collectScanGroupsBounded(ctx, 16, 4, false, read)
}

func collectScanGroupsBounded(ctx context.Context, activeLimit, quietLimit int, borrow bool, read func(context.Context, bool, int) ([]scanItem, error)) scanSelection {
	result := scanSelection{}
	var failures []error
	activeCount, succeeded := 0, 0
	for _, group := range []struct {
		name   string
		active bool
		limit  int
	}{{"active", true, activeLimit}, {"quiet", false, quietLimit}} {
		if borrow && !group.active {
			group.limit += activeLimit - activeCount
		}
		started := time.Now()
		meta := SyncSelectionGroup{Name: group.name, State: "success"}
		if err := ctx.Err(); err != nil {
			meta.State, meta.Reason = "skipped", "budget"
			result.groups = append(result.groups, meta)
			failures = append(failures, err)
			continue
		}
		queryCtx, cancel := context.WithTimeout(ctx, scanSelectionGroupBudget)
		items, err := read(queryCtx, group.active, group.limit)
		if queryCtx.Err() != nil {
			err = errors.Join(err, queryCtx.Err())
		}
		cancel()
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 3024 {
			err = errors.Join(err, context.DeadlineExceeded)
		}
		if err == nil && len(items) > group.limit {
			err = errors.New("scan selection group exceeds limit")
		}
		meta.Elapsed = time.Since(started).Milliseconds()
		if err != nil {
			meta.State, meta.Reason = "failed", syncReason(err)
			failures = append(failures, err)
		} else {
			succeeded++
			meta.Selected = len(items)
			result.items = append(result.items, items...)
			if group.active {
				activeCount = len(items)
			}
		}
		result.groups = append(result.groups, meta)
	}
	if len(failures) > 0 {
		result.err = &scanSelectionError{err: errors.Join(failures...), partial: succeeded > 0 && ctx.Err() == nil}
	}
	// Retain the existing fair rotation; a failed group supplies no candidates.
	if time.Now().Unix()/600%2 == 0 && activeCount < len(result.items) {
		result.items = append(append([]scanItem{}, result.items[activeCount:]...), result.items[:activeCount]...)
	}
	return result
}

func scanHasBudget(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	d, ok := ctx.Deadline()
	return !ok || time.Until(d) > 500*time.Millisecond
}

// Do not turn queueing near the stage deadline into a failed instance read.
// A full read plus acknowledgement must fit; a slow instance gets its own cap.
func scanItemContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if ctx.Err() != nil {
		return nil, func() {}, false
	}
	budget := nativeItemBudget
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline) - nativeCheckpointBudget
		if remaining < nativeReaderWaitBudget+nativeCheckpointBudget {
			return nil, func() {}, false
		}
		if remaining < budget {
			budget = remaining
		}
	}
	work, cancel := context.WithTimeout(ctx, budget)
	return work, cancel, true
}

func scanRetryDelay(failures uint64) time.Duration {
	if failures > 5 {
		failures = 5
	}
	if failures == 0 {
		return summaryInterval
	}
	return summaryInterval * time.Duration(1<<(failures-1))
}

func scanReadRetryDelay(failures uint64, err error) time.Duration {
	delay := scanRetryDelay(failures)
	if (errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err)) && delay > 3*summaryInterval {
		delay = 3 * summaryInterval
	}
	return delay
}

func scanErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errReaderBusy):
		return "reader_busy"
	case errors.Is(err, errScanChanged):
		return "instance_changed"
	case errors.Is(err, errIncompleteTail):
		return "appending"
	case errors.Is(err, errHistoryRestricted):
		return "restricted"
	case errors.Is(err, errHistoryCorrupt):
		return "invalid_format"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "budget"
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	default:
		return fmt.Sprintf("read_or_store:%T", err)
	}
}

// Global activity has a fixed day range. Empty-history wakeup has a correlated
// timestamp: use the covering instance prefix, not a repeatedly scanned day index.
func scheduledScanQuery(isActive bool) string {
	active := `EXISTS(SELECT 1 FROM model_invocations m FORCE INDEX(idx_model_invocations_created_at) WHERE m.instance_id=i.id AND m.created_at>=UTC_TIMESTAMP()-INTERVAL 1 DAY AND m.created_at<=UTC_TIMESTAMP())`
	if !isActive {
		active = "NOT " + active
	}
	return fmt.Sprintf(`SELECT /*+ MAX_EXECUTION_TIME(%d) */ i.id,COALESCE(i.workspace_path,''),i.user_id,LOWER(TRIM(i.owner)),i.type,IF(q.source_key=SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256) AND q.owner_normalized=LOWER(TRIM(i.owner)) AND q.scope_id=i.user_id,COALESCE(q.version,0),0),SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)
 ,COALESCE(q.status,''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(q.payload,'$.reason')),'') FROM instances i LEFT JOIN northbound_owner_records q FORCE INDEX(idx_owner_record_binding) ON q.record_type=? AND q.instance_id=i.id
WHERE i.owner IS NOT NULL AND TRIM(i.owner)<>'' AND LOWER(TRIM(i.owner)) NOT IN ('factory-reset-staging@clawmanager.local','admin@clawmanager.local')
AND i.instance_mode='lite' AND i.status='running' AND i.type IN ('openclaw','hermes','opencode','deepseek-harness')
AND (q.expires_at IS NULL OR q.expires_at<=UTC_TIMESTAMP()
 OR (q.status='retry' AND JSON_UNQUOTE(JSON_EXTRACT(q.payload,'$.reason'))='budget' AND q.occurred_at<=UTC_TIMESTAMP()-INTERVAL 30 MINUTE)
 OR q.owner_normalized<>LOWER(TRIM(i.owner)) OR q.scope_id<>i.user_id
 OR COALESCE(q.source_key,'')<>SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)
 OR (q.status IN ('empty','ready') AND EXISTS(SELECT 1 FROM model_invocations m FORCE INDEX(idx_model_invocations_instance_session) WHERE m.instance_id=i.id AND m.created_at>q.occurred_at AND m.created_at<=UTC_TIMESTAMP()))) AND `+active+`
ORDER BY CASE WHEN q.status='retry' AND JSON_UNQUOTE(JSON_EXTRACT(q.payload,'$.reason'))='budget' THEN LEAST(q.expires_at,q.occurred_at+INTERVAL 30 MINUTE) ELSE COALESCE(q.expires_at,'1970-01-01') END,i.id LIMIT ?`, scanSelectionGroupBudget.Milliseconds())
}

// Use at most 16 active and 4 quiet slots, with oldest due first in each lane.
// This does not scan workspace files or introduce writes in model requests.
func (s *Store) scheduledScans(ctx context.Context, lane string) ([]scanItem, error) {
	s.perf.scanCostMu.Lock()
	cost := s.perf.scanCosts[lane]
	s.perf.scanCostMu.Unlock()
	activeLimit, quietLimit := 16, 4
	if cost > 0 {
		total := int((nativeScanBudget * 8 / 10) / cost)
		if total < 2 {
			total = 2
		}
		if total > 128 {
			total = 128
		}
		quietLimit = total / 5
		if quietLimit < 1 {
			quietLimit = 1
		}
		activeLimit = total - quietLimit
	}
	selection := collectScanGroupsBounded(ctx, activeLimit, quietLimit, cost > 0, func(queryCtx context.Context, isActive bool, limit int) ([]scanItem, error) {
		items := []scanItem{}
		rows, err := s.Session.SQL().QueryContext(queryCtx, scheduledScanQuery(isActive), lane+"_schedule", limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var item scanItem
			err = rows.Scan(&item.identity.Instance, &item.workspace, &item.identity.Scope, &item.identity.Owner, &item.identity.Runtime, &item.failures, &item.generation, &item.previousState, &item.previousReason)
			if err != nil {
				return nil, err
			}
			item.identity.Mode = "lite"
			items = append(items, item)
		}
		err = rows.Err()
		if err != nil {
			return nil, err
		}
		return items, nil
	})
	recordSyncSelection(ctx, lane, selection.groups)
	for _, group := range selection.groups {
		if group.State != "success" {
			log.Printf("owner scan selection lane=%s group=%s state=%s elapsed_ms=%d reason=%s", lane, group.Name, group.State, group.Elapsed, group.Reason)
		}
	}
	return selection.items, selection.err
}

// Absence probing is filesystem work, not a two-second SQL checkpoint. Workers
// perform it before cancelling their instance context, within the same 45s cap.
func (s *Store) checkScanAbsence(ctx context.Context, item *scanItem, readErr error) error {
	if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	empty, err := s.noHistoryYet(ctx, *item)
	if err != nil {
		return err
	}
	item.absenceChecked, item.emptyHistory = true, empty
	return readErr
}

func (s *Store) finishScanCheckpoint(ctx context.Context, lane string, item scanItem, started time.Time, readErr error) error {
	checkpoint, cancel := context.WithTimeout(ctx, nativeCheckpointBudget)
	defer cancel()
	return s.finishScan(checkpoint, lane, item, started, readErr)
}

func (s *Store) finishScan(ctx context.Context, lane string, item scanItem, started time.Time, readErr error) error {
	failures := uint64(0)
	state := "ready"
	if item.absenceChecked && item.emptyHistory && errors.Is(readErr, os.ErrNotExist) {
		readErr, state = nil, "empty"
	}
	if errors.Is(readErr, os.ErrNotExist) && !item.absenceChecked {
		empty, err := s.noHistoryYet(ctx, item)
		if err != nil {
			return err
		}
		if empty {
			readErr = nil
			state = "empty"
		}
	}
	if readErr != nil {
		failures = item.failures + 1
		if failures > 5 {
			failures = 5
		}
		state = "retry"
	}
	if errors.Is(readErr, errIncompleteTail) {
		state, failures = "pending", 0
	}
	if errors.Is(readErr, errReaderBusy) || errors.Is(readErr, errScanChanged) {
		state, failures = "pending", 0
	}
	if errors.Is(readErr, errHistoryRestricted) {
		state = "restricted"
	}
	now := time.Now()
	fenceCtx := context.WithValue(ctx, scanRuntimeKey{}, item.runtimeGeneration)
	delay := scanReadRetryDelay(failures, readErr)
	if state == "empty" {
		delay = 3 * summaryInterval
	}
	if state == "ready" && item.unchanged {
		delay = 3 * summaryInterval
	}
	if state == "restricted" {
		delay = 6 * summaryInterval
	}
	_, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status,version,occurred_at,expires_at,payload,source_key,interval_start)
SELECT ?,?,?,?,i.id,?,?,?,?,JSON_OBJECT('reason',?,'runtime_generation',?),?,? FROM instances i WHERE i.id=? AND i.user_id=? AND LOWER(TRIM(i.owner))=? AND SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)=?`+scanRuntimeClause(fenceCtx, "i")+` ON DUPLICATE KEY UPDATE
interval_start=IF(northbound_owner_records.status=VALUES(status) AND source_key=VALUES(source_key) AND northbound_owner_records.owner_normalized=VALUES(owner_normalized) AND northbound_owner_records.scope_id=VALUES(scope_id),COALESCE(interval_start,VALUES(interval_start)),VALUES(interval_start)),
scope_id=VALUES(scope_id),owner_normalized=VALUES(owner_normalized),status=VALUES(status),version=VALUES(version),occurred_at=VALUES(occurred_at),expires_at=VALUES(expires_at),payload=VALUES(payload),source_key=VALUES(source_key)`, lane+"_schedule", Key(lane+"-schedule", strconv.FormatUint(item.identity.Instance, 10)), item.identity.Scope, item.identity.Owner, state, failures, now.UTC(), nextSummaryBatch(now).Add(delay-summaryInterval).UTC(), scanErrorClass(readErr), item.runtimeGeneration, item.generation, now.UTC(), item.identity.Instance, item.identity.Scope, item.identity.Owner, item.generation)
	if readErr != nil {
		log.Printf("owner scan deferred lane=%s instance=%d stage=read_apply elapsed_ms=%d reason=%s", lane, item.identity.Instance, time.Since(started).Milliseconds(), scanErrorClass(readErr))
	}
	if err == nil {
		reason := safeSyncReason(scanErrorClass(readErr))
		changed := state != item.previousState || scanErrorClass(readErr) != item.previousReason || (state == "retry" && item.failures == 0)
		recordSyncItem(ctx, lane, item.identity, state, reason, started, changed && (readErr != nil || item.failures > 0))
	}
	return err
}

func (s *Store) observeScanCost(lane string, processed int, started time.Time) {
	if processed == 0 {
		return
	}
	cost := time.Since(started) / time.Duration(processed)
	if cost < 20*time.Millisecond {
		cost = 20 * time.Millisecond
	}
	s.perf.scanCostMu.Lock()
	defer s.perf.scanCostMu.Unlock()
	if s.perf.scanCosts == nil {
		s.perf.scanCosts = map[string]time.Duration{}
	}
	if old := s.perf.scanCosts[lane]; old > 0 {
		cost = (old*3 + cost) / 4
	}
	s.perf.scanCosts[lane] = cost
}

// Absence is benign only for the expected source, in an accessible workspace,
// with no prior successful evidence. Never turn mount/permission errors into zero.
func (s *Store) noHistoryYet(ctx context.Context, item scanItem) (bool, error) {
	i := item.identity
	expected := filepath.Join(s.HistoryRoot, i.Runtime, fmt.Sprintf("user-%d", i.Scope), fmt.Sprintf("instance-%d", i.Instance))
	if !filepath.IsAbs(s.HistoryRoot) || filepath.Clean(item.workspace) != expected {
		return false, nil
	}
	probe, err := s.isolatedHistory(ctx, historyReadRequest{Operation: "empty", Root: s.HistoryRoot, Workspace: item.workspace, Identity: i})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !probe.Empty {
		return false, nil
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM northbound_owner_records WHERE instance_id=? AND scope_id=? AND owner_normalized=? AND ((record_type='history_scan' AND status='ready') OR (record_type='recent_question' AND status='ready') OR record_type IN ('question','question_v4')))`, i.Instance, i.Scope, i.Owner)
	if err != nil {
		return false, err
	}
	var evidence bool
	if err = row.Scan(&evidence); err != nil {
		return false, err
	}
	return !evidence, nil
}
