package k8s

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PVCService handles PersistentVolumeClaim operations
type PVCService struct {
	client           *Client
	namespaceService *NamespaceService
}

const (
	nodeHostnameLabel       = "kubernetes.io/hostname"
	noProvisionerName       = "kubernetes.io/no-provisioner"
	storageProfileLabel     = "clawmanager.io/storage-profile"
	storageProfileSingle    = "single-node"
	storageProfileLegacyNFS = "legacy-nfs"
	defaultPVCBindTimeout   = 2 * time.Minute
	// os.FileMode keeps setgid outside the Unix permission-bit range, so
	// 0o2775 alone does not set it when passed to os.Chmod.
	teamSharedDirectoryMode = os.FileMode(0o775) | os.ModeSetgid
)

// NewPVCService creates a new PVC service
func NewPVCService() *PVCService {
	return &PVCService{
		client:           globalClient,
		namespaceService: NewNamespaceService(),
	}
}

// GetClient returns the k8s client
func (s *PVCService) GetClient() *Client {
	return s.client
}

// CreatePVC creates a new PVC for an instance
func (s *PVCService) CreatePVC(ctx context.Context, userID, instanceID int, storageSizeGB int, storageClass string) (*corev1.PersistentVolumeClaim, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	// Ensure namespace exists
	if _, err := s.namespaceService.EnsureNamespace(ctx, userID); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}

	pvcName := s.client.GetPVCName(instanceID)
	namespace := s.client.GetNamespace(userID)

	// Use default storage class if not specified
	if storageClass == "" {
		storageClass = firstNonEmpty(s.client.InstanceStorageClass, s.client.StorageClass)
	}

	fmt.Printf("Creating PVC %s in namespace %s with storageClass: %s\n", pvcName, namespace, storageClass)

	storageSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":         "clawreef",
				"instance-id": fmt.Sprintf("%d", instanceID),
				"managed-by":  "clawreef",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
			StorageClassName: &storageClass,
		},
	}

	createdPVC, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		// Check if PVC already exists
		if errors.IsAlreadyExists(err) {
			// Try to get the existing PVC
			existingPVC, getErr := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if getErr == nil && existingPVC != nil {
				// Check if PVC belongs to the same instance
				if existingPVC.Labels["instance-id"] == fmt.Sprintf("%d", instanceID) {
					// PVC exists and belongs to this instance, return it
					return existingPVC, nil
				}
				// PVC exists but belongs to a different instance, delete it and recreate
				deleteErr := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvcName, metav1.DeleteOptions{})
				if deleteErr != nil && !errors.IsNotFound(deleteErr) {
					return nil, fmt.Errorf("failed to delete existing PVC %s: %w", pvcName, deleteErr)
				}
				// Wait a moment for deletion to complete
				select {
				case <-time.After(2 * time.Second):
				}
				// Retry creation
				createdPVC, err = s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
				if err != nil {
					return nil, fmt.Errorf("failed to create PVC after deletion %s: %w", pvcName, err)
				}
				return createdPVC, nil
			}
		}
		return nil, fmt.Errorf("failed to create PVC %s: %w", pvcName, err)
	}

	if s.shouldUseManualHostPathFallback(storageClass) {
		fmt.Printf("PVC %s created, scheduling manual hostPath binding monitor...\n", pvcName)
		go s.monitorPVCBinding(context.Background(), namespace, pvcName, userID, instanceID, storageSizeGB, storageClass, s.pvcBindTimeout())
	} else if usesManualHostPathFallback(storageClass) {
		fmt.Printf("PVC %s created with manual storageClass, but hostPath fallback is disabled for storage profile %q\n", pvcName, s.storageProfile())
	} else {
		fmt.Printf("PVC %s created with dynamic storageClass %s, leaving binding to the provisioner\n", pvcName, storageClass)
	}

	return createdPVC, nil
}

