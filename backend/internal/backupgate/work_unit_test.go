package backupgate

import (
	"context"
	"errors"
	"testing"
)

func TestWorkUnitDisabledAndDeniedAdmissionDoNotRunAWriter(t *testing.T) {
	restore := ConfigureWorkBoundary(nil)
	release, err := HoldWorkUnit(context.Background(), "fixture.writer")
	if err != nil {
		t.Fatal(err)
	}
	release()
	restore()
	called := false
	restore = ConfigureWorkBoundary(func(context.Context) (func(), error) { called = true; return nil, ErrMaintenance })
	defer restore()
	if release, err := HoldWorkUnit(context.Background(), "fixture.writer"); !errors.Is(err, ErrMaintenance) || release != nil || !called {
		t.Fatal("denied work admitted", err)
	}
}

func TestWorkUnitReleasesExactlyOnceAfterTheRealWriterReturns(t *testing.T) {
	count := 0
	restore := ConfigureWorkBoundary(func(context.Context) (func(), error) { return func() { count++ }, nil })
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	release, err := HoldWorkUnit(ctx, "fixture.file_write")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if count != 0 {
		t.Fatal("context cancellation claimed the writer had stopped")
	}
	release()
	release()
	if count != 1 {
		t.Fatal("release count", count)
	}
}
