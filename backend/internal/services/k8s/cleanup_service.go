package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CleanupService handles cleanup of K8s resources
type CleanupService struct {
	client *Client
}

// NewCleanupService creates a new cleanup service
func NewCleanupService() *CleanupService {
	return &CleanupService{
		client: globalClient,
	}
}

// DeleteAllInstanceResources deletes all K8s resources for an instance (including duplicates)
// If userID is 0, it will search all clawreef namespaces
func (s *CleanupService) DeleteAllInstanceResources(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	instanceLabel := fmt.Sprintf("%d", instanceID)

	// If userID is 0, find all namespaces with this instance
	var namespaces []string
	if userID == 0 {
		nsList, err := s.findNamespacesWithInstance(ctx, instanceLabel)
		if err != nil {
			fmt.Printf("Warning: failed to find namespaces: %v\n", err)
		}
		namespaces = nsList
		fmt.Printf("Found instance %d in %d namespace(s): %v\n", instanceID, len(namespaces), namespaces)
	} else {
		namespaces = []string{s.client.GetNamespace(userID)}
	}

	// Delete resources from all found namespaces
	for _, namespace := range namespaces {
		fmt.Printf("Deleting resources for instance %d in namespace %s\n", instanceID, namespace)

		// 1. Delete all Pods with matching instance-id label
		if err := s.deleteAllPods(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting pods: %v\n", err)
		}

		// 2. Delete all PVCs with matching instance-id label
		if err := s.deleteAllPVCs(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting PVCs: %v\n", err)
		}

		// 4. Delete all Services with matching instance-id label
		if err := s.deleteAllServices(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting services: %v\n", err)
		}

		// 5. Delete all NetworkPolicies with matching instance-id label
		if err := s.deleteAllNetworkPolicies(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting network policies: %v\n", err)
		}

		// 6. Delete all ConfigMaps with matching instance-id label
		if err := s.deleteAllConfigMaps(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting configmaps: %v\n", err)
		}

		// 7. Delete all Secrets with matching instance-id label
		if err := s.deleteAllSecrets(ctx, namespace, instanceLabel); err != nil {
			fmt.Printf("Warning: error deleting secrets: %v\n", err)
		}
	}

	// 3. Delete all PVs associated with this instance (PVs are not namespaced)
	if err := s.deleteAllPVs(ctx, userID, instanceID); err != nil {
		fmt.Printf("Warning: error deleting PVs: %v\n", err)
	}

	// Wait for all resources to be actually deleted (with 30 second timeout)
	if err := s.WaitForResourceDeletion(ctx, namespaces, instanceLabel, 30*time.Second); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	return nil
}

// findNamespacesWithInstance finds all namespaces containing resources for this instance
func (s *CleanupService) findNamespacesWithInstance(ctx context.Context, instanceLabel string) ([]string, error) {
	// List all namespaces with clawreef label
	namespaces, err := s.client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err != nil {
		return nil, err
	}

	var result []string
	for _, ns := range namespaces.Items {
		// Check if this namespace has any pods for this instance
		pods, err := s.client.Clientset.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
		})
		if err == nil && len(pods.Items) > 0 {
			result = append(result, ns.Name)
			continue
		}

		// Check if this namespace has any PVCs for this instance
		pvcs, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
		})
		if err == nil && len(pvcs.Items) > 0 {
			result = append(result, ns.Name)
			continue
		}

		networkPolicies, err := s.client.Clientset.NetworkingV1().NetworkPolicies(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
		})
		if err == nil && len(networkPolicies.Items) > 0 {
			result = append(result, ns.Name)
		}
	}

	return result, nil
}

// deleteAllPods deletes all pods with matching instance-id label
func (s *CleanupService) deleteAllPods(ctx context.Context, namespace, instanceLabel string) error {
	pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("No pods found for instance %s\n", instanceLabel)
		return nil
	}

	for _, pod := range pods.Items {
		fmt.Printf("Deleting pod: %s\n", pod.Name)
		if err := s.client.Clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete pod %s: %v\n", pod.Name, err)
		}
	}

	return nil
}

// deleteAllPVCs deletes all PVCs with matching instance-id label
func (s *CleanupService) deleteAllPVCs(ctx context.Context, namespace, instanceLabel string) error {
	pvcs, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list PVCs: %w", err)
	}

	if len(pvcs.Items) == 0 {
		fmt.Printf("No PVCs found for instance %s\n", instanceLabel)
		return nil
	}

	for _, pvc := range pvcs.Items {
		fmt.Printf("Deleting PVC: %s\n", pvc.Name)
		if err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete PVC %s: %v\n", pvc.Name, err)
		}
	}

	return nil
}

