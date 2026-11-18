package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"

	"clawreef/internal/models"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type StoppedSourceStorage struct{ Identity, Slot string }

// Source registration, the retained original ReplicaSet and bound volume must
// agree. A current Deployment template cannot reconstruct a removed Pod's mount.
// The absent Pod is eligibility only; the authenticated writer record supplies
// the separate positive stopped proof.
func ResolveStoppedSourceStorage(ctx context.Context, client kubernetes.Interface, source, observer models.RuntimePod, workspace string, port int) (StoppedSourceStorage, error) {
	return resolveStoppedSourceStorage(ctx, client, source, observer, workspace, port, verifyRetainedRuntimeTemplate)
}

func resolveStoppedSourceStorage(ctx context.Context, client kubernetes.Interface, source, observer models.RuntimePod, workspace string, port int, verifyTemplate func(context.Context, kubernetes.Interface, *appsv1.ReplicaSet, *appsv1.Deployment) error) (StoppedSourceStorage, error) {
	fail := func(message string) (StoppedSourceStorage, error) {
		return StoppedSourceStorage{}, fmt.Errorf("stopped source storage: %s", message)
	}
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	if client == nil || source.Namespace == "" || source.Namespace != observer.Namespace || val(source.PodUID) == "" || val(observer.PodUID) == "" || val(source.PodUID) == val(observer.PodUID) || source.CreatedAt.IsZero() || source.RuntimeType != observer.RuntimeType || val(source.NodeName) == "" || !path.IsAbs(workspace) || path.Clean(workspace) != workspace || workspace == "/" {
		return fail("incomplete identity")
	}
	old, err := client.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if err == nil && string(old.UID) == val(source.PodUID) {
		return fail("source Pod still exists")
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return StoppedSourceStorage{}, err
	}
	split := strings.LastIndex(source.PodName, "-")
	if split < 1 {
		return fail("source ReplicaSet name missing")
	}
	rs, err := client.AppsV1().ReplicaSets(source.Namespace).Get(ctx, source.PodName[:split], metav1.GetOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	hash := rs.Labels["pod-template-hash"]
	if rs.UID == "" || rs.DeletionTimestamp != nil || rs.CreationTimestamp.IsZero() || rs.CreationTimestamp.Time.After(source.CreatedAt) || hash == "" || rs.Name != source.DeploymentName+"-"+hash || rs.Spec.Template.Labels["pod-template-hash"] != hash {
		return fail("original ReplicaSet identity is unproven")
	}
	dep, err := client.AppsV1().Deployments(source.Namespace).Get(ctx, source.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	owner := metav1.GetControllerOf(rs)
	if owner == nil || owner.Kind != "Deployment" || owner.Name != dep.Name || owner.UID != dep.UID || dep.UID == "" {
		return fail("original ReplicaSet owner changed")
	}
	if err = verifyTemplate(ctx, client, rs, dep); err != nil {
		return StoppedSourceStorage{}, err
	}
	original := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: source.Namespace}, Spec: *rs.Spec.Template.Spec.DeepCopy()}
	slot := ""
	for _, container := range original.Spec.Containers {
		if container.Name != "runtime" && !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		if container.Image != source.ImageRef || !strings.Contains(source.ImageRef, "@sha256:") {
			return fail("source image is not pinned to the registered original image")
		}
		for _, env := range container.Env {
			if env.Name == "RUNTIME_GATEWAY_PORT_START" && env.Value == strconv.Itoa(port) && env.ValueFrom == nil && strings.HasPrefix(container.Name, "slot-") {
				if slot != "" {
					return fail("ambiguous source slot")
				}
				slot = container.Name
			}
		}
	}
	if slot == "" {
		return fail("source slot not present in original template")
	}
	live, err := client.CoreV1().Pods(observer.Namespace).Get(ctx, observer.PodName, metav1.GetOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	if string(live.UID) != val(observer.PodUID) || live.DeletionTimestamp != nil || live.Spec.NodeName != val(observer.NodeName) {
		return fail("observer identity changed")
	}
	if err = verifyWorkspaceMountCoverage(original, workspace); err != nil {
		return StoppedSourceStorage{}, err
	}
	if err = verifyWorkspaceMountCoverage(live, workspace); err != nil {
		return StoppedSourceStorage{}, err
	}
	workspaceRoot := path.Dir(path.Dir(path.Dir(workspace)))
	for _, pod := range []*corev1.Pod{original, live} {
		if err = verifyRecoveryLedgerCoverage(pod, workspaceRoot); err != nil {
			return StoppedSourceStorage{}, err
		}
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	ip := net.ParseIP(val(source.PodIP))
	if ip == nil {
		return fail("source Pod IP missing")
	}
	var node *corev1.Node
	allocation := "node-pod-cidr"
	for i := range nodes.Items {
		n := &nodes.Items[i]
		cidrs := append([]string{n.Spec.PodCIDR}, n.Spec.PodCIDRs...)
		matches := false
		for _, cidr := range cidrs {
			_, network, e := net.ParseCIDR(cidr)
			if e == nil && network.Contains(ip) {
				matches = true
			}
		}
		if matches {
			if node != nil {
				return fail("source node attribution is ambiguous")
			}
			node = n
		}
	}
	if node == nil {
		node, allocation, err = readCalicoRecoveryNode(ctx, client, source, nodes.Items)
		if err != nil {
			return StoppedSourceStorage{}, err
		}
	}
	if node.Name != val(source.NodeName) || node.UID == "" || node.DeletionTimestamp != nil || node.CreationTimestamp.IsZero() || node.CreationTimestamp.Time.After(source.CreatedAt) {
		return fail("source node attribution is unproven")
	}
	claimName := func(p *corev1.Pod) string {
		for _, v := range p.Spec.Volumes {
			if v.Name == runtimeWorkspaceVolume && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly {
				return v.PersistentVolumeClaim.ClaimName
			}
		}
		return ""
	}
	name := claimName(original)
	if name == "" || name != claimName(live) {
		return fail("recovery requires the same retained workspace PVC")
	}
	claim, err := client.CoreV1().PersistentVolumeClaims(source.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	if claim.UID == "" || claim.DeletionTimestamp != nil || claim.CreationTimestamp.IsZero() || claim.CreationTimestamp.Time.After(source.CreatedAt) || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" {
		return fail("original bound claim is unproven")
	}
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	ref := pv.Spec.ClaimRef
	if pv.UID == "" || pv.DeletionTimestamp != nil || pv.CreationTimestamp.IsZero() || pv.CreationTimestamp.Time.After(source.CreatedAt) || ref == nil || ref.UID != claim.UID || ref.Namespace != source.Namespace || ref.Name != name {
		return fail("original bound volume is unproven")
	}
	if pv.Spec.HostPath != nil {
		if live.Spec.NodeName != node.Name || !path.IsAbs(pv.Spec.HostPath.Path) || path.Clean(pv.Spec.HostPath.Path) != pv.Spec.HostPath.Path {
			return fail("hostPath node or path changed")
		}
	} else if pv.Spec.NFS == nil || pv.Spec.NFS.ReadOnly {
		return fail("unsupported workspace volume provenance")
	}
	ledger := path.Join(workspaceRoot, ".clawmanager-slot-locks")
	identity := recoveryWorkspaceMapping(original, claim, workspace, ledger)
	if identity == "" || identity != recoveryWorkspaceMapping(live, claim, workspace, ledger) {
		return fail("workspace mappings changed")
	}
	raw, err := json.Marshal([]any{identity, rs.UID, dep.UID, node.UID, allocation, pv.UID, pv.Spec.PersistentVolumeSource})
	if err != nil {
		return StoppedSourceStorage{}, err
	}
	digest := sha256.Sum256(raw)
	return StoppedSourceStorage{Identity: hex.EncodeToString(digest[:]), Slot: slot}, nil
}

func verifyRecoveryLedgerCoverage(pod *corev1.Pod, root string) error {
	if root == "/" || !path.IsAbs(root) {
		return fmt.Errorf("canonical runtime workspace root is unavailable")
	}
	for _, container := range pod.Spec.Containers {
		if container.Name != "runtime" && !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		count := 0
		for _, env := range container.Env {
			if env.Name == "RUNTIME_WORKSPACE_ROOT" {
				count++
				if env.ValueFrom != nil || env.Value != root {
					return fmt.Errorf("runtime writer ledger root differs from workspace")
				}
			}
		}
		if count != 1 {
			return fmt.Errorf("runtime writer ledger root must be explicit and unique")
		}
	}
	return verifyWorkspaceMountCoverage(pod, path.Join(root, ".clawmanager-slot-locks"))
}

// Coverage above proves every slot has the same mapping. A read-only observer
// may have fewer slots; container count does not change the filesystem identity.
func recoveryWorkspaceMapping(pod *corev1.Pod, claim *corev1.PersistentVolumeClaim, targets ...string) string {
	for _, container := range pod.Spec.Containers {
		if container.Name != "runtime" && !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		mapping := []string{pod.Namespace, claim.Name, string(claim.UID), claim.Spec.VolumeName}
		for _, target := range targets {
			var backing *corev1.VolumeMount
			for i := range container.VolumeMounts {
				mount := &container.VolumeMounts[i]
				if !mountCoversPath(mount.MountPath, target) {
					continue
				}
				if backing != nil && len(backing.MountPath) == len(mount.MountPath) {
					return ""
				}
				if backing == nil || len(mount.MountPath) > len(backing.MountPath) {
					backing = mount
				}
			}
			if backing == nil || backing.Name != runtimeWorkspaceVolume || backing.ReadOnly || backing.SubPathExpr != "" {
				return ""
			}
			relative := strings.TrimPrefix(strings.TrimPrefix(target, backing.MountPath), "/")
			mapping = append(mapping, path.Join(backing.SubPath, relative))
		}
		raw, _ := json.Marshal(mapping)
		return string(raw)
	}
	return ""
}