// CreateTeamSharedPVC creates the RWX PVC mounted by every member of a Team.
func (s *PVCService) CreateTeamSharedPVC(ctx context.Context, userID, teamID, storageSizeGB int, storageClass string) (*corev1.PersistentVolumeClaim, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	if storageSizeGB <= 0 {
		storageSizeGB = 10
	}

	if _, err := s.namespaceService.EnsureNamespace(ctx, userID); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}

	pvcName := s.client.GetTeamSharedPVCName(teamID)
	namespace := s.client.GetNamespace(userID)
	storageSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))
	useWorkspaceNFS := strings.TrimSpace(s.client.WorkspaceNFSServer) != ""
	storageClass = s.teamSharedPVCStorageClass(storageClass, useWorkspaceNFS)
	workspaceAccessMode := workspacePVCAccessMode(s.client.WorkspaceAccessMode)
	if useWorkspaceNFS {
		if err := s.ensureTeamSharedWorkspaceDirectory(userID, teamID); err != nil {
			return nil, err
		}
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "clawreef",
				"team-id":    fmt.Sprintf("%d", teamID),
				"managed-by": "clawreef",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				workspaceAccessMode,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
			StorageClassName: &storageClass,
		},
	}

	createdPVC, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existingPVC, getErr := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if getErr == nil && existingPVC != nil && existingPVC.Labels["team-id"] == fmt.Sprintf("%d", teamID) {
				if useWorkspaceNFS {
					if err := s.ensureWorkspaceNFSPVForTeamSharedPVC(ctx, namespace, pvcName, existingPVC, userID, teamID, storageSizeGB, storageClass, s.client.GetTeamSharedPVName(userID, teamID)); err != nil {
						return nil, err
					}
				} else if existingPVC.Status.Phase != corev1.ClaimBound && s.shouldUseManualHostPathFallback(storageClass) {
					go s.monitorTeamSharedPVCBinding(context.Background(), namespace, pvcName, userID, teamID, storageSizeGB, storageClass, s.pvcBindTimeout())
				}
				return existingPVC, nil
			}
		}
		return nil, fmt.Errorf("failed to create Team shared PVC %s: %w", pvcName, err)
	}

	if useWorkspaceNFS {
		if err := s.ensureWorkspaceNFSPVForTeamSharedPVC(ctx, namespace, pvcName, createdPVC, userID, teamID, storageSizeGB, storageClass, s.client.GetTeamSharedPVName(userID, teamID)); err != nil {
			return nil, err
		}
	} else if s.shouldUseManualHostPathFallback(storageClass) {
		go s.monitorTeamSharedPVCBinding(context.Background(), namespace, pvcName, userID, teamID, storageSizeGB, storageClass, s.pvcBindTimeout())
	}
	return createdPVC, nil
}

func usesManualHostPathFallback(storageClass string) bool {
	return strings.EqualFold(strings.TrimSpace(storageClass), "manual")
}

func (s *PVCService) teamSharedPVCStorageClass(requested string, useWorkspaceNFS bool) string {
	requested = strings.TrimSpace(requested)
	if !useWorkspaceNFS {
		return firstNonEmpty(requested, s.client.WorkspaceStorageClass, s.client.StorageClass)
	}
	workspaceClass := strings.TrimSpace(s.client.WorkspaceStorageClass)
	globalClass := strings.TrimSpace(s.client.StorageClass)
	instanceClass := strings.TrimSpace(s.client.InstanceStorageClass)
	if requested == "" ||
		strings.EqualFold(requested, globalClass) ||
		strings.EqualFold(requested, instanceClass) {
		return firstNonEmpty(workspaceClass, "manual")
	}
	return requested
}

func (s *PVCService) shouldUseManualHostPathFallback(storageClass string) bool {
	return s.hostPathFallbackEnabled() && usesManualHostPathFallback(storageClass)
}

func (s *PVCService) monitorTeamSharedPVCBinding(ctx context.Context, namespace, pvcName string, userID, teamID, storageSizeGB int, storageClass string, timeout time.Duration) {
	if _, err := s.waitForTeamSharedPVCBinding(ctx, namespace, pvcName, userID, teamID, storageSizeGB, storageClass, timeout); err != nil {
		fmt.Printf("Async Team shared PVC binding monitor failed for %s: %v\n", pvcName, err)
	}
}

func (s *PVCService) waitForTeamSharedPVCBinding(ctx context.Context, namespace, pvcName string, userID, teamID, storageSizeGB int, storageClass string, timeout time.Duration) (*corev1.PersistentVolumeClaim, error) {
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Team shared PVC %s: %w", pvcName, err)
	}
	if pvc.Status.Phase == corev1.ClaimBound {
		return pvc, nil
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutChan := time.After(timeout)

	for {
		select {
		case <-timeoutChan:
			if usesManualHostPathFallback(storageClass) && !s.hostPathFallbackEnabled() {
				return nil, fmt.Errorf("Team shared PVC %s was not bound after %s and hostPath fallback is disabled for storage profile %q", pvcName, timeout, s.storageProfile())
			}
			if !usesManualHostPathFallback(storageClass) {
				fmt.Printf("Team shared PVC %s binding timeout with dynamic storageClass %s, leaving binding to the provisioner\n", pvcName, storageClass)
				return pvc, nil
			}
			fmt.Printf("Team shared PVC %s binding timeout, creating hostPath RWX PV manually\n", pvcName)
			return s.createPVForTeamSharedPVC(ctx, namespace, pvcName, userID, teamID, storageSizeGB, storageClass)
		case <-ticker.C:
			pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get Team shared PVC %s during wait: %w", pvcName, err)
			}
			if pvc.Status.Phase == corev1.ClaimBound {
				fmt.Printf("Team shared PVC %s bound successfully to %s\n", pvcName, pvc.Spec.VolumeName)
				return pvc, nil
			}
			fmt.Printf("Waiting for Team shared PVC %s binding, current status: %s\n", pvcName, pvc.Status.Phase)
		}
	}
}

