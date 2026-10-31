package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

func (s *RuntimeManagementService) SupportedEvictionModes() []string {
	if s != nil && s.scheduler != nil && s.scheduler.deployments != nil && requiresConfirmedGatewayStop(s.scheduler.agentClient) {
		return []string{"graceful", "force"}
	}
	return []string{"graceful"}
}

// Force eviction relinquishes the source only after the existing slot Agent has
// confirmed process/namespace teardown and persisted the writer-stop receipt.
// Agent shutdown already escalates SIGTERM to SIGKILL after its configured grace.
// Capacity is deliberately left to the ordinary durable creating-intent scheduler.
func (s *RuntimeManagementService) evictSource(ctx context.Context, item RuntimeSchedulingItem, ownerID string) error {
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	plan, err := s.preflight(ctx, RuntimeSchedulingRequest{SourcePodID: item.SourcePodID, InstanceIDs: []int{item.InstanceID}}, true)
	if err != nil {
		return err
	}
	if !plan.Allowed {
		return fmt.Errorf("source eviction blocked: %v", plan.Blockers)
	}
	current := plan.Items[0]
	if current.SourceGeneration != item.SourceGeneration || current.SourceGatewayID != item.SourceGatewayID || current.SourcePodUID != item.SourcePodUID || !runtimeEvictionPlacementMatches(current.SourcePlacement, item.SourcePlacement) {
		return errors.New("source identity changed since confirmation; eviction cancelled")
	}
	instance, err := s.scheduler.instanceRepo.GetByID(item.InstanceID)
	if err != nil {
		return err
	}
	binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, item.InstanceID)
	if err != nil {
		return err
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, item.SourcePodID)
	if err != nil {
		return err
	}
	if instance == nil || binding == nil || source == nil || source.AgentEndpoint == nil || stringValue(source.PodUID) != item.SourcePodUID || binding.RuntimePodID != item.SourcePodID || binding.Generation != item.SourceGeneration || binding.GatewayID != item.SourceGatewayID || instance.RuntimeGeneration != item.SourceGeneration {
		return errors.New("source binding changed since preflight")
	}
	return s.stopSource(ctx, item, instance, binding, source, ownerID, true)
}

func (s *RuntimeManagementService) stopSource(ctx context.Context, item RuntimeSchedulingItem, instance *models.Instance, binding *models.InstanceRuntimeBinding, source *models.RuntimePod, ownerID string, queue bool) error {
	sch := s.scheduler
	fence, ok := sch.instanceRepo.(interface {
		FenceRuntimeRestart(context.Context, int, int) (bool, error)
	})
	if !ok {
		return errors.New("Runtime restart generation fence unavailable")
	}
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	fenced, err := fence.FenceRuntimeRestart(ctx, item.InstanceID, instance.RuntimeGeneration)
	if err != nil {
		return err
	}
	if !fenced {
		return errors.New("instance changed during Runtime move; source preserved")
	}
	// The slot client only returns nil after persisted matching stop + unmount evidence.
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	grace := item.StopGraceSeconds
	if grace <= 0 {
		grace = 30
	}
	stopCtx, cancelStop := context.WithTimeout(ctx, time.Duration(grace)*time.Second)
	stopErr := sch.agentClient.DeleteGateway(stopCtx, *source.AgentEndpoint, binding.GatewayID)
	cancelStop()
	if stopErr != nil {
		err = stopErr
		return fmt.Errorf("source stop unconfirmed; target was not started: %w", err)
	}
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	if queue {
		return s.queueEvictedSource(ctx, item, binding, ownerID)
	}
	var deleted bool
	if binding.State == "running" {
		deleted, err = sch.bindingRepo.DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx, item.InstanceID, item.SourcePodID, binding.Generation)
	} else if releaser, ok := sch.bindingRepo.(repository.FailedGatewayBindingReleaser); ok {
		deleted, err = releaser.DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx, item.InstanceID, item.SourcePodID, binding.Generation)
	} else {
		return errors.New("conditional failed binding release unavailable")
	}
	if err != nil {
		return err
	}
	if !deleted {
		return errors.New("source binding changed after stop; move held")
	}

	return nil
}

// Commit the creating intent and source release together. A failed commit keeps
// the old binding/occupancy; a crash cannot leave a released, unschedulable source.
func (s *RuntimeManagementService) queueEvictedSource(ctx context.Context, item RuntimeSchedulingItem, binding *models.InstanceRuntimeBinding, ownerID string) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var owner, status string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status FROM runtime_management_operations WHERE id=? FOR UPDATE", ownerID)
		if err != nil {
			return err
		}
		if err = row.Scan(&owner, &status); err != nil {
			return err
		}
		if owner != s.token || status != "processing" {
			return errors.New("operation execution ownership lost; eviction held")
		}
		now := time.Now().UTC()
		result, err := tx.SQL().ExecContext(ctx, `UPDATE instances SET status='creating',runtime_error_message=NULL,runtime_error_code=NULL,updated_at=? WHERE id=? AND runtime_generation=? AND status='stopped' AND EXISTS (SELECT 1 FROM instance_lifecycle_manual_guards WHERE instance_id=? AND token=?)`, now, item.InstanceID, item.SourceGeneration+1, item.InstanceID, ownerID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("instance intent changed after confirmed stop")
		}
		result, err = tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE instance_id=? AND runtime_pod_id=? AND generation=? AND gateway_id=? AND state IN ('running','error','failed','stopped','unhealthy')`, item.InstanceID, item.SourcePodID, item.SourceGeneration, binding.GatewayID)
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("source binding changed after confirmed stop")
		}
		var sourceUID string
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT pod_uid FROM runtime_pods WHERE id=? FOR UPDATE", item.SourcePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&sourceUID); err != nil {
			return err
		}
		if sourceUID != item.SourcePodUID {
			return errors.New("source Pod identity changed after confirmed stop")
		}
		// A stop heartbeat may already have released this gateway's occupancy.
		// Reconcile the remaining bindings, including pending reservations, rather
		// than decrementing a counter that could now belong to another gateway.
		// This first consistent read must stay outside UPDATE: a DML subquery
		// locks other bindings and can deadlock their binding -> Pod releases.
		var remaining int
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?", item.SourcePodID)
		if err != nil {
			return err
		}
		if err = row.Scan(&remaining); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_pods SET used_slots=?,updated_at=? WHERE id=?`, remaining, now, item.SourcePodID)
		return err
	}, nil)
}
