package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Lightweight orchestration fake only. Authorization, SQL locking, rollback,
// heartbeat capacity and concurrent idempotency are tested against real MySQL
// in repository/runtime_upgrade_reservation_integration_test.go.
func (r *fakeRuntimeBindingRepo) ReserveUpgradeGateway(ctx context.Context, req repository.UpgradeGatewayReservation) (*models.InstanceRuntimeBinding, error) {
	if existing := r.bindings[req.InstanceID]; existing != nil {
		return existing, nil
	}
	if r.upgradePods == nil {
		return nil, repository.ErrUpgradeReservationDenied
	}
	claimed, err := r.upgradePods.TryClaimSlot(ctx, req.Pod.ID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, repository.ErrUpgradeCapacityPending
	}
	port := req.PortStart
	for _, b := range r.bindings {
		if b.RuntimePodID == req.Pod.ID && b.GatewayPort >= port {
			port = b.GatewayPort + req.PortBlockSize
		}
	}
	b := &models.InstanceRuntimeBinding{InstanceID: req.InstanceID, RuntimePodID: req.Pod.ID, RuntimeType: "openclaw", GatewayID: fmt.Sprintf("pending-%d-%d", req.InstanceID, req.Generation), GatewayPort: port, State: "creating", Generation: req.Generation, WorkspacePath: req.WorkspacePath}
	return b, r.Create(ctx, b)
}

func (a *fakeRuntimeAgentClient) GatewayState(ctx context.Context, endpoint, id string) (*RuntimeAgentGatewayState, error) {
	return nil, ErrRuntimeAgentNotFound
}

func TestUpgradeRecoveryErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
		hold bool
	}{
		{repository.ErrUpgradeReservationDenied, "UPGRADE_RESERVATION_DENIED", true},
		{repository.ErrUpgradeRecoveryHeld, "UPGRADE_RECOVERY_REQUIRES_ATTENTION", true},
		{repository.ErrUpgradeCapacityPending, "UPGRADE_CAPACITY_PENDING", false},
		{ErrModelConfigurationRequired, "MODEL_CONFIGURATION_REQUIRED", true},
	} {
		code, hold := upgradeRecoveryError(fmt.Errorf("wrapped: %w", tc.err))
		if code != tc.code || hold != tc.hold {
			t.Fatalf("%v: %s %v", tc.err, code, hold)
		}
	}
}

func TestUpgradeRestartDeadlineDoesNotMoveWithGenericUpdates(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(-time.Second)
	item := models.RuntimeUpgradeItem{State: "restart_ready", RestartDeadlineAt: &deadline, UpdatedAt: now}
	if !upgradeGatewayRestartExpired(item, now.Add(time.Hour), now) {
		t.Fatal("unrelated timestamp reset attempt deadline")
	}
	item.RestartDeadlineAt = nil
	item.UpdatedAt = now.Add(-24 * time.Hour)
	if upgradeGatewayRestartExpired(item, time.Time{}, now) {
		t.Fatal("legacy unstarted item must be reconciled, not permanently expired")
	}
}

func TestUpgradePodIdentityUsesExactLivePodForLegacyAgent(t *testing.T) {
	uid, ip, endpoint := "live-uid", "10.1.2.3", "http://10.1.2.3:19090"
	report := models.RuntimePod{Namespace: "tenant", DeploymentName: "pool", PodName: "pod-a", AgentEndpoint: &endpoint, PodIP: &ip}
	live := models.RuntimePod{Namespace: "tenant", DeploymentName: "pool", PodName: "pod-a", PodUID: &uid, PodIP: &ip, KubernetesReady: true, State: "unhealthy"}
	got := upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live})
	if len(got) != 1 || got[0].KubernetesPodUID == nil || *got[0].KubernetesPodUID != uid {
		t.Fatal("old Agent identity not recovered from live Kubernetes")
	}
	live.PodName = "pod-b"
	if len(upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live})) != 0 {
		t.Fatal("deployment sibling borrowed Pod UID")
	}
	live.PodName = "pod-a"
	badIP := "10.1.2.4"
	live.PodIP = &badIP
	if len(upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live})) != 0 {
		t.Fatal("stale Agent endpoint accepted")
	}
}

func TestUpgradePodIdentityDoesNotRequireTrafficReadiness(t *testing.T) {
	uid, ip, endpoint := "standby-uid", "10.1.2.3", "http://10.1.2.3:19090"
	report := models.RuntimePod{Namespace: "tenant", DeploymentName: "source-u124", PodName: "target", State: "standby", AgentEndpoint: &endpoint, PodIP: &ip}
	live := models.RuntimePod{Namespace: report.Namespace, DeploymentName: report.DeploymentName, PodName: report.PodName, PodUID: &uid, PodIP: &ip, KubernetesReady: false, State: "unhealthy"}
	got := upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live})
	if len(got) != 1 || got[0].State != "standby" || stringValue(got[0].KubernetesPodUID) != uid {
		t.Fatal("valid standby identity was rejected before migration/activation")
	}
	wrongUID := "replaced-pod"
	report.PodUID = &wrongUID
	if len(upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live})) != 0 {
		t.Fatal("standby accepted a replaced Pod UID")
	}
}

func TestUpgradeReportsLongPodDeploymentMismatchWithoutAcceptingIt(t *testing.T) {
	uid, ip, endpoint := "target-uid", "10.1.2.3", "http://10.1.2.3:19090"
	live := models.RuntimePod{Namespace: "tenant", DeploymentName: "pool-u134", PodName: "pool-u134-truncatedsuffix", PodUID: &uid, PodIP: &ip}
	report := live
	report.DeploymentName = "pool"
	report.AgentEndpoint = &endpoint
	if got := upgradeTargetIdentityProblem([]models.RuntimePod{report}, []models.RuntimePod{live}); !strings.HasPrefix(got, upgradeTargetIdentityMismatch) {
		t.Fatalf("missing rejection reason: %s", got)
	}
	if got := upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live}); len(got) != 0 {
		t.Fatal("mismatched identity accepted")
	}
	report.DeploymentName = live.DeploymentName
	if got := upgradeTargetIdentityProblem([]models.RuntimePod{report}, []models.RuntimePod{live}); got != "" {
		t.Fatal(got)
	}
	if got := upgradePodsWithLiveIdentity([]models.RuntimePod{report}, []models.RuntimePod{live}); len(got) != 1 {
		t.Fatal("corrected identity rejected")
	}
	report.Namespace = "other-tenant"
	if got := upgradeTargetIdentityProblem([]models.RuntimePod{report}, []models.RuntimePod{live}); got != "" {
		t.Fatal("cross-tenant diagnostic")
	}
}