// deleteAllPVs deletes all PVs for an instance
func (s *CleanupService) deleteAllPVs(ctx context.Context, userID, instanceID int) error {
	instanceLabel := fmt.Sprintf("%d", instanceID)

	// If userID is specified, try to delete the predictable PV name first
	if userID > 0 {
		pvName := fmt.Sprintf("clawreef-pv-user-%d-instance-%d", userID, instanceID)
		fmt.Printf("Deleting PV: %s\n", pvName)
		if err := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{}); err != nil {
			// Ignore not found error
			if !errors.IsNotFound(err) {
				fmt.Printf("Warning: failed to delete PV %s: %v\n", pvName, err)
			}
		}
	}

	// Search for any PVs with matching instance-id label
	pvs, err := s.client.Clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list PVs: %w", err)
	}

	for _, pv := range pvs.Items {
		fmt.Printf("Deleting PV: %s\n", pv.Name)
		if err := s.client.Clientset.CoreV1().PersistentVolumes().Delete(ctx, pv.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete PV %s: %v\n", pv.Name, err)
		}
	}

	return nil
}

// deleteAllServices deletes all services with matching instance-id label
func (s *CleanupService) deleteAllServices(ctx context.Context, namespace, instanceLabel string) error {
	services, err := s.client.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services.Items {
		fmt.Printf("Deleting service: %s\n", svc.Name)
		if err := s.client.Clientset.CoreV1().Services(namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete service %s: %v\n", svc.Name, err)
		}
	}

	return nil
}

// deleteAllNetworkPolicies deletes all network policies with matching instance-id label.
func (s *CleanupService) deleteAllNetworkPolicies(ctx context.Context, namespace, instanceLabel string) error {
	networkPolicies, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list network policies: %w", err)
	}

	for _, policy := range networkPolicies.Items {
		fmt.Printf("Deleting network policy: %s\n", policy.Name)
		if err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, policy.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete network policy %s: %v\n", policy.Name, err)
		}
	}

	return nil
}

// deleteAllConfigMaps deletes all configmaps with matching instance-id label
func (s *CleanupService) deleteAllConfigMaps(ctx context.Context, namespace, instanceLabel string) error {
	configMaps, err := s.client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list configmaps: %w", err)
	}

	for _, cm := range configMaps.Items {
		fmt.Printf("Deleting configmap: %s\n", cm.Name)
		if err := s.client.Clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete configmap %s: %v\n", cm.Name, err)
		}
	}

	return nil
}

// deleteAllSecrets deletes all secrets with matching instance-id label
func (s *CleanupService) deleteAllSecrets(ctx context.Context, namespace, instanceLabel string) error {
	secrets, err := s.client.Clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
	})
	if err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	for _, secret := range secrets.Items {
		fmt.Printf("Deleting secret: %s\n", secret.Name)
		if err := s.client.Clientset.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
			fmt.Printf("Warning: failed to delete secret %s: %v\n", secret.Name, err)
		}
	}

	return nil
}

// WaitForResourceDeletion waits for all resources to be deleted with timeout
func (s *CleanupService) WaitForResourceDeletion(ctx context.Context, namespaces []string, instanceLabel string, timeout time.Duration) error {
	fmt.Printf("Waiting up to %v for resources to be deleted...\n", timeout)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeoutChan := time.After(timeout)

	for {
		select {
		case <-timeoutChan:
			return fmt.Errorf("timeout waiting for resources to be deleted")
		case <-ticker.C:
			allDeleted := true

			for _, namespace := range namespaces {
				// Check pods
				pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
				})
				if err == nil && len(pods.Items) > 0 {
					allDeleted = false
					fmt.Printf("  Still waiting: %d pod(s) in %s\n", len(pods.Items), namespace)
				}

				// Check PVCs
				pvcs, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
				})
				if err == nil && len(pvcs.Items) > 0 {
					allDeleted = false
					fmt.Printf("  Still waiting: %d PVC(s) in %s\n", len(pvcs.Items), namespace)
				}

				networkPolicies, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
				})
				if err == nil && len(networkPolicies.Items) > 0 {
					allDeleted = false
					fmt.Printf("  Still waiting: %d network policy(s) in %s\n", len(networkPolicies.Items), namespace)
				}
			}

			// Check PVs
			pvs, err := s.client.Clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("instance-id=%s,managed-by=clawreef", instanceLabel),
			})
			if err == nil && len(pvs.Items) > 0 {
				allDeleted = false
				fmt.Printf("  Still waiting: %d PV(s)\n", len(pvs.Items))
			}

			if allDeleted {
				fmt.Println("All resources deleted successfully")
				return nil
			}
		}
	}
}
