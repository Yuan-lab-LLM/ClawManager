package k8s

import (
	"context"
	"fmt"
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
		storageClass = s.client.StorageClass
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

	// Wait for PVC to be bound, if not bound within timeout, create PV manually
	fmt.Printf("PVC %s created, scheduling async binding monitor...\n", pvcName)
	go s.monitorPVCBinding(context.Background(), namespace, pvcName, userID, instanceID, storageSizeGB, storageClass, 15*time.Second)

	return createdPVC, nil
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
			// Timeout, try to create PV manually
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
	pvName := fmt.Sprintf("clawreef-pv-user-%d-instance-%d", userID, instanceID)
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

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
			Labels: map[string]string{
				"app":         "clawreef",
				"user-id":     fmt.Sprintf("%d", userID),
				"instance-id": fmt.Sprintf("%d", instanceID),
				"managed-by":  "clawreef",
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

// DeletePVC deletes a PVC and associated PV
func (s *PVCService) DeletePVC(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	pvcName := s.client.GetPVCName(instanceID)
	namespace := s.client.GetNamespace(userID)
	pvName := fmt.Sprintf("clawreef-pv-user-%d-instance-%d", userID, instanceID)

	// Get PVC first to find the associated PV
	pvc, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// PVC doesn't exist, still try to delete the PV directly
			fmt.Printf("PVC %s not found, trying to delete PV %s directly\n", pvcName, pvName)
			deleteErr := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
			if deleteErr != nil && !errors.IsNotFound(deleteErr) {
				fmt.Printf("Warning: failed to delete PV %s: %v\n", pvName, deleteErr)
			}
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
	err = s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("PV %s (predictable name) not found\n", pvName)
		} else {
			fmt.Printf("Warning: failed to delete PV %s: %v\n", pvName, err)
		}
	} else {
		fmt.Printf("PV %s deleted successfully\n", pvName)
	}

	// Also try to delete the bound PV if it exists and is different
	if boundPVName != "" && boundPVName != pvName {
		err = s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, boundPVName, metav1.DeleteOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				fmt.Printf("PV %s (bound) not found or already deleted\n", boundPVName)
			} else {
				fmt.Printf("Warning: failed to delete bound PV %s: %v\n", boundPVName, err)
			}
		} else {
			fmt.Printf("Bound PV %s deleted successfully\n", boundPVName)
		}
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
