package ownerrecords

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// The schedule, not an in-memory ID cursor, owns retries across leader changes.
func (s *Store) ScanHistories(ctx context.Context, after uint64) (uint64, error) {
	if s.HistoryRoot == "" {
		return 0, nil
	}
	startedBatch, processed := time.Now(), 0
	items, selectionErr := s.scheduledScans(ctx, "history")
	recordSyncSelected(ctx, "history", len(items))
	defer func() {
		s.observeScanCost("history", processed, startedBatch)
		log.Printf("owner scan batch lane=history selected=%d processed=%d elapsed_ms=%d budget_exhausted=%t", len(items), processed, time.Since(startedBatch).Milliseconds(), !scanHasBudget(ctx))
	}()
	if ctx.Err() != nil {
		return after, ctx.Err()
	}
	if len(items) == 0 {
		return after, selectionErr
	}
	for _, item := range items {
		work, cancel, admitted := scanItemContext(ctx)
		if !admitted {
			return after, errors.Join(selectionErr, errSyncYield)
		}
		started := time.Now()
		var err error
		work, err = s.captureScanRuntime(work, &item)
		if err == nil {
			err = s.scanHistoryItem(work, item)
		}
		err = s.checkScanAbsence(work, &item, err)
		cancel()
		if e := s.finishScanCheckpoint(ctx, "history", item, started, err); e != nil {
			return after, e
		}
		after = item.identity.Instance
		processed++
		if errors.Is(err, errReaderBusy) {
			return after, errors.Join(selectionErr, errReaderBusy)
		}
	}
	return after, selectionErr
}

func (s *Store) scanHistoryItem(ctx context.Context, item scanItem) error {
	ctx = context.WithValue(ctx, dshGenerationKey{}, item.generation)
	i := item.identity
	epoch, err := s.analyticsEpoch(ctx)
	if err != nil {
		return err
	}
	key := Key("history_scan", strconv.FormatUint(i.Instance, 10))
	before := ""
	{
		row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.fingerprint')),'') FROM northbound_owner_records WHERE record_type='history_scan' AND record_key=? AND status='ready' AND COALESCE(JSON_EXTRACT(payload,'$.runtime_generation'),0)=?`, key, item.runtimeGeneration)
		var fingerprint string
		if e == nil && row.Scan(&fingerprint) == nil {
			before = fingerprint
		}
	}
	identity, err := s.Bind(ctx, i.Instance)
	if err != nil {
		return err
	}
	if *identity != i {
		return fmt.Errorf("history identity changed; original binding retained")
	}
	var progress *dshProgress
	var openClawProgress *openClawColdProgress
	if i.Runtime == "openclaw" {
		ctx, openClawProgress, err = s.openClawColdProgress(ctx, item)
		if err != nil {
			return err
		}
	}
	if i.Runtime == "deepseek-harness" {
		ctx, progress, err = s.dshProgress(ctx, item)
		if err != nil {
			return err
		}
	}
	var cutoff time.Time
	if epoch != nil {
		cutoff = epoch.Start
	}
	read, err := s.inspectNative(ctx, item, false, cutoff, before)
	if err == nil && read.Unchanged {
		return nil
	}
	if progress != nil {
		progress.confirmed = read.Confirmed
	}
	facts := read.Questions
	readErr := err
	if err != nil && !usablePartial(err) {
		return err
	}
	if err = s.applyHistoryFacts(ctx, *identity, facts, epoch); err != nil {
		return err
	}
	if err = s.saveDSHProgress(ctx, item, progress); err != nil {
		return err
	}
	if readErr == nil {
		if err = s.saveOpenClawColdProgress(ctx, item, openClawProgress); err != nil {
			return err
		}
	}
	if readErr != nil {
		return readErr
	}
	_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner,owner_normalized,instance_id,runtime_type,status,occurred_at,payload,source_key) SELECT 'history_scan',?,i.user_id,i.owner,LOWER(TRIM(i.owner)),i.id,i.type,'ready',?,JSON_OBJECT('rule','native-v1','fingerprint',?,'reason','','runtime_generation',?),? FROM instances i WHERE i.id=? AND i.user_id=? AND LOWER(TRIM(i.owner))=? AND SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)=?`+scanRuntimeClause(ctx, "i")+` ON DUPLICATE KEY UPDATE status='ready',occurred_at=VALUES(occurred_at),payload=VALUES(payload),source_key=VALUES(source_key)`, key, time.Now().UTC(), read.Fingerprint, item.runtimeGeneration, item.generation, i.Instance, i.Scope, i.Owner, item.generation)
	return err
}

// A large file may require several bounded passes. Bulk-prefetching immutable
// dedupe keys lets the next pass skip committed prefixes cheaply, rather than
// spending its entire budget doing one SELECT per old question forever.
func (s *Store) applyHistoryFacts(ctx context.Context, i Identity, facts []NativeQuestion, epoch *analyticsEpoch) error {
	for begin := 0; begin < len(facts); begin += 200 {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := begin + 200
		if end > len(facts) {
			end = len(facts)
		}
		known := map[string]bool{}
		keys := []any{}
		ids := make([]string, end-begin)
		if epoch != nil {
			for n, q := range facts[begin:end] {
				key := Key("question-v4", strconv.FormatUint(i.Instance, 10), q.Session, q.ID)
				if i.Runtime == "openclaw" {
					key = Key("question-v4", strconv.FormatUint(i.Instance, 10), "openclaw", q.ID)
				}
				ids[n] = key
				keys = append(keys, key)
			}
			rows, e := s.Session.SQL().QueryContext(ctx, `SELECT record_key FROM northbound_owner_records WHERE record_type='question_v4' AND record_key IN (`+strings.TrimRight(strings.Repeat("?,", len(keys)), ",")+`)`, keys...)
			if e != nil {
				return e
			}
			for rows.Next() {
				var key string
				if e = rows.Scan(&key); e != nil {
					rows.Close()
					return e
				}
				known[key] = true
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		for n, q := range facts[begin:end] {
			if q.At <= 0 || q.At > float64(time.Now().Add(5*time.Minute).Unix()) {
				return fmt.Errorf("invalid history timestamp")
			}
			if known[ids[n]] {
				continue
			}
			_, e := s.Apply(ctx, i, Fact{Type: "question", Source: "native_" + i.Runtime, SourceID: Key(q.Session, q.ID), SessionID: q.Session, QuestionID: q.ID, At: time.UnixMilli(int64(q.At * 1000)), SubmittedAt: time.UnixMilli(int64(q.Submitted * 1000))})
			if e != nil {
				return e
			}
		}
	}
	return nil
}
