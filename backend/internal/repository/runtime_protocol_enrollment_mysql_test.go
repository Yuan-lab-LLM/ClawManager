package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestRuntimeCapabilityEnrollmentMySQL(t *testing.T) {
	r, exec := runtimeProtocolMySQLFixture(t)
	report := models.RuntimeCapabilityReport{Protocol: 1, Generation: 3, RuntimeVersion: "native.1", AdapterVersion: "adapter.1", Capabilities: models.RequiredRuntimeCapabilities()}
	get := func() *models.RuntimeProtocolState {
		t.Helper()
		state, err := r.Get(t.Context(), 10)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	wantConflict := func(err error) {
		t.Helper()
		if !errors.Is(err, models.ErrRuntimeCapabilityEnrollmentConflict) {
			t.Fatalf("expected enrollment conflict, got %v", err)
		}
	}
	wrong := strings.Repeat("0", 64)
	wantConflict(r.Enroll(t.Context(), 10, 1, report, &wrong))
	if get() != nil {
		t.Fatal("stale precondition inserted enrollment")
	}
	if err := r.Enroll(t.Context(), 10, 1, report, nil); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_runtime_protocols SET reported_at='2020-01-01 00:00:00' WHERE instance_id=10")
	original := get()
	revision, err := original.EnrollmentRevision()
	if err != nil {
		t.Fatal(err)
	}
	reordered := report
	reordered.Capabilities = []string{"live_catalog", "cancel_receipt", "turn_cancel", "task_identity"}
	for _, expected := range []*string{nil, &revision, &wrong} {
		if err := r.Enroll(t.Context(), 10, 1, reordered, expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, get()) {
			t.Fatal("idempotent enrollment rewrote contract or freshness")
		}
	}
	for _, kind := range []string{"runtime", "adapter", "capabilities"} {
		changed := report
		switch kind {
		case "runtime":
			changed.RuntimeVersion = "native.2"
		case "adapter":
			changed.AdapterVersion = "adapter.2"
		case "capabilities":
			changed.Capabilities = append(append([]string{}, report.Capabilities...), "maintenance_drain")
		}
		wantConflict(r.Enroll(t.Context(), 10, 1, changed, &revision))
		if !reflect.DeepEqual(original, get()) {
			t.Fatal("same-generation contract was overwritten")
		}
	}
	// The current generation and the observed old contract must both match.
	exec("UPDATE instances SET runtime_generation=4 WHERE id=10")
	if err := r.Enroll(t.Context(), 10, 1, report, &revision); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("stale generation: %v", err)
	}
	newReport := report
	newReport.Generation, newReport.AdapterVersion = 4, "adapter.2"
	wantConflict(r.Enroll(t.Context(), 10, 1, newReport, nil))
	wantConflict(r.Enroll(t.Context(), 10, 1, newReport, &wrong))
	// Even legacy negotiation changing the old report invalidates its fingerprint.
	exec("UPDATE gateway_runtime_protocols SET adapter_version='legacy-changed' WHERE instance_id=10")
	wantConflict(r.Enroll(t.Context(), 10, 1, newReport, &revision))
	exec("UPDATE gateway_runtime_protocols SET adapter_version='adapter.1' WHERE instance_id=10")
	if err := r.Enroll(t.Context(), 10, 1, newReport, &revision); err != nil {
		t.Fatal(err)
	}
	if !get().MatchesReport(1, newReport) {
		t.Fatal("approved transition not stored")
	}
	if err := r.Enroll(t.Context(), 10, 1, newReport, &revision); err != nil {
		t.Fatalf("lost-ack retry: %v", err)
	}
	if err := r.Renew(t.Context(), 10, 1, newReport); err != nil {
		t.Fatalf("enrolled report cannot renew: %v", err)
	}

	for _, kind := range []string{"other_owner", "transfer", "disabled", "corrupt", "future_protocol", "future_generation", "missing_instance"} {
		t.Run(kind, func(t *testing.T) {
			exec("UPDATE users SET is_active=TRUE WHERE id=1")
			exec("UPDATE instances SET user_id=1,runtime_generation=4 WHERE id=10")
			exec("UPDATE gateway_runtime_protocols SET owner_user_id=1,generation=3,protocol=1,capabilities_json=?,adapter_version='adapter.1' WHERE instance_id=10", original.CapabilitiesJSON)
			owner, instance := 1, 10
			want := models.ErrRuntimeCapabilityEnrollmentConflict
			switch kind {
			case "other_owner":
				owner = 2
				want = models.ErrModelUnavailable
			case "transfer":
				exec("UPDATE instances SET user_id=2 WHERE id=10")
				owner = 2
			case "disabled":
				exec("UPDATE users SET is_active=FALSE WHERE id=1")
				want = models.ErrModelUnavailable
			case "corrupt":
				exec("UPDATE gateway_runtime_protocols SET capabilities_json='invalid' WHERE instance_id=10")
			case "future_protocol":
				exec("UPDATE gateway_runtime_protocols SET protocol=2 WHERE instance_id=10")
			case "future_generation":
				exec("UPDATE gateway_runtime_protocols SET generation=5 WHERE instance_id=10")
			case "missing_instance":
				instance = 999
				want = models.ErrModelUnavailable
			}
			before := get()
			if err := r.Enroll(t.Context(), instance, owner, newReport, &revision); !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
			if !reflect.DeepEqual(before, get()) {
				t.Fatal("denied enrollment mutated stored state")
			}
		})
	}

	// Two processes that both observed no enrollment must not overwrite each
	// other when they present different contracts for one generation.
	exec("DELETE FROM gateway_runtime_protocols WHERE instance_id=10")
	exec("UPDATE users SET is_active=TRUE WHERE id=1")
	exec("UPDATE instances SET user_id=1,runtime_generation=3 WHERE id=10")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, adapter := range []string{"candidate.1", "candidate.2"} {
		candidate := report
		candidate.AdapterVersion = adapter
		go func() { <-start; results <- r.Enroll(ctx, 10, 1, candidate, nil) }()
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, models.ErrRuntimeCapabilityEnrollmentConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}

	// A restart committing while registration waits must invalidate old identity.
	database, err := sql.Open("mysql", os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var generation int
	if err := tx.QueryRowContext(ctx, "SELECT runtime_generation FROM instances WHERE id=10 FOR UPDATE").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- r.Enroll(ctx, 10, 1, report, nil) }()
	select {
	case err := <-finished:
		t.Fatalf("enrollment ignored instance lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	before := get()
	if _, err := tx.ExecContext(ctx, "UPDATE instances SET runtime_generation=4 WHERE id=10"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("restart race: %v", err)
	}
	if !reflect.DeepEqual(before, get()) {
		t.Fatal("restart race rewrote enrollment")
	}
}
