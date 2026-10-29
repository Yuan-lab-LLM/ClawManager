package services

import (
	"context"
	"log"
	"time"
)

// StartRuntimeSlotReconciliation polls Kubernetes desired state. Concurrent
// backend replicas can retry the same immutable assignment safely.
func StartRuntimeSlotReconciliation(ctx context.Context, client RuntimeAgentClient) {
	reconciler, ok := client.(interface{ ReconcileAssignments(context.Context) error })
	if !ok {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := reconciler.ReconcileAssignments(attemptCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				log.Printf("Lite assignment reconciliation requires retry: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
