package services

import (
	"clawreef/internal/repository"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type OpenCodeForceRetryRequest struct {
	TargetImageRef string `json:"target_image_ref"`
	Force          bool   `json:"force"`
	RequestID      string `json:"request_id"`
	Fingerprint    string `json:"fingerprint"`
}
type OpenCodeForceRetryPreflight struct {
	RolloutID         int64  `json:"rollout_id"`
	Allowed           bool   `json:"allowed"`
	Fingerprint       string `json:"fingerprint"`
	OldTargetImageRef string `json:"old_target_image_ref"`
	TargetImageRef    string `json:"target_image_ref"`
	Stage             string `json:"stage"`
	ProtectionHeld    bool   `json:"protection_held"`
}
type openCodeForceAttempt struct {
	AuditID             int64                      `json:"audit_id"`
	RequestID           string                     `json:"request_id"`
	ActorUserID         int                        `json:"actor_user_id"`
	Fingerprint         string                     `json:"fingerprint"`
	OldImage            string                     `json:"old_target_image_ref"`
	NewImage            string                     `json:"target_image_ref"`
	StartedAt           time.Time                  `json:"started_at"`
	OldCheckpointSHA256 string                     `json:"old_checkpoint_sha256"`
	OldTarget           *k8s.OpenCodeUpgradeSource `json:"old_target"`
	OldPods             []models.RuntimePod        `json:"old_pods"`
	NativeEvidence      json.RawMessage            `json:"native_evidence"`
	LeaseEvidence       string                     `json:"lease_evidence"`
}

type openCodeForceEvidenceClient interface {
	openCodeForceReadOnlySourceProof(context.Context, k8s.RuntimeAssignment, json.RawMessage) error
	openCodeForceNativeEvidence(context.Context, int64, []openCodeUpgradeInstance, models.RuntimePod) (json.RawMessage, error)
}

func forceRetryConflict(message string) error {
	return fmt.Errorf("%w: %s", models.ErrRuntimeUpgradeConflict, message)
}
func (s *RuntimeScheduler) PreflightOpenCodeForceRetry(ctx context.Context, id int64, image string) (*OpenCodeForceRetryPreflight, error) {
	if _, _, err := s.openCodeUpgradeDependencies(); err != nil {
		return nil, err
	}
	unlock, err := repository.LockOpenCodeUpgradeController(ctx, s.upgrade.sess)
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err := s.rolloutRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, forceRetryConflict("OpenCode rollout not found")
	}
	p, err := s.loadOpenCodePlan(ctx, r)
	if err != nil {
		return nil, err
	}
	view, _, _, err := s.inspectOpenCodeForceRetry(ctx, r, p, image)
	return view, err
}

