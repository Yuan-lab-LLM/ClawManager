package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"strconv"
	"strings"
	"time"
)

// Only the server constructs this capsule from a committed default and a
// completed physical retirement. It authorizes a new identity, never name reuse.
type RuntimeColdSuccessorProof struct {
	Version             int                      `json:"version"`
	ParentRolloutID     int64                    `json:"parent_rollout_id"`
	Source              k8s.RuntimeRolloutTarget `json:"source"`
	RetirementOperation string                   `json:"retirement_operation"`
	TemplateSHA256      string                   `json:"template_sha256"`
	SuccessorUID        string                   `json:"successor_uid,omitempty"`
}

func (p RuntimeColdSuccessorProof) Fingerprint() string {
	p.SuccessorUID = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func coldSuccessorProof(r *models.RuntimeRollout) (*RuntimeColdSuccessorProof, error) {
	var plan struct {
		Cold *RuntimeColdSuccessorProof `json:"cold_successor"`
	}
	if r == nil || r.PreflightJSON == nil {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(*r.PreflightJSON), &plan); err != nil {
		return nil, err
	}
	if plan.Cold == nil {
		return nil, nil
	}
	p := plan.Cold
	if r.RuntimeType != RuntimeTypeOpenClaw || r.Phase != RuntimeUpgradePhaseEmptyPoolReset || r.PreflightID != nil || p.Version != 1 || p.ParentRolloutID <= 0 || (r.ID > 0 && p.ParentRolloutID >= r.ID) || p.Source.Namespace == "" || p.Source.Name == "" || p.Source.UID == "" || p.RetirementOperation == "" || len(p.TemplateSHA256) != 64 || p.Source.Image != r.TargetImageRef || imageDigestFromReference(r.TargetImageRef) == "" || stringValue(r.PlanFingerprint) != p.Fingerprint() {
		return nil, fmt.Errorf("cold successor authorization proof is invalid")
	}
	var sources map[string]string
	if json.Unmarshal([]byte(stringValue(r.SourceImagesJSON)), &sources) != nil || len(sources) != 1 || sources[p.Source.Namespace+"/"+p.Source.Name] != p.Source.Image {
		return nil, fmt.Errorf("cold successor source scope changed")
	}
	if r.Status == "finished" && p.SuccessorUID == "" {
		return nil, fmt.Errorf("cold successor has no committed native target identity")
	}
	return p, nil
}
func (s *RuntimeScheduler) ColdRuntimeBackend() k8s.RuntimeColdSuccessorService {
	b, _ := s.deployments.(k8s.RuntimeColdSuccessorService)
	return b
}
func (s *RuntimeUpgradeService) coldBackend() (k8s.RuntimeColdSuccessorService, error) {
	p, ok := s.deployments.(interface {
		ColdRuntimeBackend() k8s.RuntimeColdSuccessorService
	})
	if !ok || p.ColdRuntimeBackend() == nil {
		return nil, fmt.Errorf("cold retirement inspection is unavailable")
	}
	return p.ColdRuntimeBackend(), nil
}
func (s *RuntimeUpgradeService) coldDefaultSource(ctx context.Context) (*models.RuntimeRollout, k8s.RuntimePodScaleTarget, error) {
	current, err := s.defaultPlacementRollout(ctx)
	if err != nil {
		return nil, k8s.RuntimePodScaleTarget{}, err
	}
	if current == nil {
		return nil, k8s.RuntimePodScaleTarget{}, fmt.Errorf("cold retirement has no approved default")
	}
	var sources map[string]string
	if json.Unmarshal([]byte(stringValue(current.SourceImagesJSON)), &sources) != nil || len(sources) == 0 {
		return nil, k8s.RuntimePodScaleTarget{}, fmt.Errorf("approved cold default has no immutable source inventory")
	}
	ns := ""
	for key := range sources {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 || parts[0] == "" || (ns != "" && ns != parts[0]) {
			return nil, k8s.RuntimePodScaleTarget{}, fmt.Errorf("ambiguous cold default namespace")
		}
		ns = parts[0]
	}
	var finished []models.RuntimeRollout
	if err = s.sess.Collection("runtime_rollouts").Find(db.Cond{"runtime_type": "openclaw", "status": "finished"}).OrderBy("id").All(&finished); err != nil {
		return nil, k8s.RuntimePodScaleTarget{}, err
	}
	targets, err := runtimeDefaultScaleTargets(ns, RuntimeTypeOpenClaw, finished)
	if err != nil {
		return nil, k8s.RuntimePodScaleTarget{}, err
	}
	if len(targets) != 1 {
		return nil, k8s.RuntimePodScaleTarget{}, fmt.Errorf("cold retirement requires exactly one approved default")
	}
	return current, targets[0], nil
}
func (s *RuntimeUpgradeService) inspectColdRetirement(ctx context.Context, p *RuntimeColdSuccessorProof) error {
	backend, err := s.coldBackend()
	if err != nil {
		return err
	}
	uid, err := repository.RuntimePodDeletionIntentUID(ctx, s.sess, p.Source.Namespace, p.Source.Name)
	if err != nil {
		return err
	}
	if uid == "" || uid != p.Source.UID {
		return fmt.Errorf("matching completed default retirement fence required")
	}
	var raw string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT items_json FROM runtime_management_operations WHERE id=? AND kind='orphan_recovery' AND status='completed'`, p.RetirementOperation)
	if err != nil {
		return err
	}
	if err = row.Scan(&raw); err != nil {
		return fmt.Errorf("completed default retirement operation required: %w", err)
	}
	var items []struct {
		Orphan *RuntimeOrphanRecoveryJournal `json:"orphan_recovery"`
	}
	if json.Unmarshal([]byte(raw), &items) != nil || len(items) != 1 || items[0].Orphan == nil {
		return fmt.Errorf("completed retirement journal is unavailable")
	}
	j := items[0].Orphan
	if j.Phase != "completed" || !j.FenceCreated || j.Source == nil || j.Source.SourcePod == nil || j.Source.Scope(p.RetirementOperation) != j.Scope || j.Source.RuntimeType != "openclaw" || j.Source.Namespace != p.Source.Namespace || j.Source.DeploymentName != p.Source.Name || j.Source.DeploymentUID != p.Source.UID || j.Scope.ControllerUID != p.Source.UID || j.Scope.OperationID != p.RetirementOperation {
		return fmt.Errorf("retirement journal identity differs from approved default")
	}
	if err = runtimeeviction.ValidatePodReceipt(j.Scope, j.Receipt); err != nil {
		return err
	}
	var count int
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_pods WHERE id=? AND pod_uid=? AND namespace=? AND deployment_name=? AND state='deleted' AND scheduling_paused=1 AND used_slots=0`, j.SourcePodID, j.Source.PodUID, p.Source.Namespace, p.Source.Name)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("retired default registration is not fully closed")
	}
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_type='openclaw'`)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("OpenClaw Runtime bindings prevent cold recovery")
	}
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_pods WHERE namespace=? AND deployment_name=? AND used_slots<>0`, p.Source.Namespace, p.Source.Name)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("retired default still has capacity reservations")
	}
	actual, err := backend.InspectRetiredRuntimePool(ctx, p.Source, p.RetirementOperation)
	if err != nil {
		return err
	}
	if actual != p.TemplateSHA256 {
		return fmt.Errorf("retired default template or storage changed")
	}
	return backend.CheckColdRuntimeExternalWriters(ctx, j.Source)
}
func (s *RuntimeUpgradeService) classifyColdSuccessor(ctx context.Context, target string) (*OpenClawTargetClassification, error) {
	current, canonical, err := s.coldDefaultSource(ctx)
	if err != nil {
		return nil, err
	}
	if target != current.TargetImageRef || imageDigestFromReference(target) == "" {
		return nil, fmt.Errorf("cold recovery only accepts the exact approved immutable default image")
	}
	a, err := s.assessOpenClawPoolSwitch(ctx, 0)
	if err != nil {
		return nil, err
	}
	if !a.Eligible {
		return nil, fmt.Errorf("cold recovery requires never-started instances: %s", a.Reason)
	}
	backend, err := s.coldBackend()
	if err != nil {
		return nil, err
	}
	uid, err := repository.RuntimePodDeletionIntentUID(ctx, s.sess, canonical.Namespace, canonical.DeploymentName)
	if err != nil {
		return nil, err
	}
	if uid == "" {
		return nil, fmt.Errorf("completed canonical default retirement required")
	}
	var operation string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT id FROM runtime_management_operations WHERE kind='orphan_recovery' AND status='completed' AND JSON_UNQUOTE(JSON_EXTRACT(items_json,'$[0].orphan_recovery.source.deployment_uid'))=? ORDER BY created_at DESC LIMIT 1`, uid)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&operation); err != nil {
		return nil, fmt.Errorf("completed canonical default retirement required: %w", err)
	}
	source := k8s.RuntimeRolloutTarget{Namespace: canonical.Namespace, Name: canonical.DeploymentName, UID: uid, Image: target}
	hash, err := backend.InspectRetiredRuntimePool(ctx, source, operation)
	if err != nil {
		return nil, err
	}
	proof := &RuntimeColdSuccessorProof{Version: 1, ParentRolloutID: current.ID, Source: source, RetirementOperation: operation, TemplateSHA256: hash}
	if err = s.inspectColdRetirement(ctx, proof); err != nil {
		return nil, err
	}
	return &OpenClawTargetClassification{Strategy: RuntimeUpgradeStrategyLegacyRolling, ImageRef: target, ImageDigest: imageDigestFromReference(target), EmptyPoolReset: true, WaitingInstanceIDs: a.Waiting, SourceImages: map[string]string{source.Namespace + "/" + source.Name: source.Image}, ColdSuccessor: proof}, nil
}
func (s *RuntimeScheduler) withColdSuccessorAdmission(ctx context.Context, r models.RuntimeRollout, fn func() error) error {
	return s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		current, readErr := s.rolloutRepo.GetByID(ctx, r.ID)
		if readErr != nil {
			return readErr
		}
		if current == nil || (current.Status != "pending" && current.Status != "running") || stringValue(current.PlanFingerprint) != stringValue(r.PlanFingerprint) {
			return fmt.Errorf("cold successor operation is no longer executable")
		}

		proof, err := coldSuccessorProof(&r)
		if err != nil {
			return err
		}
		if proof == nil {
			return fmt.Errorf("cold successor proof required")
		}
		if proof.Source.Namespace != s.runtimeNamespace {
			return fmt.Errorf("cold successor source is outside configured namespace")
		}
		parent, canonical, err := s.upgrade.coldDefaultSource(ctx)
		if err != nil {
			return err
		}
		if parent.ID != proof.ParentRolloutID || canonical.Namespace != proof.Source.Namespace || canonical.DeploymentName != proof.Source.Name {
			return fmt.Errorf("approved default changed during cold recovery")
		}
		if err = s.upgrade.ValidateEmptyOpenClawPoolReset(ctx, r.ID); err != nil {
			return err
		}
		if err = s.upgrade.inspectColdRetirement(ctx, proof); err != nil {
			return err
		}
		return fn()
	}, nil)
}

func (s *RuntimeScheduler) advanceColdSuccessor(ctx context.Context, r models.RuntimeRollout, p *poolReplacementProgress) error {
	proof, err := coldSuccessorProof(&r)
	if err != nil {
		return err
	}
	if proof == nil {
		return fmt.Errorf("cold successor proof missing")
	}
	if p.SuccessorName != runtimeUpgradeTargetDeploymentName(proof.Source.Name, r.ID) || len(p.Targets) != 1 || p.Targets[0] != proof.Source || (p.Successor != nil && (p.Successor.Name != p.SuccessorName || p.Successor.Namespace != proof.Source.Namespace)) {
		return fmt.Errorf("cold successor checkpoint differs from deterministic plan")
	}
	backend, err := s.upgrade.coldBackend()
	if err != nil {
		return err
	}
	id := strconv.FormatInt(r.ID, 10)
	transition := func(stage string) error {
		p.Stage = stage
		p.StageStarted = time.Now().UTC()
		return s.savePoolReplacement(ctx, r.ID, p)
	}
	fail := func(err error) error { p.Failure = err.Error(); return transition("cleanup_failed") }
	if time.Since(p.StageStarted) > 10*time.Minute && p.Stage != "cleanup_failed" {
		return fail(fmt.Errorf("cold successor native verification timed out"))
	}
	if p.Stage == "cold_create" {
		err = s.withColdSuccessorAdmission(ctx, r, func() error {
			var e error
			p.Successor, e = backend.EnsureColdRuntimeSuccessor(ctx, proof.Source, proof.TemplateSHA256, proof.RetirementOperation, p.SuccessorName, id)
			return e
		})
		if err != nil {
			return err
		}
		if p.Successor == nil || p.Successor.UID == "" {
			return fmt.Errorf("cold successor controller identity unavailable")
		}
		return transition("cold_activate")
	}
	if p.Stage == "cold_activate" {
		if p.Successor == nil {
			return fail(fmt.Errorf("cold successor target capsule missing"))
		}
		if err = backend.InspectColdRuntimeSuccessor(ctx, *p.Successor, proof.Source, proof.TemplateSHA256, id); err != nil {
			return fail(err)
		}
		pods, e := s.RuntimeDeploymentPodsFor(ctx, RuntimeTypeOpenClaw, []k8s.RuntimeDeploymentRef{{Namespace: proof.Source.Namespace, Name: p.SuccessorName}})
		if e != nil {
			return e
		}
		for _, pod := range pods {
			registered, readErr := s.podRepo.GetByID(ctx, pod.ID)
			if readErr != nil {
				return readErr
			}
			if registered == nil || !ordinaryRuntimeIdentityMatches(*registered, pod) || registered.Capacity != 8 {
				continue
			}
			uid := stringValue(pod.PodUID)
			if uid == "" || pod.ID <= 0 || stringValue(pod.RegisteredPodUID) != uid || stringValue(pod.KubernetesPodUID) != uid || pod.Capacity != 8 || pod.LastSeenAt == nil || pod.LastSeenAt.Before(s.schedulingHeartbeatCutoff()) || pod.Draining || !runtimePodMatchesImage(pod, r.TargetImageRef) || (pod.AgentState != "standby" && pod.AgentState != "ready") || stringValue(pod.AgentEndpoint) == "" {
				continue
			}
			if !pod.KubernetesReady {
				if pod.AgentState != "standby" {
					continue
				}
				ready, inspectErr := backend.ColdRuntimeStandbyReady(ctx, *p.Successor, uid)
				if inspectErr != nil {
					return inspectErr
				}
				if !ready {
					continue
				}
			}
			if healthErr := s.agentClient.Health(ctx, *pod.AgentEndpoint); healthErr != nil {
				continue
			}
			if !containsString(pod.Capabilities(), "openclaw.runtime-standby-v1") {
				return fail(fmt.Errorf("cold successor lacks native activation capability"))
			}
			agent, ok := s.agentClient.(interface {
				ActivateUpgrade(context.Context, string, string) error
			})
			if !ok {
				return fail(fmt.Errorf("cold successor activation client unavailable"))
			}
			err = s.withColdSuccessorAdmission(ctx, r, func() error {
				if e := backend.InspectColdRuntimeSuccessor(ctx, *p.Successor, proof.Source, proof.TemplateSHA256, id); e != nil {
					return e
				}
				return agent.ActivateUpgrade(ctx, *pod.AgentEndpoint, id)
			})
			if err != nil {
				return err
			}
			if pod.AgentState != "ready" || !pod.KubernetesReady {
				return nil
			}
			err = s.withColdSuccessorAdmission(ctx, r, func() error {
				if e := backend.InspectColdRuntimeSuccessor(ctx, *p.Successor, proof.Source, proof.TemplateSHA256, id); e != nil {
					return e
				}
				return s.deployments.(interface {
					SetUpgradePoolActivePreservingSource(context.Context, string, string, string, string) error
				}).SetUpgradePoolActivePreservingSource(ctx, proof.Source.Namespace, proof.Source.Name, p.SuccessorName, id)
			})
			if err != nil {
				return err
			}
			return transition("cold_verify")
		}
		return nil
	}
	if p.Stage == "cold_verify" {
		if p.Successor == nil {
			return fail(fmt.Errorf("cold successor target capsule missing"))
		}
		if err = backend.InspectColdRuntimeSuccessor(ctx, *p.Successor, proof.Source, proof.TemplateSHA256, id); err != nil {
			return fail(err)
		}
		reason, e := s.runtimeRolloutCompletionReason(ctx, r, []k8s.RuntimeRolloutTarget{*p.Successor})
		if e != nil {
			return e
		}
		if reason != "" {
			return nil
		}
		reason, e = s.emptyPoolPlacementCompletionReason(ctx, r)
		if e != nil {
			return e
		}
		if reason != "" {
			return nil
		}
		return transition("cleanup_success")
	}
	if p.Stage == "cleanup_success" || p.Stage == "cleanup_failed" {
		validation := s.deployments.(k8s.RuntimePoolValidationService)
		name := replacementProbeName(r.ID, 0)
		if err = validation.StopPoolValidation(ctx, proof.Source.Namespace, name, id); err != nil {
			return err
		}
		observed, e := validation.ObservePoolValidation(ctx, proof.Source.Namespace, name, id)
		if e != nil {
			return e
		}
		if !observed.Gone {
			return nil
		}
		if p.Stage == "cleanup_failed" {
			cleaner, ok := backend.(interface {
				RetireColdRuntimeSuccessor(context.Context, k8s.RuntimeRolloutTarget, string, string, string, string) (bool, error)
			})
			if !ok {
				return fmt.Errorf("cold successor cleanup unavailable")
			}
			uid := ""
			if p.Successor != nil {
				uid = p.Successor.UID
			}
			gone, e := cleaner.RetireColdRuntimeSuccessor(ctx, proof.Source, proof.TemplateSHA256, p.SuccessorName, id, uid)
			if e != nil {
				return e
			}
			if !gone {
				return nil
			}
			now := time.Now().UTC()
			return s.setPoolReplacementStatus(ctx, r, p, "error", r.StartedAt, &now, stringPtrOrNil(p.Failure+"; retired source preserved"))
		}
		reason, e := s.runtimeRolloutCompletionReason(ctx, r, []k8s.RuntimeRolloutTarget{*p.Successor})
		if e != nil {
			return e
		}
		if reason != "" {
			return transition("cold_verify")
		}
		return s.withColdSuccessorAdmission(ctx, r, func() error {
			if e := backend.InspectColdRuntimeSuccessor(ctx, *p.Successor, proof.Source, proof.TemplateSHA256, id); e != nil {
				return e
			}
			reason, e := s.runtimeRolloutCompletionReason(ctx, r, []k8s.RuntimeRolloutTarget{*p.Successor})
			if e != nil {
				return e
			}
			if reason != "" {
				return fmt.Errorf("cold successor commit verification: %s", reason)
			}
			var plan map[string]json.RawMessage
			if err = json.Unmarshal([]byte(stringValue(r.PreflightJSON)), &plan); err != nil {
				return err
			}
			proof.SuccessorUID = p.Successor.UID
			encoded, _ := json.Marshal(proof)
			plan["cold_successor"] = encoded
			raw, _ := json.Marshal(plan)
			result, e := s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET preflight_json=? WHERE id=? AND status='running' AND plan_fingerprint=?`, string(raw), r.ID, proof.Fingerprint())
			if e != nil {
				return e
			}
			count, e := result.RowsAffected()
			if e != nil || count > 1 {
				return fmt.Errorf("cold successor commit ownership changed")
			}
			now := time.Now().UTC()
			return s.setPoolReplacementStatus(ctx, r, p, "finished", r.StartedAt, &now, nil)
		})
	}
	return fmt.Errorf("unknown cold successor checkpoint stage %q", p.Stage)
}
