package k8s

import (
	"context"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"path"
	"strings"
)

// Old reports do not identify external file writers. Reject unattributed active
// writer resources rather than deriving business ownership from those reports.
func CheckRuntimeOrphanExternalWriters(ctx context.Context, client kubernetes.Interface, source *RuntimeOrphanSource) error {
	type backing struct{ server, root, host string }
	sourceBacking := []backing{}
	claims := map[string]bool{}
	for _, v := range source.SourcePod.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		claims[v.PersistentVolumeClaim.ClaimName] = true
		claim, e := client.CoreV1().PersistentVolumeClaims(source.Namespace).Get(ctx, v.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if e != nil {
			return e
		}
		pv, e := client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
		if e != nil {
			return e
		}
		if pv.Spec.NFS == nil && pv.Spec.HostPath == nil {
			return fmt.Errorf("external writer proof requires supported NFS or hostPath workspace backing")
		}
		b := backing{}
		if pv.Spec.NFS != nil {
			b.server = pv.Spec.NFS.Server
			b.root = pv.Spec.NFS.Path
		}
		if pv.Spec.HostPath != nil {
			b.host = pv.Spec.HostPath.Path
		}
		sourceBacking = append(sourceBacking, b)
	}
	overlap := func(a, b string) bool {
		a = path.Clean(a)
		b = path.Clean(b)
		return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/") || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/")
	}
	writes := func(namespace string, spec corev1.PodSpec) bool {
		volumes := map[string]bool{}
		for _, v := range spec.Volumes {
			if v.PersistentVolumeClaim != nil && namespace == source.Namespace && claims[v.PersistentVolumeClaim.ClaimName] && !v.PersistentVolumeClaim.ReadOnly {
				volumes[v.Name] = true
			}
			for _, b := range sourceBacking {
				if v.NFS != nil && !v.NFS.ReadOnly && b.server != "" && v.NFS.Server == b.server && overlap(v.NFS.Path, b.root) {
					volumes[v.Name] = true
				}
				if v.HostPath != nil && b.host != "" && overlap(v.HostPath.Path, b.host) {
					volumes[v.Name] = true
				}
			}
		}
		for _, c := range append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...) {
			for _, m := range c.VolumeMounts {
				if !m.ReadOnly && (volumes[m.Name] || overlap(m.MountPath, source.WorkspaceRoot)) {
					return true
				}
			}
		}
		for _, c := range spec.EphemeralContainers {
			for _, m := range c.VolumeMounts {
				if !m.ReadOnly && (volumes[m.Name] || overlap(m.MountPath, source.WorkspaceRoot)) {
					return true
				}
			}
		}
		return false
	}
	trustedController := func(namespace, name string, labels map[string]string) bool {
		kind := labels["clawmanager.io/runtime-type"]
		supported := (RuntimePodScaleRequest{RuntimeType: kind, PodCount: 1}).Validate() == nil
		return namespace == source.Namespace && (name == "clawmanager-app" || supported)
	}
	pods, e := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, p := range pods.Items {
		if string(p.UID) == source.PodUID || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed || !writes(p.Namespace, p.Spec) {
			continue
		}
		owned := false
		owner := metav1.GetControllerOf(&p)
		if owner != nil && owner.Kind == "ReplicaSet" {
			rs, e := client.AppsV1().ReplicaSets(p.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if e != nil {
				return e
			}
			ref := metav1.GetControllerOf(rs)
			if rs.UID == owner.UID && ref != nil && ref.Kind == "Deployment" {
				d, e := client.AppsV1().Deployments(p.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
				if e != nil {
					return e
				}
				owned = d.UID == ref.UID && trustedController(d.Namespace, d.Name, d.Labels)
			}
		}
		if !owned {
			return fmt.Errorf("unattributed external workspace writer Pod %s/%s must be retired before orphan recovery", p.Namespace, p.Name)
		}
	}
	deployments, e := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, d := range deployments.Items {
		if writes(d.Namespace, d.Spec.Template.Spec) && !trustedController(d.Namespace, d.Name, d.Labels) {
			return fmt.Errorf("external workspace writer Deployment %s/%s must be retired", d.Namespace, d.Name)
		}
	}
	jobs, e := client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, j := range jobs.Items {
		terminal := false
		for _, c := range j.Status.Conditions {
			if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
				terminal = true
			}
		}
		if !terminal && writes(j.Namespace, j.Spec.Template.Spec) {
			return fmt.Errorf("external workspace writer Job %s/%s must finish verified cleanup", j.Namespace, j.Name)
		}
	}
	stateful, e := client.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, d := range stateful.Items {
		if writes(d.Namespace, d.Spec.Template.Spec) {
			return fmt.Errorf("external workspace writer StatefulSet %s/%s prevents orphan proof", d.Namespace, d.Name)
		}
	}
	daemons, e := client.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, d := range daemons.Items {
		if writes(d.Namespace, d.Spec.Template.Spec) {
			return fmt.Errorf("external workspace writer DaemonSet %s/%s prevents orphan proof", d.Namespace, d.Name)
		}
	}
	cron, e := client.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, j := range cron.Items {
		if writes(j.Namespace, j.Spec.JobTemplate.Spec.Template.Spec) {
			return fmt.Errorf("external workspace writer CronJob %s/%s prevents orphan proof", j.Namespace, j.Name)
		}
	}
	return nil
}
