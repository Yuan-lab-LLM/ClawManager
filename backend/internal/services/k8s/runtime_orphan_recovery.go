package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"os"
	"path"
	"strings"
	"time"
)

const orphanFinalizer = "clawmanager.io/orphan-stop-proof"
const orphanOperation = "clawmanager.io/orphan-recovery"
const orphanControllerName = "clawmanager.io/orphan-controller-name"

type RuntimeOrphanSource struct {
	Namespace               string      `json:"namespace"`
	PodName                 string      `json:"pod_name"`
	PodUID                  string      `json:"pod_uid"`
	DeploymentName          string      `json:"deployment_name"`
	DeploymentUID           string      `json:"deployment_uid"`
	ReplicaSetName          string      `json:"replica_set_name"`
	ReplicaSetUID           string      `json:"replica_set_uid"`
	NodeName                string      `json:"node_name"`
	NodeUID                 string      `json:"node_uid"`
	NodeBootID              string      `json:"node_boot_id"`
	WorkspaceRoot           string      `json:"workspace_root"`
	StorageDigest           string      `json:"storage_digest"`
	HelperImage             string      `json:"helper_image"`
	RuntimeType             string      `json:"runtime_type"`
	TerminalResourceVersion string      `json:"terminal_resource_version,omitempty"`
	SourcePod               *corev1.Pod `json:"source_pod"`
}

