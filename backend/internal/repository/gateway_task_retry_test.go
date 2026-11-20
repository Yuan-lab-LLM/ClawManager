package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
)

func TestGatewayTaskWriteRetriesOnlyRolledBackDeadlocks(t *testing.T) {
	deadlock := &drivermysql.MySQLError{Number: 1213, Message: "deadlock"}
	lockTimeout := &drivermysql.MySQLError{Number: 1205}
	for _, tt := range []struct {
		name      string
		failure   error
		failures  int
		wantCalls int
		wantError error
	}{
		{"success", nil, 0, 1, nil},
		{"deadlock then success", deadlock, 2, 3, nil},
		{"wrapped deadlock", fmt.Errorf("transaction: %w", deadlock), 1, 2, nil},
		{"bounded deadlocks", deadlock, 4, 3, deadlock},
		{"denial", models.ErrModelUnavailable, 4, 1, models.ErrModelUnavailable},
		{"lock timeout", lockTimeout, 4, 1, lockTimeout},
		{"cancellation", context.Canceled, 4, 1, context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			err := retryGatewayTaskWrite(t.Context(), func() error {
				calls++
				if calls <= tt.failures {
					return tt.failure
				}
				return nil
			})
			if calls != tt.wantCalls || !errors.Is(err, tt.wantError) {
				t.Fatalf("calls=%d error=%v; want calls=%d error=%v", calls, err, tt.wantCalls, tt.wantError)
			}
		})
	}
}

func TestGatewayTaskWriteCancellationStopsRetries(t *testing.T) {
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(t.Context())
		if before {
			cancel()
		}
		calls := 0
		err := retryGatewayTaskWrite(ctx, func() error {
			calls++
			cancel()
			return &drivermysql.MySQLError{Number: 1213}
		})
		cancel()
		wantCalls := 1
		if before {
			wantCalls = 0
		}
		if !errors.Is(err, context.Canceled) || calls != wantCalls {
			t.Fatalf("cancel before=%v calls=%d error=%v", before, calls, err)
		}
	}
}
