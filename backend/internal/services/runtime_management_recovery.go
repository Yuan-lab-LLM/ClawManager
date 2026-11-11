package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type RuntimeFaultEvictionJournal struct {
	Phase               string                        `json:"phase"`
	Scope               runtimeeviction.Scope         `json:"scope"`
	Receipt             *runtimeeviction.Receipt      `json:"receipt,omitempty"`
	SourceStopConfirmed bool                          `json:"source_stop_confirmed,omitempty"`
	SourceUserID        int                           `json:"source_user_id"`
	SourceBinding       models.InstanceRuntimeBinding `json:"source_binding"`
	SourceEndpoint      string                        `json:"source_endpoint"`
	DesiredStatus       string                        `json:"desired_status"`
	ResultState         string                        `json:"result_state,omitempty"`
	Assignment          *k8s.RuntimeFaultAssignment   `json:"assignment"`
	SourcePod           *corev1.Pod                   `json:"source_pod"`
}

type faultEvictionStopper interface {
	RecoverRuntimeEviction(context.Context, runtimeeviction.Scope, *corev1.Pod) (*runtimeeviction.Receipt, error)
}

func recoveryEvictionEligible(instance *models.Instance, explicit bool) bool {
	if instance == nil || instance.InstanceMode != "lite" || instance.RuntimeType != RuntimeBackendGateway {
		return false
	}
	switch instance.Status {
	case "creating", "error", "stopped", "deleting":
		return true
	case "running":
		return explicit
	default:
		return false
	}
}

// A missing source uses the retained original template and independently checked
// node attribution; a current replacement Pod never substitutes for the old UID.
func faultEvictionPod(ctx context.Context, client kubernetes.Interface, source models.RuntimePod) (*corev1.Pod, error) {
	pod, err := client.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if err == nil && string(pod.UID) == stringValue(source.PodUID) {
		if pod.Spec.NodeName != stringValue(source.NodeName) {
			return nil, fmt.Errorf("source node changed")
		}
		return pod, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	split := strings.LastIndex(source.PodName, "-")
	if split < 1 {
		return nil, fmt.Errorf("source original ReplicaSet cannot be identified")
	}
	rs, err := client.AppsV1().ReplicaSets(source.Namespace).Get(ctx, source.PodName[:split], metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("source original template unavailable: %w", err)
	}
	owner := metav1.GetControllerOf(rs)
	if owner == nil || owner.Kind != "Deployment" || owner.Name != source.DeploymentName || rs.CreationTimestamp.IsZero() || rs.CreationTimestamp.Time.After(source.CreatedAt) {
		return nil, fmt.Errorf("source original controller identity is unproven")
	}
	dep, err := client.AppsV1().Deployments(source.Namespace).Get(ctx, source.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if dep.UID != owner.UID {
		return nil, fmt.Errorf("source controller UID changed")
	}
	if err = k8s.VerifyRuntimeFaultSourceTemplate(ctx, client, rs, dep); err != nil {
		return nil, err
	}
	node, err := k8s.RuntimeFaultHistoricalNode(ctx, client, source)
	if err != nil {
		return nil, err
	}

	pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: source.PodName, Namespace: source.Namespace, UID: types.UID(*source.PodUID)}, Spec: *rs.Spec.Template.Spec.DeepCopy()}
	pod.Spec.NodeName = node.Name
	pod.Annotations = map[string]string{"clawmanager.io/reconstructed-source": "true"}
	return pod, nil
}

