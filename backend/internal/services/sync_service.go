package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// SyncService handles synchronization between database and K8s state
type SyncService struct {
	instanceRepo         repository.InstanceRepository
	runtimeStatusService InstanceRuntimeStatusService
	podService           *k8s.PodService
	deploymentService    *k8s.InstanceDeploymentService
	interval             time.Duration

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

// NewSyncService creates a new sync service
func NewSyncService(instanceRepo repository.InstanceRepository, runtimeStatusService InstanceRuntimeStatusService) *SyncService {
	return &SyncService{
		instanceRepo:         instanceRepo,
		runtimeStatusService: runtimeStatusService,
		podService:           k8s.NewPodService(),
		deploymentService:    k8s.NewInstanceDeploymentService(),
		interval:             5 * time.Second, // Sync every 5 seconds for more responsive status updates
	}
}

// Start starts the sync loop. It is safe to call repeatedly: a second call
// while already running is a no-op, and after Stop the service can be started
// again (used by leader-election re-acquisition).
func (s *SyncService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.stopChan = make(chan struct{})
	s.running = true
	fmt.Println("Starting K8s state sync service...")
	go s.syncLoop(s.stopChan)
}

// Stop stops the sync loop. It is idempotent: calling Stop when not running is
// a no-op, and it never closes the same channel twice.
func (s *SyncService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopChan)
	s.running = false
}

