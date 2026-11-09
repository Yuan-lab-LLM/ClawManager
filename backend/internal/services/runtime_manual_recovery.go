package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Inspection grants no stop/adoption authority. Mutations always reload under
// the same distributed lifecycle lock used by Start/Stop and the scheduler.
type RuntimeRecoveryInspection struct {
	InstanceID  int                             `json:"instance_id"`
	Generation  int                             `json:"generation"`
	Status      string                          `json:"status"`
	BlockerCode string                          `json:"blocker_code,omitempty"`
	Blocker     string                          `json:"blocker,omitempty"`
	Failover    *models.RuntimeFailoverIntent   `json:"failover,omitempty"`
	Recovery    *models.InstanceRuntimeRecovery `json:"recovery,omitempty"`
	Binding     *models.InstanceRuntimeBinding  `json:"binding,omitempty"`
	Pod         *models.RuntimePod              `json:"pod,omitempty"`
}

func (s *RuntimeScheduler) InspectInstanceRecovery(ctx context.Context, id int) (*RuntimeRecoveryInspection, error) {
	if s == nil || s.instanceRepo == nil || s.bindingRepo == nil || id < 1 {
		return nil, fmt.Errorf("runtime recovery is unavailable")
	}
	instance, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if !isSchedulerManagedV2Instance(*instance) {
		return nil, fmt.Errorf("instance does not use the managed Runtime")
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, id)
	if err != nil {
		return nil, err
	}
	info := &RuntimeRecoveryInspection{InstanceID: id, Generation: instance.RuntimeGeneration, Status: instance.Status, Binding: binding}
	if store, ok := s.instanceRepo.(repository.InstanceRuntimeRecoveryRepository); ok {
		info.Recovery, err = store.GetRuntimeRecovery(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if store, ok := s.instanceRepo.(repository.RuntimeFailoverRepository); ok {
		info.Failover, err = store.GetRuntimeFailover(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	podID := int64(0)
	if binding != nil {
		podID = binding.RuntimePodID
	} else if info.Recovery != nil {
		podID = info.Recovery.SourcePodID
	} else if info.Failover != nil {
		podID = info.Failover.SourcePodID
	}
	if podID > 0 && s.podRepo != nil {
		info.Pod, err = s.podRepo.GetByID(ctx, podID)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case instance.Status != "error" && instance.Status != "creating" && instance.Status != "running":
		info.BlockerCode = "stopped"
		info.Blocker = "Instance is stopped, sleeping or deleting; use Start to change its desired power state."
	case binding != nil && binding.Generation != instance.RuntimeGeneration:
		info.BlockerCode = "generation"
		info.Blocker = "Source binding belongs to a different generation; ownership must be reconciled first."
	case info.Failover != nil && info.Failover.Generation < instance.RuntimeGeneration:
		info.BlockerCode = "historical_failover"
		info.Blocker = "Previous-generation failover remains recorded; retry verifies current authority before retirement."
	case info.Failover != nil:
		info.BlockerCode = "failover"
		info.Blocker = "An interrupted failover owns this source. Retry resumes its exact source verification and retained generation."
	case binding != nil && binding.State == RuntimeGatewayBindingRunning:
		info.BlockerCode = "running"
		info.Blocker = "The source is still recorded as running; recovery cannot stop a healthy writer. Recheck its Runtime first."
	case runtimeRecoveryPermanentFailure(*instance, binding):
		info.BlockerCode = "configuration"
		info.Blocker = "Runtime configuration or credentials require correction before recovery."
	case binding == nil && (info.Recovery == nil || info.Recovery.Attempts == 0):
		info.BlockerCode = "missing"
		info.Blocker = "No failed source or durable recovery intent is available. Recheck the Runtime or let the pending start reconcile."
	case info.Recovery != nil && info.Recovery.Attempts >= len(runtimeRecoveryBackoff) && info.Recovery.Phase == "":
		info.BlockerCode = "budget"
		info.Blocker = "Automatic recovery budget exhausted. An administrator can retry after checking the source."
	}
	return info, nil
}

func (s *RuntimeScheduler) RetryInstanceRecovery(ctx context.Context, id, generation, actor int) (*RuntimeRecoveryInspection, error) {
	if s == nil || s.instanceRepo == nil || actor < 1 || id < 1 || generation < 1 {
		return nil, fmt.Errorf("administrator and exact instance generation are required")
	}
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, id)
	if err != nil {
		return nil, err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if instance == nil || !isSchedulerManagedV2Instance(*instance) || instance.RuntimeGeneration != generation || (instance.Status != "creating" && instance.Status != "error" && instance.Status != "running") {
		return nil, fmt.Errorf("instance state or generation changed; inspect again")
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, id)
	if err != nil {
		return nil, err
	}
	if handled, failoverErr := s.RetryOwnedRuntimeFailoverLocked(ctx, *instance); handled {
		log.Printf("admin Runtime failover retry actor=%d instance=%d generation=%d", actor, id, generation)
		info, inspectErr := s.InspectInstanceRecovery(ctx, id)
		return info, errors.Join(failoverErr, inspectErr)
	}
	if binding != nil && (binding.Generation != generation || binding.State == RuntimeGatewayBindingRunning) {
		return nil, fmt.Errorf("source is running or its generation changed; recheck before recovery")
	}
	if runtimeRecoveryPermanentFailure(*instance, binding) {
		return nil, fmt.Errorf("correct Runtime configuration or credentials before retrying")
	}
	store, ok := s.instanceRepo.(repository.InstanceRuntimeRecoveryRepository)
	if !ok {
		return nil, fmt.Errorf("durable Runtime recovery is unavailable")
	}
	state, err := store.GetRuntimeRecovery(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	continuation := state != nil && state.Generation == generation && (state.Phase == "source_stopped" || state.Phase == "placing")
	if !continuation {
		reason, failed := runtimeRecoveryFailureReason(*instance, binding)
		if !failed {
			return nil, fmt.Errorf("no verified failed source is available; pending starts must reconcile without clearing their reservation")
		}
		state = &models.InstanceRuntimeRecovery{InstanceID: id, Generation: generation, Reason: reason, NextRetryAt: &now, UpdatedAt: now}
		if binding != nil {
			state.SourcePodID = binding.RuntimePodID
			state.SourceGatewayPort = binding.GatewayPort
		}
		saved, saveErr := store.SaveRuntimeRecovery(ctx, *instance, *state)
		if saveErr != nil {
			return nil, saveErr
		}
		if !saved {
			return nil, fmt.Errorf("instance desired power or generation changed; inspect again")
		}
	}
	if continuation && state.Phase == "placing" && instance.Status == "error" {
		resume, ok := s.instanceRepo.(interface {
			ResumeRuntimeRecoveryPlacement(context.Context, models.Instance) (bool, error)
		})
		if !ok {
			return nil, fmt.Errorf("durable placement continuation is unavailable")
		}
		resumed, resumeErr := resume.ResumeRuntimeRecoveryPlacement(ctx, *instance)
		if resumeErr != nil {
			return nil, resumeErr
		}
		if !resumed {
			return nil, fmt.Errorf("placement identity or desired state changed; inspect again")
		}
		instance, err = s.instanceRepo.GetByID(id)
		if err != nil {
			return nil, err
		}
		if instance == nil || instance.Status != "creating" || instance.RuntimeGeneration != generation {
			return nil, fmt.Errorf("placement changed; inspect again")
		}
	}
	log.Printf("admin Runtime recovery requested actor=%d instance=%d generation=%d continuation=%t", actor, id, generation, continuation)
	if s.events != nil {
		_ = s.events.Publish(ctx, "runtime.instance.recovery_requested", map[string]any{"actor_user_id": actor, "instance_id": id, "generation": generation, "continuation": continuation})
	}
	_, failures := s.reconcileInstanceRecoveryLocked(ctx, *instance, now)
	info, inspectErr := s.InspectInstanceRecovery(ctx, id)
	return info, errors.Join(append(failures, inspectErr)...)
}

// RecheckRuntimePod performs the same fenced audit used by automatic repair,
// bypassing only its periodic timer. It does not free an unknown writer.
func (s *RuntimeScheduler) RecheckRuntimePod(ctx context.Context, id int64, uid string, actor int) (*models.RuntimePod, error) {
	if s == nil || s.podRepo == nil || actor < 1 || id < 1 || strings.TrimSpace(uid) == "" {
		return nil, fmt.Errorf("administrator and exact Runtime Pod identity are required")
	}
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return nil, fmt.Errorf("Runtime does not support physical slot inspection; repair or upgrade the source before retrying")
	}
	pod, err := s.podRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if pod == nil || stringValue(pod.PodUID) != uid {
		return nil, fmt.Errorf("Runtime Pod identity changed; refresh before checking")
	}
	pass, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	log.Printf("admin Runtime recheck requested actor=%d pod=%d uid=%s", actor, id, uid)
	auditErr := s.auditRuntimePodSlots(pass, client, *pod)
	current, readErr := s.podRepo.GetByID(ctx, id)
	return current, errors.Join(auditErr, readErr)
}
