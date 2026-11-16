package k8s

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"path"
	"strings"
)

// Read-only proof for an explicitly stopped cold adoption. General idle/wake
// identities remain unchanged. An inline legacy NFS export may be wrapped by a
// bound Retain PVC only if the live backing and every workspace mapping agree.
func VerifyColdNFSWorkspace(ctx context.Context, kube kubernetes.Interface, namespace, sourceName, sourceUID, targetName, targetUID, workspace string) (bool, error) {
	if kube == nil || sourceUID == "" || targetUID == "" {
		return false, nil
	}
	source, e := kube.CoreV1().Pods(namespace).Get(ctx, sourceName, metav1.GetOptions{})
	if e != nil {
		return false, e
	}
	target, e := kube.CoreV1().Pods(namespace).Get(ctx, targetName, metav1.GetOptions{})
	if e != nil {
		return false, e
	}
	if string(source.UID) != sourceUID || string(target.UID) != targetUID || source.DeletionTimestamp != nil || target.DeletionTimestamp != nil {
		return false, nil
	}
	if verifyWorkspaceMountCoverage(source, workspace) != nil || verifyWorkspaceMountCoverage(target, workspace) != nil {
		return false, nil
	}
	mapping := func(p *corev1.Pod) [2]string {
		for _, c := range p.Spec.Containers {
			if c.Name == "runtime" {
				for _, m := range c.VolumeMounts {
					if m.Name == runtimeWorkspaceVolume {
						return [2]string{m.MountPath, m.SubPath}
					}
				}
			}
		}
		return [2]string{}
	}
	if mapping(source) == [2]string{} || mapping(source) != mapping(target) {
		return false, nil
	}
	var old *corev1.NFSVolumeSource
	var claimName string
	for _, v := range source.Spec.Volumes {
		if v.Name == runtimeWorkspaceVolume {
			old = v.NFS
		}
	}
	for _, v := range target.Spec.Volumes {
		if v.Name == runtimeWorkspaceVolume && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly {
			claimName = v.PersistentVolumeClaim.ClaimName
		}
	}
	if old == nil || old.ReadOnly || claimName == "" || old.Server == "" || old.Server != strings.TrimSpace(old.Server) || !path.IsAbs(old.Path) || path.Clean(old.Path) != old.Path {
		return false, nil
	}
	claim, e := kube.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claimName, metav1.GetOptions{})
	if e != nil {
		return false, e
	}
	if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" {
		return false, nil
	}
	pv, e := kube.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
	if e != nil {
		return false, e
	}
	if pv.UID == "" || pv.DeletionTimestamp != nil || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.UID != claim.UID || pv.Spec.ClaimRef.Namespace != namespace || pv.Spec.ClaimRef.Name != claim.Name || pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimRetain || pv.Spec.NFS == nil || pv.Spec.NFS.ReadOnly || !runtimeVolumeHasMode(claim.Spec.AccessModes, corev1.ReadWriteMany) || !runtimeVolumeHasMode(pv.Spec.AccessModes, corev1.ReadWriteMany) {
		return false, nil
	}
	return pv.Spec.NFS.Server == old.Server && pv.Spec.NFS.Path == old.Path, nil
}
