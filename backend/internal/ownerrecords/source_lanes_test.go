package ownerrecords

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSourceLaneTimeoutAndFailureCannotStarveSiblings(t *testing.T) {
	for _, failure := range []error{nil, errors.New("synthetic SQL error")} {
		visited := false
		err := runSourceLanes(context.Background(), []sourceLane{
			{budget: time.Millisecond, run: func(ctx context.Context) error {
				if failure != nil {
					return failure
				}
				<-ctx.Done()
				return ctx.Err()
			}},
			{budget: time.Second, run: func(ctx context.Context) error { visited = ctx.Err() == nil; return errSyncYield }},
		})
		if !visited || err == nil {
			t.Fatal("one lane blocked its sibling or hid pending work", visited, err)
		}
		if failure != nil && (!errors.Is(err, failure) || errors.Is(err, errSyncYield)) {
			t.Fatal("normal yield disguised an actual SQL failure", err)
		}
		if failure == nil && !errors.Is(err, errSyncYield) {
			t.Fatal("a slice deadline should yield rather than cancel siblings", err)
		}
	}
}

func TestSourceLaneShutdownStopsSubsequentWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := runSourceLanes(ctx, []sourceLane{
		{budget: time.Second, run: func(context.Context) error { cancel(); return nil }},
		{budget: time.Second, run: func(context.Context) error { t.Fatal("ran after shutdown"); return nil }},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost shutdown", err)
	}
}
