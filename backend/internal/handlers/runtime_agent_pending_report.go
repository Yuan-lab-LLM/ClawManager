package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
	"context"
	"fmt"
	"strings"
	"time"
)

type RuntimeGatewayReportVerifier interface {
	VerifyReportedGateway(context.Context, runtimeauth.Identity, string, string, int) error
}

func (h *RuntimeAgentHandler) SetRuntimeGatewayReportVerifier(verifier RuntimeGatewayReportVerifier) {
	h.reportVerifier = verifier
}

// handled prevents a protected report from falling through to legacy orphan or
// previous-generation OpenClaw recovery, including when current authority fails.
func (h *RuntimeAgentHandler) reconcileProtectedPendingReport(ctx context.Context, binding *models.InstanceRuntimeBinding, podID int64, gateway runtimeAgentGatewayReport) (handled, reconciled bool, err error) {
	// An accepted native Create can already have a canonical gateway ID while
	// still creating. Its reports belong to the normal assignment lifecycle.
	if binding != nil && !strings.HasPrefix(binding.GatewayID, "pending-") {
		return false, false, nil
	}
	if h.instanceRepo == nil {
		return false, false, nil
	}
	instance, err := h.instanceRepo.GetByID(gateway.InstanceID)
	if err != nil {
		return false, false, err
	}
	if instance == nil || instance.RuntimeSecurityProfile == "" {
		return false, false, nil
	}
	if h.reportVerifier == nil || binding == nil || instance.RuntimeSecurityProfile != services.RuntimeSecurityProfile || instance.ID != gateway.InstanceID || instance.UserID < 1 || instance.Status != "creating" || instance.InstanceMode != "lite" || instance.RuntimeType != "gateway" || instance.RuntimeGeneration != gateway.Generation || gateway.Generation < 1 {
		return true, false, nil
	}
	kind, ok := services.NormalizeV2RuntimeType(instance.Type)
	workspace := services.RuntimeWorkspacePathWithRoot(h.cfg.WorkspaceRoot, instance.Type, instance.UserID, instance.ID)
	if !ok || kind != instance.Type || binding.InstanceID != instance.ID || binding.RuntimePodID != podID || binding.RuntimeType != kind || binding.Generation != gateway.Generation || binding.State != "creating" || binding.GatewayID != fmt.Sprintf("pending-%d-%d", instance.ID, gateway.Generation) || binding.GatewayPort != gateway.GatewayPort || binding.WorkspacePath != workspace || gateway.WorkspacePath != workspace || gateway.GatewayID != fmt.Sprintf("gw-%d-%d", instance.ID, gateway.Generation) || !services.NormalizeRuntimeGatewayLifecycle(gateway.State, gateway.ErrorMessage).Running {
		return true, false, nil
	}
	pod, err := h.podRepo.GetByID(ctx, podID)
	if err != nil {
		return true, false, err
	}
	if pod == nil || pod.ID != podID || pod.RuntimeType != kind || pod.PodUID == nil || *pod.PodUID == "" {
		return true, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	id := runtimeauth.Identity{InstanceID: instance.ID, UserID: instance.UserID, Generation: gateway.Generation, PodUID: *pod.PodUID}
	if h.reportVerifier.VerifyReportedGateway(ctx, id, kind, workspace, gateway.GatewayPort) != nil || ctx.Err() != nil {
		return true, false, nil
	}
	repo, ok := h.bindingRepo.(repository.PendingReportedGatewayBindingReconciler)
	if !ok {
		return true, false, nil
	}
	health := gateway.HealthAt
	if health == nil {
		now := time.Now().UTC()
		health = &now
	}
	reconciled, err = repo.ReconcilePendingReportedGateway(ctx, &models.InstanceRuntimeBinding{InstanceID: instance.ID, RuntimePodID: podID, RuntimeType: kind, GatewayID: gateway.GatewayID, GatewayPort: gateway.GatewayPort, GatewayPID: gateway.GatewayPID, WorkspacePath: workspace, State: services.RuntimeGatewayBindingRunning, Generation: gateway.Generation, LastHealthAt: health}, instance.UserID, services.RuntimeGatewayPortBlockSize(kind))
	return true, reconciled, err
}
