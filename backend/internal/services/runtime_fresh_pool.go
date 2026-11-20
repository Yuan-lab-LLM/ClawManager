package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FreshRuntimeCapacityRequest struct {
	TeamID   int    `json:"team_id"`
	Image    string `json:"image"`
	Activate bool   `json:"activate"`
}

type FreshRuntimeCapacityResult struct {
	DeploymentName string `json:"deployment_name"`
	DeploymentUID  string `json:"deployment_uid"`
	Active         bool   `json:"active"`
}

func (s *RuntimeScheduler) FreshRuntimeCapacity(ctx context.Context, id int, req FreshRuntimeCapacityRequest, actor int) (*FreshRuntimeCapacityResult, error) {
	if s == nil || s.management == nil || s.upgrade == nil || s.instanceRepo == nil || s.podRepo == nil || s.management.sess == nil || s.runtimeNamespace == "" {
		return nil, fmt.Errorf("fresh Runtime admission backend is unavailable")
	}
	if id < 1 || req.TeamID < 1 || actor < 1 || !IsImmutableRuntimeImage(req.Image) {
		return nil, fmt.Errorf("fresh Runtime capacity requires an exact Team member and immutable image")
	}
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || client.kube == nil {
		return nil, fmt.Errorf("fresh Runtime admission requires native slot verification")
	}
	kube, ok := s.deployments.(k8s.FreshRuntimePoolDeploymentService)
	if !ok {
		return nil, fmt.Errorf("fresh Runtime admission backend is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, id)
	if err != nil {
		return nil, err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if instance == nil || !isSchedulerManagedV2Instance(*instance) || instance.RuntimeGeneration != 1 || instance.Status != "creating" && instance.Status != "error" || instance.Type != RuntimeTypeOpenClaw && instance.Type != RuntimeTypeOpenCode || s.upgrade.InstanceBlocked(ctx, instance.Type, id) {
		return nil, fmt.Errorf("fresh Runtime capacity is restricted to an unstarted OpenClaw or OpenCode Lite Team member")
	}
	if err = s.validateFreshRuntimeWorkspace(ctx, client, *instance, req.TeamID); err != nil {
		return nil, err
	}
	maintenance := &RuntimePodMaintenanceService{sess: s.management.sess, scheduler: s}
	scaler, err := maintenance.scaleKube()
	if err != nil {
		return nil, err
	}
	base, snapshot, err := maintenance.defaultScaleSnapshot(ctx, s.management.sess, scaler, instance.Type, scaleHealthySource)
	if err != nil {
		return nil, err
	}
	base.DeploymentUID = snapshot.Option.DeploymentUID
	name := s.DefaultRuntimePoolName(instance.Type) + "-new-" + strconv.Itoa(id)
	scope := k8s.FreshRuntimePoolScope{Source: base, SourceGeneration: snapshot.DeploymentGeneration, Name: name, Image: req.Image, InstanceID: id}
	if err = s.upgrade.audit(ctx, nil, &actor, "fresh_runtime_capacity", "admission", "requested", map[string]any{"team_id": req.TeamID, "instance_id": id, "deployment_name": name, "image": req.Image, "activate": req.Activate}); err != nil {
		return nil, err
	}
	if !req.Activate {
		deployment, err := kube.EnsureFreshRuntimePool(ctx, scope)
		if err != nil {
			return nil, err
		}
		return &FreshRuntimeCapacityResult{DeploymentName: deployment.Name, DeploymentUID: string(deployment.UID)}, nil
	}
	deployment, err := client.kube.AppsV1().Deployments(s.runtimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	result := &FreshRuntimeCapacityResult{DeploymentName: deployment.Name, DeploymentUID: string(deployment.UID)}
	pods, err := s.RuntimeDeploymentPodsFor(ctx, instance.Type, []k8s.RuntimeDeploymentRef{{Namespace: s.runtimeNamespace, Name: name}})
	if err != nil {
		return nil, err
	}
	if len(pods) != 1 {
		return nil, fmt.Errorf("fresh Runtime has not published its single Pod")
	}
	pod := pods[0]
	if pod.PoolPurpose != k8s.RuntimeFreshPoolPurposePrefix+strconv.Itoa(id) || !pod.KubernetesReady || pod.AgentState != "ready" || pod.LastSeenAt == nil || pod.LastSeenAt.Before(s.schedulingHeartbeatCutoff()) || pod.ID < 1 || stringValue(pod.PodUID) == "" || stringValue(pod.RegisteredPodUID) != *pod.PodUID || !runtimePodMatchesImage(pod, req.Image) || !containsString(pod.Capabilities(), "container-slots-v1") || instance.Type == RuntimeTypeOpenCode && stringValue(pod.TeamPluginVersion) != openCodeTeamProtocol {
		return nil, fmt.Errorf("fresh Runtime is not yet verified by Kubernetes and its Agent report")
	}
	observations, err := client.observeRuntimeSlots(ctx, pod)
	if err != nil || len(observations) != snapshot.Option.SlotCount {
		return nil, fmt.Errorf("fresh Runtime slot inventory is incomplete")
	}
	for _, observation := range observations {
		if !observation.free() {
			return nil, fmt.Errorf("fresh Runtime has an occupied or unverified slot")
		}
	}
	if err = s.validateFreshRuntimeWorkspace(ctx, client, *instance, req.TeamID); err != nil {
		return nil, err
	}
	if err = kube.ActivateFreshRuntimePool(ctx, scope, result.DeploymentUID); err != nil {
		return nil, err
	}
	if instance.Status == "error" && stringValue(instance.RuntimeErrorCode) == "RUNTIME_POOL_UNAVAILABLE" {
		retrier, ok := s.instanceRepo.(interface {
			RetryRuntimeSchedulingError(context.Context, models.Instance) (bool, error)
		})
		if !ok {
			return nil, fmt.Errorf("initial Runtime placement retry is unavailable")
		}
		changed, retryErr := retrier.RetryRuntimeSchedulingError(ctx, *instance)
		if retryErr != nil {
			return nil, retryErr
		}
		if !changed {
			return nil, fmt.Errorf("initial Runtime placement intent changed")
		}
	}
	result.Active = true
	return result, nil
}

func (s *RuntimeScheduler) validateFreshRuntimeWorkspace(ctx context.Context, client *slotRuntimeAgentClient, instance models.Instance, teamID int) error {
	sess := s.management.sess
	var member, prior int
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_members WHERE instance_id=? AND team_id=? AND status NOT IN ('deleted','deleting')),
		(SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?)+(SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?)+
		(SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=?
		 AND NOT (r.status='preflight_blocked' AND r.phase='preflight' AND ui.state='pending'
		  AND ui.source_gateway_id IS NULL AND ui.runtime_pod_id IS NULL AND ui.snapshot_ref IS NULL
		  AND ui.operation_token IS NULL AND ui.started_at IS NULL AND ui.isolated_at IS NULL AND ui.recovery_status IS NULL))`, instance.ID, teamID, instance.ID, instance.ID, instance.ID)
	if err == nil {
		err = row.Scan(&member, &prior)
	}
	if err != nil {
		return err
	}
	if member != 1 || prior != 0 {
		return fmt.Errorf("Team membership or unused Runtime identity could not be verified")
	}
	maps, err := client.kube.CoreV1().ConfigMaps(client.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if err != nil {
		return err
	}
	for _, record := range maps.Items {
		if record.Data["instance_id"] == strconv.Itoa(instance.ID) {
			return fmt.Errorf("Team member already has a published Runtime assignment")
		}
	}
	root := RuntimeWorkspacePathWithRoot(s.workspaceRoot, instance.Type, instance.UserID, instance.ID)
	if instance.WorkspacePath != nil && !sameCleanPath(*instance.WorkspacePath, root) {
		return fmt.Errorf("Team member workspace does not match its scoped path")
	}
	ledger := filepath.Join(s.workspaceRoot, ".clawmanager-slot-locks", fmt.Sprintf("%s-%d-%d.json", instance.Type, instance.UserID, instance.ID))
	if _, err := os.Lstat(ledger); !os.IsNotExist(err) {
		return fmt.Errorf("fresh Runtime workspace ledger is present or cannot be inspected")
	}
	if instance.Type == RuntimeTypeOpenClaw {
		clean, err := unstartedOpenClawWorkspace(root)
		if err != nil || !clean {
			return fmt.Errorf("fresh OpenClaw workspace has native data or cannot be inspected")
		}
		return nil
	}
	count := 0
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		count++
		if count > 10000 {
			return fmt.Errorf("fresh OpenCode workspace inspection exceeded its bounds")
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type().IsRegular() && filepath.Dir(path) == root {
			switch entry.Name() {
			case "AGENTS.md", "SOUL.md", "team.json":
				return nil // Team provisioning stages these before the first launch.
			}
		}
		return fmt.Errorf("fresh OpenCode workspace contains files from a prior use")
	})
}

func freshRuntimePoolForInstance(pod models.RuntimePod, instanceID int) bool {
	return instanceID > 0 && pod.PoolPurpose == k8s.RuntimeFreshPoolPurposePrefix+strconv.Itoa(instanceID)
}

func isFreshRuntimePool(pod models.RuntimePod) bool {
	return strings.HasPrefix(pod.PoolPurpose, k8s.RuntimeFreshPoolPurposePrefix)
}
