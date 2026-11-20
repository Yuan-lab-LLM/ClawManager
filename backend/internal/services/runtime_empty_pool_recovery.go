package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

const RuntimeEmptyPoolRecoveryStrategy = "empty_pool_recovery_v1"
const RuntimeEmptyPoolRecoveryPhase = "empty_pool_recovery"

type emptyPoolRecoveryPlan struct {
	Kind      string                     `json:"kind"`
	Version   int                        `json:"version"`
	SourceUID string                     `json:"source_uid"`
	Proof     *k8s.EmptyRuntimePoolProof `json:"proof"`
}

func emptyPoolFingerprint(plan emptyPoolRecoveryPlan, image string) string {
	raw, _ := json.Marshal(struct {
		Plan  emptyPoolRecoveryPlan
		Image string
	}{plan, image})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func loadEmptyPoolRecovery(r *models.RuntimeRollout) (*emptyPoolRecoveryPlan, error) {
	if r == nil || r.Phase != RuntimeEmptyPoolRecoveryPhase {
		return nil, nil
	}
	var p emptyPoolRecoveryPlan
	if json.Unmarshal([]byte(stringValue(r.PreflightJSON)), &p) != nil || p.Kind != RuntimeEmptyPoolRecoveryStrategy || p.Version != 1 || p.Proof == nil || p.SourceUID == "" || p.SourceUID != p.Proof.Source.UID || p.Proof.SlotCount < 1 || len(p.Proof.Configuration) != 64 || !IsImmutableRuntimeImage(r.TargetImageRef) || stringValue(r.PlanFingerprint) != emptyPoolFingerprint(p, r.TargetImageRef) {
		return nil, fmt.Errorf("empty pool recovery proof is invalid")
	}
	var sources map[string]string
	if json.Unmarshal([]byte(stringValue(r.SourceImagesJSON)), &sources) != nil || len(sources) != 1 || sources[replacementKey(p.Proof.Source)] != p.Proof.Source.Image {
		return nil, fmt.Errorf("empty pool recovery source inventory changed")
	}
	return &p, nil
}

// This path cannot migrate, reset or take over any instance. It is an explicit
// admin recovery of a retained canonical template after physical pool retirement.
func (s *RuntimeScheduler) requireEmptyRuntimeRecovery(ctx context.Context, rt string) error {
	if s == nil || s.management == nil || s.management.sess == nil {
		return fmt.Errorf("empty Runtime inventory unavailable")
	}
	sess := s.management.sess
	// Historical registration counters are not execution authority. Kubernetes
	// separately proves every same-type controller is at zero with no Pods;
	// the durable instance and binding inventories must independently be empty.
	var occupied int
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM instances WHERE type=? AND instance_mode='lite')+
	 (SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_type=?)`, rt, rt)
	if err != nil {
		return err
	}
	if err = row.Scan(&occupied); err != nil {
		return err
	}
	if occupied != 0 {
		return fmt.Errorf("empty pool recovery requires no Lite instances or bindings")
	}
	return nil
}

func (s *RuntimeScheduler) SubmitEmptyRuntimeRecovery(ctx context.Context, rt, image, uid string, actor *int) (*models.RuntimeRollout, error) {
	if _, ok := NormalizeV2RuntimeType(rt); !ok || actor == nil || *actor < 1 || uid == "" || !IsImmutableRuntimeImage(image) {
		return nil, fmt.Errorf("admin actor, Runtime type, immutable image and retired controller UID required")
	}
	if s == nil || s.management == nil || s.management.sess == nil || s.rolloutRepo == nil || s.upgrade == nil {
		return nil, fmt.Errorf("empty Runtime recovery unavailable")
	}
	// A lost response is observed through the same immutable intent. Never
	// create another recovery for an already submitted source/image pair.
	var prior int64
	row, err := s.management.sess.SQL().QueryRowContext(ctx, `SELECT id FROM runtime_rollouts WHERE runtime_type=? AND phase=? AND target_image_ref=? AND JSON_UNQUOTE(JSON_EXTRACT(preflight_json,'$.source_uid'))=? ORDER BY id DESC LIMIT 1`, rt, RuntimeEmptyPoolRecoveryPhase, image, uid)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&prior); err == nil {
		return s.rolloutRepo.GetByID(ctx, prior)
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	if err = s.requireEmptyRuntimeRecovery(ctx, rt); err != nil {
		return nil, err
	}
	backend, ok := s.deployments.(k8s.EmptyRuntimePoolRecoveryService)
	if !ok {
		return nil, fmt.Errorf("native empty Runtime recovery unavailable")
	}
	target := k8s.RuntimePodScaleTarget{RuntimeType: rt, Namespace: s.runtimeNamespace, DeploymentName: s.DefaultRuntimePoolName(rt), DeploymentUID: uid}
	proof, err := backend.InspectEmptyRuntimePool(ctx, target)
	if err != nil {
		return nil, err
	}
	if strings.SplitN(image, "@", 2)[0] != strings.SplitN(proof.Source.Image, "@", 2)[0] {
		return nil, fmt.Errorf("target must stay in the frozen native Runtime repository")
	}
	plan := emptyPoolRecoveryPlan{Kind: RuntimeEmptyPoolRecoveryStrategy, Version: 1, SourceUID: uid, Proof: proof}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	sources, _ := json.Marshal(map[string]string{replacementKey(proof.Source): proof.Source.Image})
	digest := imageDigestFromReference(image)
	fingerprint := emptyPoolFingerprint(plan, image)
	r := &models.RuntimeRollout{RuntimeType: rt, TargetImageRef: image, TargetImageDigest: &digest, Status: "pending", Phase: RuntimeEmptyPoolRecoveryPhase, BatchSize: 1, MaxUnavailable: 0, StartedBy: actor, PreflightJSON: stringPtrOrNil(string(raw)), SourceImagesJSON: stringPtrOrNil(string(sources)), PlanFingerprint: &fingerprint}
	// Repository admission holds the normal short control lock and checks all
	// same-type upgrade/recovery owners. No protection records are removed.
	ctx = repository.WithRuntimeRolloutAdmissionValidation(ctx, func() error {
		if err := s.requireEmptyRuntimeRecovery(ctx, rt); err != nil {
			return err
		}
		active, err := s.rolloutRepo.ListActive(ctx, rt)
		if err != nil {
			return err
		}
		if len(active) != 0 {
			return fmt.Errorf("another same-type Runtime rollout is active")
		}
		return nil
	})
	if err = s.rolloutRepo.Create(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *RuntimeScheduler) advanceEmptyRuntimeRecovery(ctx context.Context, r *models.RuntimeRollout) error {
	plan, err := loadEmptyPoolRecovery(r)
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("empty Runtime recovery journal required")
	}
	if r.Status == "pending" {
		now := time.Now().UTC()
		return s.rolloutRepo.UpdateStatus(ctx, r.ID, "running", &now, nil, nil)
	}
	if r.Status != "running" {
		return nil
	}
	if r.StartedAt == nil {
		return fmt.Errorf("empty Runtime recovery has no execution boundary")
	}
	if time.Since(*r.StartedAt) > 10*time.Minute {
		now, message := time.Now().UTC(), "Empty Runtime recovery verification timed out; source identity and workspaces retained"
		return s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", r.StartedAt, &now, &message)
	}
	backend, ok := s.deployments.(k8s.EmptyRuntimePoolRecoveryService)
	if !ok {
		return fmt.Errorf("native empty Runtime recovery unavailable")
	}
	check := func(ctx context.Context) error {
		if s.leader != nil && !s.leader.IsLeader(ctx) {
			return fmt.Errorf("Runtime recovery leadership lost")
		}
		return s.requireEmptyRuntimeRecovery(ctx, r.RuntimeType)
	}
	if err = backend.RecoverEmptyRuntimePool(ctx, plan.Proof, r.RuntimeType, r.TargetImageRef, strconv.FormatInt(r.ID, 10), check); err != nil {
		return err
	}
	reason, err := s.runtimeRolloutCompletionReason(ctx, *r, []k8s.RuntimeRolloutTarget{plan.Proof.Source})
	if err != nil {
		return err
	}
	if reason != "" {
		return nil
	}
	client, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return fmt.Errorf("native slot proof client unavailable")
	}
	pods, err := s.podRepo.List(ctx, r.RuntimeType)
	if err != nil {
		return err
	}
	verified := 0
	for _, pod := range pods {
		if pod.Namespace != plan.Proof.Source.Namespace || pod.DeploymentName != plan.Proof.Source.Name || pod.State != "ready" || !runtimePodMatchesImage(pod, r.TargetImageRef) || pod.LastSeenAt == nil || pod.LastSeenAt.Before(*r.StartedAt) {
			continue
		}
		if err = s.verifyScaleSourceProtocol(ctx, client, pod); err != nil {
			return err
		}
		slots, err := client.observeRuntimeSlots(ctx, pod)
		if err != nil {
			return err
		}
		if len(slots) != plan.Proof.SlotCount {
			return fmt.Errorf("recovered native slot count differs from the retired template")
		}
		for _, slot := range slots {
			if !slot.free() {
				return fmt.Errorf("recovered Runtime contains an occupied or unverified slot")
			}
		}
		verified++
	}
	if verified != 1 {
		return fmt.Errorf("recovered Runtime needs one exact current native registration")
	}
	if err = s.requireEmptyRuntimeRecovery(ctx, r.RuntimeType); err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.rolloutRepo.UpdateStatus(ctx, r.ID, "finished", r.StartedAt, &now, nil)
}

// An empty recovery resets the default only after a fully empty type was proven
// and the new native worker was verified. It never adopts an existing instance.
func emptyRecoveryScaleTarget(namespace, rt string, r models.RuntimeRollout) (*k8s.RuntimePodScaleTarget, error) {
	p, err := loadEmptyPoolRecovery(&r)
	if err != nil || p == nil {
		return nil, err
	}
	if r.RuntimeType != rt || r.Status != "finished" || p.Proof.Source.Namespace != namespace {
		return nil, fmt.Errorf("empty recovery default identity changed")
	}
	return &k8s.RuntimePodScaleTarget{RuntimeType: rt, Namespace: namespace, DeploymentName: p.Proof.Source.Name, DeploymentUID: p.SourceUID, ImageRef: r.TargetImageRef, ApprovalSource: "empty-recovery:" + strconv.FormatInt(r.ID, 10) + ":" + stringValue(r.PlanFingerprint)}, nil
}
