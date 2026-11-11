package services

import (
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
	"strings"
)

// This initial recovery path is limited to OpenCode Lite, which cannot admit
// Browser Workers. It proves no configuration, durable exposure history, or
// current/orphan browser resources exist. Async deletion is never a receipt.
func (s *BrowserWorkerService) VerifyNoBrowserWriterHistory(ctx context.Context, id runtimeauth.Identity) error {
	if s == nil || s.repo == nil || s.instances == nil || s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("external browser writer authority unavailable")
	}
	instance, err := s.instances.GetByID(id.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil || instance.UserID != id.UserID || instance.Type != RuntimeTypeOpenCode || instance.InstanceMode != InstanceModeLite || normalizeInstanceRuntimeType(instance.RuntimeType) != RuntimeBackendGateway || browserWorkerSupportedInstance(instance) {
		return fmt.Errorf("stopped-source recovery currently requires OpenCode Lite without browser admission")
	}
	if stringValue(instance.WorkspacePath) != RuntimeWorkspacePathWithRoot(s.client.WorkspaceRoot, RuntimeTypeOpenCode, id.UserID, id.InstanceID) {
		return fmt.Errorf("recovery requires the canonical standalone instance workspace")
	}
	if strings.Contains(strings.ToUpper(stringValue(instance.EnvironmentOverridesJSON)), "CLAWMANAGER_TEAM_") {
		return fmt.Errorf("Team environment overrides require coordinated writer teardown")
	}
	isolation, ok := s.repo.(repository.RecoveryWorkspaceIsolation)
	if !ok {
		return fmt.Errorf("standalone workspace authority unavailable")
	}
	if err = isolation.VerifyRecoveryWorkspaceIsolation(ctx, id.InstanceID); err != nil {
		return err
	}
	config, err := s.repo.Get(id.InstanceID)
	if err != nil {
		return err
	}
	if config != nil {
		return fmt.Errorf("browser configuration/history requires confirmed external teardown")
	}
	registry, ok := s.repo.(repository.BrowserWriterRegistry)
	if !ok {
		return fmt.Errorf("durable browser writer history unavailable")
	}
	raw, revision, err := registry.LoadBrowserWriter(ctx, id.InstanceID)
	if err != nil {
		return err
	}
	if revision != 0 || raw != "" {
		return fmt.Errorf("browser writer admission history requires confirmed external teardown")
	}
	name := s.name(id.InstanceID)
	iid := strconv.Itoa(id.InstanceID)
	owned := func(m metav1.ObjectMeta) bool {
		return m.Name == name || strings.HasPrefix(m.Name, name+"-") || (m.Labels["instance-id"] == iid && (m.Labels["app"] == "clawbrowser" || m.Labels["managed-by"] == browserWorkerManagedBy))
	}
	segment := fmt.Sprintf("/user-%d/instance-%d/", id.UserID, id.InstanceID)
	mentions := func(p string) bool { return strings.Contains("/"+strings.Trim(p, "/")+"/", segment) }
	mounted := func(spec corev1.PodSpec) bool {
		for _, v := range spec.Volumes {
			if v.HostPath != nil && mentions(v.HostPath.Path) {
				return true
			}
			if v.NFS != nil && mentions(v.NFS.Path) {
				return true
			}
		}
		check := func(mounts []corev1.VolumeMount) bool {
			for _, m := range mounts {
				if !m.ReadOnly && (mentions(m.SubPath) || mentions(m.MountPath)) {
					return true
				}
			}
			return false
		}
		for _, c := range append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...) {
			if check(c.VolumeMounts) {
				return true
			}
		}
		for _, c := range spec.EphemeralContainers {
			if check(c.VolumeMounts) {
				return true
			}
		}
		return false
	}
	reject := func() error {
		return fmt.Errorf("external browser resource or workspace writer exists; absence is unproven")
	}
	kube := s.client.Clientset
	pods, err := kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		if owned(p.ObjectMeta) || mounted(p.Spec) {
			return reject()
		}
	}
	deployments, err := kube.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range deployments.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.Template.ObjectMeta) || mounted(d.Spec.Template.Spec) {
			return reject()
		}
	}
	sets, err := kube.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range sets.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.Template.ObjectMeta) || mounted(d.Spec.Template.Spec) {
			return reject()
		}
	}
	replicas, err := kube.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range replicas.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.Template.ObjectMeta) || mounted(d.Spec.Template.Spec) {
			return reject()
		}
	}
	daemons, err := kube.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range daemons.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.Template.ObjectMeta) || mounted(d.Spec.Template.Spec) {
			return reject()
		}
	}
	jobs, err := kube.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range jobs.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.Template.ObjectMeta) || mounted(d.Spec.Template.Spec) {
			return reject()
		}
	}
	cron, err := kube.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, d := range cron.Items {
		if owned(d.ObjectMeta) || owned(d.Spec.JobTemplate.Spec.Template.ObjectMeta) || mounted(d.Spec.JobTemplate.Spec.Template.Spec) {
			return reject()
		}
	}
	services, err := kube.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, r := range services.Items {
		if owned(r.ObjectMeta) {
			return reject()
		}
	}
	secrets, err := kube.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, r := range secrets.Items {
		if owned(r.ObjectMeta) {
			return reject()
		}
	}
	configs, err := kube.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, r := range configs.Items {
		if owned(r.ObjectMeta) {
			return reject()
		}
	}
	return nil
}
