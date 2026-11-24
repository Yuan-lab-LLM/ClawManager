package records

import (
	"context"
	"sync"
	"time"
)

// CaptureTimingStage is a closed set of diagnostic labels. No SQL, source
// identity, table/column name, credential, error text or business value is stored.
type CaptureTimingStage uint8

const (
	CaptureGuard CaptureTimingStage = iota
	CaptureKeyCheck
	CaptureSourceAuthorize
	CaptureSQLQuery
	CaptureSQLNext
	CaptureRowScan
	CaptureConnection
	CaptureSessionBefore
	CaptureSessionAfter
	CaptureBeginRR
	CaptureCatalogInitial
	CaptureExactCounts
	CaptureCatalogLocked
	CaptureCommitRR
	CaptureDomain
	CaptureSnapshotVerify
	CaptureEncodeCompress
	CaptureDEKUse
	CaptureCipherStaging
	CaptureEnvelopeStaging
	CaptureWorkerTotal
	captureTimingStageCount
)

var captureTimingLabels = [...]string{
	"guard", "key_check", "source_authorize", "sql_query", "sql_next", "row_scan",
	"connection", "session_before", "session_after", "begin_rr", "catalog_initial",
	"exact_counts", "catalog_locked", "commit_rr", "domain_capture", "snapshot_verify",
	"encode_compress", "dek_use", "cipher_staging", "envelope_staging", "worker_total",
}

type CaptureTimingSample struct {
	Stage      string `json:"stage"`
	Calls      uint64 `json:"calls"`
	Failures   uint64 `json:"failures"`
	TotalNanos int64  `json:"total_ns"`
	MaxNanos   int64  `json:"max_ns"`
}

// CaptureTimings contains bounded, process-local DATA only. Parent durations
// include their children: guard includes key/source checks, domain_capture
// includes SQL/guards, and worker_total includes all phases. Never sum parents
// and children or use these measurements as authorization, a lease or evidence
// of capture/publication completion.
type CaptureTimings struct {
	mu      sync.Mutex
	samples [captureTimingStageCount]CaptureTimingSample
}

type captureTimingsKey struct{}

// WithCaptureTimings preserves the original context's values, cancellation and
// deadline. It neither creates a capability nor caches any authorization.
func WithCaptureTimings(ctx context.Context, timings *CaptureTimings) context.Context {
	if ctx == nil || timings == nil {
		return ctx
	}
	return context.WithValue(ctx, captureTimingsKey{}, timings)
}

// BeginCaptureTiming only measures the caller's existing synchronous operation.
// Finish the returned span once, with that operation's original error. No
// background reporter, callback supplied by an external caller or per-row log
// is involved. A context without a recorder is the normal untimed path.
func BeginCaptureTiming(ctx context.Context, stage CaptureTimingStage) CaptureTimingSpan {
	if ctx == nil || stage >= captureTimingStageCount {
		return CaptureTimingSpan{}
	}
	timings, _ := ctx.Value(captureTimingsKey{}).(*CaptureTimings)
	if timings == nil {
		return CaptureTimingSpan{}
	}
	return CaptureTimingSpan{timings, stage, time.Now()}
}

// A value span avoids allocating a closure at every SQL/authorization boundary.
type CaptureTimingSpan struct {
	timings *CaptureTimings
	stage   CaptureTimingStage
	started time.Time
}

func (s CaptureTimingSpan) Finish(err error) {
	if s.timings != nil {
		s.timings.observe(s.stage, time.Since(s.started), err != nil)
	}
}

func (t *CaptureTimings) observe(stage CaptureTimingStage, elapsed time.Duration, failed bool) {
	if t == nil || stage >= captureTimingStageCount || elapsed < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	v := &t.samples[stage]
	v.Calls++
	if failed {
		v.Failures++
	}
	v.TotalNanos += elapsed.Nanoseconds()
	v.MaxNanos = max(v.MaxNanos, elapsed.Nanoseconds())
}

// Snapshot returns a detached, deterministic list of fixed labels with at least
// one completed measurement. It contains no pointers back to the recorder.
func (t *CaptureTimings) Snapshot() []CaptureTimingSample {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []CaptureTimingSample
	for stage, value := range t.samples {
		if value.Calls != 0 {
			value.Stage = captureTimingLabels[stage]
			out = append(out, value)
		}
	}
	return out
}
