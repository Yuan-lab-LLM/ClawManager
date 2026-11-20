package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FailedCompatibilityPoolRequest struct {
	DeploymentName string `json:"deployment_name"`
	DeploymentUID  string `json:"deployment_uid"`
}

// RetireFailedCompatibilityPool only removes the empty target of a failed
// compatibility rollout. Source pools and instance approvals are independent.
func (s *RuntimeUpgradeService) RetireFailedCompatibilityPool(ctx context.Context, rolloutID int64, req FailedCompatibilityPoolRequest, actor int) error {
	if s == nil || s.sess == nil || s.rollouts == nil || s.pods == nil || req.DeploymentName == "" || req.DeploymentUID == "" || rolloutID < 1 || actor < 1 {
		return fmt.Errorf("exact failed rollout target and admin actor are required")
	}
	c, ok := s.agent.(*slotRuntimeAgentClient)
	if !ok || c.kube == nil || c.namespace == "" {
		return fmt.Errorf("failed compatibility pool retirement requires native control")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	rollout, err := s.rollouts.GetByID(ctx, rolloutID)
	if err != nil {
		return err
	}
	if rollout == nil || rollout.Status != "error" || rollout.Phase != "compatibility_check" || rollout.RuntimeType != RuntimeTypeOpenClaw || !IsImmutableRuntimeImage(rollout.TargetImageRef) {
		return fmt.Errorf("expected a failed OpenClaw compatibility rollout with an immutable image")
	}
	expected, err := rolloutExpectedTargetDeployments(rollout)
	if err != nil {
		return err
	}
	key := c.namespace + "/" + req.DeploymentName
	if _, ok := expected[key]; !ok {
		return fmt.Errorf("pool is outside the failed rollout target scope")
	}
	deployments := c.kube.AppsV1().Deployments(c.namespace)
	controller, err := deployments.Get(ctx, req.DeploymentName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		uid, readErr := repository.RuntimePodDeletionIntentUID(ctx, s.sess, c.namespace, req.DeploymentName)
		if readErr == nil && uid == req.DeploymentUID {
			return nil
		}
	}
	if err != nil {
		return err
	}
	if !failedCompatibilityControllerMatches(controller, rollout, req) {
		return fmt.Errorf("failed compatibility controller identity changed")
	}
	if controller.DeletionTimestamp != nil {
		uid, err := repository.RuntimePodDeletionIntentUID(ctx, s.sess, c.namespace, req.DeploymentName)
		if err == nil && uid == req.DeploymentUID {
			return nil
		}
		return fmt.Errorf("failed compatibility controller is already terminating without its retirement fence")
	}
	selector, err := metav1.LabelSelectorAsSelector(controller.Spec.Selector)
	if err != nil || selector.Empty() {
		return fmt.Errorf("failed compatibility controller scope is unavailable")
	}
	backend := k8s.NewRuntimeDeploymentService(c.kube)
	resolver, ok := backend.(k8s.RuntimePodControllerResolver)
	if !ok {
		return fmt.Errorf("native controller ownership verification is unavailable")
	}
	assignments, ok := backend.(interface {
		ConfirmRuntimeScaleInAssignmentsEmpty(context.Context, string, []k8s.RuntimePodScaleInIdentity) error
	})
	if !ok {
		return fmt.Errorf("native assignment inspection is unavailable")
	}
	verifyPods := func() error {
		pods, err := c.kube.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return err
		}
		if controller.Spec.Replicas == nil || *controller.Spec.Replicas <= 0 || len(pods.Items) != int(*controller.Spec.Replicas) {
			return fmt.Errorf("failed compatibility controller has unsettled replicas")
		}
		ids := make([]k8s.RuntimePodScaleInIdentity, 0, len(pods.Items))
		var slots []runtimeSlotAuditObservation
		for _, pod := range pods.Items {
			uid := string(pod.UID)
			identity, err := resolver.ResolveRuntimePodController(ctx, c.namespace, pod.Name, uid)
			if err != nil || identity.DeploymentUID != req.DeploymentUID || pod.DeletionTimestamp != nil {
				return fmt.Errorf("failed compatibility Pod owner changed")
			}
			registered, err := s.pods.GetByNamespaceName(ctx, c.namespace, pod.Name)
			if err != nil {
				return err
			}
			if registered == nil || stringValue(registered.PodUID) != uid || registered.State != "standby" || !runtimePodMatchesImage(*registered, rollout.TargetImageRef) {
				return fmt.Errorf("failed compatibility Pod is not the verified standby target")
			}
			observations, err := c.observeRuntimeSlots(ctx, *registered)
			if err != nil || len(observations) != registered.Capacity {
				return fmt.Errorf("failed compatibility Pod slot observation is incomplete")
			}
			slots = append(slots, observations...)
			ids = append(ids, k8s.RuntimePodScaleInIdentity{PodName: pod.Name, PodUID: uid})
		}
		releases, err := c.failedCompatibilityReleases(ctx, ids)
		if err != nil {
			return err
		}
		for _, slot := range slots {
			if !slot.free() && !releasedMaintenanceSlot(slot, releases[slot.Snapshot.MaintenanceOperationID]) {
				return fmt.Errorf("failed compatibility Pod has an occupied or unverified slot")
			}
		}
		return assignments.ConfirmRuntimeScaleInAssignmentsEmpty(ctx, c.namespace, ids)
	}
	if err := verifyPods(); err != nil {
		return err
	}
	detail := map[string]any{"deployment_name": req.DeploymentName, "deployment_uid": req.DeploymentUID}
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var current []models.RuntimeRollout
		if err := tx.Collection("runtime_rollouts").Find(db.Cond{"runtime_type": RuntimeTypeOpenClaw, "status IN": []string{"pending", "running", "finished"}}).All(&current); err != nil {
			return err
		}
		for _, candidate := range current {
			if candidate.Status != "finished" {
				return fmt.Errorf("another OpenClaw rollout owns the control scope")
			}
			if candidate.SourceImagesJSON == nil {
				continue
			}
			var sources map[string]string
			if err := json.Unmarshal([]byte(*candidate.SourceImagesJSON), &sources); err != nil {
				return err
			}
			if _, used := sources[key]; used {
				return fmt.Errorf("failed compatibility pool is retained by a committed rollout")
			}
		}
		if err := repository.FenceRuntimePodDeletion(ctx, tx, c.namespace, req.DeploymentName, req.DeploymentUID); err != nil {
			return err
		}
		return insertRuntimeUpgradeAudit(ctx, tx, &rolloutID, &actor, "failed_compatibility_pool_retirement", rollout.Phase, "fenced", detail)
	}, nil)
	if err != nil {
		return err
	}
	if err := verifyPods(); err != nil {
		return err
	}
	current, err := deployments.Get(ctx, req.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if current.Generation != controller.Generation || !failedCompatibilityControllerMatches(current, rollout, req) {
		return fmt.Errorf("failed compatibility controller changed after fencing")
	}
	foreground := metav1.DeletePropagationForeground
	return deployments.Delete(ctx, current.Name, metav1.DeleteOptions{PropagationPolicy: &foreground, Preconditions: &metav1.Preconditions{UID: &current.UID, ResourceVersion: &current.ResourceVersion}})
}

