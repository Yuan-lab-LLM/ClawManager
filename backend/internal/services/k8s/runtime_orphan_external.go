package k8s

import (
	"context"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net"
	"path"
	"strings"
)

// Old reports do not identify external file writers. Reject unattributed active
// writer resources rather than deriving business ownership from those reports.
func CheckRuntimeOrphanExternalWriters(ctx context.Context, client kubernetes.Interface, source *RuntimeOrphanSource) error {

	type backing struct{ kind, server, root, pv string }
	overlap := func(a, b string) bool {
		a = path.Clean(a)
		b = path.Clean(b)
		return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/") || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/")
	}
	fromPV := func(pv *corev1.PersistentVolume) (backing, error) {
		b := backing{pv: pv.Name}
		switch {
		case pv.Spec.HostPath != nil:
			b.kind = "node-path"
			b.root = pv.Spec.HostPath.Path
		case pv.Spec.NFS != nil:
			b.kind = "nfs"
			b.server = pv.Spec.NFS.Server
			b.root = pv.Spec.NFS.Path
		default:
			return b, fmt.Errorf("unsupported persistent backing for PV %s", pv.Name)
		}
		if !path.IsAbs(b.root) || path.Clean(b.root) != b.root {
			return b, fmt.Errorf("noncanonical backing path for PV %s", pv.Name)
		}
		if b.kind == "nfs" && b.server == "" {
			return b, fmt.Errorf("NFS backing server missing")
		}
		return b, nil
	}
	claimCache := map[string]backing{}
	pvCache := map[string]backing{}
	resolveClaim := func(namespace, name string) (backing, error) {
		key := namespace + "/" + name
		if b, ok := claimCache[key]; ok {
			return b, nil
		}
		claim, e := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if e != nil {
			return backing{}, fmt.Errorf("resolve workspace PVC %s: %w", key, e)
		}
		if claim.Spec.VolumeName == "" {
			return backing{}, fmt.Errorf("workspace PVC %s has no fixed PV backing", key)
		}
		b, ok := pvCache[claim.Spec.VolumeName]
		if !ok {
			pv, e := client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
			if e != nil {
				return backing{}, fmt.Errorf("resolve workspace PV %s: %w", claim.Spec.VolumeName, e)
			}
			b, e = fromPV(pv)
			if e != nil {
				return backing{}, e
			}
			pvCache[pv.Name] = b
		}
		claimCache[key] = b
		return b, nil
	}
	ipCache := map[string][]net.IP{}
	resolveServer := func(server string) ([]net.IP, error) {
		if addresses, ok := ipCache[server]; ok {
			return addresses, nil
		}
		if ip := net.ParseIP(server); ip != nil {
			ipCache[server] = []net.IP{ip}
			return []net.IP{ip}, nil
		}
		addresses, e := net.DefaultResolver.LookupIPAddr(ctx, server)
		if e != nil {
			return nil, fmt.Errorf("resolve NFS server %s: %w", server, e)
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("NFS server %s has no verified addresses", server)
		}
		ips := []net.IP{}
		for _, address := range addresses {
			ips = append(ips, address.IP)
		}
		ipCache[server] = ips
		return ips, nil
	}
	shared := func(a, b backing) (bool, error) {
		if a.pv != "" && a.pv == b.pv {
			return true, nil
		}
		if a.kind != b.kind {
			// A node directory can also be exported through NFS. Overlapping physical
			// paths are not proof of distinct storage, even with different PV types.
			return overlap(a.root, b.root), nil
		}
		if a.kind == "node-path" {
			return overlap(a.root, b.root), nil
		}

		if !overlap(a.root, b.root) {
			return false, nil
		}
		if strings.EqualFold(strings.TrimSuffix(a.server, "."), strings.TrimSuffix(b.server, ".")) {
			return true, nil
		}
		left, e := resolveServer(a.server)
		if e != nil {
			return false, e
		}
		right, e := resolveServer(b.server)
		if e != nil {
			return false, e
		}
		for _, l := range left {
			for _, r := range right {
				if l.Equal(r) || (l.IsLoopback() && r.IsLoopback()) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	sourceBacking := []backing{}
	for _, v := range source.SourcePod.Spec.Volumes {
		if v.PersistentVolumeClaim == nil {
			continue
		}
		b, e := resolveClaim(source.Namespace, v.PersistentVolumeClaim.ClaimName)
		if e != nil {
			return e
		}
		if b.kind != "node-path" && b.kind != "nfs" {
			return fmt.Errorf("external writer proof requires supported NFS or hostPath workspace backing")
		}
		sourceBacking = append(sourceBacking, b)
	}
	if len(sourceBacking) == 0 {
		return fmt.Errorf("source workspace PVC backing unavailable")
	}
	writes := func(namespace string, spec corev1.PodSpec) (bool, error) {
		writable := map[string]bool{}
		for _, c := range append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...) {
			for _, m := range c.VolumeMounts {
				if !m.ReadOnly {
					writable[m.Name] = true
				}
			}
			for _, device := range c.VolumeDevices {
				writable[device.Name] = true
			}
		}
		for _, c := range spec.EphemeralContainers {
			for _, m := range c.VolumeMounts {
				if !m.ReadOnly {
					writable[m.Name] = true
				}
			}
			for _, device := range c.VolumeDevices {
				writable[device.Name] = true
			}
		}
		for _, v := range spec.Volumes {
			if !writable[v.Name] {
				continue
			}
			delete(writable, v.Name)
			var b backing
			switch {
			case v.PersistentVolumeClaim != nil:
				if v.PersistentVolumeClaim.ReadOnly {
					continue
				}
				var e error
				b, e = resolveClaim(namespace, v.PersistentVolumeClaim.ClaimName)
				if e != nil {
					return false, e
				}
			case v.HostPath != nil:
				b = backing{kind: "node-path", root: v.HostPath.Path}
			case v.NFS != nil:
				if v.NFS.ReadOnly {
					continue
				}
				b = backing{kind: "nfs", server: v.NFS.Server, root: v.NFS.Path}
			case v.EmptyDir != nil || v.Secret != nil || v.ConfigMap != nil || v.Projected != nil || v.DownwardAPI != nil:
				continue
			default:
				return false, fmt.Errorf("writable volume %s has unresolvable workspace backing", v.Name)
			}
			if b.kind == "nfs" && b.server == "" {
				return false, fmt.Errorf("writable volume %s has no verified NFS server", v.Name)
			}
			if !path.IsAbs(b.root) || path.Clean(b.root) != b.root {
				return false, fmt.Errorf("writable volume %s has unverified backing path", v.Name)
			}
			for _, sourceVolume := range sourceBacking {
				matches, e := shared(sourceVolume, b)
				if e != nil {
					return false, e
				}
				if matches {
					return true, nil
				}
			}
		}
		if len(writable) != 0 {
			return false, fmt.Errorf("writable mount refers to an unavailable volume")
		}
		return false, nil
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
		if string(p.UID) == source.PodUID || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		writable, e := writes(p.Namespace, p.Spec)
		if e != nil {
			return fmt.Errorf("inspect external Pod %s/%s: %w", p.Namespace, p.Name, e)
		}
		if !writable {
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
		writable, e := writes(d.Namespace, d.Spec.Template.Spec)
		if e != nil {
			return fmt.Errorf("inspect external Deployment %s/%s: %w", d.Namespace, d.Name, e)
		}
		if writable && !trustedController(d.Namespace, d.Name, d.Labels) {
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
		if terminal {
			continue
		}
		writable, e := writes(j.Namespace, j.Spec.Template.Spec)
		if e != nil {
			return fmt.Errorf("inspect external Job %s/%s: %w", j.Namespace, j.Name, e)
		}
		if writable {
			return fmt.Errorf("external workspace writer Job %s/%s must finish verified cleanup", j.Namespace, j.Name)
		}
	}
	stateful, e := client.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, d := range stateful.Items {
		writable, e := writes(d.Namespace, d.Spec.Template.Spec)
		if e != nil {
			return fmt.Errorf("inspect external StatefulSet %s/%s: %w", d.Namespace, d.Name, e)
		}
		if writable {
			return fmt.Errorf("external workspace writer StatefulSet %s/%s prevents orphan proof", d.Namespace, d.Name)
		}
	}
	daemons, e := client.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, d := range daemons.Items {
		writable, e := writes(d.Namespace, d.Spec.Template.Spec)
		if e != nil {
			return fmt.Errorf("inspect external DaemonSet %s/%s: %w", d.Namespace, d.Name, e)
		}
		if writable {
			return fmt.Errorf("external workspace writer DaemonSet %s/%s prevents orphan proof", d.Namespace, d.Name)
		}
	}
	cron, e := client.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if e != nil {
		return e
	}
	for _, j := range cron.Items {
		writable, e := writes(j.Namespace, j.Spec.JobTemplate.Spec.Template.Spec)
		if e != nil {
			return fmt.Errorf("inspect external CronJob %s/%s: %w", j.Namespace, j.Name, e)
		}
		if writable {
			return fmt.Errorf("external workspace writer CronJob %s/%s prevents orphan proof", j.Namespace, j.Name)
		}
	}
	return nil
}
