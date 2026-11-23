package runtimeauth

// Request-local, bounded control diagnostics. This follows the personal
// project's cancellation/diagnostic approach without its unrelated controllers.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"
)

type diagnosticKey struct{}
type diagnosticStage struct {
	Name       string          `json:"name"`
	ElapsedMS  int64           `json:"elapsed_ms"`
	ErrorClass string          `json:"error_class,omitempty"`
	Pool       *poolDiagnostic `json:"process_pool,omitempty"`
}
type poolDiagnostic struct {
	InUse          int   `json:"in_use"`
	Idle           int   `json:"idle"`
	MaxOpen        int   `json:"max_open"`
	WaitCountDelta int64 `json:"wait_count_delta"`
	WaitMSDelta    int64 `json:"wait_ms_delta"`
}
type controlDiagnostics struct {
	mu     sync.Mutex
	stages []diagnosticStage
}

func WithDiagnostics(ctx context.Context) context.Context {
	return context.WithValue(ctx, diagnosticKey{}, &controlDiagnostics{})
}
func DiagnosticsEnabled(ctx context.Context) bool {
	_, ok := ctx.Value(diagnosticKey{}).(*controlDiagnostics)
	return ok
}
func ErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if IsUnavailable(err) {
		return "dependency_unavailable"
	}
	return "denied"
}
func RecordStage(ctx context.Context, name string, started time.Time, err error) {
	recordDiagnostic(ctx, diagnosticStage{Name: name, ElapsedMS: time.Since(started).Milliseconds(), ErrorClass: ErrorClass(err)})
}
func recordDiagnostic(ctx context.Context, stage diagnosticStage) {
	d, ok := ctx.Value(diagnosticKey{}).(*controlDiagnostics)
	if !ok {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.stages) < 24 {
		d.stages = append(d.stages, stage)
	}
}

// Pool deltas describe the entire shared process, not this query's exclusive
// wait. Stage elapsed includes acquire, execution and result decoding.
func RecordPoolStage(ctx context.Context, name string, started time.Time, before, after sql.DBStats, err error) {
	recordDiagnostic(ctx, diagnosticStage{Name: name, ElapsedMS: time.Since(started).Milliseconds(), ErrorClass: ErrorClass(err), Pool: &poolDiagnostic{
		InUse: after.InUse, Idle: after.Idle, MaxOpen: after.MaxOpenConnections,
		WaitCountDelta: after.WaitCount - before.WaitCount, WaitMSDelta: (after.WaitDuration - before.WaitDuration).Milliseconds(),
	}})
}
func LogDiagnostics(ctx context.Context, instanceID, generation int, started time.Time, err error) {
	if err == nil && time.Since(started) < 500*time.Millisecond {
		return
	}
	d, ok := ctx.Value(diagnosticKey{}).(*controlDiagnostics)
	if !ok {
		return
	}
	d.mu.Lock()
	stages := append([]diagnosticStage(nil), d.stages...)
	d.mu.Unlock()
	b, _ := json.Marshal(struct {
		InstanceID int               `json:"instance_id"`
		Generation int               `json:"generation"`
		ElapsedMS  int64             `json:"elapsed_ms"`
		ErrorClass string            `json:"error_class"`
		Stages     []diagnosticStage `json:"stages"`
	}{instanceID, generation, time.Since(started).Milliseconds(), ErrorClass(err), stages})
	log.Printf("runtime authority diagnostics: %s", b)
}