func (s *PVCService) createPVForTeamSharedPVC(ctx context.Context, namespace, pvcName string, userID, teamID, storageSizeGB int, storageClass string) (*corev1.PersistentVolumeClaim, error) {
	pvName := s.client.GetTeamSharedPVName(userID, teamID)

	existingPV, err := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err == nil && existingPV != nil {
		if existingPV.Status.Phase == corev1.VolumeReleased {
			if deleteErr := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{}); deleteErr != nil && !errors.IsNotFound(deleteErr) {
				return nil, fmt.Errorf("failed to delete released Team shared PV %s: %w", pvName, deleteErr)
			}
			time.Sleep(3 * time.Second)
		} else if existingPV.Spec.ClaimRef != nil &&
			existingPV.Spec.ClaimRef.Namespace == namespace &&
			existingPV.Spec.ClaimRef.Name == pvcName {
			if strings.TrimSpace(s.client.WorkspaceNFSServer) != "" {
				nfsPath := teamSharedWorkspaceNFSPath(s.client.WorkspaceNFSPath, userID, teamID)
				nfsServer, nfsErr := s.workspaceNFSServerForPV(ctx)
				if nfsErr != nil {
					return nil, nfsErr
				}
				if existingPV.Spec.NFS == nil ||
					strings.TrimSpace(existingPV.Spec.NFS.Server) != nfsServer ||
					strings.TrimSpace(existingPV.Spec.NFS.Path) != nfsPath {
					return nil, fmt.Errorf("Team shared PV %s already exists for %s/%s but is not workspace NFS-backed", pvName, namespace, pvcName)
				}
			}
			time.Sleep(2 * time.Second)
			return s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
		} else if existingPV.Spec.ClaimRef != nil {
			return nil, fmt.Errorf("Team shared PV %s already belongs to %s/%s", pvName, existingPV.Spec.ClaimRef.Namespace, existingPV.Spec.ClaimRef.Name)
		}
	}

	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Team shared PVC %s for UID: %w", pvcName, err)
	}

	if strings.TrimSpace(s.client.WorkspaceNFSServer) != "" {
		return s.createWorkspaceNFSPVForTeamSharedPVC(ctx, namespace, pvcName, pvc, userID, teamID, storageSizeGB, storageClass, pvName)
	}

	hostPathPrefix := "/data/clawreef"
	if s.client != nil && s.client.HostPathPrefix != "" {
		hostPathPrefix = s.client.HostPathPrefix
	}
	hostPath := fmt.Sprintf("%s/user-%d/team-%d-shared", hostPathPrefix, userID, teamID)
	storageSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))
	nodeAffinity, err := s.hostPathPVNodeAffinity(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to select node for Team shared hostPath PV %s: %w", pvName, err)
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				"app":               "clawreef",
				"user-id":           fmt.Sprintf("%d", userID),
				"team-id":           fmt.Sprintf("%d", teamID),
				"managed-by":        "clawreef",
				storageProfileLabel: storageProfileSingle,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				workspacePVCAccessMode(s.client.WorkspaceAccessMode),
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              storageClass,
			NodeAffinity:                  nodeAffinity,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
					Type: func() *corev1.HostPathType {
						t := corev1.HostPathDirectoryOrCreate
						return &t
					}(),
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Kind:            "PersistentVolumeClaim",
				APIVersion:      "v1",
				Namespace:       namespace,
				Name:            pvcName,
				UID:             pvc.UID,
				ResourceVersion: pvc.ResourceVersion,
			},
		},
	}
	if _, err := s.client.Clientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("failed to create Team shared PV %s: %w", pvName, err)
	}

	time.Sleep(3 * time.Second)
	pvc, err = s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Team shared PVC after PV creation: %w", err)
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, fmt.Errorf("Team shared PVC %s is still not bound after PV creation, status: %s", pvcName, pvc.Status.Phase)
	}
	fmt.Printf("Team shared PVC %s successfully bound to PV %s\n", pvcName, pvName)
	return pvc, nil
}

