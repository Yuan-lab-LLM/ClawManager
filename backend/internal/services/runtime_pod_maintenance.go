package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"time"
)

type RuntimePodMaintenanceService struct {
	sess      db.Session
	scheduler *RuntimeScheduler
}

func NewRuntimePodMaintenanceService(sess db.Session, scheduler *RuntimeScheduler) *RuntimePodMaintenanceService {
	if sess != nil && scheduler != nil {
		resolver, proof := scheduler.deployments.(k8s.RuntimePodControllerResolver)
		if client, native := scheduler.agentClient.(*slotRuntimeAgentClient); native && proof {
			if setter, ok := scheduler.podRepo.(interface {
				SetRuntimePodRegistrationVerifier(func(context.Context, *models.RuntimePod) error)
			}); ok {
				setter.SetRuntimePodRegistrationVerifier(func(ctx context.Context, pod *models.RuntimePod) error {
					if pod.Namespace != scheduler.runtimeNamespace || pod.PodUID == nil || *pod.PodUID == "" {
						return fmt.Errorf("native registration requires the configured namespace and current Pod UID")
					}
					actual, err := resolver.ResolveRuntimePodController(ctx, scheduler.runtimeNamespace, pod.PodName, *pod.PodUID)
					if err != nil {
						return err
					}
					if actual.DeploymentName != pod.DeploymentName || actual.RuntimeType != pod.RuntimeType {
						return fmt.Errorf("reported Runtime controller/type differs from verified Kubernetes ownership")
					}
					return nil
				})
			}
			client.admissionMu.Lock()
			client.assignmentAdmission = func(ctx context.Context, podUID, runtimeType string, instanceID, generation int, publish func() error) error {
				actual, err := resolver.ResolveRuntimePodController(ctx, scheduler.runtimeNamespace, "", podUID)
				if err != nil {
					return err
				}
				if actual.RuntimeType != runtimeType {
					return fmt.Errorf("Runtime assignment type differs from its Kubernetes controller")
				}
				identity := models.RuntimePod{Namespace: actual.Namespace, PodName: actual.PodName, PodUID: &actual.PodUID, DeploymentName: actual.DeploymentName, RuntimeType: actual.RuntimeType}
				return repository.WithRuntimePodAssignmentAdmission(ctx, sess, identity, instanceID, generation, publish)
			}
			client.admissionMu.Unlock()
		}
	}
	return &RuntimePodMaintenanceService{sess: sess, scheduler: scheduler}
}

type RuntimeManagedPools struct {
	Items []k8s.RuntimeManagedPool `json:"items"`
}

func (s *RuntimePodMaintenanceService) maintenanceKube() (k8s.RuntimePodMaintenanceDeploymentService, error) {
	if s == nil || s.scheduler == nil || s.scheduler.podRepo == nil {
		return nil, fmt.Errorf("Runtime maintenance is unavailable")
	}
	kube, ok := s.scheduler.deployments.(k8s.RuntimePodMaintenanceDeploymentService)
	if !ok {
		return nil, fmt.Errorf("Kubernetes Runtime maintenance is unavailable")
	}
	return kube, nil
}
func (s *RuntimePodMaintenanceService) ManagedPools(ctx context.Context) (*RuntimeManagedPools, error) {
	kube, err := s.maintenanceKube()
	if err != nil {
		return nil, err
	}
	pools, err := kube.ListManagedRuntimePools(ctx, s.scheduler.runtimeNamespace)
	if err != nil {
		return nil, err
	}
	return &RuntimeManagedPools{Items: pools}, nil
}

