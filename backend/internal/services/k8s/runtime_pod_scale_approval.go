package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const runtimeExpansionAnnotation = "clawmanager.io/runtime-expansion"

// InspectRuntimeScaleConfiguration reads identity and configuration only. The
// caller must find a persisted approval for its digest or corroborate it with
// InspectRuntimeScale before using it to expand.
type RuntimeScaleApprovalDeploymentService interface {
	RuntimePodScaleDeploymentService
	InspectRuntimeScaleConfiguration(context.Context, RuntimePodScaleTarget) (*RuntimePodScaleSnapshot, error)
	InspectRuntimeExpansion(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) (RuntimeExpansionObservation, error)
	ScaleOutApproved(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot, int32, int) error
}

func (s *runtimeDeploymentService) InspectRuntimeScaleConfiguration(ctx context.Context, target RuntimePodScaleTarget) (*RuntimePodScaleSnapshot, error) {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	snapshot, err := s.runtimeScaleConfiguration(ctx, target, d)
	if err != nil {
		return nil, err
	}
	for _, volume := range d.Spec.Template.Spec.Volumes {
		if volume.Name != runtimeWorkspaceVolume || volume.PersistentVolumeClaim == nil {
			continue
		}
		claim, err := s.client.CoreV1().PersistentVolumeClaims(target.Namespace).Get(ctx, volume.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		pv, err := s.client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("verify Runtime workspace PV: %w", err)
		}
		bound := pv.Spec.ClaimRef
		if pv.UID == "" || pv.DeletionTimestamp != nil || bound == nil || bound.UID != claim.UID || bound.Name != claim.Name || bound.Namespace != claim.Namespace {
			return nil, fmt.Errorf("Runtime workspace PV does not bind the approved claim identity")
		}
		// Store a template fingerprint and approved resources/security without
		// copying literal environment values into the approval journal.
		fingerprint, err := runtimeScaleFingerprint([]any{d.Spec.Selector, d.Spec.Template,
			d.Labels[runtimePoolRoleLabel], d.Labels[runtimeSchedulingLabel], d.Labels[runtimeUpgradeIDLabel], d.Labels[runtimeSourceLabel],
			d.Labels[upgradeLabPurposeLabel], d.Labels[runtimeManagedPoolLabel]})
		if err != nil {
			return nil, err
		}
		storageFingerprint, err := runtimeScaleFingerprint(pv.Spec.PersistentVolumeSource)
		if err != nil {
			return nil, err
		}
		containerResources := map[string]corev1.ResourceRequirements{}
		containerSecurity := map[string]*corev1.SecurityContext{}
		for _, container := range d.Spec.Template.Spec.Containers {
			containerResources[container.Name] = container.Resources
			containerSecurity[container.Name] = container.SecurityContext
		}
		configuration := map[string]any{
			"runtime_type": target.RuntimeType, "namespace": d.Namespace, "deployment": d.Name, "deployment_uid": d.UID,
			"template_fingerprint": fingerprint, "image": snapshot.Option.ImageRef,
			"protocol": d.Spec.Template.Labels["clawmanager.io/runtime-protocol"], "slot_count": snapshot.Option.SlotCount,
			"approval_source": target.ApprovalSource, "workspace_storage_identity": snapshot.WorkspaceStorageIdentity,
			"persistent_volume_uid": pv.UID, "persistent_volume_source_fingerprint": storageFingerprint,
			"pod_resources": d.Spec.Template.Spec.Resources, "pod_security": d.Spec.Template.Spec.SecurityContext,
			"container_resources": containerResources, "container_security": containerSecurity,
		}
		raw, err := json.Marshal(configuration)
		if err != nil {
			return nil, err
		}
		snapshot.ApprovalDigest = fmt.Sprintf("%x", sha256.Sum256(raw))
		snapshot.ApprovalConfiguration = raw
		snapshot.expansion = d.Annotations[runtimeExpansionAnnotation]
		snapshot.hasAnnotations = len(d.Annotations) > 0
		return snapshot, nil
	}
	return nil, fmt.Errorf("Runtime workspace PVC is unavailable")
}

func runtimeScaleFingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

type runtimeExpansionAttempt struct {
	ID             string    `json:"id"`
	ApprovalDigest string    `json:"approval_digest"`
	Previous       int32     `json:"previous"`
	Replicas       int32     `json:"replicas"`
	Deadline       time.Time `json:"deadline"`
	Failures       int       `json:"failures"`
}

type RuntimeExpansionBaseline struct {
	ID      string
	PodUIDs []string
}

func (snapshot *RuntimePodScaleSnapshot) ExpansionAttemptID() string {
	return snapshot.expansionAttempt().ID
}

func (snapshot *RuntimePodScaleSnapshot) expansionAttempt() runtimeExpansionAttempt {
	var attempt runtimeExpansionAttempt
	if json.Unmarshal([]byte(snapshot.expansion), &attempt) != nil ||
		attempt.ApprovalDigest != snapshot.ApprovalDigest || attempt.Replicas != snapshot.Option.Replicas ||
		attempt.Previous < 1 || attempt.Previous >= attempt.Replicas || attempt.Failures < 0 || attempt.Deadline.IsZero() {
		return runtimeExpansionAttempt{}
	}
	return attempt
}

const runtimeExpansionStartupWindow = 3 * time.Minute

