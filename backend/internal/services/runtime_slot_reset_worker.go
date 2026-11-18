package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"time"
)

type physicalSlotResetter interface {
	ResetPhysicalSlot(context.Context, runtimeeviction.SlotScope, string) (*runtimeeviction.SlotReceipt, error)
}

func slotResetKubeletReceipt(pod *corev1.Pod, scope runtimeeviction.SlotScope, notBefore time.Time) (*runtimeeviction.SlotReceipt, error) {
	if pod == nil || string(pod.UID) != scope.PodUID || pod.Spec.NodeName != scope.NodeName {
		return nil, errors.New("physical reset Pod changed")
	}
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name == scope.ContainerName {
			old := st.LastTerminationState.Terminated
			if old == nil && st.State.Terminated != nil {
				old = st.State.Terminated
			}
			// metav1.Time serializes whole seconds; SQL creation retains micros.
			// Match the API precision while still requiring the exact old CID.
			if old != nil && old.ContainerID == scope.ContainerID && !old.FinishedAt.IsZero() && !old.FinishedAt.Time.Before(notBefore.Truncate(time.Second)) {
				r := &runtimeeviction.SlotReceipt{ScopeDigest: runtimeeviction.SlotScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, ContainerID: scope.ContainerID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, FinishedAt: old.FinishedAt.Time, Confirmed: true}
				return r, runtimeeviction.ValidateSlotReceipt(scope, r)
			}
		}
	}
	return nil, k8sservice.ErrRuntimeFaultEvictionPending
}

