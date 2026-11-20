package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CompatibilityReservationRequest struct {
	InstanceID   int    `json:"instance_id"`
	RuntimePodID int64  `json:"runtime_pod_id"`
	OperationID  string `json:"operation_id"`
}

func (r CompatibilityReservationRequest) Validate() error {
	if r.InstanceID < 1 || r.RuntimePodID < 1 || !stopTokenValid(r.OperationID) {
		return fmt.Errorf("valid instance_id, runtime_pod_id and operation_id are required")
	}
	return nil
}

func runtimeCompatibilityRequest(rollout *models.RuntimeRollout, instanceID, userID, generation int) RuntimeAgentWorkspaceRequest {
	uid := RuntimeLinuxID(instanceID)
	return RuntimeAgentWorkspaceRequest{RolloutID: strconv.FormatInt(rollout.ID, 10), InstanceID: instanceID, UserID: userID, Generation: generation, UID: uid, GID: uid, RecoveryMode: runtimeUpgradeScopeFromRollout(rollout).RecoveryMode}
}

// RecoverCompatibilityReservation settles the native operation only. The failed
// rollout and the instance's compatibility, storage and placement gates stand.
func (s *RuntimeUpgradeService) RecoverCompatibilityReservation(ctx context.Context, rolloutID int64, req CompatibilityReservationRequest, actor int) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s == nil || s.sess == nil || s.rollouts == nil || s.pods == nil || s.deployments == nil || rolloutID < 1 || actor < 1 {
		return fmt.Errorf("compatibility recovery authority is unavailable")
	}
	client, ok := s.agent.(*slotRuntimeAgentClient)
	if !ok || client.kube == nil {
		return fmt.Errorf("compatibility recovery requires the container slot protocol")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	instances := repository.NewInstanceRepository(s.sess)
	release, err := acquireInstanceLifecycleOperation(ctx, instances, req.InstanceID)
	if err != nil {
		return err
	}
	defer release()
	rollout, err := s.rollouts.GetByID(ctx, rolloutID)
	if err != nil {
		return err
	}
	if rollout == nil || rollout.RuntimeType != RuntimeTypeOpenClaw || rollout.Status != "error" || rollout.Phase != "compatibility_check" {
		return fmt.Errorf("expected a failed OpenClaw compatibility check")
	}
	var item models.RuntimeUpgradeItem
	if err = s.sess.Collection("runtime_upgrade_items").Find("rollout_id", rolloutID).And("instance_id", req.InstanceID).One(&item); err != nil {
		return err
	}
	if item.State != "prepared" || item.SourceGeneration == nil {
		return fmt.Errorf("instance does not have a prepared compatibility reservation")
	}
	instance, err := instances.GetByID(req.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil || instance.Type != RuntimeTypeOpenClaw || instance.RuntimeGeneration != *item.SourceGeneration || instance.Status == "deleted" || instance.Status == "deleting" {
		return fmt.Errorf("compatibility reservation generation or instance changed")
	}
	target, err := s.pods.GetByID(ctx, req.RuntimePodID)
	if err != nil {
		return err
	}
	if target == nil || target.Namespace != client.namespace || target.RuntimeType != RuntimeTypeOpenClaw || stringValue(target.PodUID) == "" || !runtimePodMatchesImage(*target, rollout.TargetImageRef) {
		return fmt.Errorf("compatibility recovery requires the exact rollout target Pod")
	}
	expected, err := rolloutExpectedTargetDeployments(rollout)
	if err != nil {
		return err
	}
	if _, ok := expected[target.Namespace+"/"+target.DeploymentName]; !ok {
		return fmt.Errorf("Pod is outside the approved compatibility target")
	}
	live, err := s.deployments.RuntimeDeploymentPods(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return err
	}
	verified := false
	for _, pod := range live {
		if ordinaryRuntimeIdentityMatches(*target, pod) && runtimePodMatchesImage(pod, rollout.TargetImageRef) {
			verified = true
			break
		}
	}
	if !verified {
		return fmt.Errorf("original compatibility target is no longer verified live")
	}
	request := runtimeCompatibilityRequest(rollout, req.InstanceID, instance.UserID, instance.RuntimeGeneration)
	detail := map[string]any{"instance_id": req.InstanceID, "runtime_pod_id": req.RuntimePodID, "operation_id": req.OperationID}
	if err = s.audit(ctx, &rolloutID, &actor, "compatibility_reservation_recovery", rollout.Phase, "requested", detail); err != nil {
		return err
	}
	if err = client.recoverRecordedNativeCompatibility(ctx, *target, request, req.OperationID); err != nil {
		return err
	}
	return s.audit(ctx, &rolloutID, &actor, "compatibility_reservation_recovery", rollout.Phase, "released", detail)
}

func (c *slotRuntimeAgentClient) recoverRecordedNativeCompatibility(ctx context.Context, target models.RuntimePod, req RuntimeAgentWorkspaceRequest, operationID string) error {
	const action = "compatibility"
	endpoint, uid := stringValue(target.AgentEndpoint), stringValue(target.PodUID)
	pod, err := c.containerStopPod(ctx, endpoint, uid)
	if err != nil {
		return err
	}
	if pod.Name != target.PodName || pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return fmt.Errorf("compatibility target Pod identity or profile changed")
	}
	req, digest, err := nativeMaintenanceRequest(endpoint, pod, action, req)
	if err != nil {
		return err
	}
	cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, "lite-maint-"+digest[:40], metav1.GetOptions{})
	if err != nil {
		return err
	}
	if cm.Labels["clawmanager.io/purpose"] != "lite-native-maintenance" || cm.Data["request_digest"] != digest || cm.Data["operation_id"] != operationID || !stopTokenValid(operationID) || cm.Data["instance_id"] != strconv.Itoa(req.InstanceID) || cm.Data["user_id"] != strconv.Itoa(req.UserID) || cm.Data["generation"] != strconv.Itoa(req.Generation) || cm.Data["endpoint"] != endpoint || cm.Data["pod_uid"] != uid || cm.Data["action"] != action {
		return fmt.Errorf("recorded compatibility request or authority changed")
	}
	switch cm.Data["state"] {
	case "reserved", "running", "terminated", "completed":
		if err = c.acquireNativeMaintenanceOwner(ctx, cm, req.InstanceID); err != nil {
			return err
		}
	case "released":
	default:
		return fmt.Errorf("recorded compatibility state cannot be replayed")
	}
	return c.finishNativeMaintenanceExecution(ctx, endpoint, pod, cm, runtimeNativeUpgradeOperation{OperationID: operationID, Action: action, Request: req}, nil)
}