func (s *PVCService) createWorkspaceNFSPVForTeamSharedPVC(ctx context.Context, namespace, pvcName string, pvc *corev1.PersistentVolumeClaim, userID, teamID, storageSizeGB int, storageClass, pvName string) (*corev1.PersistentVolumeClaim, error) {
	if err := s.ensureWorkspaceNFSPVForTeamSharedPVC(ctx, namespace, pvcName, pvc, userID, teamID, storageSizeGB, storageClass, pvName); err != nil {
		return nil, err
	}

	time.Sleep(3 * time.Second)
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Team shared PVC after NFS PV creation: %w", err)
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, fmt.Errorf("Team shared PVC %s is still not bound after NFS PV creation, status: %s", pvcName, pvc.Status.Phase)
	}
	fmt.Printf("Team shared PVC %s successfully bound to NFS PV %s (%s:%s)\n", pvcName, pvName, s.client.WorkspaceNFSServer, teamSharedWorkspaceNFSPath(s.client.WorkspaceNFSPath, userID, teamID))
	return pvc, nil
}

func (s *PVCService) ensureWorkspaceNFSPVForTeamSharedPVC(ctx context.Context, namespace, pvcName string, pvc *corev1.PersistentVolumeClaim, userID, teamID, storageSizeGB int, storageClass, pvName string) error {
	if err := s.ensureTeamSharedWorkspaceDirectory(userID, teamID); err != nil {
		return err
	}

	storageSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))
	nfsPath := teamSharedWorkspaceNFSPath(s.client.WorkspaceNFSPath, userID, teamID)
	nfsServer, err := s.workspaceNFSServerForPV(ctx)
	if err != nil {
		return err
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				"app":               "clawreef",
				"user-id":           fmt.Sprintf("%d", userID),
				"team-id":           fmt.Sprintf("%d", teamID),
				"managed-by":        "clawreef",
				storageProfileLabel: storageProfileLegacyNFS,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteMany,
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              storageClass,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: nfsServer,
					Path:   nfsPath,
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Kind:            "PersistentVolumeClaim",
				APIVersion:      "v1",
				Namespace:       namespace,
				Name:            pvcName,
				UID:             pvc.UID,
				ResourceVersion: pvc.ResourceVersion,
			},
		},
	}
	if _, err := s.client.Clientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing Team shared NFS PV %s: %w", pvName, getErr)
			}
			if existing.Spec.ClaimRef != nil && existing.Spec.ClaimRef.Namespace == namespace && existing.Spec.ClaimRef.Name == pvcName {
				if existing.Spec.NFS != nil &&
					strings.TrimSpace(existing.Spec.NFS.Server) == nfsServer &&
					strings.TrimSpace(existing.Spec.NFS.Path) == nfsPath {
					return nil
				}
				return fmt.Errorf("Team shared PV %s already exists for %s/%s but is not workspace NFS-backed", pvName, namespace, pvcName)
			}
		}
		return fmt.Errorf("failed to create Team shared NFS PV %s: %w", pvName, err)
	}
	return nil
}

func (s *PVCService) workspaceNFSServerForPV(ctx context.Context) (string, error) {
	server := strings.TrimSpace(s.client.WorkspaceNFSServer)
	if server == "" || s.client.Clientset == nil {
		return server, nil
	}
	serviceName, namespace, ok := workspaceNFSServiceRef(server, s.client.Namespace)
	if !ok {
		return server, nil
	}
	svc, err := s.client.Clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return server, nil
		}
		return "", fmt.Errorf("failed to resolve workspace NFS service %s/%s: %w", namespace, serviceName, err)
	}
	clusterIP := strings.TrimSpace(svc.Spec.ClusterIP)
	if clusterIP == "" || strings.EqualFold(clusterIP, corev1.ClusterIPNone) {
		return server, nil
	}
	return clusterIP, nil
}

