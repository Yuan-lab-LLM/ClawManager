package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"testing"
	"time"
)

type usageTestStore struct {
	items     []models.Instance
	published []int
	sizes     []int64
	cursors   []int
}

func (s *usageTestStore) Candidates(ctx context.Context, after, limit int) ([]models.Instance, error) {
	s.cursors = append(s.cursors, after)
	out := []models.Instance{}
	for _, i := range s.items {
		if i.ID > after && len(out) < limit {
			out = append(out, i)
		}
	}
	return out, nil
}
func (s *usageTestStore) Publish(ctx context.Context, i models.Instance, n int64, at time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.published = append(s.published, i.ID)
	s.sizes = append(s.sizes, n)
	return true, nil
}

func TestWorkspaceUsageCollectorFailureFairnessAndCancellation(t *testing.T) {
	path := "/workspaces/openclaw/user-3/instance-7"
	old := time.Now().Add(-time.Hour)
	store := &usageTestStore{items: []models.Instance{{ID: 1, WorkspacePath: &path, WorkspaceUsageBytes: 99, WorkspaceUsageObservedAt: &old}, {ID: 2, WorkspacePath: &path}, {ID: 3, WorkspacePath: &path}}}
	c := &WorkspaceUsageCollector{store: store, measure: func(ctx context.Context, s WorkspaceFileScope) (int64, error) {
		if s.InstanceID == 1 {
			return 0, errors.New("denied")
		}
		return 0, nil
	}, passLimit: 2, passTimeout: time.Second}
	c.collect(t.Context())
	if len(store.published) != 1 || store.published[0] != 2 || c.cursor != 2 {
		t.Fatal("failure must retain old observation and advance fairly", store.published, c.cursor)
	}
	c.collect(t.Context())
	if len(store.published) != 2 || store.published[1] != 3 {
		t.Fatal("later instance starved", store.published)
	}
	c.collect(t.Context())
	if c.cursor != 0 {
		t.Fatal("keyset did not wrap")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := len(store.cursors)
	c.collect(ctx)
	if len(store.cursors) != before {
		t.Fatal("cancelled pass queried storage")
	}
}

func TestWorkspaceUsageCollectorRunStartsImmediatelyAndStops(t *testing.T) {
	path := "/workspaces/openclaw/user-3/instance-7"
	store := &usageTestStore{items: []models.Instance{{ID: 7, WorkspacePath: &path}}}
	sampled := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &WorkspaceUsageCollector{store: store, measure: func(context.Context, WorkspaceFileScope) (int64, error) { sampled <- struct{}{}; return 7, nil }, passLimit: 100, passTimeout: time.Second}
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	select {
	case <-sampled:
	case <-time.After(time.Second):
		t.Fatal("no startup collection")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker ignored lifecycle cancellation")
	}
}

func TestWorkspaceUsageCollectorDeadlineContinuesNextPass(t *testing.T) {
	path := "/workspaces/openclaw/user-3/instance-7"
	store := &usageTestStore{items: []models.Instance{{ID: 1, WorkspacePath: &path}, {ID: 2, WorkspacePath: &path}}}
	c := &WorkspaceUsageCollector{store: store, measure: func(ctx context.Context, s WorkspaceFileScope) (int64, error) {
		if s.InstanceID == 1 {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return 1, nil
	}, passLimit: 100, passTimeout: 10 * time.Millisecond}
	c.collect(t.Context())
	if c.cursor != 1 || len(store.published) != 0 {
		t.Fatal("deadline did not bound/advance pass", c.cursor, store.published)
	}
	c.collect(t.Context())
	if len(store.published) != 1 || store.published[0] != 2 {
		t.Fatal("timed-out tree starved later instance", store.published)
	}
}
