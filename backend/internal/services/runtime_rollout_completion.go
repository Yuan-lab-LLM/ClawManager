package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

// A frozen target changing identity is different from a failed observation.
// Neither permits widening the plan, mutating bindings, or guessing rollback.
type runtimeRolloutTargetChanged struct{ reason string }

func (e *runtimeRolloutTargetChanged) Error() string { return e.reason }

func (s *RuntimeScheduler) runtimeRolloutTargets(ctx context.Context, r models.RuntimeRollout, frozen []k8s.RuntimeRolloutTarget) ([]k8s.RuntimeRolloutTarget, error) {
	if scoped, ok := s.deployments.(k8s.RuntimeRolloutScopedInventory); ok && len(frozen) > 0 {
		refs := make([]k8s.RuntimeDeploymentRef, 0, len(frozen))
		for _, target := range frozen {
			if target.Namespace != s.runtimeNamespace {
				return nil, &runtimeRolloutTargetChanged{reason: "rollout target is outside the runtime namespace"}
			}
			refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: target.Namespace, Name: target.Name})
		}
		return scoped.RolloutTargetsFor(ctx, refs, r.RuntimeType)
	}
	inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
	if !ok {
		return nil, fmt.Errorf("runtime rollout inventory unavailable")
	}
	return inventory.RolloutTargets(ctx, s.runtimeNamespace, r.RuntimeType)
}

func (s *RuntimeScheduler) finishLiveRuntimeRollout(ctx context.Context, r models.RuntimeRollout, p *RuntimePoolProgress) error {
	var targets []k8s.RuntimeRolloutTarget
	if p != nil {
		targets = p.Targets
	}
	reason, err := s.runtimeRolloutCompletionReason(ctx, r, targets)
	if err == nil && reason == "" && r.RuntimeType == RuntimeTypeOpenClaw && r.PreflightID == nil {
		reason, err = s.emptyPoolPlacementCompletionReason(ctx, r)
	}
	if err != nil {
		var changed *runtimeRolloutTargetChanged
		if errors.As(err, &changed) {
			now, message := time.Now().UTC(), changed.Error()
			return s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", r.StartedAt, &now, &message)
		}
		if ctx.Err() != nil {
			return err
		}
		// Preserve the observation error for logs and eventual terminal diagnosis.
		// A failed Kubernetes/DB read is neither a successful rollout nor a dead Pod.
		return errors.Join(err, s.waitForRuntimeRolloutVerification(ctx, r, p, "control-plane observation failed: "+err.Error()))
	}
	if reason == "" {
		now := time.Now().UTC()
		return s.rolloutRepo.UpdateStatus(ctx, r.ID, "finished", r.StartedAt, &now, nil)
	}
	return s.waitForRuntimeRolloutVerification(ctx, r, p, reason)
}

// A ready image is insufficient if new instances cannot use the pool. This
// read-only prospective check shares the actual placement predicate; publishing
// finished is the only commit point, so no mutable default can drift from it.
func (s *RuntimeScheduler) emptyPoolPlacementCompletionReason(ctx context.Context, r models.RuntimeRollout) (string, error) {
	reported, err := s.podRepo.List(ctx, r.RuntimeType)
	if err != nil {
		return "", err
	}
	current, err := s.verifiedOrdinaryRuntimePods(ctx, r.RuntimeType, reported)
	if err != nil {
		return "", err
	}
	if r.SourceImagesJSON == nil && !isEmptyOpenClawPoolReset(&r) && s.upgrade != nil {
		current, err = s.upgrade.legacyDefaultPlacement(ctx, &r, current)
	} else {
		current, err = filterPlacementTarget(current, r.ID, stringValue(r.SourceImagesJSON), r.TargetImageRef, false, true)
	}
	if err != nil {
		return "", err
	}
	if len(current) == 0 {
		return "target image is ready but no verified ordinary Runtime can accept a new instance", nil
	}
	return "", nil
}

func (s *RuntimeScheduler) waitForRuntimeRolloutVerification(ctx context.Context, r models.RuntimeRollout, p *RuntimePoolProgress, reason string) error {
	if p == nil {
		return s.waitForLegacyRollout(ctx, r, reason)
	}
	// Each pool already has its own deployment deadline. Final Agent verification
	// has a durable stage deadline, not the sum of all preceding pool durations.
	// Old v1 checkpoints acquire this timestamp once, without resubmitting images.
	if p.VerificationStarted.IsZero() {
		p.VerificationStarted = time.Now().UTC()
		return s.savePoolProgress(ctx, r.ID, p)
	}
	if time.Since(p.VerificationStarted) < 15*time.Minute {
		return nil
	}
	now := time.Now().UTC()
	message := "Runtime rollout verification timed out after 15 minutes: " + reason + "; no automatic rollback was performed"
	return s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", r.StartedAt, &now, &message)
}