func workspaceNFSServiceRef(server, defaultNamespace string) (string, string, bool) {
	host := strings.TrimSuffix(strings.TrimSpace(server), ".")
	if host == "" || net.ParseIP(host) != nil {
		return "", "", false
	}
	parts := strings.Split(strings.ToLower(host), ".")
	if len(parts) == 1 {
		namespace := strings.TrimSpace(defaultNamespace)
		if namespace == "" {
			namespace = "default"
		}
		return parts[0], namespace, true
	}
	if len(parts) >= 3 && parts[2] == "svc" && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func (s *PVCService) ensureTeamSharedWorkspaceDirectory(userID, teamID int) error {
	root := strings.TrimSpace(s.client.WorkspaceRoot)
	if root == "" {
		return nil
	}
	dir := filepath.Join(root, filepath.FromSlash(TeamSharedWorkspaceRelativePath(userID, teamID)))
	dirs := append([]string{dir}, teamSharedRuntimeSubdirectories(dir)...)
	for _, target := range dirs {
		if err := os.MkdirAll(target, teamSharedDirectoryMode); err != nil {
			return fmt.Errorf("failed to create Team shared runtime workspace directory %s: %w", target, err)
		}
		_ = os.Chown(target, 1000, 1000)
		if err := os.Chmod(target, teamSharedDirectoryMode); err != nil {
			return fmt.Errorf("failed to chmod Team shared runtime workspace directory %s: %w", target, err)
		}
	}
	return nil
}

func teamSharedRuntimeSubdirectories(root string) []string {
	return []string{
		filepath.Join(root, "status"),
		filepath.Join(root, "inbox"),
		filepath.Join(root, "results"),
		filepath.Join(root, "tasks"),
		filepath.Join(root, ".openclaw-redis-team"),
		filepath.Join(root, ".openclaw-redis-team", "tasks"),
	}
}

func TeamSharedWorkspaceRelativePath(userID, teamID int) string {
	return fmt.Sprintf("teams/user-%d/team-%d-shared", userID, teamID)
}

func TeamSharedWorkspacePath(workspaceRoot string, userID, teamID int) string {
	root := strings.TrimRight(strings.TrimSpace(workspaceRoot), "/")
	if root == "" {
		root = "/workspaces"
	}
	return root + "/" + TeamSharedWorkspaceRelativePath(userID, teamID)
}

func teamSharedWorkspaceNFSPath(basePath string, userID, teamID int) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		basePath = "/"
	}
	relativePath := TeamSharedWorkspaceRelativePath(userID, teamID)
	if basePath == "/" {
		return "/" + relativePath
	}
	return path.Join(basePath, relativePath)
}

func (s *PVCService) hostPathPVNodeHostname(ctx context.Context) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("k8s client not initialized")
	}
	nodes, err := s.client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	type candidate struct {
		name     string
		hostname string
	}
	candidates := []candidate{}
	for _, node := range nodes.Items {
		if !isHostPathPVNodeCandidate(node) {
			continue
		}
		hostname := strings.TrimSpace(node.Labels[nodeHostnameLabel])
		if hostname == "" {
			hostname = strings.TrimSpace(node.Name)
		}
		if hostname == "" {
			continue
		}
		candidates = append(candidates, candidate{name: node.Name, hostname: hostname})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no ready node found for hostPath PV")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].name < candidates[j].name
	})
	return candidates[0].hostname, nil
}

func (s *PVCService) hostPathPVNodeSelector(ctx context.Context) (map[string]string, error) {
	hostname, err := s.hostPathPVNodeHostname(ctx)
	if err != nil {
		return nil, err
	}
	return nodeSelectorForHostname(hostname), nil
}

func (s *PVCService) hostPathPVNodeAffinity(ctx context.Context) (*corev1.VolumeNodeAffinity, error) {
	hostname, err := s.hostPathPVNodeHostname(ctx)
	if err != nil {
		return nil, err
	}
	return hostPathPVNodeAffinityForHostname(hostname), nil
}

func hostPathPVNodeAffinityForHostname(hostname string) *corev1.VolumeNodeAffinity {
	return &corev1.VolumeNodeAffinity{
		Required: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      nodeHostnameLabel,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{hostname},
						},
					},
				},
			},
		},
	}
}

func isStorageNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func isHostPathPVNodeCandidate(node corev1.Node) bool {
	if node.Spec.Unschedulable || !isStorageNodeReady(node) {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	return true
}

func (s *PVCService) monitorPVCBinding(ctx context.Context, namespace, pvcName string, userID, instanceID, storageSizeGB int, storageClass string, timeout time.Duration) {
	if _, err := s.waitForPVCBinding(ctx, namespace, pvcName, userID, instanceID, storageSizeGB, storageClass, timeout); err != nil {
		fmt.Printf("Async PVC binding monitor failed for %s: %v\n", pvcName, err)
	}
}

// waitForPVCBinding waits for PVC to be bound, if timeout creates PV manually
func (s *PVCService) waitForPVCBinding(ctx context.Context, namespace, pvcName string, userID, instanceID, storageSizeGB int, storageClass string, timeout time.Duration) (*corev1.PersistentVolumeClaim, error) {
	// Check if PVC is already bound
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC %s: %w", pvcName, err)
	}

	if pvc.Status.Phase == corev1.ClaimBound {
		fmt.Printf("PVC %s is already bound to %s\n", pvcName, pvc.Spec.VolumeName)
		return pvc, nil
	}

	// Wait for binding with timeout
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutChan := time.After(timeout)

	for {
		select {
		case <-timeoutChan:
			if usesManualHostPathFallback(storageClass) && !s.hostPathFallbackEnabled() {
				return nil, fmt.Errorf("PVC %s was not bound after %s and hostPath fallback is disabled for storage profile %q", pvcName, timeout, s.storageProfile())
			}
			if !usesManualHostPathFallback(storageClass) {
				fmt.Printf("PVC %s binding timeout with dynamic storageClass %s, leaving binding to the provisioner\n", pvcName, storageClass)
				return pvc, nil
			}
			// Timeout, try to create PV manually.
			fmt.Printf("PVC %s binding timeout, creating PV manually\n", pvcName)
			return s.createPVForPVC(ctx, namespace, pvcName, userID, instanceID, storageSizeGB, storageClass)
		case <-ticker.C:
			pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get PVC %s during wait: %w", pvcName, err)
			}
			if pvc.Status.Phase == corev1.ClaimBound {
				fmt.Printf("PVC %s bound successfully to %s\n", pvcName, pvc.Spec.VolumeName)
				return pvc, nil
			}
			fmt.Printf("Waiting for PVC %s binding, current status: %s\n", pvcName, pvc.Status.Phase)
		}
	}
}

