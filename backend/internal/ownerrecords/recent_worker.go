package ownerrecords

import (
	"context"
	"errors"
	"log"
	"time"
)

// Stable excerpts survive quiet days; bounded schedules prioritize active users.
func (s *Store) scanRecentQuestions(ctx context.Context, after uint64, fingerprints map[uint64]string) (uint64, error) {
	if s.HistoryRoot == "" {
		return 0, nil
	}
	startedBatch, processed := time.Now(), 0
	items, selectionErr := s.scheduledScans(ctx, "recent")
	recordSyncSelected(ctx, "recent", len(items))
	defer func() {
		s.observeScanCost("recent", processed, startedBatch)
		log.Printf("owner scan batch lane=recent selected=%d processed=%d elapsed_ms=%d budget_exhausted=%t", len(items), processed, time.Since(startedBatch).Milliseconds(), !scanHasBudget(ctx))
	}()
	if ctx.Err() != nil {
		return after, ctx.Err()
	}
	if len(items) == 0 {
		return after, selectionErr
	}
	var err error
	for _, item := range items {
		work, cancel, admitted := scanItemContext(ctx)
		if !admitted {
			return after, errors.Join(selectionErr, errSyncYield)
		}
		started := time.Now()
		var captureErr error
		work, captureErr = s.captureScanRuntime(work, &item)
		if captureErr != nil {
			cancel()
			if e := s.finishScanCheckpoint(ctx, "recent", item, started, captureErr); e != nil {
				return after, e
			}
			processed++
			continue
		}
		cacheGeneration := scanReadGeneration(item)
		previous := ""
		if value := fingerprints[item.identity.Instance]; len(value) > len(cacheGeneration) && value[len(value)-len(cacheGeneration):] == cacheGeneration {
			previous = value[:len(value)-len(cacheGeneration)]
		}
		var cutoff time.Time
		var read historyReadResponse
		epoch, e := s.analyticsEpoch(work)
		err = e
		if epoch != nil {
			cutoff = epoch.Start
		}
		if e == nil && item.identity.Runtime == "openclaw" {
			// Reuse only history already acknowledged by the full fact worker.
			// Recent excerpts must not acknowledge previously unseen archives.
			work, _, e = s.openClawColdProgress(work, item)
			err = e
		}
		if e == nil && item.identity.Runtime == "deepseek-harness" {
			work, _, e = s.dshProgress(work, item)
			err = e
		}
		if e == nil {
			read, err = s.inspectNative(work, item, true, cutoff, previous)
		}
		item.unchanged = err == nil && read.Unchanged
		if !read.Unchanged && (err == nil || usablePartial(err)) {
			err = s.saveRecentQuestions(work, item.identity, item.workspace, time.Now().UTC(), read.Questions, err)
		}
		if err == nil && read.Fingerprint != "" {
			if len(fingerprints) >= 4096 {
				clear(fingerprints)
			}
			fingerprints[item.identity.Instance] = read.Fingerprint + cacheGeneration
		}
		err = s.checkScanAbsence(work, &item, err)
		cancel()
		if e := s.finishScanCheckpoint(ctx, "recent", item, started, err); e != nil {
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
