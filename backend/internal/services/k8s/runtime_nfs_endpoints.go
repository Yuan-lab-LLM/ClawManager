package k8s

import (
	"context"
	"fmt"
	"net/netip"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RuntimeNFSStorageEndpoints pins the bound export's literal addresses into the
// protected egress deny list. Blocking every port also covers RPC and proxies
// to the storage server. DNS names cannot pin the kernel client's destination.
func RuntimeNFSStorageEndpoints(ctx context.Context, client kubernetes.Interface, namespace, claim string) ([]string, error) {
	if client == nil || namespace == "" || claim == "" {
		return nil, fmt.Errorf("NFS workspace binding is required")
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claim, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" || pvc.UID == "" {
		return nil, fmt.Errorf("NFS workspace is not bound")
	}
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ref := pv.Spec.ClaimRef
	if ref == nil || ref.UID != pvc.UID || ref.Name != pvc.Name || ref.Namespace != namespace {
		return nil, fmt.Errorf("NFS workspace binding changed")
	}
	return nfsStorageEndpoints(pv)
}

func nfsStorageEndpoints(pv *corev1.PersistentVolume) ([]string, error) {
	server := ""
	if pv.Spec.NFS != nil {
		server = pv.Spec.NFS.Server
	} else if pv.Spec.CSI != nil && pv.Spec.CSI.Driver == "nfs.csi.k8s.io" {
		server = pv.Spec.CSI.VolumeAttributes["server"]
	} else if pv.Spec.CSI != nil && pv.Spec.CSI.Driver == "driver.longhorn.io" {
		// Longhorn share managers use cluster Pod/Service addresses, which the
		// continuously refreshed inventory already denies on every port.
		return nil, nil
	} else {
		return nil, fmt.Errorf("unrecognized NFS workspace source")
	}
	address, err := netip.ParseAddr(server)
	if err != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() || address.Is4In6() {
		return nil, fmt.Errorf("NFS rootless workspace requires a literal pinned storage server address")
	}
	return []string{address.String()}, nil
}