// createPVForPVC creates a PV manually to bind to the PVC
func (s *PVCService) createPVForPVC(ctx context.Context, namespace, pvcName string, userID, instanceID, storageSizeGB int, storageClass string) (*corev1.PersistentVolumeClaim, error) {
	pvName := s.client.GetInstancePVName(userID, instanceID)
	// Use configurable host path prefix for persistent storage
	hostPathPrefix := "/data/clawreef"
	if s.client != nil && s.client.HostPathPrefix != "" {
		hostPathPrefix = s.client.HostPathPrefix
	}
	hostPath := fmt.Sprintf("%s/user-%d/instance-%d", hostPathPrefix, userID, instanceID)

	fmt.Printf("Creating PV %s with hostPath %s for PVC %s\n", pvName, hostPath, pvcName)

	// Check if PV already exists
	existingPV, err := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err == nil && existingPV != nil {
		fmt.Printf("PV %s already exists (status: %s), checking if it's bound to our PVC\n", pvName, existingPV.Status.Phase)

		// Check if PV is in Released state - if so, we need to delete and recreate it
		if existingPV.Status.Phase == corev1.VolumeReleased {
			fmt.Printf("PV %s is in Released state, deleting it to recreate\n", pvName)
			deleteErr := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
			if deleteErr != nil && !errors.IsNotFound(deleteErr) {
				return nil, fmt.Errorf("failed to delete released PV %s: %w", pvName, deleteErr)
			}
			// Wait for PV deletion
			time.Sleep(3 * time.Second)
		} else if existingPV.Spec.ClaimRef != nil &&
			existingPV.Spec.ClaimRef.Namespace == namespace &&
			existingPV.Spec.ClaimRef.Name == pvcName {
			// PV is already bound to our PVC, wait a bit for binding to complete
			time.Sleep(2 * time.Second)
			pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get PVC after existing PV check: %w", err)
			}
			return pvc, nil
		} else if existingPV.Spec.ClaimRef != nil {
			// PV exists but bound to different PVC, delete it
			fmt.Printf("PV %s exists but bound to different claim (%s/%s), deleting it\n",
				pvName, existingPV.Spec.ClaimRef.Namespace, existingPV.Spec.ClaimRef.Name)
			deleteErr := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
			if deleteErr != nil && !errors.IsNotFound(deleteErr) {
				return nil, fmt.Errorf("failed to delete existing PV %s: %w", pvName, deleteErr)
			}
			// Wait for PV deletion
			time.Sleep(3 * time.Second)
		}
	}

	// Get PVC to get its UID
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC %s for UID: %w", pvcName, err)
	}

	storageSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))
	nodeAffinity, err := s.hostPathPVNodeAffinity(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to select node for hostPath PV %s: %w", pvName, err)
	}

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				"app":               "clawreef",
				"user-id":           fmt.Sprintf("%d", userID),
				"instance-id":       fmt.Sprintf("%d", instanceID),
				"managed-by":        "clawreef",
				storageProfileLabel: storageProfileSingle,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              storageClass,
			NodeAffinity:                  nodeAffinity,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
					Type: func() *corev1.HostPathType {
						t := corev1.HostPathDirectoryOrCreate
						return &t
					}(),
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Kind:            "PersistentVolumeClaim",
				APIVersion:      "v1",
				Namespace:       namespace,
				Name:            pvcName,
				UID:             pvc.UID,
				ResourceVersion: pvc.ResourceVersion,
			},
		},
	}

	_, err = s.client.Clientset.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// PV was created by another process, wait for it to bind
			fmt.Printf("PV %s was created by another process, waiting for binding\n", pvName)
			time.Sleep(3 * time.Second)
			pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to get PVC after PV creation: %w", err)
			}
			return pvc, nil
		}
		return nil, fmt.Errorf("failed to create PV %s: %w", pvName, err)
	}

	fmt.Printf("PV %s created successfully, waiting for binding\n", pvName)

	// Wait for PVC to be bound
	time.Sleep(3 * time.Second)
	pvc, err = s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC after PV creation: %w", err)
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return nil, fmt.Errorf("PVC %s is still not bound after PV creation, status: %s", pvcName, pvc.Status.Phase)
	}

	fmt.Printf("PVC %s successfully bound to PV %s\n", pvcName, pvName)
	return pvc, nil
}