// All observations precede the audit transaction. Neither preflight nor
// confirmation dispatches native work or changes a Kubernetes object.
func (s *RuntimeScheduler) inspectOpenCodeForceRetry(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, image string) (*OpenCodeForceRetryPreflight, json.RawMessage, string, error) {
	refuse := func(msg string) (*OpenCodeForceRetryPreflight, json.RawMessage, string, error) {
		return nil, nil, "", forceRetryConflict(msg)
	}
	if r.Status != "running" || p.Stage != "preserve" || p.ForceRetry != nil || len(p.Sources) != 1 || len(p.TargetProofs) != 1 || len(p.Targets) != 1 || len(p.TargetControllers) != 1 || len(p.Instances) == 0 || !IsImmutableRuntimeImage(image) || image == r.TargetImageRef {
		return refuse("OpenCode force retry requires the original pre-capsule preserved operation")
	}
	backend, client, err := s.openCodeUpgradeDependencies()
	if err != nil {
		return nil, nil, "", err
	}
	evidence, ok := s.agentClient.(openCodeForceEvidenceClient)
	if !ok {
		return refuse("OpenCode read-only stop and immutable outcome inspection unavailable")
	}
	probe, ok := s.deployments.(k8s.OpenCodeForceLeaseProbe)
	if !ok {
		return refuse("OpenCode read-only lease inspection unavailable")
	}
	if _, ok = s.deployments.(k8s.OpenCodeForceTargetService); !ok {
		return refuse("OpenCode exact force target CAS unavailable")
	}
	if err = backend.ValidateOpenCodeUpgradeSource(ctx, p.Sources[0]); err != nil {
		return nil, nil, "", err
	}
	if err = backend.ValidateOpenCodeUpgradeSource(ctx, p.TargetProofs[0]); err != nil {
		return nil, nil, "", err
	}
	if err = backend.ValidateOpenCodeUpgradeScheduling(ctx, p.Sources[0], p.TargetProofs[0], strconv.FormatInt(r.ID, 10), "preserve"); err != nil {
		return nil, nil, "", err
	}
	if len(p.TargetProofs[0].PodUIDs) != 1 || p.TargetProofs[0].PodUIDs[0] != stringValue(p.Targets[0].PodUID) || p.Targets[0].Namespace != p.TargetProofs[0].Target.Namespace || p.Targets[0].DeploymentName != p.TargetProofs[0].Target.Name {
		return refuse("OpenCode frozen target Pod does not match target controller proof")
	}
	if p.TargetControllers[0] != p.TargetProofs[0].Target || p.TargetProofs[0].Target.Image != r.TargetImageRef || p.TargetProofs[0].StorageIdentity != p.Sources[0].StorageIdentity {
		return refuse("OpenCode frozen target controller/storage differs")
	}
	if err = s.verifyOpenCodeForceOldPods(ctx, p); err != nil {
		return nil, nil, "", err
	}
	if err = client.openCodePoolIdle(ctx, p.Targets[0]); err != nil {
		return nil, nil, "", err
	}
	if err = s.verifyOpenCodeForceSQL(ctx, r, p, true); err != nil {
		return nil, nil, "", err
	}
	ids := []int{}
	for _, item := range p.Instances {
		if item.State != "quiesced" || len(item.StopProof) == 0 || item.Compatibility != nil || item.Capsule != nil || item.Restore != nil || item.RollbackState != "" || item.HistoricalStop {
			return refuse("OpenCode force retry lacks a fresh source stop or may have native data work")
		}
		if err = evidence.openCodeForceReadOnlySourceProof(ctx, item.Assignment, item.StopProof); err != nil {
			return nil, nil, "", err
		}
		ids = append(ids, item.ID)
	}
	native, err := evidence.openCodeForceNativeEvidence(ctx, r.ID, p.Instances, p.Targets[0])
	if err != nil {
		return nil, nil, "", err
	}
	lease, err := probe.InspectOpenCodeForceLeaseAbsence(ctx, p.TargetProofs[0], p.Targets[0], ids)
	if err != nil {
		return nil, nil, "", err
	}
	sources := map[string]string{p.Sources[0].Target.Name: p.Sources[0].Target.Image}
	inspect := s.ocForceInspect
	if inspect == nil {
		inspect = inspectOpenCodeRegistryContract
	}
	contract, err := inspect(ctx, image, sources)
	if err != nil {
		return nil, nil, "", err
	}
	if contract != p.Contract {
		return refuse("OpenCode force retry changes the pinned native contract")
	}
	material, _ := json.Marshal(struct {
		Checkpoint string
		Target     string
		Native     json.RawMessage
		Lease      string
		Contract   openCodeNativeContract
	}{fmt.Sprintf("%x", sha256.Sum256([]byte(p.raw))), image, native, lease, contract})
	return &OpenCodeForceRetryPreflight{RolloutID: r.ID, Allowed: true, Fingerprint: openCodePreflightFingerprint(material), OldTargetImageRef: r.TargetImageRef, TargetImageRef: image, Stage: p.Stage, ProtectionHeld: true}, native, lease, nil
}
func (s *RuntimeScheduler) verifyOpenCodeForceOldPods(ctx context.Context, p *openCodeUpgradePlan) error {
	// The old target predates scoped payload grants. Its five original
	// capabilities are evidence only; no maintenance action is authorized here.
	live, err := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	reports, err := s.podRepo.List(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	for _, want := range p.Targets {
		found := false
		for _, physical := range live {
			if physical.Namespace != want.Namespace || physical.PodName != want.PodName || physical.DeploymentName != want.DeploymentName || stringValue(physical.KubernetesPodUID) != stringValue(want.PodUID) || stringValue(physical.PodIP) != stringValue(want.PodIP) || !runtimePodMatchesImage(physical, p.TargetImage) {
				continue
			}
			for _, actual := range reports {
				if actual.ID != want.ID || actual.ID <= 0 || stringValue(actual.PodUID) == "" || stringValue(actual.PodUID) != stringValue(want.PodUID) || actual.Namespace != want.Namespace || actual.PodName != want.PodName || actual.DeploymentName != want.DeploymentName || stringValue(actual.PodIP) != stringValue(want.PodIP) || !runtimePodMatchesImage(actual, p.TargetImage) || stringValue(actual.AgentProtocolVersion) != openCodeNativeProtocol {
					continue
				}
				for _, cap := range openCodeUpgradeCapabilities[:5] {
					if !containsString(actual.Capabilities(), cap) {
						return forceRetryConflict("OpenCode original target capability identity changed")
					}
				}
				found = true
			}
		}
		if !found {
			return forceRetryConflict("OpenCode original target physical/registration identity changed")
		}
	}
	return nil
}
func (s *RuntimeScheduler) verifyOpenCodeForceSQL(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, preCapsule bool) error {
	var count int
	check := func(q string, args ...any) error {
		row, e := s.upgrade.sess.SQL().QueryRowContext(ctx, q, args...)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return forceRetryConflict("OpenCode force retry SQL source protection differs")
		}
		return nil
	}
	if err := check(`SELECT COUNT(*) FROM runtime_rollouts WHERE id=? AND runtime_type='opencode' AND status='running'`, r.ID); err != nil {
		return err
	}
	active, err := s.rolloutRepo.ListActive(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	if len(active) != 1 || active[0].ID != r.ID {
		return forceRetryConflict("OpenCode has another active operation")
	}
	targetProof := p.TargetProofs
	if p.ForceRetry != nil {
		targetProof = []*k8s.OpenCodeUpgradeSource{p.ForceRetry.OldTarget}
	}
	if len(targetProof) != 1 || targetProof[0] == nil {
		return forceRetryConflict("OpenCode target binding proof unavailable")
	}
	if err = check(`SELECT COUNT(*)+1 FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE p.namespace=? AND p.deployment_name=?`, targetProof[0].Target.Namespace, targetProof[0].Target.Name); err != nil {
		return err
	}
	for _, item := range p.Instances {
		if err = check(`SELECT COUNT(*) FROM instances WHERE id=? AND user_id=? AND runtime_generation=? AND status=?`, item.ID, item.Owner, item.Generation, item.Status); err != nil {
			return err
		}
		if err = check(`SELECT COUNT(*) FROM instance_workspace_write_fences WHERE instance_id=? AND owner_user_id=? AND generation=? AND pod_uid=? AND state='closed'`, item.ID, item.Owner, item.Generation, item.Assignment.PodUID); err != nil {
			return err
		}
		if err = check(`SELECT COUNT(*) FROM instance_lifecycle_manual_guards WHERE instance_id=? AND token=?`, item.ID, "opencode_upgrade_"+p.Operation); err != nil {
			return err
		}
		if err = check(`SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND owner_user_id=? AND generation=? AND operation_id=? AND state='process_group_confirmed'`, item.ID, item.Owner, item.Generation, p.Operation); err != nil {
			return err
		}
		if err = check(`SELECT COUNT(*)+1 FROM instance_runtime_bindings WHERE instance_id=?`, item.ID); err != nil {
			return err
		}
		if err = check(`SELECT COUNT(*)+1 FROM instance_workspace_write_intents WHERE instance_id=?`, item.ID); err != nil {
			return err
		}
		if preCapsule {
			if err = check(`SELECT COUNT(*) FROM runtime_upgrade_items WHERE rollout_id=? AND instance_id=? AND snapshot_ref IS NULL AND workspace_manifest_sha256 IS NULL AND postflight_json IS NULL`, r.ID, item.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *RuntimeScheduler) ConfirmOpenCodeForceRetry(ctx context.Context, id int64, req OpenCodeForceRetryRequest, actor int) (*models.RuntimeRollout, error) {
	parsed, e := uuid.Parse(req.RequestID)
	if e != nil || parsed.String() != req.RequestID || !req.Force || actor <= 0 || !validOpenCodeSHA(req.Fingerprint) {
		return nil, forceRetryConflict("explicit force, canonical request UUID, actor and fingerprint required")
	}
	if _, _, e = s.openCodeUpgradeDependencies(); e != nil {
		return nil, e
	}
	unlock, e := repository.LockOpenCodeUpgradeController(ctx, s.upgrade.sess)
	if e != nil {
		return nil, e
	}
	defer unlock()
	r, e := s.rolloutRepo.GetByID(ctx, id)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, forceRetryConflict("OpenCode rollout not found")
	}
	p, e := s.loadOpenCodePlan(ctx, r)
	if e != nil {
		return nil, e
	}
	if a := p.ForceRetry; a != nil {
		if a.RequestID == req.RequestID && a.ActorUserID == actor && a.NewImage == req.TargetImageRef && a.Fingerprint == req.Fingerprint {
			return r, nil
		}
		return nil, forceRetryConflict("OpenCode force attempt already confirmed with different inputs")
	}
	pf, native, lease, e := s.inspectOpenCodeForceRetry(ctx, r, p, req.TargetImageRef)
	if e != nil {
		return nil, e
	}
	if pf.Fingerprint != req.Fingerprint {
		return nil, forceRetryConflict("OpenCode force preflight fingerprint changed")
	}
	oldRaw := p.raw
	a := &openCodeForceAttempt{RequestID: req.RequestID, ActorUserID: actor, Fingerprint: req.Fingerprint, OldImage: r.TargetImageRef, NewImage: req.TargetImageRef, StartedAt: time.Now().UTC(), OldCheckpointSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(oldRaw))), OldTarget: p.TargetProofs[0], OldPods: p.Targets, NativeEvidence: native, LeaseEvidence: lease}
	audit, _ := json.Marshal(struct {
		Attempt       *openCodeForceAttempt `json:"attempt"`
		OldCheckpoint string                `json:"old_checkpoint"`
		OldPreflight  string                `json:"old_preflight"`
		OldError      string                `json:"old_error"`
	}{a, oldRaw, *r.PreflightJSON, stringValue(r.ErrorMessage)})
	e = s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		// Row and checkpoint locks prevent confirmation racing a stale controller.
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id WHERE r.id=? AND r.status='running' AND r.target_image_ref=? AND BINARY c.state_json=BINARY ? FOR UPDATE`, id, r.TargetImageRef, oldRaw)
		if err != nil {
			return err
		}
		var count int
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return forceRetryConflict("OpenCode source changed before confirmation")
		}
		result, err := tx.SQL().ExecContext(ctx, `INSERT INTO runtime_upgrade_audits(rollout_id,actor_user_id,action,phase,outcome,detail_json,created_at) VALUES(?,?,'opencode_force_retry','preserve','confirmed',?,?)`, id, actor, string(audit), a.StartedAt)
		if err != nil {
			return err
		}
		a.AuditID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		p.ForceRetry = a
		p.TargetImage = req.TargetImageRef
		p.Targets = nil
		p.TargetControllers = nil
		p.TargetProofs = nil
		p.Stage = "force_zero"
		p.Failure = ""
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		fingerprint := openCodePreflightFingerprint(raw)
		caps, _ := json.Marshal(openCodeUpgradeCapabilities)
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET target_image_ref=?,target_image_digest=?,preflight_json=?,plan_fingerprint=?,required_capabilities_json=?,phase='opencode_force_retry',error_message=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND status='running'`, req.TargetImageRef, imageDigestFromReference(req.TargetImageRef), string(raw), fingerprint, string(caps), id); err != nil {
			return err
		}
		return repository.NewRuntimeRolloutRepository(tx).(interface {
			CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
		}).CompareAndSavePoolCheckpoint(ctx, id, oldRaw, string(raw))
	}, nil)
	if e != nil {
		return nil, e
	}
	return s.rolloutRepo.GetByID(ctx, id)
}
func (s *RuntimeScheduler) advanceOpenCodeForceRetry(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, backend k8s.OpenCodeUpgradeDeploymentService) error {
	a := p.ForceRetry
	if a == nil || a.AuditID <= 0 || len(p.Sources) != 1 || a.OldTarget == nil || len(a.OldPods) != 1 {
		return forceRetryConflict("OpenCode force attempt audit incomplete")
	}
	force, ok := s.deployments.(k8s.OpenCodeForceTargetService)
	if !ok {
		return forceRetryConflict("OpenCode force target CAS unavailable")
	}
	if err := s.verifyOpenCodeForceSQL(ctx, r, p, true); err != nil {
		return err
	}
	evidence, ok := s.agentClient.(openCodeForceEvidenceClient)
	if !ok {
		return forceRetryConflict("OpenCode read-only source authority unavailable")
	}
	for _, item := range p.Instances {
		if item.State != "quiesced" || item.Capsule != nil || len(item.StopProof) == 0 {
			return forceRetryConflict("OpenCode force source preservation state changed")
		}
		if err := evidence.openCodeForceReadOnlySourceProof(ctx, item.Assignment, item.StopProof); err != nil {
			return err
		}
	}
	op := strconv.FormatInt(r.ID, 10)
	switch p.Stage {
	case "force_zero":
		if err := force.ScaleOpenCodeForceTargetZero(ctx, p.Sources[0], a.OldTarget, op, a.RequestID); err != nil {
			return err
		}
		p.Stage = "force_replace"
	case "force_replace":
		if err := force.ReplaceOpenCodeForceTarget(ctx, p.Sources[0], a.OldTarget, a.NewImage, op, a.RequestID); err != nil {
			if errors.Is(err, k8s.ErrRuntimeFaultEvictionPending) {
				return nil
			}
			return err
		}
		p.Stage = "force_ready"
	case "force_ready":
		gone, err := force.ObserveOpenCodeForceTarget(ctx, p.Sources[0], a.OldTarget, a.NewImage, op, a.RequestID)
		if err != nil {
			return err
		}
		if !gone {
			return nil
		}
		target, err := backend.InspectOpenCodeUpgradeTarget(ctx, p.Sources[0].Target.Namespace, a.OldTarget.Target.Name, a.NewImage, op)
		if err != nil {
			return err
		}
		if target.UID != a.OldTarget.Target.UID {
			return forceRetryConflict("OpenCode force target controller UID replaced")
		}
		if err = backend.ValidateOpenCodeUpgradeTarget(ctx, p.Sources[0], target, op); err != nil {
			return err
		}
		reports, err := s.podRepo.List(ctx, RuntimeTypeOpenCode)
		if err != nil {
			return err
		}
		live, err := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
		if err != nil {
			return err
		}
		pods := []models.RuntimePod{}
		for _, pod := range upgradePodsWithLiveIdentity(reports, live) {
			if pod.Namespace != target.Namespace || pod.DeploymentName != target.Name || !runtimePodMatchesImage(pod, a.NewImage) {
				continue
			}
			if stringValue(pod.PodUID) == "" || pod.ID <= 0 {
				return forceRetryConflict("OpenCode force new registration UID missing")
			}
			for _, old := range a.OldPods {
				if stringValue(old.PodUID) == stringValue(pod.PodUID) {
					return forceRetryConflict("OpenCode force old Pod survived")
				}
			}
			if err = validateOpenCodeTargetCapabilities(pod); err != nil {
				return err
			}
			ready, err := backend.ColdRuntimeStandbyReady(ctx, target, stringValue(pod.PodUID))
			if err != nil {
				return err
			}
			if !ready {
				return nil
			}
			pods = append(pods, pod)
		}
		if len(pods) == 0 {
			return nil
		}
		if len(pods) != 1 {
			return forceRetryConflict("OpenCode force new Pod inventory ambiguous")
		}
		proof, err := backend.CaptureOpenCodeUpgradeSource(ctx, target)
		if err != nil {
			return err
		}
		if proof.StorageIdentity != p.Sources[0].StorageIdentity {
			return forceRetryConflict("OpenCode force new storage differs")
		}
		p.TargetControllers = []k8s.RuntimeRolloutTarget{target}
		p.TargetProofs = []*k8s.OpenCodeUpgradeSource{proof}
		p.Targets = pods
		if err = s.verifyFrozenOpenCodeTargetPods(ctx, p); err != nil {
			return err
		}
		client := s.agentClient.(openCodeUpgradeSourceClient)
		if err = client.openCodePoolIdle(ctx, p.Targets[0]); err != nil {
			return err
		}
		if err = s.probeOpenCodeForceLeases(ctx, p); err != nil {
			return err
		}
		p.Stage = "preserve"
	default:
		return forceRetryConflict("OpenCode force phase invalid")
	}
	return s.saveOpenCodePlan(ctx, r, p)
}
func (s *RuntimeScheduler) probeOpenCodeForceLeases(ctx context.Context, p *openCodeUpgradePlan) error {
	probe, ok := s.deployments.(k8s.OpenCodeForceLeaseProbe)
	if !ok || len(p.TargetProofs) != 1 || len(p.Targets) != 1 {
		return forceRetryConflict("OpenCode forced lease absence observation unavailable")
	}
	ids := []int{}
	for _, item := range p.Instances {
		ids = append(ids, item.ID)
	}
	_, err := probe.InspectOpenCodeForceLeaseAbsence(ctx, p.TargetProofs[0], p.Targets[0], ids)
	return err
}