func (s *RuntimeOrphanSource) Scope(operation string) runtimeeviction.PodScope {
	return runtimeeviction.PodScope{Version: 1, OperationID: operation, Namespace: s.Namespace, PodName: s.PodName, PodUID: s.PodUID, NodeName: s.NodeName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, RuntimeType: s.RuntimeType, WorkspaceRoot: s.WorkspaceRoot, ControllerUID: s.DeploymentUID, StorageDigest: s.StorageDigest, TerminalResourceVersion: s.TerminalResourceVersion}
}
func (s *runtimeDeploymentService) orphanController(ctx context.Context, source *RuntimeOrphanSource) (*appsv1.Deployment, error) {
	d, e := s.client.AppsV1().Deployments(source.Namespace).Get(ctx, source.DeploymentName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if string(d.UID) != source.DeploymentUID || d.UID == "" || d.Spec.Replicas == nil || *d.Spec.Replicas > 1 || d.Labels["clawmanager.io/runtime-type"] != source.RuntimeType && source.RuntimeType != "" {
		return nil, fmt.Errorf("orphan controller identity or replica count changed")
	}
	hpas, e := s.client.AutoscalingV2().HorizontalPodAutoscalers(source.Namespace).List(ctx, metav1.ListOptions{})
	if e != nil {
		return nil, e
	}
	for _, hpa := range hpas.Items {
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" && hpa.Spec.ScaleTargetRef.Name == d.Name {
			return nil, fmt.Errorf("autoscaled Runtime controller requires coordinated maintenance")
		}
	}
	role := d.Labels[runtimePoolRoleLabel]
	if role != "" && role != "upgrade-target" || d.Labels[upgradeLabPurposeLabel] == upgradeLabPurposeValue {
		return nil, fmt.Errorf("unknown or laboratory controller cannot be retired")
	}
	rs, e := s.client.AppsV1().ReplicaSets(source.Namespace).Get(ctx, source.ReplicaSetName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	owner := metav1.GetControllerOf(rs)
	if string(rs.UID) != source.ReplicaSetUID || owner == nil || owner.UID != d.UID || owner.Kind != "Deployment" || owner.Name != d.Name {
		return nil, fmt.Errorf("orphan ReplicaSet owner changed")
	}
	pods, e := s.orphanOwnedPods(ctx, d)
	if e != nil {
		return nil, e
	}
	if len(pods) != 1 || string(pods[0].UID) != source.PodUID {
		return nil, fmt.Errorf("controller must own only the selected Pod; refusing to interrupt a neighbor")
	}

	return d, nil
}
func (s *runtimeDeploymentService) PrepareRuntimeOrphanSource(ctx context.Context, namespace, name, uid, root string) (*RuntimeOrphanSource, error) {
	identity, e := s.ResolveRuntimePodController(ctx, namespace, name, uid)
	if e != nil {
		return nil, e
	}
	p, e := s.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if p.DeletionTimestamp != nil || p.Spec.NodeName == "" || p.ResourceVersion == "" {
		return nil, fmt.Errorf("stable scheduled source Pod required")
	}
	ref := metav1.GetControllerOf(p)
	source := &RuntimeOrphanSource{Namespace: namespace, PodName: name, PodUID: uid, DeploymentName: identity.DeploymentName, DeploymentUID: identity.DeploymentUID, ReplicaSetName: ref.Name, ReplicaSetUID: string(ref.UID), NodeName: p.Spec.NodeName, WorkspaceRoot: root, RuntimeType: identity.RuntimeType}
	d, e := s.orphanController(ctx, source)
	if e != nil {
		return nil, e
	}
	if *d.Spec.Replicas != 1 {
		return nil, fmt.Errorf("source controller must have one replica before orphan retirement")
	}
	node, e := s.client.CoreV1().Nodes().Get(ctx, p.Spec.NodeName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	ready := false
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if !ready || node.UID == "" || node.Status.NodeInfo.BootID == "" {
		return nil, fmt.Errorf("source node is not verifiably available")
	}
	source.NodeUID = string(node.UID)
	source.NodeBootID = node.Status.NodeInfo.BootID
	source.StorageDigest, e = RuntimeFaultStorageIdentity(ctx, s.client, p, root, p.CreationTimestamp.Time)
	if e != nil {
		return nil, e
	}
	image := strings.TrimSpace(os.Getenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE"))
	if image == "" {
		app, e := s.client.AppsV1().Deployments(namespace).Get(ctx, "clawmanager-app", metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		for _, c := range app.Spec.Template.Spec.Containers {
			if strings.Contains(c.Image, "@sha256:") {
				if image != "" && image != c.Image {
					return nil, fmt.Errorf("ambiguous helper image")
				}
				image = c.Image
			}
		}
	}
	if !strings.Contains(image, "@sha256:") {
		return nil, fmt.Errorf("immutable recovery helper image required")
	}
	source.HelperImage = image
	// Journal only the fixed identity and PVC mapping, never ambient credentials.
	clean := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace, UID: p.UID, Annotations: map[string]string{RuntimeFaultHelperImageAnnotation: image, RuntimeFaultStorageAnnotation: source.StorageDigest, orphanControllerName: source.DeploymentName}}, Spec: corev1.PodSpec{NodeName: p.Spec.NodeName, Tolerations: p.Spec.Tolerations}}
	for _, v := range p.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			clean.Spec.Volumes = append(clean.Spec.Volumes, *v.DeepCopy())
		}
	}
	for _, c := range p.Spec.Containers {
		if c.Name != "runtime" && !strings.HasPrefix(c.Name, "slot-") {
			continue
		}
		copy := corev1.Container{Name: c.Name, Image: c.Image}
		for _, env := range c.Env {
			if env.Name == "RUNTIME_WORKSPACE_ROOT" || env.Name == "RUNTIME_GATEWAY_PORT_START" {
				if env.ValueFrom != nil {
					return nil, fmt.Errorf("source identity environment must be fixed")
				}
				copy.Env = append(copy.Env, env)
			}
		}
		for _, mount := range c.VolumeMounts {
			for _, v := range clean.Spec.Volumes {
				if mount.Name == v.Name {
					copy.VolumeMounts = append(copy.VolumeMounts, mount)
				}
			}
		}
		clean.Spec.Containers = append(clean.Spec.Containers, copy)
	}
	if len(clean.Spec.Containers) == 0 {
		return nil, fmt.Errorf("source runtime containers unavailable")
	}
	source.SourcePod = clean
	return source, nil
}
func (s *runtimeDeploymentService) RetireRuntimeOrphanController(ctx context.Context, source *RuntimeOrphanSource, operation string) (bool, error) {
	d, e := s.orphanController(ctx, source)
	if e != nil {
		return false, e
	}
	p, e := s.client.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if e != nil {
		return false, fmt.Errorf("old Pod must remain inspectable until terminal proof: %w", e)
	}
	if string(p.UID) != source.PodUID || p.Spec.NodeName != source.NodeName {
		return false, fmt.Errorf("orphan Pod identity changed")
	}
	ref := metav1.GetControllerOf(p)
	if ref == nil || ref.Kind != "ReplicaSet" || ref.Name != source.ReplicaSetName || string(ref.UID) != source.ReplicaSetUID {
		return false, fmt.Errorf("orphan Pod controller changed")
	}
	if old := p.Annotations[orphanOperation]; old != "" && old != operation {
		return false, fmt.Errorf("another orphan operation owns the Pod")
	}
	owned := false
	for _, f := range p.Finalizers {
		if f == orphanFinalizer {
			owned = true
		}
	}
	if owned && p.Annotations[orphanOperation] != operation {
		return false, fmt.Errorf("Pod finalizer has another operation owner")
	}
	if p.DeletionTimestamp != nil && !owned {
		return false, fmt.Errorf("Pod began deletion without retained proof barrier")
	}
	if !owned {
		p.Finalizers = append(p.Finalizers, orphanFinalizer)
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations[orphanOperation] = operation
		p, e = s.client.CoreV1().Pods(source.Namespace).Update(ctx, p, metav1.UpdateOptions{})
		if e != nil {
			return false, e
		}
	}
	if d.Annotations[orphanOperation] != "" && d.Annotations[orphanOperation] != operation {
		return false, fmt.Errorf("another operation owns controller retirement")
	}
	if *d.Spec.Replicas != 0 {
		zero := int32(0)
		d.Spec.Replicas = &zero
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[orphanOperation] = operation
		if d.Labels == nil {
			d.Labels = map[string]string{}
		}
		d.Labels[runtimeSchedulingLabel] = "false"
		if d, e = s.client.AppsV1().Deployments(source.Namespace).Update(ctx, d, metav1.UpdateOptions{}); e != nil {
			return false, e
		}
	}
	if p.DeletionTimestamp == nil {
		if e = s.orphanControllerScaleObserved(ctx, d); e != nil {
			return false, e
		}
		podUID := types.UID(source.PodUID)
		if e = s.client.CoreV1().Pods(source.Namespace).Delete(ctx, source.PodName, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &podUID}}); e != nil {
			return false, e
		}
		return false, nil
	}
	if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
		return false, nil
	}
	if p.ResourceVersion == "" {
		return false, fmt.Errorf("terminal Pod resource version missing")
	}
	if e = s.orphanControllerConverged(ctx, d); e != nil {
		return false, e
	}
	source.TerminalResourceVersion = p.ResourceVersion
	return true, nil
}
func (s *runtimeDeploymentService) FinalizeRuntimeOrphanSource(ctx context.Context, source *RuntimeOrphanSource) error {
	p, e := s.client.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if apierrors.IsNotFound(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if string(p.UID) != source.PodUID {
		return fmt.Errorf("retired Pod name was reused")
	}
	if source.SourcePod == nil || source.SourcePod.Annotations[orphanOperation] == "" || p.Annotations[orphanOperation] != source.SourcePod.Annotations[orphanOperation] {
		return fmt.Errorf("retired Pod finalizer operation owner changed")
	}
	if p.DeletionTimestamp == nil || p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
		return fmt.Errorf("retired Pod termination barrier is not terminal")
	}
	finalizers := []string{}
	for _, f := range p.Finalizers {
		if f != orphanFinalizer {
			finalizers = append(finalizers, f)
		}
	}
	p.Finalizers = finalizers
	_, e = s.client.CoreV1().Pods(source.Namespace).Update(ctx, p, metav1.UpdateOptions{})
	return e
}
func (s *runtimeDeploymentService) RecoverRuntimeOrphan(ctx context.Context, scope runtimeeviction.PodScope, pod *corev1.Pod) (*runtimeeviction.PodReceipt, error) {
	if e := scope.Validate(); e != nil {
		return nil, e
	}
	node, e := s.client.CoreV1().Nodes().Get(ctx, scope.NodeName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if string(node.UID) != scope.NodeUID || node.Status.NodeInfo.BootID != scope.NodeBootID {
		return nil, fmt.Errorf("orphan source node boot identity changed")
	}
	if e = s.verifyOrphanControllerStopped(ctx, scope, pod); e != nil {
		return nil, e
	}
	storage, e := RuntimeFaultStorageIdentity(ctx, s.client, pod, scope.WorkspaceRoot, time.Time{})
	if e != nil {
		return nil, e
	}
	if storage != scope.StorageDigest {
		return nil, fmt.Errorf("orphan source storage identity changed")
	}
	expected, e := buildRuntimeOrphanJob(scope, pod)
	if e != nil {
		return nil, e
	}
	digest := runtimeeviction.PodScopeDigest(scope)

	jobs := s.client.BatchV1().Jobs(scope.Namespace)
	job, e := jobs.Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(e) {
		job, e = jobs.Create(ctx, expected, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(e) {
			job, e = jobs.Get(ctx, expected.Name, metav1.GetOptions{})
		}
	}
	if e != nil {
		return nil, e
	}
	if job.Annotations["clawmanager.io/recovery-scope"] != digest || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
		return nil, fmt.Errorf("orphan helper Job identity conflict")
	}
	if job.DeletionTimestamp != nil {
		return nil, ErrRuntimeFaultEvictionPending
	}
	pods, e := s.client.CoreV1().Pods(scope.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if e != nil {
		return nil, e
	}
	active := false
	diagnostic := "no safe helper diagnostic; inspect source node"
	for _, p := range pods.Items {
		owner := metav1.GetControllerOf(&p)
		if owner != nil && owner.Kind == "Job" && owner.Name == job.Name && owner.UID != job.UID && (p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed) {
			continue
		}
		if owner == nil || owner.Kind != "Job" || owner.Name != job.Name || owner.UID != job.UID || job.UID == "" || !trustedRecoveryTemplate(p.Spec, expected.Spec.Template.Spec) {
			return nil, fmt.Errorf("orphan helper Pod identity conflict")
		}
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			active = true
		}
		for _, st := range p.Status.ContainerStatuses {
			if st.Name != "recovery" || st.State.Terminated == nil {
				continue
			}
			if st.State.Terminated.ExitCode != 0 {
				if m, e := runtimeeviction.ParseFailureJSON([]byte(st.State.Terminated.Message)); e == nil {
					diagnostic = m
				}
				continue
			}
			var receipt runtimeeviction.PodReceipt
			if e = json.Unmarshal([]byte(st.State.Terminated.Message), &receipt); e != nil {
				return nil, e
			}
			if e = runtimeeviction.ValidatePodReceipt(scope, &receipt); e != nil {
				return nil, e
			}
			return &receipt, nil
		}
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if active || job.Status.Active > 0 {
				return nil, ErrRuntimeFaultEvictionPending
			}
			const marker = "clawmanager.io/recovery-failure-observed"
			if job.UID == "" {
				return nil, fmt.Errorf("failed orphan helper Job UID missing")
			}
			if job.Annotations[marker] != string(job.UID) {
				job.Annotations[marker] = string(job.UID)
				if _, e = jobs.Update(ctx, job, metav1.UpdateOptions{}); e != nil {
					return nil, e
				}
				return nil, fmt.Errorf("orphan helper failed: %s; Retry recreates exact failed Job", diagnostic)
			}
			uid := job.UID
			policy := metav1.DeletePropagationForeground
			if e = jobs.Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy}); e != nil && !apierrors.IsNotFound(e) {
				return nil, e
			}
			return nil, ErrRuntimeFaultEvictionPending
		}
	}
	return nil, ErrRuntimeFaultEvictionPending
}