func (s *RuntimeManagementService) recoveryEvictionPreflight(ctx context.Context, req RuntimeSchedulingRequest) (*RuntimeSchedulingPreflight, error) {
	if s == nil || s.scheduler == nil || s.sess == nil || req.SourcePodID < 1 || len(req.InstanceIDs) > 100 {
		return nil, fmt.Errorf("fault recovery requires a source Runtime and at most 100 selected instances")
	}
	plan := &RuntimeSchedulingPreflight{Allowed: true, Items: []RuntimeSchedulingItem{}, Blockers: []string{}}
	c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok || c.kube == nil {
		return nil, fmt.Errorf("fault eviction requires authenticated Kubernetes and slot lifecycle")
	}
	if _, ok := s.scheduler.deployments.(faultEvictionStopper); !ok {
		return nil, fmt.Errorf("node-side fault eviction is unavailable")
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, req.SourcePodID)
	if err != nil {
		return nil, err
	}
	if source == nil || source.PodUID == nil || source.AgentEndpoint == nil {
		return nil, fmt.Errorf("source Runtime identity unavailable")
	}
	bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	hasExplicit := len(req.InstanceIDs) > 0
	explicit := map[int]bool{}
	for _, id := range req.InstanceIDs {
		if id < 1 || explicit[id] {
			return nil, fmt.Errorf("invalid or duplicate recovery instance")
		}
		explicit[id] = true
	}
	for _, binding := range bindings {
		if hasExplicit && !explicit[binding.InstanceID] {
			continue
		}
		instance, e := s.scheduler.instanceRepo.GetByID(binding.InstanceID)
		if e != nil {
			return nil, e
		}
		if !recoveryEvictionEligible(instance, hasExplicit) {
			if explicit[binding.InstanceID] {
				plan.Allowed = false
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("instance %d is not eligible for fault eviction", binding.InstanceID))
			}
			continue
		}
		item, e := s.prepareRecoveryEviction(ctx, c, *source, *instance, binding)
		if e != nil {
			plan.Allowed = false
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("instance %d: %s", binding.InstanceID, e))
		} else {
			plan.Items = append(plan.Items, item)
		}
		delete(explicit, binding.InstanceID)
	}
	if len(explicit) > 0 {
		return nil, fmt.Errorf("selected instances are not bound to this source")
	}
	if len(plan.Items) == 0 && len(plan.Blockers) == 0 {
		plan.Allowed = false
		plan.Blockers = append(plan.Blockers, "No abnormal bound instance was found. Unknown slots without a verified database owner require inspection, not automatic eviction.")
	}
	return plan, nil
}