func runtimeExpansionPatch(snapshot *RuntimePodScaleSnapshot, replicas int32, failures int) ([]byte, error) {
	ops := []map[string]any{{"op": "test", "path": "/metadata/uid", "value": snapshot.Option.DeploymentUID}, {"op": "test", "path": "/metadata/resourceVersion", "value": snapshot.ResourceVersion}, {"op": "replace", "path": "/spec/replicas", "value": replicas}}
	if snapshot.ApprovalDigest != "" {
		if snapshot.ExpansionWriteID == "" {
			return nil, fmt.Errorf("new Runtime expansion membership must be recorded before changing replicas")
		}
		attempt, err := json.Marshal(runtimeExpansionAttempt{
			ID: snapshot.ExpansionWriteID, ApprovalDigest: snapshot.ApprovalDigest,
			Previous: snapshot.Option.Replicas, Replicas: replicas, Deadline: time.Now().UTC().Add(runtimeExpansionStartupWindow),
			Failures: failures,
		})
		if err != nil {
			return nil, err
		}
		if snapshot.hasAnnotations {
			ops = append(ops, map[string]any{"op": "add", "path": "/metadata/annotations/clawmanager.io~1runtime-expansion", "value": string(attempt)})
		} else {
			ops = append(ops, map[string]any{"op": "add", "path": "/metadata/annotations", "value": map[string]string{runtimeExpansionAnnotation: string(attempt)}})
		}
	}
	return json.Marshal(ops)
}

type RuntimeExpansionObservation struct {
	NewPodUIDs          map[string]struct{}
	StartingPodUIDs     map[string]struct{}
	AwaitingPods        bool
	ResourceBlockReason string
	Attempted           bool
	FailedAttempts      int
	RetryAfter          time.Time
	ExistingPodUIDs     []string
	StartedAt           time.Time
}

// Count current-template Pods owned through the same ReplicaSet and
// Deployment UIDs that appeared after the durable replica increment.
func (s *runtimeDeploymentService) InspectRuntimeExpansion(ctx context.Context, target RuntimePodScaleTarget, expected *RuntimePodScaleSnapshot) (RuntimeExpansionObservation, error) {
	observation := RuntimeExpansionObservation{NewPodUIDs: map[string]struct{}{}, StartingPodUIDs: map[string]struct{}{}}
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return observation, err
	}
	if string(d.UID) != expected.Option.DeploymentUID || d.ResourceVersion != expected.ResourceVersion {
		return observation, ErrRuntimeScaleOutPending
	}
	selector, err := runtimeDeploymentPodSelector(d)
	if err != nil {
		return observation, err
	}
	list, err := s.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return observation, err
	}
	replicaSets, err := s.client.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return observation, err
	}
	replicaSetUIDs, currentReplicaSets := map[string]string{}, map[string]bool{}
	for _, rs := range replicaSets.Items {
		owner := metav1.GetControllerOf(&rs)
		if owner == nil || owner.Kind != "Deployment" || owner.UID != d.UID || owner.Name != d.Name || rs.UID == "" {
			continue
		}
		replicaSetUIDs[rs.Name] = string(rs.UID)
		currentReplicaSets[rs.Name] = runtimeScaleReplicaSetMatches(&rs, d)
	}
	now := time.Now().UTC()
	attempt := expected.expansionAttempt()
	observation.Attempted = attempt.ID != "" && !attempt.Deadline.After(now.Add(runtimeExpansionStartupWindow))
	attemptStart := attempt.Deadline.Add(-runtimeExpansionStartupWindow)
	validStartup := observation.Attempted && attempt.Deadline.After(now)
	if observation.Attempted {
		observation.FailedAttempts = attempt.Failures
		observation.StartedAt = attemptStart
		observation.RetryAfter = attemptStart.Add(30 * time.Second)
	}
	membershipKnown := expected.ExpansionBaseline != nil && expected.ExpansionBaseline.ID == attempt.ID
	existing := map[string]struct{}{}
	if membershipKnown {
		for _, uid := range expected.ExpansionBaseline.PodUIDs {
			existing[uid] = struct{}{}
		}
	}
	for _, pod := range list.Items {
		ref := metav1.GetControllerOf(&pod)
		if ref == nil || ref.Kind != "ReplicaSet" || ref.UID == "" || replicaSetUIDs[ref.Name] != string(ref.UID) || pod.UID == "" || pod.DeletionTimestamp != nil {
			continue
		}
		uid := string(pod.UID)
		observation.ExistingPodUIDs = append(observation.ExistingPodUIDs, uid)
		if !currentReplicaSets[ref.Name] {
			continue
		}
		if pod.Status.Phase == corev1.PodPending {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && condition.Reason == corev1.PodReasonUnschedulable {
					observation.ResourceBlockReason = condition.Message
					if observation.ResourceBlockReason == "" {
						observation.ResourceBlockReason = condition.Reason
					}
				}
			}
		}
		_, preexisting := existing[uid]
		if !observation.Attempted || !membershipKnown || preexisting || pod.CreationTimestamp.IsZero() || pod.CreationTimestamp.Time.Before(attemptStart.Truncate(time.Second)) || pod.CreationTimestamp.After(now) {
			continue
		}
		observation.NewPodUIDs[uid] = struct{}{}
		if validStartup && len(observation.StartingPodUIDs) < int(attempt.Replicas-attempt.Previous) && runtimePodStillStarting(pod) {
			observation.StartingPodUIDs[uid] = struct{}{}
		}
	}
	sort.Strings(observation.ExistingPodUIDs)
	observation.AwaitingPods = validStartup && len(observation.NewPodUIDs) < int(attempt.Replicas-attempt.Previous)
	return observation, nil
}

func runtimeScaleReplicaSetMatches(rs *appsv1.ReplicaSet, d *appsv1.Deployment) bool {
	template := rs.Spec.Template.DeepCopy()
	delete(template.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	return apiequality.Semantic.DeepEqual(*template, d.Spec.Template)
}

func runtimePodStillStarting(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodPending && pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, status := range statuses {
			if status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
				return false
			}
			if status.State.Waiting != nil {
				switch status.State.Waiting.Reason {
				case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
					return false
				}
			}
		}
	}
	return true
}