// GetPVC gets a PVC by user ID and instance ID
func (s *PVCService) GetPVC(ctx context.Context, userID, instanceID int) (*corev1.PersistentVolumeClaim, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	pvcName := s.client.GetPVCName(instanceID)
	namespace := s.client.GetNamespace(userID)

	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC %s: %w", pvcName, err)
	}

	return pvc, nil
}

func (s *PVCService) NodeSelectorForPVC(ctx context.Context, userID, instanceID int, storageClass string) (map[string]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	pvcName := s.client.GetPVCName(instanceID)
	namespace := s.client.GetNamespace(userID)
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get PVC %s: %w", pvcName, err)
	}

	storageClass = pvcStorageClassName(pvc, firstNonEmpty(storageClass, s.client.InstanceStorageClass, s.client.StorageClass))

	if strings.TrimSpace(pvc.Spec.VolumeName) != "" {
		pv, err := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get bound PV %s for PVC %s: %w", pvc.Spec.VolumeName, pvcName, err)
		}
		return nodeSelectorFromPVNodeAffinity(pv.Spec.NodeAffinity), nil
	}

	return s.nodeSelectorForStorageClass(ctx, storageClass)
}

func (s *PVCService) nodeSelectorForStorageClass(ctx context.Context, storageClass string) (map[string]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	storageClass = strings.TrimSpace(storageClass)
	if storageClass == "" {
		storageClass = firstNonEmpty(s.client.InstanceStorageClass, s.client.StorageClass)
	}
	if storageClass == "" {
		return nil, nil
	}

	sc, err := s.client.Clientset.StorageV1().StorageClasses().Get(ctx, storageClass, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get storage class %s: %w", storageClass, err)
	}
	if sc.Provisioner != noProvisionerName {
		return nil, nil
	}
	return s.hostPathPVNodeSelector(ctx)
}

func pvcStorageClassName(pvc *corev1.PersistentVolumeClaim, fallback string) string {
	storageClass := strings.TrimSpace(fallback)
	if storageClass == "" && pvc != nil && pvc.Spec.StorageClassName != nil {
		storageClass = strings.TrimSpace(*pvc.Spec.StorageClassName)
	}
	return storageClass
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func workspacePVCAccessMode(value string) corev1.PersistentVolumeAccessMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "readwriteonce", "rwo":
		return corev1.ReadWriteOnce
	case "readonlymany", "rox":
		return corev1.ReadOnlyMany
	case "readwritemany", "rwx", "":
		return corev1.ReadWriteMany
	default:
		return corev1.ReadWriteMany
	}
}

func (s *PVCService) pvcBindTimeout() time.Duration {
	if s != nil && s.client != nil && s.client.PVCBindTimeout > 0 {
		return s.client.PVCBindTimeout
	}
	return defaultPVCBindTimeout
}

func (s *PVCService) storageProfile() string {
	if s == nil || s.client == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s.client.StorageProfile))
}

func (s *PVCService) hostPathFallbackEnabled() bool {
	if s == nil || s.client == nil || !s.client.HostPathFallbackEnabled {
		return false
	}
	profile := s.storageProfile()
	return profile == "" || profile == storageProfileSingle
}

func isClawManagerHostPathPV(pv *corev1.PersistentVolume) bool {
	if pv == nil || pv.Spec.HostPath == nil {
		return false
	}
	if pv.Labels["managed-by"] != "clawreef" {
		return false
	}
	if strings.EqualFold(pv.Labels[storageProfileLabel], storageProfileSingle) {
		return true
	}
	return strings.HasPrefix(pv.Name, "clawreef-pv-user-")
}

func nodeSelectorForHostname(hostname string) map[string]string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil
	}
	return map[string]string{nodeHostnameLabel: hostname}
}

func nodeSelectorFromPVNodeAffinity(affinity *corev1.VolumeNodeAffinity) map[string]string {
	if affinity == nil || affinity.Required == nil {
		return nil
	}
	for _, term := range affinity.Required.NodeSelectorTerms {
		for _, expression := range term.MatchExpressions {
			if expression.Key != nodeHostnameLabel || expression.Operator != corev1.NodeSelectorOpIn {
				continue
			}
			if len(expression.Values) != 1 {
				continue
			}
			return nodeSelectorForHostname(expression.Values[0])
		}
	}
	return nil
}