func failedCompatibilityControllerMatches(d *appsv1.Deployment, rollout *models.RuntimeRollout, req FailedCompatibilityPoolRequest) bool {
	if string(d.UID) != req.DeploymentUID || d.ResourceVersion == "" || d.Labels["clawmanager.io/runtime-type"] != RuntimeTypeOpenClaw || d.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || d.Labels["clawmanager.io/pool-role"] != "upgrade-target" || d.Labels["clawmanager.io/upgrade-id"] != strconv.FormatInt(rollout.ID, 10) || len(d.Spec.Template.Spec.Containers) < 2 {
		return false
	}
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Image != rollout.TargetImageRef {
			return false
		}
	}
	return true
}

func (c *slotRuntimeAgentClient) failedCompatibilityReleases(ctx context.Context, ids []k8s.RuntimePodScaleInIdentity) (map[string]runtimeSlotContainerProof, error) {
	pods := make(map[string]bool, len(ids))
	for _, id := range ids {
		pods[id.PodUID] = true
	}
	maps, err := c.kube.CoreV1().ConfigMaps(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-native-maintenance"})
	if err != nil {
		return nil, err
	}
	releases := map[string]runtimeSlotContainerProof{}
	for _, record := range maps.Items {
		if !pods[record.Data["pod_uid"]] {
			continue
		}
		proof, confirmed := nativeMaintenanceReleasedProof(record)
		if !confirmed {
			return nil, fmt.Errorf("failed compatibility pool has a reservation without container exit proof")
		}
		owner, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, "lite-maint-owner-"+record.Data["instance_id"], metav1.GetOptions{})
		if err != nil || owner.Labels["clawmanager.io/purpose"] != "lite-native-maintenance-owner" || owner.Data["operation_id"] == record.Data["operation_id"] && owner.Data["state"] != "released" {
			return nil, fmt.Errorf("failed compatibility reservation owner has not been safely released")
		}
		releases[proof.OperationID] = proof
	}
	return releases, nil
}

func nativeMaintenanceReleasedProof(record corev1.ConfigMap) (runtimeSlotContainerProof, bool) {
	var intent runtimeSlotStopIntent
	var proof runtimeSlotContainerProof
	confirmed := record.Data["state"] == "released" && record.Data["result_secret"] != "" && decodeContainerProof([]byte(record.Data["intent"]), &intent) == nil && validContainerIntent(intent) && intent.Kind == "maintenance" && intent.PodUID == record.Data["pod_uid"] && intent.OperationID == record.Data["operation_id"] && decodeContainerProof([]byte(record.Data["termination_proof"]), &proof) == nil && proof.runtimeSlotStopIntent == intent && proof.CleanupConfirmed && proof.ContainerID != "" && !proof.FinishedAt.IsZero()
	return proof, confirmed
}

// Some agents retain the stopped maintenance assignment as a tombstone. Its
// exact released proof can authorize retirement, never another slot claim.
func releasedMaintenanceSlot(slot runtimeSlotAuditObservation, proof runtimeSlotContainerProof) bool {
	snapshot := slot.Snapshot
	assignment := snapshot.Assignment
	if !slot.ReadOK || slot.Err != nil && slot.Err != errRuntimeSlotOwnerIdentity || proof.OperationID == "" || snapshot.MaintenanceOperationID != proof.OperationID || snapshot.PodUID != proof.PodUID || slot.Name != proof.Slot || snapshot.Digest != proof.AssignmentDigest {
		return false
	}
	if !stopTokenValid(snapshot.WorkerEpoch) || snapshot.WorkerEpoch == proof.WorkerEpoch || snapshot.IdleConfirmed == nil || snapshot.Quarantined == nil || *snapshot.Quarantined || snapshot.Gateways == nil || len(snapshot.Gateways) != 0 {
		return false
	}
	return assignment != nil && assignment.State == "stopped" && assignment.PodUID == proof.PodUID && assignment.WorkerEpoch == proof.WorkerEpoch && assignment.GatewayID == proof.GatewayID && assignment.InstanceID == proof.InstanceID && assignment.UserID == proof.UserID && assignment.Generation == proof.Generation
}
