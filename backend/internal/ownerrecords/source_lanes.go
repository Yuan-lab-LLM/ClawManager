package ownerrecords

import (
	"context"
	"errors"
	"time"
)

type sourceLane struct {
	budget time.Duration
	run    func(context.Context) error
}

// Independent finite slices reserve capacity for fresh data and older repairs.
// A lane reaching its allowance yields; actual SQL failures remain failures.
// Parent cancellation alone prevents the remaining lanes from running.
func runSourceLanes(ctx context.Context, lanes []sourceLane) error {
	var failures []error
	yield := false
	for _, lane := range lanes {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		work, cancel := context.WithTimeout(ctx, lane.budget)
		err := lane.run(work)
		if work.Err() != nil && (err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			err = errSyncYield
		}
		cancel()
		if err != nil {
			if errors.Is(err, errSyncYield) {
				yield = true
			} else {
				failures = append(failures, err)
			}
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	if yield {
		return errSyncYield
	}
	return ctx.Err()
}