// DeletePVC deletes a PVC and associated PV
func (s *PVCService) DeletePVC(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	pvcName := s.client.GetPVCName(instanceID)
	namespace := s.client.GetNamespace(userID)
	pvName := s.client.GetInstancePVName(userID, instanceID)

	// Get PVC first to find the associated PV
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// PVC doesn't exist, still try to delete the PV directly
			fmt.Printf("PVC %s not found, trying to delete PV %s directly\n", pvcName, pvName)
			s.deleteManagedHostPathPV(ctx, pvName)
			return nil
		}
		return fmt.Errorf("failed to get PVC %s: %w", pvcName, err)
	}

	// Store the PV name before deleting PVC
	boundPVName := pvc.Spec.VolumeName

	// Delete the PVC first
	err = s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvcName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// PVC already deleted, continue to delete PV
			fmt.Printf("PVC %s already deleted\n", pvcName)
		} else {
			return fmt.Errorf("failed to delete PVC %s: %w", pvcName, err)
		}
	} else {
		fmt.Printf("PVC %s deleted successfully\n", pvcName)
	}

	// Delete the manually created PV
	// First try the predictable name
	s.deleteManagedHostPathPV(ctx, pvName)

	// Also try to delete the bound PV if it exists and is different
	if boundPVName != "" && boundPVName != pvName {
		s.deleteManagedHostPathPV(ctx, boundPVName)
	}

	return nil
}

func (s *PVCService) deleteManagedHostPathPV(ctx context.Context, pvName string) {
	pvName = strings.TrimSpace(pvName)
	if pvName == "" || s == nil || s.client == nil || s.client.Clientset == nil {
		return
	}
	pv, err := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("PV %s not found or already deleted\n", pvName)
		} else {
			fmt.Printf("Warning: failed to get PV %s before delete: %v\n", pvName, err)
		}
		return
	}
	if !isClawManagerHostPathPV(pv) {
		fmt.Printf("Skipping PV %s delete because it is not a ClawManager-managed hostPath PV\n", pvName)
		return
	}
	if err := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			fmt.Printf("Warning: failed to delete PV %s: %v\n", pvName, err)
		}
		return
	}
	fmt.Printf("PV %s deleted successfully\n", pvName)
}

func (s *PVCService) deleteManagedTeamSharedPV(ctx context.Context, pvName string, userID, teamID int) {
	pvName = strings.TrimSpace(pvName)
	if pvName == "" || s == nil || s.client == nil || s.client.Clientset == nil {
		return
	}
	pv, err := s.client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("Team shared PV %s not found or already deleted\n", pvName)
		} else {
			fmt.Printf("Warning: failed to get Team shared PV %s before delete: %v\n", pvName, err)
		}
		return
	}
	if pv.Labels["managed-by"] != "clawreef" ||
		pv.Labels["user-id"] != fmt.Sprintf("%d", userID) ||
		pv.Labels["team-id"] != fmt.Sprintf("%d", teamID) {
		fmt.Printf("Skipping Team shared PV %s delete because it is not owned by user %d team %d\n", pvName, userID, teamID)
		return
	}
	if pv.Spec.HostPath == nil && pv.Spec.NFS == nil {
		fmt.Printf("Skipping Team shared PV %s delete because it is not a ClawManager hostPath/NFS PV\n", pvName)
		return
	}
	if err := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			fmt.Printf("Warning: failed to delete Team shared PV %s: %v\n", pvName, err)
		}
		return
	}
	fmt.Printf("Team shared PV %s deleted successfully\n", pvName)
}

// DeleteTeamSharedPVC deletes a Team shared PVC and the predictable Team PV
// managed by ClawManager. It only deletes a bound PV when that PV is explicitly
// labeled as belonging to this Team, avoiding accidental deletion of external
// provisioner volumes.
func (s *PVCService) DeleteTeamSharedPVC(ctx context.Context, userID, teamID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	pvcName := s.client.GetTeamSharedPVCName(teamID)
	namespace := s.client.GetNamespace(userID)
	pvName := s.client.GetTeamSharedPVName(userID, teamID)
	boundPVName := ""

	if pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{}); err == nil && pvc != nil {
		boundPVName = pvc.Spec.VolumeName
	} else if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get Team shared PVC %s/%s before delete: %w", namespace, pvcName, err)
	}

	if err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvcName, metav1.DeleteOptions{}); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Team shared PVC %s/%s: %w", namespace, pvcName, err)
		}
	}
	s.deleteManagedTeamSharedPV(ctx, pvName, userID, teamID)
	if boundPVName != "" && boundPVName != pvName {
		s.deleteManagedTeamSharedPV(ctx, boundPVName, userID, teamID)
	}
	return nil
}

// PVCExists checks if a PVC exists
func (s *PVCService) PVCExists(ctx context.Context, userID, instanceID int) (bool, error) {
	_, err := s.GetPVC(ctx, userID, instanceID)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