func (s *RuntimeManagementService) prepareRecoveryEviction(ctx context.Context, c *slotRuntimeAgentClient, source models.RuntimePod, instance models.Instance, binding models.InstanceRuntimeBinding) (RuntimeSchedulingItem, error) {
	fail := func(e error) (RuntimeSchedulingItem, error) { return RuntimeSchedulingItem{}, e }
	if binding.Generation < 1 || instance.RuntimeGeneration < binding.Generation || instance.RuntimeGeneration > binding.Generation+1 {
		return fail(fmt.Errorf("binding belongs to an incompatible generation"))
	}
	var team int
	row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM team_members WHERE instance_id=? AND status NOT IN ('deleted','deleting')", instance.ID)
	if err == nil {
		err = row.Scan(&team)
	}
	if err != nil {
		return fail(err)
	}
	if team > 0 {
		return fail(fmt.Errorf("Team members require coordinated writer recovery"))
	}
	gateway, err := c.assignments.ResolvePendingGatewayID(ctx, *source.AgentEndpoint, binding.GatewayID)
	if err != nil {
		return fail(err)
	}
	assignment, err := c.assignments.FaultEvictionSource(ctx, *source.AgentEndpoint, gateway)
	if err != nil {
		return fail(err)
	}
	if assignment.InstanceID != instance.ID || assignment.UserID != instance.UserID || assignment.Generation != binding.Generation || assignment.PodUID != *source.PodUID || assignment.RuntimeType != instance.Type {
		return fail(fmt.Errorf("database binding and source assignment disagree"))
	}
	var request struct {
		GatewayPort   int    `json:"gateway_port"`
		WorkspacePath string `json:"workspace_path"`
	}
	if json.Unmarshal(assignment.Request, &request) != nil || request.GatewayPort != binding.GatewayPort || request.WorkspacePath != binding.WorkspacePath {
		return fail(fmt.Errorf("source request differs from the database workspace or port"))
	}
	root := s.scheduler.workspaceRoot
	pod, err := faultEvictionPod(ctx, c.kube, source)
	if err != nil && (instance.Status == "stopped" || instance.Status == "deleting") {
		pod, err = retiredFaultSourcePod(ctx, c, source, instance, binding, root)
	}
	if err != nil {
		return fail(err)
	}
	slot, err := containerSlotName(pod, assignment.Request)
	if err != nil {
		return fail(err)
	}

	if root == "" || !path.IsAbs(root) || path.Clean(root) != root {
		return fail(fmt.Errorf("canonical workspace root is unavailable"))
	}
	expected := path.Join(root, instance.Type, "user-"+strconv.Itoa(instance.UserID), "instance-"+strconv.Itoa(instance.ID))
	if binding.WorkspacePath != expected {
		return fail(fmt.Errorf("source workspace is not canonical"))
	}
	storage, err := k8s.RuntimeFaultStorageIdentity(ctx, c.kube, pod, root, source.CreatedAt)
	if err != nil {
		return fail(err)
	}
	node, err := c.kube.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return fail(err)
	}
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if !ready || node.UID == "" || node.Status.NodeInfo.BootID == "" || node.CreationTimestamp.IsZero() || node.CreationTimestamp.Time.After(source.CreatedAt) {
		return fail(fmt.Errorf("source node is unavailable; restore or isolate that node before recovery"))
	}
	scope := runtimeeviction.Scope{Version: 1, OperationID: "preflight", Namespace: source.Namespace, PodName: source.PodName, PodUID: *source.PodUID, NodeName: node.Name, NodeUID: string(node.UID), NodeBootID: node.Status.NodeInfo.BootID, ContainerName: slot, RuntimeType: instance.Type, GatewayID: gateway, WorkspaceRoot: root, InstanceID: instance.ID, UserID: instance.UserID, Generation: binding.Generation}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == slot {
			scope.ContainerID = status.ContainerID
		}
	}
	if scope.ContainerID == "" && pod.Annotations["clawmanager.io/reconstructed-source"] != "true" {
		return fail(fmt.Errorf("source slot container identity is not yet observed; recheck before fault eviction"))
	}
	if err = scope.Validate(); err != nil {
		return fail(err)
	}
	// Only keep the workspace mapping and fixed identity environment. Journal and
	// admin responses must never copy ambient provider/control credentials.
	clean := pod.DeepCopy()
	clean.Status = corev1.PodStatus{}
	clean.Spec.InitContainers = nil
	clean.Spec.EphemeralContainers = nil
	clean.Spec.Volumes = nil
	for _, v := range pod.Spec.Volumes {
		if v.Name == "workspaces" {
			clean.Spec.Volumes = append(clean.Spec.Volumes, v)
		}
	}
	clean.Spec.Containers = nil
	for _, container := range pod.Spec.Containers {
		if container.Name != slot {
			continue
		}
		selected := corev1.Container{Name: slot, Image: container.Image}
		for _, env := range container.Env {
			if env.Name == "RUNTIME_WORKSPACE_ROOT" || env.Name == "RUNTIME_GATEWAY_PORT_START" {
				selected.Env = append(selected.Env, env)
			}
		}
		for _, mount := range container.VolumeMounts {
			if mount.Name == "workspaces" {
				selected.VolumeMounts = append(selected.VolumeMounts, mount)
			}
		}
		clean.Spec.Containers = append(clean.Spec.Containers, selected)
	}
	helperImage := strings.TrimSpace(os.Getenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE"))
	if helperImage == "" {
		app, e := c.kube.AppsV1().Deployments(source.Namespace).Get(ctx, "clawmanager-app", metav1.GetOptions{})
		if e != nil {
			return fail(fmt.Errorf("pin recovery helper image: %w", e))
		}
		for _, container := range app.Spec.Template.Spec.Containers {
			if container.Name == "clawmanager-app" {
				helperImage = container.Image
			}
		}
	}
	if !strings.Contains(helperImage, "@sha256:") {
		return fail(fmt.Errorf("recovery helper image must be pinned by digest"))
	}
	clean.Annotations = map[string]string{k8s.RuntimeFaultStorageAnnotation: storage, k8s.RuntimeFaultHelperImageAnnotation: helperImage}
	if pod.Annotations["clawmanager.io/retirement-ledger-source"] == "true" {
		clean.Annotations["clawmanager.io/retirement-ledger-source"] = "true"
	}
	clean.ManagedFields = nil
	item := RuntimeSchedulingItem{InstanceID: instance.ID, SourcePodID: source.ID, SourceGeneration: binding.Generation, SourceGatewayID: binding.GatewayID, SourcePodUID: *source.PodUID, State: "ready", Recovery: &RuntimeFaultEvictionJournal{Phase: "planned", Scope: scope, SourceUserID: instance.UserID, SourceBinding: binding, SourceEndpoint: *source.AgentEndpoint, DesiredStatus: instance.Status, Assignment: assignment, SourcePod: clean}}
	// A future placement must retain image/storage/resource compatibility. A dead
	// source without this evidence can still finish stop/delete, never guess a target.
	if instance.Status != "stopped" && instance.Status != "deleting" {
		inventory, e := s.scheduler.RuntimeDeploymentPods(ctx, source.RuntimeType)
		if e != nil {
			return fail(e)
		}
		for _, live := range inventory {
			if live.ID != source.ID {
				continue
			}
			required, e := runtimeManagementSourceSlot(live, binding)
			if e == nil && live.ImageRef != "" && stringValue(live.ImageDigest) != "" && live.WorkspaceStorageIdentity != "" {
				item.SourcePlacement = runtimeEvictionPlacement(live, required)
			}
			break
		}
		if item.SourcePlacement == nil {
			var claim *corev1.PersistentVolumeClaim
			for _, volume := range pod.Spec.Volumes {
				if volume.Name == "workspaces" && volume.PersistentVolumeClaim != nil {
					claim, err = c.kube.CoreV1().PersistentVolumeClaims(source.Namespace).Get(ctx, volume.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
					if err != nil {
						return fail(err)
					}
				}
			}
			historical, e := k8s.RuntimeFaultSourcePlacement(pod, claim, source, slot)
			if e != nil {
				return fail(e)
			}
			item.SourcePlacement = runtimeEvictionPlacement(historical, historical.Slots[0])
		}
	}
	return item, nil
}

func (s *RuntimeManagementService) saveRecoveryItem(ctx context.Context, operation string, item RuntimeSchedulingItem) error {
	op, _, _, err := s.runtimeManagementOperation(ctx, operation)
	if err != nil {
		return err
	}
	if err = s.assertOwner(ctx, operation); err != nil {
		return err
	}
	for i := range op.Items {
		if op.Items[i].InstanceID == item.InstanceID {
			op.Items[i] = item
			return s.save(ctx, op)
		}
	}
	return fmt.Errorf("recovery item disappeared")
}

func (s *RuntimeManagementService) recoverEvictedSource(ctx context.Context, operation string, item RuntimeSchedulingItem) error {
	j := item.Recovery
	if j == nil {
		return fmt.Errorf("fault eviction checkpoint is missing")
	}
	if j.Phase == "released" {
		return nil
	}
	if j.Phase != "source_stopped" {
		c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
		if !ok {
			return fmt.Errorf("slot lifecycle unavailable")
		}
		current, err := s.scheduler.instanceRepo.GetByID(item.InstanceID)
		if err != nil {
			return err
		}
		binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, item.InstanceID)
		if err != nil {
			return err
		}
		if current == nil || current.UserID != j.SourceUserID || binding == nil || binding.ID != j.SourceBinding.ID || binding.RuntimePodID != item.SourcePodID || binding.Generation != item.SourceGeneration || binding.GatewayPort != j.SourceBinding.GatewayPort || binding.WorkspacePath != j.SourceBinding.WorkspacePath || binding.RuntimeType != j.SourceBinding.RuntimeType {
			return fmt.Errorf("source owner or binding changed; recovery held")
		}
		if binding.GatewayID != item.SourceGatewayID {
			if j.Phase != "planned" || item.SourceGatewayID != fmt.Sprintf("pending-%d-%d", item.InstanceID, item.SourceGeneration) || binding.GatewayID != j.Assignment.GatewayID {
				return fmt.Errorf("source gateway identity changed; recovery held")
			}
			// A previously pending source may publish its canonical report after
			// preflight; keep the exact same UID/owner/port, never adopt a new source.
			item.SourceGatewayID = binding.GatewayID
			j.SourceBinding.GatewayID = binding.GatewayID
			if err = s.saveRecoveryItem(ctx, operation, item); err != nil {
				return err
			}
		}
		var protected int
		row, e := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND state NOT IN ('cancelled','completed')", item.InstanceID)
		if e == nil {
			e = row.Scan(&protected)
		}
		if e != nil {
			return e
		}
		if protected > 0 {
			return fmt.Errorf("protected Runtime upgrade is active; source was not interrupted")
		}
		row, e = s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM team_members WHERE instance_id=? AND status NOT IN ('deleted','deleting')", item.InstanceID)
		if e == nil {
			e = row.Scan(&protected)
		}
		if e != nil {
			return e
		}
		if protected > 0 {
			return fmt.Errorf("Team membership changed; coordinated recovery is required")
		}

		if j.Phase == "planned" {
			if err = s.beginRecoveryFence(ctx, operation, item); err != nil {
				return err
			}
			j.Phase = "fencing"
		}
		if err = s.assertOwner(ctx, operation); err != nil {
			return err
		}
		if err = c.assignments.FenceFaultEviction(ctx, j.Assignment, j.Scope.OperationID); err != nil {
			return err
		}
		identity := runtimeauth.Identity{InstanceID: item.InstanceID, UserID: j.SourceUserID, Generation: item.SourceGeneration, PodUID: item.SourcePodUID}
		if err = s.assertOwner(ctx, operation); err != nil {
			return err
		}
		if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
			return err
		}
		if err = s.assertOwner(ctx, operation); err != nil {
			return err
		}
		if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
			return err
		}
		if j.Phase == "fencing" {
			j.Phase = "fenced"
			if err = s.saveRecoveryItem(ctx, operation, item); err != nil {
				return err
			}
		}
		if j.Phase == "fenced" {
			// Existing authenticated stop remains preferred. Persist the fallback phase
			// before contacting either execution domain, so a lost reply is observable.
			j.Phase = "stopping"
			if err = s.saveRecoveryItem(ctx, operation, item); err != nil {
				return err
			}
			stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = c.DeleteGateway(stopCtx, j.SourceEndpoint, j.Assignment.GatewayID)
			cancel()
			if err == nil {
				j.SourceStopConfirmed = true
				j.Phase = "source_stopped"
				if err = s.saveRecoveryItem(ctx, operation, item); err != nil {
					return err
				}
			}
		}
		if j.Phase == "stopping" {
			proof, e := c.assignments.FaultEvictionReceipt(ctx, j.Assignment, j.Scope)
			if e != nil {
				return e
			}
			if proof != nil {
				j.Receipt = proof
				j.Phase = "source_stopped"
				if e = s.saveRecoveryItem(ctx, operation, item); e != nil {
					return e
				}
			}
		}
		if j.Phase == "stopping" {
			node, e := c.kube.CoreV1().Nodes().Get(ctx, j.Scope.NodeName, metav1.GetOptions{})
			if e != nil {
				return e
			}
			if string(node.UID) != j.Scope.NodeUID || node.Status.NodeInfo.BootID == "" {
				return fmt.Errorf("source node identity changed; infrastructure isolation is required")
			}
			if node.Status.NodeInfo.BootID != j.Scope.NodeBootID {
				j.Scope.NodeBootID = node.Status.NodeInfo.BootID
				if e = s.saveRecoveryItem(ctx, operation, item); e != nil {
					return e
				}
			}
			stopper, ok := s.scheduler.deployments.(faultEvictionStopper)
			if !ok {
				return fmt.Errorf("node-side fault eviction is unavailable")
			}
			if err = s.assertOwner(ctx, operation); err != nil {
				return err
			}
			storage, e := k8s.RuntimeFaultStorageIdentity(ctx, c.kube, j.SourcePod, j.Scope.WorkspaceRoot, time.Time{})
			if e != nil {
				return e
			}
			if storage != j.SourcePod.Annotations[k8s.RuntimeFaultStorageAnnotation] {
				return fmt.Errorf("source workspace storage identity changed")
			}
			receipt, err := stopper.RecoverRuntimeEviction(ctx, j.Scope, j.SourcePod)
			if err != nil {
				return err
			}
			if err = runtimeeviction.ValidateReceipt(j.Scope, receipt); err != nil {
				return err
			}
			if err = s.assertOwner(ctx, operation); err != nil {
				return err
			}
			if err = c.assignments.ConfirmFaultEvictionStopped(ctx, j.Assignment, j.Scope, receipt); err != nil {
				return err
			}
			j.Receipt = receipt
			j.Phase = "source_stopped"
			if err = s.saveRecoveryItem(ctx, operation, item); err != nil {
				return err
			}
		}
	}
	_, err := s.commitRecoveredEviction(ctx, item, operation)
	return err
}

