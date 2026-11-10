package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path"
	"reflect"
	"strings"
)

const RuntimeFaultHelperImageAnnotation = "clawmanager.io/fault-eviction-helper-image"

var ErrRuntimeFaultEvictionPending = errors.New("runtime fault eviction helper pending")

func buildRuntimeEvictionJob(s runtimeeviction.Scope, p *corev1.Pod, image string) (*batchv1.Job, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	if p == nil || p.Namespace != s.Namespace || p.Name != s.PodName || string(p.UID) != s.PodUID || p.Spec.NodeName != s.NodeName {
		return nil, fmt.Errorf("source Pod identity differs")
	}
	if !strings.Contains(image, "@sha256:") {
		return nil, fmt.Errorf("helper image must be immutable")
	}
	var source *corev1.Container
	for i := range p.Spec.Containers {
		if p.Spec.Containers[i].Name == s.ContainerName {
			source = &p.Spec.Containers[i]
		}
	}
	if source == nil {
		return nil, fmt.Errorf("source slot missing")
	}
	mounts := []corev1.VolumeMount{}
	volumes := []corev1.Volume{}
	used := map[string]bool{}
	target := path.Join(s.WorkspaceRoot, ".clawmanager-slot-locks")
	best := ""
	for _, m := range source.VolumeMounts {
		if target == m.MountPath || strings.HasPrefix(target, m.MountPath+"/") {
			if len(m.MountPath) > len(best) {
				best = m.MountPath
			}
		}
	}
	if best == "" {
		return nil, fmt.Errorf("ledger mount missing")
	}
	for _, m := range source.VolumeMounts {
		if m.MountPath != best {
			continue
		}
		if m.ReadOnly || m.SubPathExpr != "" {
			return nil, fmt.Errorf("ledger mount is not fixed writable storage")
		}
		for _, v := range p.Spec.Volumes {
			if v.Name == m.Name {
				if v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ReadOnly {
					return nil, fmt.Errorf("ledger must use original PVC")
				}
				if !used[v.Name] {
					volumes = append(volumes, *v.DeepCopy())
					used[v.Name] = true
				}
			}
		}
		if !used[m.Name] {
			return nil, fmt.Errorf("ledger volume unavailable")
		}
		mounts = append(mounts, m)
	}
	if len(mounts) != 1 {
		return nil, fmt.Errorf("ambiguous ledger mount")
	}
	for _, n := range []string{"recovery-cri", "recovery-boot"} {
		if used[n] {
			return nil, fmt.Errorf("reserved helper volume collision")
		}
	}
	socketType := corev1.HostPathSocket
	fileType := corev1.HostPathFile
	volumes = append(volumes, corev1.Volume{Name: "recovery-cri", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/containerd.sock", Type: &socketType}}}, corev1.Volume{Name: "recovery-boot", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/proc/sys/kernel/random/boot_id", Type: &fileType}}})
	mounts = append(mounts, corev1.VolumeMount{Name: "recovery-cri", MountPath: "/run/containerd/containerd.sock"}, corev1.VolumeMount{Name: "recovery-boot", MountPath: "/host-boot-id", ReadOnly: true})
	b, _ := json.Marshal(s)
	no := false
	yes := false
	uid := int64(0)
	procMount := corev1.DefaultProcMount
	zero := int32(3)
	deadline := int64(600)
	ttl := int32(86400)
	digest := runtimeeviction.ScopeDigest(s)
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "runtime-evict-" + digest[:40], Namespace: s.Namespace, Labels: map[string]string{"clawmanager.io/component": "runtime-recovery"}, Annotations: map[string]string{"clawmanager.io/recovery-scope": digest}}, Spec: batchv1.JobSpec{BackoffLimit: &zero, ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"clawmanager.io/recovery-scope": digest[:40], "clawmanager.io/component": "runtime-recovery"}}, Spec: corev1.PodSpec{NodeName: s.NodeName, AutomountServiceAccountToken: &no, RestartPolicy: corev1.RestartPolicyNever, Tolerations: p.Spec.Tolerations, Volumes: volumes, Containers: []corev1.Container{{Name: "recovery", Image: image, ImagePullPolicy: corev1.PullIfNotPresent, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")}}, Command: []string{"/usr/local/bin/runtime-eviction-helper"}, Env: []corev1.EnvVar{{Name: "RECOVERY_SCOPE", Value: string(b)}}, VolumeMounts: mounts, SecurityContext: &corev1.SecurityContext{RunAsUser: &uid, ProcMount: &procMount, Privileged: &yes, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile}}}}}}, nil
}
func trustedRecoveryTemplate(actual, expected corev1.PodSpec) bool {
	if actual.NodeName != expected.NodeName || actual.RestartPolicy != expected.RestartPolicy || actual.AutomountServiceAccountToken == nil || *actual.AutomountServiceAccountToken || actual.HostPID || actual.HostIPC || actual.HostNetwork || len(actual.InitContainers) != 0 || len(actual.EphemeralContainers) != 0 || len(actual.Containers) != 1 {
		return false
	}
	a, e := *actual.Containers[0].DeepCopy(), *expected.Containers[0].DeepCopy()
	// Normalize only documented harmless API defaults; compare the whole container
	// so injected lifecycle hooks, devices, probes, arguments or credentials fail.
	normalize := func(c *corev1.Container) {
		if c.ImagePullPolicy == "" {
			c.ImagePullPolicy = corev1.PullIfNotPresent
		}
		if c.TerminationMessagePath == "" {
			c.TerminationMessagePath = "/dev/termination-log"
		}
		if c.TerminationMessagePolicy == "" {
			c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
		}
		if c.SecurityContext != nil && c.SecurityContext.SeccompProfile != nil && c.SecurityContext.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault && c.SecurityContext.SeccompProfile.LocalhostProfile == nil {
			c.SecurityContext.SeccompProfile = nil
		}
		if c.SecurityContext != nil && c.SecurityContext.ProcMount != nil && *c.SecurityContext.ProcMount == corev1.DefaultProcMount {
			c.SecurityContext.ProcMount = nil
		}
	}
	normalize(&a)
	normalize(&e)
	if actual.SecurityContext != nil {
		security := actual.SecurityContext.DeepCopy()
		if security.SeccompProfile != nil && security.SeccompProfile.Type == corev1.SeccompProfileTypeRuntimeDefault && security.SeccompProfile.LocalhostProfile == nil {
			security.SeccompProfile = nil
		}
		if !reflect.DeepEqual(*security, corev1.PodSecurityContext{}) {
			return false
		}
	}
	return reflect.DeepEqual(a, e) && reflect.DeepEqual(actual.Volumes, expected.Volumes)

}
func (svc *runtimeDeploymentService) RecoverRuntimeEviction(ctx context.Context, s runtimeeviction.Scope, sourcePod *corev1.Pod) (*runtimeeviction.Receipt, error) {
	node, e := svc.client.CoreV1().Nodes().Get(ctx, s.NodeName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if string(node.UID) != s.NodeUID || node.Status.NodeInfo.BootID != s.NodeBootID {
		return nil, fmt.Errorf("source node identity changed")
	}
	image := ""
	if sourcePod != nil {
		image = strings.TrimSpace(sourcePod.Annotations[RuntimeFaultHelperImageAnnotation])
	}
	if image == "" {
		image = strings.TrimSpace(os.Getenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE"))
	}
	if image == "" {
		dep, e := svc.client.AppsV1().Deployments(s.Namespace).Get(ctx, "clawmanager-app", metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		for _, c := range dep.Spec.Template.Spec.Containers {
			if strings.Contains(c.Image, "@sha256:") {
				if image != "" && image != c.Image {
					return nil, fmt.Errorf("ambiguous application image")
				}
				image = c.Image
			}
		}
	}
	expected, e := buildRuntimeEvictionJob(s, sourcePod, image)
	if e != nil {
		return nil, e
	}
	jobs := svc.client.BatchV1().Jobs(s.Namespace)
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
	if job.Annotations["clawmanager.io/recovery-scope"] != runtimeeviction.ScopeDigest(s) || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
		return nil, fmt.Errorf("recovery Job identity conflict")
	}
	if job.DeletionTimestamp != nil {
		return nil, ErrRuntimeFaultEvictionPending
	}
	pods, e := svc.client.CoreV1().Pods(s.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if e != nil {
		return nil, e
	}
	failureDiagnostic := "helper returned no safe diagnostic; inspect node and helper health"
	active := false
	for _, p := range pods.Items {
		owner := metav1.GetControllerOf(&p)
		if owner != nil && owner.Kind == "Job" && owner.Name == job.Name && owner.UID != job.UID && (p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodSucceeded) {
			continue
		}
		if owner == nil || owner.Kind != "Job" || owner.Name != job.Name || owner.UID != job.UID || job.UID == "" || !trustedRecoveryTemplate(p.Spec, expected.Spec.Template.Spec) {
			return nil, fmt.Errorf("recovery Pod identity conflict")
		}
		if p.Status.Phase != corev1.PodFailed && p.Status.Phase != corev1.PodSucceeded {
			active = true
		}
		for _, st := range p.Status.ContainerStatuses {
			if st.Name != "recovery" || st.State.Terminated == nil {
				continue
			}
			if st.State.Terminated.ExitCode != 0 {
				if diagnostic, err := runtimeeviction.ParseFailureJSON([]byte(st.State.Terminated.Message)); err == nil {
					failureDiagnostic = diagnostic
				}
				continue
			}
			var r runtimeeviction.Receipt
			if e = json.Unmarshal([]byte(st.State.Terminated.Message), &r); e != nil {
				return nil, fmt.Errorf("invalid helper receipt: %w", e)
			}
			if e = runtimeeviction.ValidateReceipt(s, &r); e != nil {
				return nil, e
			}
			return &r, nil
		}
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if active || job.Status.Active > 0 {
				return nil, ErrRuntimeFaultEvictionPending
			}
			if job.UID == "" {
				return nil, fmt.Errorf("failed helper Job has no UID")
			}
			const observed = "clawmanager.io/recovery-failure-observed"
			if job.Annotations[observed] != string(job.UID) {
				job.Annotations[observed] = string(job.UID)
				if _, e = jobs.Update(ctx, job, metav1.UpdateOptions{}); e != nil {
					return nil, e
				}
				return nil, fmt.Errorf("recovery helper Job failed (%s): %s; Retry recreates this exact failed helper Job", c.Reason, failureDiagnostic)
			}
			uid := job.UID
			propagation := metav1.DeletePropagationForeground
			if e = jobs.Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &propagation}); e != nil && !apierrors.IsNotFound(e) {
				return nil, e
			}
			return nil, ErrRuntimeFaultEvictionPending
		}
	}
	return nil, ErrRuntimeFaultEvictionPending
}