// syncLoop runs the synchronization loop until its stop channel is closed. The
// channel is passed in (rather than read from the struct) so a restarted loop
// never races with a previously stopped one.
func (s *SyncService) syncLoop(stop <-chan struct{}) {
	fmt.Printf("[SyncService] Starting sync loop with interval %v\n", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run immediately on start
	fmt.Println("[SyncService] Running initial sync...")
	s.syncAllInstances()
	fmt.Println("[SyncService] Initial sync complete")

	for {
		select {
		case <-ticker.C:
			fmt.Println("[SyncService] Tick - running scheduled sync...")
			s.syncAllInstances()
		case <-stop:
			fmt.Println("[SyncService] Stopping K8s state sync service...")
			return
		}
	}
}

// syncAllInstances synchronizes the state of all instances
func (s *SyncService) syncAllInstances() {
	ctx := context.Background()
	release, gateErr := backupgate.HoldWorkUnit(ctx, "instance.sync")
	if gateErr != nil {
		return
	}
	defer release()

	fmt.Println("[SyncService] Fetching all instances from database...")
	// Get all running instances from database
	instances, err := s.instanceRepo.GetAllRunning()
	if err != nil {
		fmt.Printf("[SyncService] Error getting running instances: %v\n", err)
		return
	}

	fmt.Printf("[SyncService] Found %d instances to sync\n", len(instances))

	if len(instances) == 0 {
		fmt.Println("[SyncService] No instances found, skipping sync")
		return
	}

	for i, instance := range instances {
		fmt.Printf("[SyncService] Syncing instance %d/%d: ID=%d, Status=%s\n",
			i+1, len(instances), instance.ID, instance.Status)
		s.syncInstance(ctx, &instance)
	}

	fmt.Println("[SyncService] Sync complete")
}

// syncInstance synchronizes a single instance's state
func (s *SyncService) syncInstance(ctx context.Context, instance *models.Instance) {
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, instance.ID)
	if err != nil {
		return
	}
	defer release()
	current, err := currentLifecycleSnapshot(s.instanceRepo, *instance)
	if err != nil || current == nil || current.Status == "deleting" {
		return
	}
	instance = current
	if _, ok := v2RuntimeTypeForInstance(instance); ok {
		s.updateInfraStatus(instance.ID, instance.Status)
		return
	}
	if instanceUsesDesktopRuntime(instance) {
		s.syncDeploymentInstance(ctx, instance)
		return
	}

	// Check if pod exists in K8s
	pod, err := s.podService.GetPod(ctx, instance.UserID, instance.ID)
	if err != nil {
		deploymentExists, deploymentErr := s.podService.DeploymentExists(ctx, instance.UserID, instance.ID)
		if deploymentErr != nil {
			fmt.Printf("Instance %d: failed to check deployment while pod was missing: %v\n", instance.ID, deploymentErr)
		}
		if deploymentExists {
			if instance.Status != "creating" {
				fmt.Printf("Instance %d has deployment but no pod yet, updating status to creating\n", instance.ID)
				instance.Status = "creating"
				instance.PodName = nil
				instance.PodNamespace = nil
				instance.PodIP = nil
				instance.UpdatedAt = time.Now()

				if err := s.instanceRepo.Update(instance); err != nil {
					fmt.Printf("Error updating instance %d status: %v\n", instance.ID, err)
				} else {
					s.updateInfraStatus(instance.ID, "creating")
					GetHub().BroadcastInstanceStatus(instance.UserID, instance)
				}
			} else {
				s.updateInfraStatus(instance.ID, "creating")
			}
			return
		}

		// Pod doesn't exist in K8s
		if instance.Status == "running" || instance.Status == "creating" {
			nextStatus := "stopped"
			if instance.Status == "creating" {
				nextStatus = "error"
			}

			fmt.Printf("Instance %d marked as %s but pod not found in K8s, updating status to %s\n",
				instance.ID, instance.Status, nextStatus)
			instance.Status = nextStatus
			instance.PodName = nil
			instance.PodNamespace = nil
			instance.PodIP = nil
			instance.UpdatedAt = time.Now()

			if err := s.instanceRepo.Update(instance); err != nil {
				fmt.Printf("Error updating instance %d status: %v\n", instance.ID, err)
			} else {
				s.updateInfraStatus(instance.ID, nextStatus)
				// Broadcast status update
				GetHub().BroadcastInstanceStatus(instance.UserID, instance)
			}
		}
		return
	}

	// Pod exists, update instance info
	needsUpdate := false

	// Check pod status and update instance accordingly
	desiredStatus := mapPodToInstanceStatus(pod)
	if instance.Status != desiredStatus {
		fmt.Printf("Instance %d: Pod status %s/ready=%v but instance status is %s, updating to %s\n",
			instance.ID, pod.Status.Phase, isPodReady(pod), instance.Status, desiredStatus)
		instance.Status = desiredStatus
		needsUpdate = true
	}
	s.updateInfraStatus(instance.ID, desiredStatus)

	// Update Pod IP if changed
	if pod.Status.PodIP != "" {
		if instance.PodIP == nil || *instance.PodIP != pod.Status.PodIP {
			instance.PodIP = &pod.Status.PodIP
			needsUpdate = true
		}
	}

	// Update Pod name if changed
	if instance.PodName == nil || *instance.PodName != pod.Name {
		instance.PodName = &pod.Name
		needsUpdate = true
	}

	// Update namespace if changed
	if instance.PodNamespace == nil || *instance.PodNamespace != pod.Namespace {
		instance.PodNamespace = &pod.Namespace
		needsUpdate = true
	}

	if needsUpdate {
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			fmt.Printf("Error updating instance %d: %v\n", instance.ID, err)
		} else {
			fmt.Printf("Instance %d status synced: %s (Pod: %s, IP: %s)\n",
				instance.ID, instance.Status, pod.Name, pod.Status.PodIP)
			// Broadcast status update
			GetHub().BroadcastInstanceStatus(instance.UserID, instance)
		}
	}
}