// Kubernetes owns membership; the registration ledger only supplies health for
// those exact live members. Retained/deleted rows are never completion targets.
// An inspection error stays an error, never an empty/successful inventory.
func (s *RuntimeScheduler) runtimeRolloutCompletionReason(ctx context.Context, r models.RuntimeRollout, frozen []k8s.RuntimeRolloutTarget) (string, error) {
	live, err := s.runtimeRolloutTargets(ctx, r, frozen)
	if err != nil {
		return "", err
	}
	targets := frozen
	if len(targets) == 0 {
		targets = live
	} // legacy rollout without a checkpoint
	if len(targets) == 0 {
		return "no eligible live Runtime deployment", nil
	}
	byDeployment := make(map[string]k8s.RuntimeRolloutTarget, len(live))
	for _, target := range live {
		byDeployment[target.Namespace+"/"+target.Name] = target
	}
	refs := make([]k8s.RuntimeDeploymentRef, 0, len(targets))
	for _, source := range targets {
		key := source.Namespace + "/" + source.Name
		target, exists := byDeployment[key]
		if source.Namespace != s.runtimeNamespace || !exists || target.UID != source.UID {
			return "", &runtimeRolloutTargetChanged{reason: fmt.Sprintf("rollout target %s disappeared or was replaced", key)}
		}
		if target.Image != strings.TrimSpace(r.TargetImageRef) {
			return fmt.Sprintf("deployment %s is not at the target image", key), nil
		}
		if !target.Ready {
			return fmt.Sprintf("deployment %s has not completed its rolling update", key), nil
		}
		if r.RuntimeType == RuntimeTypeHermes && r.Phase == k8s.HermesWebRolloutPhase {
			web, ok := s.deployments.(k8s.HermesWebDeploymentService)
			if !ok {
				return "", fmt.Errorf("Hermes Web deployment configuration unavailable")
			}
			ready, err := web.HermesWebRolloutReady(ctx, source.Namespace, source.Name)
			if err != nil {
				return "", err
			}
			if !ready {
				return fmt.Sprintf("Hermes Web deployment %s has not rolled out", key), nil
			}
		}
		refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: source.Namespace, Name: source.Name})
	}
	pods, err := s.deployments.ListDeploymentPods(ctx, refs)
	if err != nil {
		return "", err
	}
	reported, err := s.podRepo.List(ctx, r.RuntimeType)
	if err != nil {
		return "", err
	}
	timeout := s.heartbeatTimeout
	if timeout <= 0 {
		timeout = heartbeat.Timeout
	}
	cutoff := time.Now().UTC().Add(-timeout)
	for _, ref := range refs {
		count, desired := 0, 0
		for _, pod := range pods {
			if pod.Namespace != ref.Namespace || pod.DeploymentName != ref.Name {
				continue
			}
			count++
			desired = int(pod.DesiredReplicas)
			if pod.RuntimeType != r.RuntimeType || pod.PodUID == "" || pod.State != "ready" || strings.TrimSpace(pod.ImageRef) != strings.TrimSpace(r.TargetImageRef) {
				return fmt.Sprintf("live Runtime pod %s/%s is not ready at the target image", pod.Namespace, pod.PodName), nil
			}
			var agent *models.RuntimePod
			for i := range reported {
				if !runtimeRolloutAgentMatchesPod(reported[i], pod) {
					continue
				}
				if agent != nil {
					return fmt.Sprintf("live Runtime pod %s has ambiguous Agent registrations", pod.PodName), nil
				}
				agent = &reported[i]
			}
			if agent == nil {
				return fmt.Sprintf("live Runtime pod %s has not registered its Agent identity", pod.PodName), nil
			}
			if agent.State != "ready" || agent.Draining || strings.TrimSpace(agent.ImageRef) != strings.TrimSpace(r.TargetImageRef) {
				return fmt.Sprintf("live Runtime Agent %s is not ready at the target image (state=%s, draining=%t)", pod.PodName, agent.State, agent.Draining), nil
			}
			if agent.LastSeenAt == nil || agent.LastSeenAt.Before(cutoff) {
				return fmt.Sprintf("live Runtime Agent %s has no fresh heartbeat", pod.PodName), nil
			}
		}
		if count == 0 || desired <= 0 || count != desired {
			return fmt.Sprintf("deployment %s/%s has %d live Runtime pods, expected %d", ref.Namespace, ref.Name, count, desired), nil
		}
	}
	return "", nil
}

func runtimeRolloutAgentMatchesPod(agent models.RuntimePod, live k8s.RuntimeDeploymentPod) bool {
	if agent.RuntimeType != live.RuntimeType || agent.Namespace != live.Namespace || agent.DeploymentName != live.DeploymentName || agent.PodName != live.PodName {
		return false
	}
	if agent.PodUID != nil && *agent.PodUID != "" {
		return *agent.PodUID == live.PodUID
	}
	// Older Agent rows have no UID. Keep compatibility only with the existing
	// exact namespace/deployment/name/IP/endpoint identity, never by name alone.
	if live.PodIP == nil || agent.PodIP == nil || *agent.PodIP != *live.PodIP {
		return false
	}
	u, err := url.Parse(stringValue(agent.AgentEndpoint))
	return err == nil && net.ParseIP(u.Hostname()) != nil && u.Hostname() == *live.PodIP
}
