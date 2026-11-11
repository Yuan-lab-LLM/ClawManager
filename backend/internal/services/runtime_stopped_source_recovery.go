package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
)

type StoppedSourceRecoveryRequest struct {
	SuccessorGeneration  int    `json:"successor_generation"`
	SourcePodUID         string `json:"source_pod_uid"`
	ObserverRuntimePodID int64  `json:"observer_runtime_pod_id"`
}

func (r StoppedSourceRecoveryRequest) Validate() error {
	if r.SuccessorGeneration < 2 || r.SourcePodUID == "" || r.ObserverRuntimePodID < 1 {
		return fmt.Errorf("successor_generation, source_pod_uid and observer_runtime_pod_id are required")
	}
	return nil
}

type stoppedSourceProofScope struct {
	InstanceID  int    `json:"instance_id"`
	UserID      int    `json:"user_id"`
	Generation  int    `json:"generation"`
	RuntimeType string `json:"runtime_type"`
	PodUID      string `json:"pod_uid"`
	Slot        string `json:"slot"`
	Nonce       string `json:"nonce"`
}
type stoppedSourceProof struct {
	stoppedSourceProofScope
	Confirmed          bool   `json:"confirmed"`
	ObserverPodUID     string `json:"observer_pod_uid"`
	SourceRecordDigest string `json:"source_record_digest"`
}

func (p stoppedSourceProof) validate(want stoppedSourceProofScope, observer string) error {
	digest, err := hex.DecodeString(p.SourceRecordDigest)
	if !p.Confirmed || p.stoppedSourceProofScope != want || observer == "" || p.ObserverPodUID != observer || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != p.SourceRecordDigest {
		return fmt.Errorf("runtime stopped source proof does not match requested identity")
	}
	return nil
}
func validateStoppedRecoveryReservation(instance *models.Instance, binding *models.InstanceRuntimeBinding, req StoppedSourceRecoveryRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("recovery instance is unavailable")
	}
	runtimeType, known := schedulerRuntimeType(*instance)
	if !known || normalizeInstanceRuntimeType(instance.RuntimeType) != RuntimeBackendGateway || instance.InstanceMode != InstanceModeLite || binding == nil || instance.UserID < 1 || instance.Status != "creating" || instance.RuntimeGeneration != req.SuccessorGeneration || binding.InstanceID != instance.ID || binding.RuntimeType != runtimeType || binding.Generation != req.SuccessorGeneration || binding.State != "creating" || binding.GatewayID != fmt.Sprintf("pending-%d-%d", instance.ID, req.SuccessorGeneration) || binding.RuntimePodID < 1 {
		return fmt.Errorf("recovery requires the exact unpublished pending successor")
	}
	return nil
}