func (s *RuntimeManagementService) processSlotReset(ctx context.Context, op *RuntimeManagementOperation) error {
	if len(op.Items) != 1 || op.Items[0].SlotReset == nil {
		return errors.New("slot reset journal missing")
	}
	j := op.Items[0].SlotReset
	lifecycleHeld := false
	if j.InstanceID > 0 {
		release, err := acquireInstanceLifecycleOperation(ctx, s.scheduler.instanceRepo, j.InstanceID)
		if err != nil {
			return err
		}
		defer release()
		lifecycleHeld = true
	}
	step := func() error {
		c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
		if !ok {
			return errors.New("native slot reset client unavailable")
		}
		if j.Receipt == nil {
			if reader, ok := s.scheduler.deployments.(interface {
				PhysicalSlotResetReceipt(context.Context, runtimeeviction.SlotScope, string) (*runtimeeviction.SlotReceipt, error)
			}); ok {
				if receipt, e := reader.PhysicalSlotResetReceipt(ctx, j.Scope, j.HelperImage); e == nil {
					if e = runtimeeviction.ValidateSlotReceipt(j.Scope, receipt); e != nil {
						return e
					}
					j.Receipt = receipt
					j.Phase = "source_stopped"
					if e = s.save(ctx, op); e != nil {
						return e
					}
				}
			}
		}
		pod, err := c.kube.CoreV1().Pods(j.Scope.Namespace).Get(ctx, j.Scope.PodName, metav1.GetOptions{})
		retired := apierrors.IsNotFound(err) || err == nil && string(pod.UID) != j.Scope.PodUID
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if retired && j.Receipt == nil {
			return errors.New("fixed source Pod was replaced without a receipt; no replacement was stopped")
		}
		if !retired && pod.Spec.NodeName != j.Scope.NodeName {
			return errors.New("fixed source Pod node changed")
		}
		if j.Receipt == nil {
			node, err := c.kube.CoreV1().Nodes().Get(ctx, j.Scope.NodeName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if string(node.UID) != j.Scope.NodeUID || node.Status.NodeInfo.BootID != j.Scope.NodeBootID {
				return errors.New("fixed reset node identity changed")
			}
			if receipt, e := slotResetKubeletReceipt(pod, j.Scope, op.CreatedAt); e == nil {
				j.Receipt = receipt
			} else {
				var liveID string
				for _, st := range pod.Status.ContainerStatuses {
					if st.Name == j.Slot {
						liveID = st.ContainerID
					}
				}
				if liveID != j.ContainerID {
					return errors.New("old container changed without a matching exit receipt; replacement retained")
				}
				if j.Owner != nil && j.Source.AgentEndpoint != nil {
					var state runtimeSlotAuditSnapshot
					if e = c.do(ctx, http.MethodGet, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/state", nil, &state); e == nil && (state.PodUID != j.PodUID || state.Digest != j.AssignmentDigest || !sameSlotResetOwner(state.Assignment, j.Owner)) {
						return errors.New("fixed slot assignment changed before termination")
					}
				}
				j.Phase = "stopping"
				if err = s.save(ctx, op); err != nil {
					return err
				}
				resetter, ok := s.scheduler.deployments.(physicalSlotResetter)
				if !ok {
					return errors.New("independent physical reset helper is unavailable")
				}
				j.Receipt, err = resetter.ResetPhysicalSlot(ctx, j.Scope, j.HelperImage)
				if err != nil {
					return err
				}
			}
			if err = runtimeeviction.ValidateSlotReceipt(j.Scope, j.Receipt); err != nil {
				return err
			}
			j.Phase = "source_stopped"
			if err = s.save(ctx, op); err != nil {
				return err
			}
		}
		if !j.CleanupCommitted {
			// When the old worker could not answer, only a replacement worker's
			// retained old book may add business ownership after physical stop.
			if j.Owner == nil && j.Source.AgentEndpoint != nil && !retired {
				var state runtimeSlotAuditSnapshot
				if err = c.do(ctx, http.MethodGet, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/state", nil, &state); err != nil {
					return fmt.Errorf("%w: inspect retained source book: %v", k8sservice.ErrRuntimeFaultEvictionPending, err)
				}
				if state.PodUID != j.PodUID || !stopTokenValid(state.WorkerEpoch) {
					return errors.New("replacement worker identity is not confirmed")
				}
				if state.Assignment != nil {
					if !validSlotResetOwner(state.Assignment, j.PodUID, state.Digest) || state.Assignment.WorkerEpoch == state.WorkerEpoch {
						return errors.New("replacement book is not a retained old execution")
					}
					if j.InstanceID > 0 && j.InstanceID != state.Assignment.InstanceID || j.SourceBinding != nil && (j.SourceBinding.InstanceID != state.Assignment.InstanceID || j.SourceBinding.Generation != state.Assignment.Generation || j.SourceBinding.GatewayID != state.Assignment.GatewayID && j.SourceBinding.GatewayID != fmt.Sprintf("pending-%d-%d", state.Assignment.InstanceID, state.Assignment.Generation)) {
						return errors.New("retained source owner conflicts with the pinned SQL owner")
					}
					j.Owner = state.Assignment
					j.AssignmentDigest = state.Digest
					j.InstanceID = state.Assignment.InstanceID
					op.Items[0].InstanceID = j.InstanceID
				}
				if err = s.save(ctx, op); err != nil {
					return err
				}
			}
			run := func() error {
				if err := s.retireSlotResetWriter(ctx, op, j); err != nil {
					return err
				}
				if err := s.save(ctx, op); err != nil {
					return err
				}
				if j.WriterJobAuthorized {
					if j.WriterScope == nil || j.WriterScope.OperationID != j.Scope.OperationID {
						return errors.New("slot reset PVC retained: writer scope missing")
					}
					if err = k8sservice.SlotResetWriterFinished(ctx, c.kube, *j.WriterScope, j.SourcePod, j.HelperImage, j.WriterReceipt); err != nil {
						return err
					}
				}
				return s.commitSlotResetRecovery(ctx, op)
			}
			if j.InstanceID > 0 && !lifecycleHeld {
				release, err := acquireInstanceLifecycleOperation(ctx, s.scheduler.instanceRepo, j.InstanceID)
				if err != nil {
					return err
				}
				defer release()
			}
			if j.InstanceID > 0 && s.scheduler.upgrade != nil {
				err = s.scheduler.upgrade.WithInstanceMutation(ctx, j.InstanceID, run)
			} else {
				err = run()
			}
			if err != nil {
				return err
			}
			j = op.Items[0].SlotReset
		}
		if j.WriterClaim != nil {
			if err = k8sservice.ReleaseRuntimeSlotResetClaim(ctx, c.kube, *j.WriterClaim, j.Scope.OperationID); err != nil {
				return err
			}
			j.WriterClaim = nil
			if err = s.save(ctx, op); err != nil {
				return err
			}
		}
		if retired {
			j.Phase = "released"
			j.ErrorMessage = ""
			op.Items[0].State = "completed"
			op.Items[0].ErrorMessage = ""
			op.Status = "completed"
			return s.finishSlotReset(ctx, op)
		}
		j.Phase = "waiting_worker"
		if j.Source.AgentEndpoint == nil {
			return errors.New("original slot endpoint is unavailable")
		}
		var snapshot runtimeSlotAuditSnapshot
		if err = c.do(ctx, http.MethodGet, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/state", nil, &snapshot); err != nil {
			return fmt.Errorf("%w: wait replacement worker: %v", k8sservice.ErrRuntimeFaultEvictionPending, err)
		}
		var liveID string
		for _, st := range pod.Status.ContainerStatuses {
			if st.Name == j.Slot && st.State.Running != nil {
				liveID = st.ContainerID
			}
		}
		if liveID == "" || liveID == j.ContainerID || snapshot.PodUID != j.PodUID || !stopTokenValid(snapshot.WorkerEpoch) {
			return k8sservice.ErrRuntimeFaultEvictionPending
		}
		if j.Owner != nil && (snapshot.Assignment == nil || snapshot.Assignment.WorkerEpoch != j.Owner.WorkerEpoch || snapshot.Digest != j.AssignmentDigest) {
			return errors.New("replacement slot owner changed; current execution retained")
		}
		j.CurrentWorkerEpoch = snapshot.WorkerEpoch
		var oldEpoch string
		if j.Owner != nil {
			oldEpoch = j.Owner.WorkerEpoch
		}
		proof := struct {
			Scope              runtimeeviction.SlotScope    `json:"scope"`
			Receipt            *runtimeeviction.SlotReceipt `json:"receipt"`
			WorkerEpoch        string                       `json:"worker_epoch"`
			AssignmentDigest   string                       `json:"assignment_digest"`
			CurrentWorkerEpoch string                       `json:"current_worker_epoch"`
		}{j.Scope, j.Receipt, oldEpoch, j.AssignmentDigest, snapshot.WorkerEpoch}
		for _, action := range []string{"reconcile-reset", "release-reset"} {
			var ack struct {
				OperationID string `json:"operation_id"`
				ScopeDigest string `json:"scope_digest"`
				ContainerID string `json:"container_id"`
				WorkerEpoch string `json:"worker_epoch"`
				Stopped     bool   `json:"stopped"`
				Confirmed   bool   `json:"confirmed"`
				Released    bool   `json:"released"`
			}
			if err = c.containerStopRequest(ctx, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/"+action, proof, &ack); err != nil {
				return err
			}
			if ack.OperationID != j.Scope.OperationID || ack.ScopeDigest != runtimeeviction.SlotScopeDigest(j.Scope) || ack.ContainerID != j.ContainerID || ack.WorkerEpoch != snapshot.WorkerEpoch || !ack.Stopped || !ack.Confirmed || ack.Released != (action == "release-reset") {
				return errors.New("native physical reset acknowledgement changed")
			}
		}
		var fresh runtimeSlotAuditSnapshot
		if err = c.do(ctx, http.MethodGet, *j.Source.AgentEndpoint, "/v1/slots/"+j.Slot+"/state", nil, &fresh); err != nil {
			return err
		}
		if fresh.PodUID != j.PodUID || fresh.WorkerEpoch != snapshot.WorkerEpoch || !(runtimeSlotAuditObservation{Snapshot: fresh}).free() {
			return errors.New("reset slot lacks a fresh free proof")
		}
		j.Phase = "released"
		j.ErrorMessage = ""
		op.Items[0].State = "completed"
		op.Items[0].ErrorMessage = ""
		op.Status = "completed"
		return s.finishSlotReset(ctx, op)
	}
	err := step()
	if err == nil {
		return nil
	}
	// A lost commit response must be resolved by reloading the operation on the
	// next elected pass, never by saving this worker's speculative memory.
	if errors.Is(err, ErrRuntimeSlotResetCommitUncertain) {
		return err
	}
	j = op.Items[0].SlotReset
	if j.Phase == "released" {
		j.Phase = "waiting_worker"
	}
	if j.WriterClaim != nil {
		err = fmt.Errorf("slot reset workspace PVC %s/%s protection recorded; retry to finish cleanup: %w", j.WriterClaim.Namespace, j.WriterClaim.Name, err)
	}
	j.ErrorMessage = err.Error()
	op.Items[0].ErrorMessage = err.Error()
	op.Items[0].State = "processing"
	if errors.Is(err, k8sservice.ErrRuntimeFaultEvictionPending) || errors.Is(err, models.ErrInstanceOperationInProgress) {
		op.Status = "pending"
	} else {
		op.Status = "needs_attention"
	}
	if saveErr := s.save(ctx, op); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func (s *RuntimeManagementService) retireSlotResetWriter(ctx context.Context, op *RuntimeManagementOperation, j *RuntimeSlotResetJournal) error {
	if j.InstanceID == 0 {
		return nil
	}
	instance, err := s.scheduler.instanceRepo.GetByID(j.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return nil
	}
	current, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, j.InstanceID)
	if err != nil {
		return err
	}
	old := models.InstanceRuntimeBinding{InstanceID: j.InstanceID, RuntimePodID: j.Source.ID, RuntimeType: j.Source.RuntimeType, GatewayPort: j.GatewayPort}
	if j.Owner != nil {
		old.Generation = j.Owner.Generation
		old.GatewayID = j.Owner.GatewayID
	}
	if j.SourceBinding != nil {
		old = *j.SourceBinding
	}
	if current != nil && !sameSlotResetBinding(*current, old) {
		j.RecoveryAction = "preserve_current"
		return nil
	}
	if j.Owner == nil {
		return s.confirmUnpublishedSlotReset(ctx, j, *instance)
	}
	if instance.RuntimeGeneration < old.Generation || !isSchedulerManagedV2Instance(*instance) || instance.Type != j.Source.RuntimeType {
		return errors.New("current instance profile cannot be recovered from this old slot")
	}
	c := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if j.Assignment == nil && j.Source.AgentEndpoint != nil {
		j.Assignment, err = c.assignments.FaultEvictionSource(ctx, *j.Source.AgentEndpoint, j.Owner.GatewayID)
		if err != nil {
			return err
		}
	}
	if j.Assignment == nil || j.Assignment.PodUID != j.PodUID || j.Assignment.InstanceID != j.InstanceID || j.Assignment.UserID != j.Owner.UserID || j.Assignment.Generation != j.Owner.Generation {
		return errors.New("old publication identity changed")
	}
	assignedSlot, err := containerSlotName(j.SourcePod, j.Assignment.Request)
	if err != nil || assignedSlot != j.Slot {
		return errors.New("current publication belongs to another slot; its writers were retained")
	}
	var request struct {
		GatewayPort   int    `json:"gateway_port"`
		WorkspacePath string `json:"workspace_path"`
	}
	if json.Unmarshal(j.Assignment.Request, &request) != nil || request.GatewayPort != j.GatewayPort || request.WorkspacePath != RuntimeWorkspacePathWithRoot(s.scheduler.workspaceRoot, j.Source.RuntimeType, j.Owner.UserID, j.InstanceID) || j.SourceBinding != nil && request.WorkspacePath != j.SourceBinding.WorkspacePath {
		return errors.New("old publication port or workspace changed")
	}
	if j.WriterClaim == nil {
		claim, e := k8sservice.RuntimeSlotResetWorkspaceClaim(ctx, c.kube, j.SourcePod, s.scheduler.workspaceRoot, j.StorageIdentity)
		if e != nil {
			return e
		}
		j.WriterClaim = claim
		// Record ownership before the Kubernetes update so a lost response or
		// crash can always find and remove this operation's original PVC pin.
		if err = s.save(ctx, op); err != nil {
			return err
		}
	}
	if err = k8sservice.PinRuntimeSlotResetClaim(ctx, c.kube, *j.WriterClaim, j.Scope.OperationID); err != nil {
		return err
	}
	if err = k8sservice.VerifyRuntimeSlotResetStorage(ctx, c.kube, j.SourcePod, s.scheduler.workspaceRoot, j.StorageIdentity, j.WriterClaim, j.Scope.OperationID); err != nil {
		return err
	}
	if err = c.assignments.FenceFaultEviction(ctx, j.Assignment, j.Scope.OperationID); err != nil {
		return err
	}
	identity := runtimeauth.Identity{InstanceID: j.InstanceID, UserID: j.Owner.UserID, Generation: j.Owner.Generation, PodUID: j.PodUID}
	if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
		return err
	}
	if err = c.requireSlotResetExternalWriterCleanup(ctx, identity, current != nil); err != nil {
		return err
	}
	if j.WriterScope == nil {
		p := j.Scope
		j.WriterScope = &runtimeeviction.Scope{Version: 1, OperationID: p.OperationID, Namespace: p.Namespace, PodName: p.PodName, PodUID: p.PodUID, NodeName: p.NodeName, NodeUID: p.NodeUID, NodeBootID: p.NodeBootID, ContainerName: p.ContainerName, ContainerID: p.ContainerID, RuntimeType: j.Source.RuntimeType, GatewayID: j.Owner.GatewayID, WorkspaceRoot: s.scheduler.workspaceRoot, InstanceID: j.InstanceID, UserID: j.Owner.UserID, Generation: j.Owner.Generation}
	}
	if j.WriterReceipt == nil {
		evicter, ok := s.scheduler.deployments.(interface {
			RecoverRuntimeEviction(context.Context, runtimeeviction.Scope, *corev1.Pod) (*runtimeeviction.Receipt, error)
		})
		if !ok {
			return errors.New("old workspace writer recovery helper unavailable")
		}
		if !j.WriterJobAuthorized {
			// A timeout can hide a successful Job create. Publish that possibility
			// before allowing the helper to use its name-based mount.
			j.WriterJobAuthorized = true
			if err = s.save(ctx, op); err != nil {
				return err
			}
		}
		original := j.SourcePod.DeepCopy()
		if original.Annotations == nil {
			original.Annotations = map[string]string{}
		}
		original.Annotations[k8sservice.RuntimeFaultHelperImageAnnotation] = j.HelperImage
		j.WriterReceipt, err = evicter.RecoverRuntimeEviction(ctx, *j.WriterScope, original)
		if err != nil {
			return err
		}
	}
	return c.assignments.ConfirmFaultEvictionStopped(ctx, j.Assignment, *j.WriterScope, j.WriterReceipt)
}