// Local ledger inspection is allowed only when this running management Pod and
// the selected source mount the same PVC at the same canonical root/subpath.
func VerifyRuntimeOrphanManagementWorkspace(ctx context.Context, client interface {
	CoreV1() typedcorev1.CoreV1Interface
}, source *RuntimeOrphanSource) error {
	name := os.Getenv("POD_NAME")
	if name == "" {
		name = os.Getenv("HOSTNAME")
	}
	if name == "" {
		name, _ = os.Hostname()
	}
	manager, e := client.CoreV1().Pods(source.Namespace).Get(ctx, name, metav1.GetOptions{})
	if e != nil {
		return fmt.Errorf("running management workspace mapping unavailable: %w", e)
	}
	mapping := func(p *corev1.Pod) string {
		result := ""
		for _, c := range p.Spec.Containers {
			parts := []string{}
			for _, target := range []string{source.WorkspaceRoot, path.Join(source.WorkspaceRoot, ".clawmanager-slot-locks")} {
				var chosen *corev1.VolumeMount
				ambiguous := false
				for i := range c.VolumeMounts {
					m := &c.VolumeMounts[i]
					if target != m.MountPath && !strings.HasPrefix(target, strings.TrimSuffix(m.MountPath, "/")+"/") {
						continue
					}
					if chosen != nil && len(chosen.MountPath) == len(m.MountPath) {
						ambiguous = true
					}
					if chosen == nil || len(m.MountPath) > len(chosen.MountPath) {
						chosen = m
						ambiguous = false
					}
				}
				if chosen == nil {
					parts = nil
					break
				}
				if ambiguous || chosen.ReadOnly || chosen.SubPathExpr != "" || path.IsAbs(chosen.SubPath) || chosen.SubPath == ".." || strings.HasPrefix(chosen.SubPath, "../") {
					return ""
				}
				claim := ""
				for _, v := range p.Spec.Volumes {
					if v.Name == chosen.Name && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly {
						claim = v.PersistentVolumeClaim.ClaimName
					}
				}
				if claim == "" {
					return ""
				}
				relative := strings.TrimPrefix(strings.TrimPrefix(target, chosen.MountPath), "/")
				parts = append(parts, claim+"/"+path.Join(chosen.SubPath, relative))
			}
			if len(parts) == 0 {
				continue
			}
			encoded, _ := json.Marshal(parts)
			next := string(encoded)
			if result != "" && result != next {
				return ""
			}
			result = next
		}
		return result
	}

	if current, want := mapping(manager), mapping(source.SourcePod); current == "" || current != want {
		return fmt.Errorf("management and source protected ledgers do not share verified PVC mapping")
	}
	return nil
}