// Adding capacity must remain available to unblock scheduling and recovery.
// Keep the admission lock and same-type rollout guard: a rollout must not
// change this Runtime's pool inventory while verified capacity is added.
func (s *RuntimePodMaintenanceService) createAdmission(ctx context.Context, runtimeType string, fn func(db.Session) error) error {
	if s.sess == nil {
		return fmt.Errorf("persistent Runtime maintenance admission is unavailable")
	}
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var active int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_rollouts WHERE runtime_type=? AND status IN ('pending','running')", runtimeType)
		if err != nil {
			return err
		}
		if err = row.Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("Runtime %s upgrade is pending or running; wait before adding capacity", runtimeType)
		}
		row, err = tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_management_operations WHERE kind='scale_in' AND status IN ('pending','processing','starting','needs_attention') AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.runtime_type'))=?", runtimeType)
		if err != nil {
			return err
		}
		if err = row.Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("Runtime %s scale-in is active; wait before adding capacity", runtimeType)
		}
		return fn(tx)
	}, nil)
}
func (s *RuntimePodMaintenanceService) admissionTx(ctx context.Context, fn func(db.Session) error) error {
	if s.sess == nil {
		return fmt.Errorf("persistent Runtime maintenance admission is unavailable")
	}
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		for _, query := range []string{
			"SELECT COUNT(*) FROM runtime_rollouts WHERE status IN ('pending','running')",
			"SELECT COUNT(*) FROM runtime_management_operations WHERE status IN ('pending','processing','starting','needs_attention')",
			"SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.recovery_status IN ('pending','starting','retrying','needs_attention') AND ui.state NOT IN ('verified','gateway_verified') AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items newer JOIN runtime_rollouts nr ON nr.id=newer.rollout_id WHERE newer.instance_id=ui.instance_id AND nr.id>r.id AND nr.status IN ('pending','running','finished','error'))",
		} {
			var count int
			row, err := tx.SQL().QueryRowContext(ctx, query)
			if err != nil {
				return err
			}
			if err = row.Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("Runtime upgrade, recovery or scheduling operation is active")
			}
		}
		return fn(tx)
	}, nil)
}
func runtimeMaintenanceEmptyReport(pod *models.RuntimePod, uid string, now time.Time) error {
	if pod == nil || uid == "" || pod.PodUID == nil || *pod.PodUID != uid {
		return fmt.Errorf("fresh registered Pod UID required")
	}
	if pod.UsedSlots != 0 {
		return fmt.Errorf("Pod has occupied slots or in-flight slot claims")
	}
	if pod.State != "ready" || pod.LastSeenAt == nil || now.Sub(*pod.LastSeenAt) > 2*time.Minute || pod.LastSeenAt.After(now.Add(time.Minute)) {
		return fmt.Errorf("fresh healthy Runtime occupancy report required")
	}
	return nil
}
func (s *RuntimePodMaintenanceService) Delete(ctx context.Context, id int64, uid string) error {
	kube, err := s.maintenanceKube()
	if err != nil {
		return err
	}
	pod, err := s.scheduler.podRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if pod == nil || pod.Namespace != s.scheduler.runtimeNamespace {
		return fmt.Errorf("Runtime Pod not found")
	}
	if uid == "" || stringValue(pod.PodUID) != uid {
		return fmt.Errorf("pod_uid is required and must match the current Pod")
	}
	// Reject unsupported controllers before changing scheduling admission.
	if _, err = kube.InspectManagedRuntimePool(ctx, pod.Namespace, pod.DeploymentName, ""); err != nil {
		return s.retryGonePool(ctx, kube, pod.Namespace, pod.DeploymentName, "", err)
	}
	return s.deletePool(ctx, kube, pod.Namespace, pod.DeploymentName, "", id, uid)
}
func (s *RuntimePodMaintenanceService) DeletePool(ctx context.Context, namespace, name, uid string) error {
	kube, err := s.maintenanceKube()
	if err != nil {
		return err
	}
	if uid == "" || namespace != s.scheduler.runtimeNamespace || name == "" {
		return fmt.Errorf("configured namespace, deployment name and deployment_uid required")
	}
	pool, err := kube.InspectManagedRuntimePool(ctx, namespace, name, uid)
	if err != nil {
		return s.retryGonePool(ctx, kube, namespace, name, uid, err)
	}
	var id int64
	var podUID string
	if len(pool.Pods) == 1 {
		pod := pool.Pods[0]
		podUID = string(pod.UID)
		registered, err := s.scheduler.podRepo.GetByNamespaceName(ctx, namespace, pod.Name)
		if err != nil {
			return err
		}
		if registered != nil {
			id = registered.ID
		}
		if id == 0 {
			for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
				if status.State.Running != nil || status.State.Terminated != nil || status.RestartCount > 0 {
					return fmt.Errorf("unregistered Pod has executed containers; wait for a fresh Runtime report")
				}
			}
			if pod.Status.Phase != corev1.PodPending {
				return fmt.Errorf("unregistered Pod must be pending and never executed")
			}
		}
	}
	return s.deletePool(ctx, kube, namespace, name, uid, id, podUID)
}
func (s *RuntimePodMaintenanceService) deletePool(ctx context.Context, kube k8s.RuntimePodMaintenanceDeploymentService, namespace, name, deploymentUID string, id int64, podUID string) error {
	pool, err := kube.InspectManagedRuntimePool(ctx, namespace, name, deploymentUID)
	if err != nil {
		return err
	}
	deploymentUID = string(pool.Deployment.UID)
	if id > 0 {
		unlock := s.scheduler.lockGatewayCreateForPod(id)
		defer unlock()
	}
	// Commit the controller fence with empty SQL admission and the pause. No
	// Kubernetes RPC runs while this transaction owns the global admission row.
	err = s.admissionTx(ctx, func(tx db.Session) error {
		if id > 0 {
			pod, err := repository.NewRuntimePodRepository(tx).GetByID(ctx, id)
			if err != nil {
				return err
			}
			if err = runtimeMaintenanceEmptyReport(pod, podUID, time.Now()); err != nil {
				return err
			}
			if pod.Namespace != namespace || pod.DeploymentName != name {
				return fmt.Errorf("selected Pod controller changed")
			}
		}
		return repository.FenceRuntimePodDeletion(ctx, tx, namespace, name, deploymentUID)
	})
	if err != nil {
		return err
	}
	fail := func(err error) error {
		return fmt.Errorf("Runtime pool deletion intent remains active; admission is fenced until deletion is retried: %w", err)
	}
	pool, err = kube.InspectManagedRuntimePool(ctx, namespace, name, deploymentUID)
	if err != nil {
		return fail(err)
	}
	if id > 0 {
		if len(pool.Pods) != 1 || string(pool.Pods[0].UID) != podUID {
			return fail(fmt.Errorf("selected Pod UID changed"))
		}
	}
	// A Pending Pod can register after the fence commits. Its new SQL row is
	// excluded by controller name, so neither Resume nor a new claim can admit it.
	if err = kube.DeleteManagedRuntimePool(ctx, namespace, name, deploymentUID); err != nil {
		return fail(err)
	}
	return nil
}

func (s *RuntimePodMaintenanceService) retryGonePool(ctx context.Context, kube k8s.RuntimePodMaintenanceDeploymentService, namespace, name, expectedUID string, original error) error {
	if !apierrors.IsNotFound(original) || s.sess == nil {
		return original
	}
	uid, err := repository.RuntimePodDeletionIntentUID(ctx, s.sess, namespace, name)
	if err != nil {
		return err
	}
	if uid == "" || (expectedUID != "" && expectedUID != uid) {
		return original
	}
	// The same immutable intent may be retried after a lost successful response.
	// Kubernetes must also confirm that no Pod remains before accepting it.
	return kube.DeleteManagedRuntimePool(ctx, namespace, name, uid)
}
