package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"time"
)

type workspaceUsageStore interface {
	Candidates(context.Context, int, int) ([]models.Instance, error)
	Publish(context.Context, models.Instance, int64, time.Time) (bool, error)
}

type WorkspaceUsageCollector struct {
	store       workspaceUsageStore
	measure     func(context.Context, WorkspaceFileScope) (int64, error)
	cursor      int
	passLimit   int
	passTimeout time.Duration
}

func NewWorkspaceUsageCollector(store *repository.WorkspaceUsageRepository, files WorkspaceFileService) *WorkspaceUsageCollector {
	s, ok := files.(*workspaceFileService)
	if !ok {
		return nil
	}
	return &WorkspaceUsageCollector{store: store, measure: s.MeasureWorkspaceUsage, passLimit: 100, passTimeout: 30 * time.Second}
}

// Run belongs to the existing leader callback context. One sequential sampler
// runs immediately and every minute; successful trees are sampled at most once
// per five minutes. Keyset progression survives individual scan failures.
func (c *WorkspaceUsageCollector) Run(ctx context.Context) {
	if c == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		c.collect(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *WorkspaceUsageCollector) collect(parent context.Context) {
	if parent.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, c.passTimeout)
	defer cancel()
	items, err := c.store.Candidates(ctx, c.cursor, c.passLimit)
	if err != nil {
		return
	}
	if len(items) == 0 {
		c.cursor = 0
		return
	}
	for _, i := range items {
		if ctx.Err() != nil {
			return
		}
		c.cursor = i.ID
		if i.WorkspaceUsageObservedAt != nil && time.Since(*i.WorkspaceUsageObservedAt) < 5*time.Minute {
			continue
		}
		if i.WorkspacePath == nil {
			continue
		}
		started := time.Now().UTC()
		n, err := c.measure(ctx, WorkspaceFileScope{InstanceID: i.ID, UserID: i.UserID, RuntimeGeneration: i.RuntimeGeneration, WorkspacePath: *i.WorkspacePath})
		if err != nil {
			continue
		}
		// The timestamp marks scan start. An older, overlapping replica cannot
		// overwrite a later observation, even if it finishes last.
		_, _ = c.store.Publish(ctx, i, n, started)
	}
}
