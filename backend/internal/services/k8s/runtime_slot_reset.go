package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"strings"
)

func RuntimePhysicalResetHelperImage() string {
	return strings.TrimSpace(os.Getenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE"))
}

func BuildRuntimeSlotResetJob(scope runtimeeviction.SlotScope, image string) (*batchv1.Job, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	parts := strings.Split(image, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) != 64 {
		return nil, errors.New("physical reset helper requires immutable image")
	}
	if decoded, err := hex.DecodeString(parts[1]); err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != parts[1] {
		return nil, errors.New("invalid immutable reset helper digest")
	}
	no := false
	uid := int64(10001)
	gid := int64(0)
	attempts := int32(0)
	deadline := int64(240)
	ttl := int32(86400)
	socket := corev1.HostPathSocket
	file := corev1.HostPathFile
	b, _ := json.Marshal(scope)
	digest := runtimeeviction.SlotScopeDigest(scope)
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "runtime-slot-reset-" + digest[:32], Namespace: scope.Namespace, Annotations: map[string]string{"clawmanager.io/slot-reset-scope": digest}, Labels: map[string]string{"clawmanager.io/component": "runtime-slot-reset"}}, Spec: batchv1.JobSpec{BackoffLimit: &attempts, ActiveDeadlineSeconds: &deadline, TTLSecondsAfterFinished: &ttl, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"clawmanager.io/component": "runtime-slot-reset"}}, Spec: corev1.PodSpec{NodeName: scope.NodeName, RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: &no, Volumes: []corev1.Volume{
		{Name: "reset-cri", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/containerd.sock", Type: &socket}}},
		{Name: "reset-boot", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/proc/sys/kernel/random/boot_id", Type: &file}}},
	}, Containers: []corev1.Container{{Name: "recovery", Image: image, ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"/usr/local/bin/runtime-eviction-helper"}, Env: []corev1.EnvVar{{Name: "RECOVERY_SLOT_SCOPE", Value: string(b)}}, VolumeMounts: []corev1.VolumeMount{{Name: "reset-cri", MountPath: "/run/containerd/containerd.sock"}, {Name: "reset-boot", MountPath: "/host-boot-id", ReadOnly: true}}, SecurityContext: &corev1.SecurityContext{RunAsUser: &uid, RunAsGroup: &gid, Privileged: &no, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile}}}}}}, nil
}

func (s *runtimeDeploymentService) ResetPhysicalSlot(ctx context.Context, scope runtimeeviction.SlotScope, image string) (*runtimeeviction.SlotReceipt, error) {
	expected, err := BuildRuntimeSlotResetJob(scope, image)
	if err != nil {
		return nil, err
	}
	node, err := s.client.CoreV1().Nodes().Get(ctx, scope.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(node.UID) != scope.NodeUID || node.Status.NodeInfo.BootID != scope.NodeBootID {
		return nil, errors.New("physical reset node identity changed")
	}
	jobs := s.client.BatchV1().Jobs(scope.Namespace)
	job, err := jobs.Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		job, err = jobs.Create(ctx, expected, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	if job.Annotations["clawmanager.io/slot-reset-scope"] != runtimeeviction.SlotScopeDigest(scope) || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
		return nil, errors.New("physical reset helper scope changed")
	}
	receipt, readErr := s.readPhysicalSlotResetReceipt(ctx, scope, expected, job)
	if readErr == nil {
		return receipt, readErr
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue && job.Status.Active == 0 {
			if errors.Is(readErr, ErrRuntimeFaultEvictionPending) {
				readErr = fmt.Errorf("physical reset helper failed without a valid exit receipt; inspect exact helper %s", job.Name)
			}
			const observed = "clawmanager.io/slot-reset-failure-observed"
			if job.Annotations[observed] != string(job.UID) {
				job.Annotations[observed] = string(job.UID)
				if _, err = jobs.Update(ctx, job, metav1.UpdateOptions{}); err != nil {
					return nil, err
				}
				return nil, readErr
			}
			if job.UID == "" {
				return nil, errors.New("failed physical reset helper has no UID")
			}
			uid := job.UID
			policy := metav1.DeletePropagationForeground
			if err = jobs.Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
				return nil, err
			}
			return nil, ErrRuntimeFaultEvictionPending
		}
	}
	return nil, readErr
}

// This lookup never starts a new helper or requires the source to still exist.
// An exact owned Job's durable receipt survives further kubelet restarts.
func (s *runtimeDeploymentService) PhysicalSlotResetReceipt(ctx context.Context, scope runtimeeviction.SlotScope, image string) (*runtimeeviction.SlotReceipt, error) {
	expected, err := BuildRuntimeSlotResetJob(scope, image)
	if err != nil {
		return nil, err
	}
	job, err := s.client.BatchV1().Jobs(scope.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrRuntimeFaultEvictionPending
	}
	if err != nil {
		return nil, err
	}
	return s.readPhysicalSlotResetReceipt(ctx, scope, expected, job)
}

func (s *runtimeDeploymentService) readPhysicalSlotResetReceipt(ctx context.Context, scope runtimeeviction.SlotScope, expected, job *batchv1.Job) (*runtimeeviction.SlotReceipt, error) {
	if job.Annotations["clawmanager.io/slot-reset-scope"] != runtimeeviction.SlotScopeDigest(scope) || !trustedRecoveryTemplate(job.Spec.Template.Spec, expected.Spec.Template.Spec) {
		return nil, errors.New("physical reset helper scope changed")
	}
	pods, err := s.client.CoreV1().Pods(scope.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.Kind != "Job" || owner.UID != job.UID || job.UID == "" || !trustedRecoveryTemplate(pod.Spec, expected.Spec.Template.Spec) {
			return nil, errors.New("physical reset helper Pod identity changed")
		}
		for _, st := range pod.Status.ContainerStatuses {
			if st.Name != "recovery" || st.State.Terminated == nil {
				continue
			}
			t := st.State.Terminated
			if t.ExitCode != 0 {
				return nil, fmt.Errorf("physical reset helper failed; inspect exact helper %s", job.Name)
			}
			var receipt runtimeeviction.SlotReceipt
			if json.Unmarshal([]byte(t.Message), &receipt) != nil {
				return nil, errors.New("invalid physical reset helper receipt")
			}
			if err = runtimeeviction.ValidateSlotReceipt(scope, &receipt); err != nil {
				return nil, err
			}
			return &receipt, nil
		}
	}
	return nil, ErrRuntimeFaultEvictionPending
}