func buildRuntimeOrphanJob(scope runtimeeviction.PodScope, pod *corev1.Pod) (*batchv1.Job, error) {
	if e := scope.Validate(); e != nil {
		return nil, e
	}
	if len(pod.Spec.Containers) == 0 {
		return nil, fmt.Errorf("source runtime slot missing")
	}
	dummy := runtimeeviction.Scope{Version: 1, OperationID: scope.OperationID, Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, ContainerName: "slot-0", RuntimeType: scope.RuntimeType, GatewayID: "gw-1-1", WorkspaceRoot: scope.WorkspaceRoot, InstanceID: 1, UserID: 1, Generation: 1}
	volumeSource := pod.DeepCopy()
	volumeSource.Spec.Containers[0].Name = "slot-0"
	expected, e := buildRuntimeEvictionJob(dummy, volumeSource, pod.Annotations[RuntimeFaultHelperImageAnnotation])
	if e != nil {
		return nil, e
	}
	digest := runtimeeviction.PodScopeDigest(scope)
	expected.Name = "runtime-orphan-" + digest[:40]
	expected.Annotations["clawmanager.io/recovery-scope"] = digest
	expected.Spec.Template.Labels["clawmanager.io/recovery-scope"] = digest[:40]
	body, _ := json.Marshal(scope)
	expected.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "RECOVERY_POD_SCOPE", Value: string(body)}}
	return expected, nil
}

