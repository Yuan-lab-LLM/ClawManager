package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type lifecycleLockRepo struct {
	repository.InstanceRepository
	acquire func() (func(), error)
}

func (r lifecycleLockRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	return r.acquire()
}

func TestLifecycleRejectsConflictingOperationsBeforeReadingOrSaving(t *testing.T) {
	s := &instanceService{}
	release, err := s.acquireLifecycleOperation(91234)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// The nil repository would panic if any conflicting operation reached it.
	for name, call := range map[string]func() error{
		"start":       func() error { return s.Start(91234) },
		"stop":        func() error { return s.Stop(91234) },
		"restart":     func() error { return s.Restart(91234) },
		"restart-env": func() error { return s.RestartWithEnvironment(91234, map[string]string{"TEST": "value"}, nil) },
		"delete":      func() error { return s.Delete(91234) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, models.ErrInstanceOperationInProgress) {
				t.Fatalf("got %v, want operation conflict", err)
			}
		})
	}
	other, err := s.acquireLifecycleOperation(91235)
	if err != nil {
		t.Fatalf("unrelated instance blocked: %v", err)
	}
	other()
}

func TestLifecycleReleasesLocalGuardAfterDatabaseConflict(t *testing.T) {
	calls := 0
	s := &instanceService{instanceRepo: lifecycleLockRepo{acquire: func() (func(), error) {
		calls++
		return nil, models.ErrInstanceOperationInProgress
	}}}
	for n := 0; n < 2; n++ {
		if _, err := s.acquireLifecycleOperation(91236); !errors.Is(err, models.ErrInstanceOperationInProgress) {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("local guard was not released after the database conflict")
	}
}

func TestLifecycleHoldsDatabaseGuardUntilOperationCompletes(t *testing.T) {
	released := false
	s := &instanceService{instanceRepo: lifecycleLockRepo{acquire: func() (func(), error) {
		return func() { released = true }, nil
	}}}
	release, err := s.acquireLifecycleOperation(91237)
	if err != nil {
		t.Fatal(err)
	}
	if released {
		t.Fatal("database guard released early")
	}
	release()
	if !released {
		t.Fatal("database guard not released")
	}
	next, err := s.acquireLifecycleOperation(91237)
	if err != nil {
		t.Fatal(err)
	}
	next()
}
