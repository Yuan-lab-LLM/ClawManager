package k8s

import (
	"context"
	"fmt"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *runtimeDeploymentService) validateNFSRootlessStorage(ctx context.Context, spec RuntimeDeploymentSpec, pvc *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume) error {
	if _, err := nfsStorageEndpoints(pv); err != nil {
		return err
	}
	if spec.PodUserNamespaces || !spec.NetworkIsolation {
		return fmt.Errorf("NFS rootless storage requires isolated networking without Pod idmapped mounts")
	}
	if !runtimeVolumeHasMode(pvc.Spec.AccessModes, corev1.ReadWriteMany) || !runtimeVolumeHasMode(pv.Spec.AccessModes, corev1.ReadWriteMany) ||
		(pvc.Spec.VolumeMode != nil && *pvc.Spec.VolumeMode != corev1.PersistentVolumeFilesystem) ||
		(pv.Spec.VolumeMode != nil && *pv.Spec.VolumeMode != corev1.PersistentVolumeFilesystem) {
		return fmt.Errorf("NFS rootless slots require a shared filesystem RWX workspace")
	}
	scName := ""
	if pvc.Spec.StorageClassName != nil {
		scName = *pvc.Spec.StorageClassName
	}
	if scName != pv.Spec.StorageClassName {
		return fmt.Errorf("NFS workspace storage class binding is inconsistent")
	}
	if nfs := pv.Spec.NFS; nfs != nil {
		if strings.TrimSpace(nfs.Server) == "" || nfs.Server != strings.TrimSpace(nfs.Server) || nfs.ReadOnly || !path.IsAbs(nfs.Path) || path.Clean(nfs.Path) != nfs.Path {
			return fmt.Errorf("NFS workspace requires a writable canonical export and server")
		}
		// A pre-bound static NFS PV may intentionally have no StorageClass.
		return nil
	}
	csi := pv.Spec.CSI
	if csi == nil || csi.ReadOnly || strings.TrimSpace(csi.VolumeHandle) == "" || scName == "" {
		return fmt.Errorf("NFS rootless slots require a writable bound NFS volume")
	}
	if csi.Driver != "nfs.csi.k8s.io" && csi.Driver != "driver.longhorn.io" {
		return fmt.Errorf("NFS rootless slots do not recognize CSI driver %q", csi.Driver)
	}
	sc, err := s.client.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect NFS workspace StorageClass: %w", err)
	}
	if sc.Provisioner != csi.Driver {
		return fmt.Errorf("NFS workspace CSI driver and StorageClass provisioner disagree")
	}
	return nil
}