// Observe a completed old-scope reply before a same-node boot rebase. Read-only:
// the old success remains historical proof even if its helper reply was lost.
func (s *runtimeDeploymentService) ObserveRuntimeOrphanReceipt(ctx context.Context, scope runtimeeviction.PodScope, pod *corev1.Pod) (*runtimeeviction.PodReceipt, error) {
	expected, e := buildRuntimeOrphanJob(scope, pod)
	if e != nil {
		return nil, e
	}
	job, e := s.client.BatchV1().Jobs(scope.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if job.Annotations["clawmanager.io/recovery-scope"] != runtimeeviction.PodScopeDigest(scope) || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
		return nil, fmt.Errorf("old orphan helper Job identity conflict")
	}
	pods, e := s.client.CoreV1().Pods(scope.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if e != nil {
		return nil, e
	}
	for _, p := range pods.Items {
		owner := metav1.GetControllerOf(&p)
		if owner == nil || owner.Kind != "Job" || owner.Name != job.Name || owner.UID != job.UID || job.UID == "" || !trustedRecoveryTemplate(p.Spec, expected.Spec.Template.Spec) {
			return nil, fmt.Errorf("old orphan helper Pod identity conflict")
		}
		for _, st := range p.Status.ContainerStatuses {
			if st.Name == "recovery" && st.State.Terminated != nil && st.State.Terminated.ExitCode == 0 {
				var r runtimeeviction.PodReceipt
				if e = json.Unmarshal([]byte(st.State.Terminated.Message), &r); e != nil {
					return nil, e
				}
				if e = runtimeeviction.ValidatePodReceipt(scope, &r); e != nil {
					return nil, e
				}
				return &r, nil
			}
		}
	}
	return nil, nil
}

func (s *runtimeDeploymentService) verifyOrphanControllerStopped(ctx context.Context, scope runtimeeviction.PodScope, pod *corev1.Pod) error {
	name := pod.Annotations[orphanControllerName]
	if name == "" {
		return fmt.Errorf("source controller journal name missing")
	}
	d, e := s.client.AppsV1().Deployments(scope.Namespace).Get(ctx, name, metav1.GetOptions{})
	if e != nil {
		return e
	}
	if string(d.UID) != scope.ControllerUID || d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.Annotations[orphanOperation] != scope.OperationID || d.Labels[runtimeSchedulingLabel] != "false" {
		return fmt.Errorf("source controller retirement fence changed")
	}
	if e = s.orphanControllerConverged(ctx, d); e != nil {
		return e
	}
	pods, e := s.orphanOwnedPods(ctx, d)
	if e != nil {
		return e
	}
	for _, current := range pods {
		if string(current.UID) != scope.PodUID {
			return fmt.Errorf("retired controller created another Pod; its UID is outside this recovery")
		}
	}

	return nil
}

func (s *runtimeDeploymentService) orphanOwnedPods(ctx context.Context, d *appsv1.Deployment) ([]corev1.Pod, error) {
	sets, e := s.client.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{})
	if e != nil {
		return nil, e
	}
	owned := map[types.UID]bool{}
	for _, rs := range sets.Items {
		owner := metav1.GetControllerOf(&rs)
		if owner != nil && owner.Kind == "Deployment" && owner.UID == d.UID {
			owned[rs.UID] = true
		}
	}
	pods, e := s.client.CoreV1().Pods(d.Namespace).List(ctx, metav1.ListOptions{})
	if e != nil {
		return nil, e
	}
	result := []corev1.Pod{}
	for _, p := range pods.Items {
		owner := metav1.GetControllerOf(&p)
		if owner != nil && owner.Kind == "ReplicaSet" && owned[owner.UID] {
			result = append(result, p)
		}
	}
	return result, nil
}
func (s *runtimeDeploymentService) orphanControllerConverged(ctx context.Context, d *appsv1.Deployment) error {
	if d.Status.ObservedGeneration < d.Generation || d.Status.Replicas != 0 {
		return fmt.Errorf("%w: waiting for exact retired controller to converge", ErrRuntimeFaultEvictionPending)
	}
	sets, e := s.client.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, rs := range sets.Items {
		owner := metav1.GetControllerOf(&rs)
		if owner != nil && owner.Kind == "Deployment" && owner.UID == d.UID {
			if rs.Spec.Replicas == nil || *rs.Spec.Replicas != 0 || rs.Status.Replicas != 0 || rs.Status.ObservedGeneration < rs.Generation {
				return fmt.Errorf("%w: waiting for retired ReplicaSet zero-replica acknowledgement", ErrRuntimeFaultEvictionPending)
			}
		}
	}
	return nil
}

// Wait for the controller's requested generation and every owned ReplicaSet to
// acknowledge desired zero before explicitly deleting the selected Pod. Actual
// zero counts and Pod terminal status remain separate, later stop barriers.
func (s *runtimeDeploymentService) orphanControllerScaleObserved(ctx context.Context, d *appsv1.Deployment) error {
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 0 || d.Status.ObservedGeneration < d.Generation {
		return fmt.Errorf("%w: waiting for requested zero-replica Deployment generation", ErrRuntimeFaultEvictionPending)
	}
	sets, e := s.client.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, rs := range sets.Items {
		owner := metav1.GetControllerOf(&rs)
		if owner != nil && owner.Kind == "Deployment" && owner.UID == d.UID {
			if rs.Spec.Replicas == nil || *rs.Spec.Replicas != 0 || rs.Status.ObservedGeneration < rs.Generation {
				return fmt.Errorf("%w: waiting for owned ReplicaSet desired-zero acknowledgement", ErrRuntimeFaultEvictionPending)
			}
		}
	}
	return nil
}