// RecoverStoppedSource never advances SQL generation or publishes credentials.
// It joins independent stopped-writer, storage, and external-writer proofs, then
// retires only the exact predecessor through the normal assignment CAS.
func (s *RuntimeScheduler) RecoverStoppedSource(ctx context.Context, id int, req StoppedSourceRecoveryRequest, actor int) error {
	if s == nil || s.instanceRepo == nil || s.bindingRepo == nil || s.podRepo == nil || id < 1 || actor < 1 {
		return fmt.Errorf("recovery authority is unavailable")
	}
	c, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || c.assignments == nil || c.recoveryStorageProof == nil {
		return fmt.Errorf("recovery requires the slot runtime protocol")
	}
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, id)
	if err != nil {
		return err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return err
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, id)
	if err != nil {
		return err
	}
	if err = validateStoppedRecoveryReservation(instance, binding, req); err != nil {
		return err
	}
	instanceCopy, bindingCopy := *instance, *binding
	instance, binding = &instanceCopy, &bindingCopy
	runtimeType, _ := schedulerRuntimeType(*instance)
	// Snapshot requires successor Secret absence before any worker action.
	prior, err := c.assignments.StoppedRecoverySource(ctx, id, req.SuccessorGeneration)
	if err != nil {
		return err
	}
	if prior.PodUID != req.SourcePodUID || prior.UserID != instance.UserID || prior.RuntimeType != runtimeType {
		return fmt.Errorf("predecessor does not match requested owner and source")
	}
	var start RuntimeAgentCreateGatewayRequest
	if err = json.Unmarshal(prior.Request, &start); err != nil {
		return err
	}
	workspace := RuntimeWorkspacePathWithRoot(s.workspaceRoot, runtimeType, instance.UserID, id)
	if start.InstanceID != id || start.UserID != instance.UserID || start.Generation != prior.Generation || start.PodUID != prior.PodUID || start.AgentType != prior.RuntimeType || start.WorkspacePath != workspace || binding.WorkspacePath != workspace || start.GatewayPort < 1 {
		return fmt.Errorf("predecessor workspace or launch identity is inconsistent")
	}
	pods, err := s.podRepo.List(ctx, runtimeType)
	if err != nil {
		return err
	}
	var source *models.RuntimePod
	for i := range pods {
		p := &pods[i]
		if stringValue(p.PodUID) == prior.PodUID && stringValue(p.AgentEndpoint) == prior.Endpoint {
			if source != nil {
				return fmt.Errorf("source registration is ambiguous")
			}
			source = p
		}
	}
	observer, err := s.podRepo.GetByID(ctx, req.ObserverRuntimePodID)
	if err != nil {
		return err
	}
	target, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return err
	}
	if source == nil || observer == nil || target == nil || source.Namespace != c.namespace {
		return fmt.Errorf("source, pending target or observer registration is unavailable")
	}
	sourceURL, err := url.Parse(prior.Endpoint)
	if err != nil || sourceURL.Hostname() != stringValue(source.PodIP) {
		return fmt.Errorf("source registration Pod IP does not match assignment")
	}
	// The read-only observer may be a separate, scheduling-disabled pool. It
	// must prove the same filesystem as both the source and pending target.
	pinned := []models.RuntimePod{*target, *observer}
	observerUID := stringValue(observer.PodUID)
	var storage k8s.StoppedSourceStorage
	for i, pod := range pinned {
		if err = s.verifyStoppedRecoveryPod(ctx, c, pod, runtimeType); err != nil {
			return err
		}
		candidate, e := c.recoveryStorageProof(ctx, c.kube, *source, pod, workspace, start.GatewayPort)
		if e != nil {
			return e
		}
		if i == 0 {
			storage = candidate
		} else if candidate != storage {
			return fmt.Errorf("observer and pending target storage differ")
		}
	}
	identity := runtimeauth.Identity{InstanceID: id, UserID: instance.UserID, Generation: prior.Generation, PodUID: prior.PodUID}
	if err = c.requireExternalWriterAbsence(ctx, identity); err != nil {
		return err
	}
	if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
		return err
	}
	if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
		return err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	scope := stoppedSourceProofScope{InstanceID: id, UserID: instance.UserID, Generation: prior.Generation, RuntimeType: prior.RuntimeType, PodUID: prior.PodUID, Slot: storage.Slot, Nonce: hex.EncodeToString(nonce)}
	var proof stoppedSourceProof
	if err = c.do(ctx, http.MethodPost, stringValue(observer.AgentEndpoint), "/v1/source-writer/stopped-proof", scope, &proof); err != nil {
		return err
	}
	if err = proof.validate(scope, observerUID); err != nil {
		return err
	}
	latest, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return err
	}
	reservation, err := s.bindingRepo.GetByInstanceID(ctx, id)
	if err != nil {
		return err
	}
	if err = validateStoppedRecoveryReservation(latest, reservation, req); err != nil {
		return err
	}
	latestRuntimeType, _ := schedulerRuntimeType(*latest)
	if latest.UserID != instance.UserID || latestRuntimeType != runtimeType || latest.RuntimeType != instance.RuntimeType || reservation.WorkspacePath != workspace || reservation.GatewayPort != binding.GatewayPort || reservation.ID != binding.ID || reservation.RuntimePodID != binding.RuntimePodID {
		return fmt.Errorf("pending reservation changed during proof")
	}
	latestSource, err := s.podRepo.GetByID(ctx, source.ID)
	if err != nil {
		return err
	}
	if latestSource == nil || stringValue(latestSource.PodUID) != prior.PodUID || stringValue(latestSource.AgentEndpoint) != prior.Endpoint {
		return fmt.Errorf("runtime registration changed during proof")
	}
	for _, pod := range pinned {
		current, e := s.podRepo.GetByID(ctx, pod.ID)
		if e != nil {
			return e
		}
		if current == nil || stringValue(current.PodUID) != stringValue(pod.PodUID) || stringValue(current.AgentEndpoint) != stringValue(pod.AgentEndpoint) {
			return fmt.Errorf("pending target or observer registration changed during proof")
		}
		if err = s.verifyStoppedRecoveryPod(ctx, c, *current, runtimeType); err != nil {
			return err
		}
		rechecked, e := c.recoveryStorageProof(ctx, c.kube, *latestSource, *current, workspace, start.GatewayPort)
		if e != nil {
			return e
		}
		if rechecked != storage {
			return fmt.Errorf("storage provenance changed during proof")
		}
	}
	if err = c.requireExternalWriterAbsence(ctx, identity); err != nil {
		return err
	}
	return c.assignments.ConfirmRecoveredStopped(ctx, prior, actor, observerUID, scope.Nonce, proof.SourceRecordDigest, storage.Identity)
}

func (s *RuntimeScheduler) verifyStoppedRecoveryPod(ctx context.Context, c *slotRuntimeAgentClient, pod models.RuntimePod, runtimeType string) error {
	if pod.Namespace != c.namespace || pod.RuntimeType != runtimeType || pod.State != "ready" || pod.LastSeenAt == nil || pod.LastSeenAt.Before(s.schedulingHeartbeatCutoff()) {
		return fmt.Errorf("pending target or observer registration is not ready")
	}
	uid, err := c.targetPodUID(ctx, stringValue(pod.AgentEndpoint))
	if err != nil {
		return err
	}
	if uid != stringValue(pod.PodUID) {
		return fmt.Errorf("pending target or observer Pod identity changed")
	}
	return nil
}