func (s *SyncService) syncDeploymentInstance(ctx context.Context, instance *models.Instance) {
	deployment, err := s.deploymentService.GetDeployment(ctx, instance.UserID, instance.ID)
	if err != nil {
		if instance.Status == "running" || instance.Status == "creating" {
			nextStatus := "stopped"
			if instance.Status == "creating" {
				nextStatus = "error"
			}
			fmt.Printf("Instance %d marked as %s but deployment not found in K8s, updating status to %s\n",
				instance.ID, instance.Status, nextStatus)
			instance.Status = nextStatus
			instance.PodName = nil
			instance.PodNamespace = nil
			instance.PodIP = nil
			instance.UpdatedAt = time.Now()
			if err := s.instanceRepo.Update(instance); err != nil {
				fmt.Printf("Error updating instance %d status: %v\n", instance.ID, err)
			} else {
				s.updateInfraStatus(instance.ID, nextStatus)
				GetHub().BroadcastInstanceStatus(instance.UserID, instance)
			}
		}
		return
	}

	desiredStatus := mapDeploymentToInstanceStatus(deployment)
	needsUpdate := false
	if instance.Status != desiredStatus {
		fmt.Printf("Instance %d: Deployment replicas=%d available=%d but instance status is %s, updating to %s\n",
			instance.ID, desiredReplicas(deployment), deployment.Status.AvailableReplicas, instance.Status, desiredStatus)
		instance.Status = desiredStatus
		needsUpdate = true
	}
	s.updateInfraStatus(instance.ID, desiredStatus)

	if pod, podErr := s.deploymentService.GetActivePod(ctx, instance.UserID, instance.ID); podErr == nil && pod != nil {
		if pod.Status.PodIP != "" && (instance.PodIP == nil || *instance.PodIP != pod.Status.PodIP) {
			instance.PodIP = &pod.Status.PodIP
			needsUpdate = true
		}
		if instance.PodName == nil || *instance.PodName != pod.Name {
			instance.PodName = &pod.Name
			needsUpdate = true
		}
		if instance.PodNamespace == nil || *instance.PodNamespace != pod.Namespace {
			instance.PodNamespace = &pod.Namespace
			needsUpdate = true
		}
	} else if desiredStatus == "stopped" {
		if instance.PodName != nil || instance.PodNamespace != nil || instance.PodIP != nil {
			instance.PodName = nil
			instance.PodNamespace = nil
			instance.PodIP = nil
			needsUpdate = true
		}
	}

	if needsUpdate {
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			fmt.Printf("Error updating instance %d: %v\n", instance.ID, err)
		} else {
			GetHub().BroadcastInstanceStatus(instance.UserID, instance)
		}
	}
}

func (s *SyncService) updateInfraStatus(instanceID int, instanceStatus string) {
	if s.runtimeStatusService == nil {
		return
	}
	infraStatus := mapInstanceStatusToInfraStatus(instanceStatus)
	if err := s.runtimeStatusService.UpsertInfraStatus(instanceID, infraStatus); err != nil {
		fmt.Printf("Error updating runtime infra status for instance %d: %v\n", instanceID, err)
	}
}

func mapInstanceStatusToInfraStatus(instanceStatus string) string {
	switch instanceStatus {
	case "running":
		return "ready"
	case "stopped":
		return "stopped"
	case "error":
		return "error"
	case "creating":
		return "creating"
	default:
		return "creating"
	}
}

func mapPodToInstanceStatus(pod *corev1.Pod) string {
	if pod == nil {
		return "error"
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		if isPodReady(pod) {
			return "running"
		}
		return "creating"
	case corev1.PodPending:
		return "creating"
	case corev1.PodSucceeded:
		return "stopped"
	case corev1.PodFailed, corev1.PodUnknown:
		return "error"
	default:
		return "creating"
	}
}

func mapDeploymentToInstanceStatus(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return "error"
	}
	if desiredReplicas(deployment) == 0 {
		return "stopped"
	}
	if deployment.Status.AvailableReplicas > 0 {
		return "running"
	}
	return "creating"
}

func desiredReplicas(deployment *appsv1.Deployment) int32 {
	if deployment == nil || deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}

func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ForceSync forces an immediate sync of all instances
func (s *SyncService) ForceSync() {
	s.syncAllInstances()
}
