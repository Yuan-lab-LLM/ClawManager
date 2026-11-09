package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"path"
	"strings"
)

// ResolveRuntimePersistentStorage reads the live Pod and bound claim rather
// than trusting an Agent report or treating emptyDir as persistent storage.
func ResolveRuntimePersistentStorage(ctx context.Context, client kubernetes.Interface, namespace, name, uid string, workspacePaths ...string) (string, error) {
	if client == nil || uid == "" {
		return "", fmt.Errorf("persistent storage cannot be verified")
	}
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if string(pod.UID) != uid || pod.DeletionTimestamp != nil {
		return "", fmt.Errorf("runtime Pod identity changed or is deleting")
	}
	workspace := "/workspaces"
	if len(workspacePaths) > 1 {
		return "", fmt.Errorf("exactly one workspace path is required")
	}
	if len(workspacePaths) == 1 {
		workspace = workspacePaths[0]
	}
	if !path.IsAbs(workspace) || path.Clean(workspace) != workspace || workspace == "/" {
		return "", fmt.Errorf("workspace path must be canonical and absolute")
	}
	if err := verifyWorkspaceMountCoverage(pod, workspace); err != nil {
		return "", err
	}
	var claim *corev1.PersistentVolumeClaim
	for _, v := range pod.Spec.Volumes {
		if v.Name != runtimeWorkspaceVolume || v.PersistentVolumeClaim == nil {
			continue
		}
		claim, err = client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, v.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" {
			return "", fmt.Errorf("workspace claim is not durably bound")
		}
	}
	identity := runtimeWorkspaceStorageIdentity(*pod, claim)
	if identity == "" {
		return "", fmt.Errorf("workspace requires a writable PVC or NFS mount on every runtime slot")
	}
	return identity, nil
}

func mountCoversPath(mount, target string) bool {
	return mount == target || strings.HasPrefix(target, strings.TrimSuffix(mount, "/")+"/")
}
func verifyWorkspaceMountCoverage(pod *corev1.Pod, workspace string) error {
	var common [2]string
	count := 0
	for _, container := range pod.Spec.Containers {
		if container.Name != "runtime" && !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		var backing *corev1.VolumeMount
		for n := range container.VolumeMounts {
			mount := &container.VolumeMounts[n]
			if !path.IsAbs(mount.MountPath) || path.Clean(mount.MountPath) != mount.MountPath {
				return fmt.Errorf("runtime mount path is not canonical")
			}
			if mount.Name == runtimeWorkspaceVolume && mountCoversPath(mount.MountPath, workspace) {
				if backing != nil {
					return fmt.Errorf("ambiguous workspace mounts")
				}
				backing = mount
			}
		}
		if backing == nil || backing.ReadOnly || backing.SubPathExpr != "" {
			return fmt.Errorf("instance workspace is not covered by a writable persistent workspace mount on %s", container.Name)
		}
		if backing.SubPath != "" && (path.IsAbs(backing.SubPath) || path.Clean(backing.SubPath) != backing.SubPath || backing.SubPath == ".." || strings.HasPrefix(backing.SubPath, "../")) {
			return fmt.Errorf("workspace subpath is not canonical")
		}
		mapping := [2]string{backing.MountPath, backing.SubPath}
		if count > 0 && mapping != common {
			return fmt.Errorf("runtime slots do not share the same workspace mapping")
		}
		common = mapping
		count++
		for n := range container.VolumeMounts {
			mount := &container.VolumeMounts[n]
			if mount == backing {
				continue
			}
			// An overlaid parent or any nested mount can redirect files away from the
			// retained volume, even when the volume named workspaces is persistent.
			if mountCoversPath(workspace, mount.MountPath) || (mountCoversPath(mount.MountPath, workspace) && len(mount.MountPath) >= len(backing.MountPath)) {
				return fmt.Errorf("mount %s shadows the instance workspace on %s", mount.MountPath, container.Name)
			}
		}
	}
	if count == 0 {
		return fmt.Errorf("runtime workspace mount coverage is unknown")
	}
	return nil
}