func (s *RuntimeManagementService) processRecoveryEviction(ctx context.Context, op *RuntimeManagementOperation) error {
	cancelled, err := s.cancelRequested(ctx, op.ID)
	if err != nil {
		return err
	}
	if cancelled {
		for i := range op.Items {
			if op.Items[i].Recovery == nil || op.Items[i].Recovery.Phase != "planned" {
				return fmt.Errorf("cannot cancel a fenced fault recovery")
			}
			op.Items[i].State = "cancelled"
		}
		op.Status = "cancelled"
		return s.completeRecoveryEviction(ctx, op)
	}
	hasPending := false
	for _, item := range op.Items {
		if item.Recovery == nil {
			return fmt.Errorf("recovery item lacks its source snapshot")
		}
		if item.Recovery.Phase == "released" {
			continue
		}
		run := func() error { return s.recoverEvictedSource(ctx, op.ID, item) }
		release, err := acquireInstanceLifecycleOperation(ctx, s.scheduler.instanceRepo, item.InstanceID)
		if err == nil {
			if s.scheduler.upgrade != nil {
				err = s.scheduler.upgrade.WithInstanceMutation(ctx, item.InstanceID, run)
			} else {
				err = run()
			}
			release()
		}

		if err != nil {
			if errors.Is(err, errFaultRecoveryCancelled) {
				latest, _, _, loadErr := s.runtimeManagementOperation(ctx, op.ID)
				if loadErr != nil {
					return loadErr
				}
				return s.processRecoveryEviction(ctx, latest)
			}

			latest, _, _, loadErr := s.runtimeManagementOperation(ctx, op.ID)
			if loadErr != nil {
				return loadErr
			}
			released := false
			for i := range latest.Items {
				if latest.Items[i].InstanceID == item.InstanceID {
					if latest.Items[i].Recovery != nil && latest.Items[i].Recovery.Phase == "released" {
						released = true
						break
					}
					latest.Items[i].State = "processing"
					latest.Items[i].ErrorMessage = err.Error()
				}
			}
			if released {
				continue
			}
			// Pending Jobs stay resumable. Structural conflicts require explicit Retry;
			// neither branch discards an old owner nor releases lifecycle admission.
			if errors.Is(err, k8s.ErrRuntimeFaultEvictionPending) || errors.Is(err, models.ErrInstanceOperationInProgress) {
				hasPending = true
			}
			if e := s.save(ctx, latest); e != nil {
				return e
			}
		}
	}
	latest, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil {
		return err
	}
	unfinished := false
	for _, item := range latest.Items {
		if item.Recovery.Phase != "released" {
			unfinished = true
		}
	}
	if unfinished {
		latest.Status = "needs_attention"
		if hasPending {
			latest.Status = "pending"
		}
		// Pending state and the phase checkpoint commit together; a crash must
		// not lose an independent retry_requested write and strand the Job.
		return s.save(ctx, latest)
	}
	latest.Status = runtimeManagementStatus(latest.Items)
	return s.completeRecoveryEviction(ctx, latest)
}
