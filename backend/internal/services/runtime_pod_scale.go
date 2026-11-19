package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/upper/db/v4"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

type RuntimePodScaleOptions struct {
	Items       []k8s.RuntimePodScaleOption  `json:"items"`
	Unavailable []RuntimePodScaleUnavailable `json:"unavailable,omitempty"`
	Limits      struct {
		MinPods int `json:"min_pods"`
		MaxPods int `json:"max_pods"`
	} `json:"limits"`
}
type RuntimePodScaleUnavailable struct {
	RuntimeType string `json:"runtime_type"`
	Reason      string `json:"reason"`
}

func (s *RuntimePodMaintenanceService) scaleKube() (k8s.RuntimePodScaleDeploymentService, error) {
	if s == nil || s.scheduler == nil || s.scheduler.podRepo == nil || s.sess == nil {
		return nil, fmt.Errorf("persistent Runtime expansion is unavailable")
	}
	kube, ok := s.scheduler.deployments.(k8s.RuntimePodScaleDeploymentService)
	if !ok {
		return nil, fmt.Errorf("Kubernetes Runtime expansion is unavailable")
	}
	return kube, nil
}

// OpenClaw retains serving old pools after scoped upgrades. Only committed,
// approved source inventories establish which descendants are current defaults.
// Registration order, replicas and names alone never select an upgrade pool.
func runtimeDefaultScaleTargets(namespace, runtimeType string, rollouts []models.RuntimeRollout, checkpoints map[int64]string) ([]k8s.RuntimePodScaleTarget, error) {
	base := runtimeType + "-runtime"
	if runtimeType == "deepseek-harness" {
		base = "deepseek-harness-runtime"
	}
	targets := []k8s.RuntimePodScaleTarget{{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: base, ApprovalSource: "canonical-default"}}
	if runtimeType == RuntimeTypeOpenCode {
		ordered := append([]models.RuntimeRollout(nil), rollouts...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		canonical := map[string]bool{base: true}
		for _, r := range ordered {
			if r.RuntimeType != runtimeType || r.Status != "finished" {
				continue
			}
			if !isOpenCodeNativeRollout(&r) {
				continue
			}
			if r.PlanFingerprint == nil || r.PreflightID == nil {
				return nil, fmt.Errorf("OpenCode default has no committed native preservation proof")
			}
			var sources map[string]string
			if json.Unmarshal([]byte(stringValue(r.SourceImagesJSON)), &sources) != nil || len(sources) == 0 {
				return nil, fmt.Errorf("OpenCode default source scope is missing")
			}
			next := []k8s.RuntimePodScaleTarget{}
			for key := range sources {
				parts := strings.SplitN(key, "/", 2)
				if len(parts) != 2 || parts[0] != namespace || !canonical[parts[1]] {
					return nil, fmt.Errorf("OpenCode default source lineage changed")
				}
				next = append(next, k8s.RuntimePodScaleTarget{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: runtimeUpgradeTargetDeploymentName(parts[1], r.ID), UpgradeID: strconv.FormatInt(r.ID, 10), SourceDeployment: parts[1], ImageRef: r.TargetImageRef, ApprovalSource: "opencode-native:" + *r.PlanFingerprint})
			}
			for _, target := range next {
				canonical[target.DeploymentName] = true
			}
			targets = next
		}
		return targets, nil
	}
	if runtimeType != RuntimeTypeOpenClaw {
		// Hermes and DeepSeek Harness commit their default in-place image
		// deliveries without changing Deployment UID. Pin the latest eligible
		// commit even if it used the historical mutable-image convention.
		for _, rollout := range rollouts {
			if rollout.RuntimeType != runtimeType || rollout.Status != "finished" {
				continue
			}
			targets[0].ApprovalSource = fmt.Sprintf("committed-in-place:%d:%s:%x", rollout.ID, stringValue(rollout.PlanFingerprint), sha256.Sum256([]byte(stringValue(rollout.SourceImagesJSON))))
			if IsImmutableRuntimeImage(rollout.TargetImageRef) {
				targets[0].ImageRef = rollout.TargetImageRef
			}
			break
		}
		return targets, nil
	}
	canonical := map[string]bool{base: true}
	ordered := append([]models.RuntimeRollout(nil), rollouts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for _, rollout := range ordered {
		if rollout.RuntimeType != "openclaw" || rollout.Status != "finished" {
			continue
		}
		proof, proofErr := coldSuccessorProof(&rollout)
		if proofErr != nil {
			return nil, proofErr
		}
		if proof != nil {
			if proof.Source.Namespace != namespace || !canonical[proof.Source.Name] {
				return nil, fmt.Errorf("cold successor has no approved default lineage")
			}
			target := k8s.RuntimePodScaleTarget{DeploymentUID: proof.SuccessorUID, RuntimeType: runtimeType, Namespace: namespace, DeploymentName: runtimeUpgradeTargetDeploymentName(proof.Source.Name, rollout.ID), UpgradeID: strconv.FormatInt(rollout.ID, 10), SourceDeployment: proof.Source.Name, ImageRef: rollout.TargetImageRef, ApprovalSource: "cold-successor:" + proof.Fingerprint()}
			canonical[target.DeploymentName] = true
			targets = []k8s.RuntimePodScaleTarget{target}
			continue
		}
		if isEmptyOpenClawPoolReset(&rollout) {
			targets, proofErr = openClawInPlaceScaleTargets(rollout, checkpoints[rollout.ID], targets)
			if proofErr != nil {
				return nil, proofErr
			}
			continue
		}
		if rollout.PreflightID == nil || strings.TrimSpace(*rollout.PreflightID) == "" || rollout.PlanFingerprint == nil || strings.TrimSpace(*rollout.PlanFingerprint) == "" || rollout.SourceImagesJSON == nil || strings.TrimSpace(rollout.TargetImageRef) == "" {
			return nil, fmt.Errorf("finished OpenClaw rollout %d lacks approved default source proof", rollout.ID)
		}
		var sources map[string]string
		if err := json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources); err != nil || len(sources) == 0 {
			return nil, fmt.Errorf("finished OpenClaw rollout %d source inventory is unavailable", rollout.ID)
		}
		current := []k8s.RuntimePodScaleTarget{}
		for key := range sources {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid OpenClaw default source inventory")
			}
			if parts[0] != namespace || !canonical[parts[1]] {
				continue
			}
			current = append(current, k8s.RuntimePodScaleTarget{RuntimeType: runtimeType, Namespace: namespace, DeploymentName: runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID), UpgradeID: strconv.FormatInt(rollout.ID, 10), SourceDeployment: parts[1], ImageRef: rollout.TargetImageRef, ApprovalSource: fmt.Sprintf("committed:%s:%s:%x", *rollout.PreflightID, *rollout.PlanFingerprint, sha256.Sum256([]byte(*rollout.SourceImagesJSON)))})
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("finished OpenClaw rollout %d has no proven canonical default lineage", rollout.ID)
		}
		// Add after processing all sources: a rollout cannot authorize itself.
		for _, target := range current {
			canonical[target.DeploymentName] = true
		}
		targets = current
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].DeploymentName < targets[j].DeploymentName })
	return targets, nil
}

// An empty-pool reset updates the source Deployment in place. Its completed
// checkpoint records which Deployment UID was actually switched and verified.
func openClawInPlaceScaleTargets(rollout models.RuntimeRollout, raw string, current []k8s.RuntimePodScaleTarget) ([]k8s.RuntimePodScaleTarget, error) {
	unproven := fmt.Errorf("finished OpenClaw rollout %d lacks approved in-place default source proof", rollout.ID)
	var progress poolReplacementProgress
	if json.Unmarshal([]byte(raw), &progress) != nil || progress.Kind != "openclaw-pool-replacement" ||
		progress.Mode != "" || progress.Version != 2 || progress.Stage != "cleanup_success" ||
		!progress.Changed || progress.Restored || progress.Submitted || progress.Failure != "" ||
		len(progress.Targets) == 0 || progress.PoolIndex != len(progress.Targets) || progress.ProbeIndex != len(progress.Targets) ||
		!IsImmutableRuntimeImage(rollout.TargetImageRef) {
		return nil, unproven
	}
	var sources map[string]string
	if json.Unmarshal([]byte(stringValue(rollout.SourceImagesJSON)), &sources) != nil || len(sources) != len(progress.Targets) {
		return nil, unproven
	}
	proven := make(map[string]k8s.RuntimeRolloutTarget, len(progress.Targets))
	for _, source := range progress.Targets {
		key := replacementKey(source)
		if source.Namespace == "" || source.Name == "" || source.UID == "" || source.Image == "" || sources[key] != source.Image {
			return nil, unproven
		}
		if _, duplicate := proven[key]; duplicate {
			return nil, unproven
		}
		proven[key] = source
	}
	var targets []k8s.RuntimePodScaleTarget
	for _, target := range current {
		source, ok := proven[target.Namespace+"/"+target.DeploymentName]
		if !ok {
			continue
		}
		if (target.DeploymentUID != "" && target.DeploymentUID != source.UID) ||
			(target.ImageRef != "" && target.ImageRef != source.Image) {
			return nil, unproven
		}
		target.DeploymentUID = source.UID
		target.ImageRef = rollout.TargetImageRef
		target.ApprovalSource = fmt.Sprintf("committed-in-place:%d:%x", rollout.ID, sha256.Sum256([]byte(raw)))
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, unproven
	}
	return targets, nil
}

type runtimeScaleEvidence uint8

const (
	scaleHealthySource runtimeScaleEvidence = iota
	scaleAllowApproval
	scaleRequireApproval
)

func runtimeScaleDefaultRollouts(ctx context.Context, sess db.Session, runtimeType string) ([]models.RuntimeRollout, map[int64]string, error) {
	switch runtimeType {
	case RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeDeepSeekHarness, RuntimeTypeOpenCode:
	default:
		return nil, nil, nil
	}
	checkpointColumn, checkpointJoin := "''", ""
	if runtimeType == RuntimeTypeOpenClaw {
		checkpointColumn = "COALESCE(c.state_json,'')"
		checkpointJoin = " LEFT JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id AND r.phase='empty_pool_reset' AND r.preflight_id IS NULL"
	}
	query := `SELECT r.id,r.runtime_type,r.status,r.phase,r.target_image_ref,r.preflight_id,r.preflight_json,r.source_images_json,r.plan_fingerprint,` +
		checkpointColumn + ` FROM runtime_rollouts r` + checkpointJoin + ` WHERE r.runtime_type=? AND ` + runtimeCommittedDefaultPredicate + ` ORDER BY r.id DESC`
	if runtimeType != RuntimeTypeOpenClaw && runtimeType != RuntimeTypeOpenCode {
		query += " LIMIT 1"
	}
	rows, err := sess.SQL().QueryContext(ctx, query, runtimeType)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var rollouts []models.RuntimeRollout
	checkpoints := map[int64]string{}
	for rows.Next() {
		var r models.RuntimeRollout
		var checkpoint string
		if err := rows.Scan(&r.ID, &r.RuntimeType, &r.Status, &r.Phase, &r.TargetImageRef, &r.PreflightID, &r.PreflightJSON, &r.SourceImagesJSON, &r.PlanFingerprint, &checkpoint); err != nil {
			return nil, nil, err
		}
		rollouts = append(rollouts, r)
		if checkpoint != "" {
			checkpoints[r.ID] = checkpoint
		}
	}
	return rollouts, checkpoints, rows.Err()
}

func (s *RuntimePodMaintenanceService) defaultScaleSnapshot(ctx context.Context, sess db.Session, kube k8s.RuntimePodScaleDeploymentService, runtimeType string, evidence runtimeScaleEvidence) (k8s.RuntimePodScaleTarget, *k8s.RuntimePodScaleSnapshot, error) {
	if evidence == scaleAllowApproval {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	rollouts, checkpoints, err := runtimeScaleDefaultRollouts(ctx, sess, runtimeType)
	if err != nil {
		return k8s.RuntimePodScaleTarget{}, nil, err
	}
	targets, err := runtimeDefaultScaleTargets(s.scheduler.runtimeNamespace, runtimeType, rollouts, checkpoints)
	if err != nil {
		return k8s.RuntimePodScaleTarget{}, nil, err
	}
	pods, err := repository.NewRuntimePodRepository(sess).List(ctx, runtimeType)
	if err != nil {
		return k8s.RuntimePodScaleTarget{}, nil, err
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].PodName < pods[j].PodName })
	var lastErr error
	var selectedTarget k8s.RuntimePodScaleTarget
	var selected *k8s.RuntimePodScaleSnapshot
	for _, target := range targets {
		target, snapshot, err := s.approvedScaleSnapshot(ctx, sess, kube, target, pods, evidence)
		if err != nil {
			lastErr = err
			continue
		}
		if selected == nil {
			selectedTarget, selected = target, snapshot
			continue
		}
		a, b := selected.Option, snapshot.Option
		equivalent := a.RuntimeType == b.RuntimeType && a.SlotCount == b.SlotCount && a.PodResources == b.PodResources && a.CoordinatorResources == b.CoordinatorResources && a.SlotResources == b.SlotResources && a.ImageRef == b.ImageRef && apiequality.Semantic.DeepEqual(selected.Template.Spec.Resources, snapshot.Template.Spec.Resources)
		if equivalent {
			for i, c := range selected.Template.Spec.Containers {
				if !apiequality.Semantic.DeepEqual(c.Resources, snapshot.Template.Spec.Containers[i].Resources) {
					equivalent = false
					break
				}
			}
		}
		if !equivalent {
			return k8s.RuntimePodScaleTarget{}, nil, fmt.Errorf("current %s default targets have conflicting slot, resource or image configurations", runtimeType)
		}
	}
	if selected != nil {
		return selectedTarget, selected, nil
	}
	if lastErr != nil {
		return k8s.RuntimePodScaleTarget{}, nil, lastErr
	}
	return k8s.RuntimePodScaleTarget{}, nil, fmt.Errorf("no fresh healthy serving Pod proves the current %s default Deployment", runtimeType)
}

func (s *RuntimePodMaintenanceService) approvedScaleSnapshot(ctx context.Context, sess db.Session, kube k8s.RuntimePodScaleDeploymentService, target k8s.RuntimePodScaleTarget, pods []models.RuntimePod, evidence runtimeScaleEvidence) (k8s.RuntimePodScaleTarget, *k8s.RuntimePodScaleSnapshot, error) {
	if err := repository.CheckRuntimePodDeletionIntent(ctx, sess, target.Namespace, target.DeploymentName); err != nil {
		return target, nil, err
	}
	approvalKube, persistent := kube.(k8s.RuntimeScaleApprovalDeploymentService)
	persistent = persistent && evidence != scaleHealthySource
	var configuration *k8s.RuntimePodScaleSnapshot
	if persistent {
		var err error
		configuration, err = approvalKube.InspectRuntimeScaleConfiguration(ctx, target)
		if err != nil {
			return target, nil, err
		}
		approved, err := repository.RuntimeScaleConfigurationApproved(ctx, sess, target.RuntimeType, target.Namespace, target.DeploymentName, configuration.Option.DeploymentUID, configuration.ApprovalDigest)
		if err != nil {
			return target, nil, err
		}
		if approved {
			return target, configuration, nil
		}
	}

	lastErr := fmt.Errorf("current %s/%s configuration has no matching approval or fresh healthy source", target.Namespace, target.DeploymentName)
	if evidence == scaleRequireApproval {
		return target, nil, lastErr
	}
	client, native := s.scheduler.agentClient.(runtimeScaleNativeProbe)
	if persistent && !native {
		return target, nil, fmt.Errorf("native per-slot worker protocol proof is required to approve %s/%s", target.Namespace, target.DeploymentName)
	}
	now := time.Now()
	var candidates []models.RuntimePod
	for _, pod := range pods {
		if pod.RuntimeType != target.RuntimeType || pod.Namespace != target.Namespace || pod.DeploymentName != target.DeploymentName || pod.State != "ready" || pod.Draining || pod.SchedulingPaused || stringValue(pod.PodUID) == "" || pod.LastSeenAt == nil || now.Sub(*pod.LastSeenAt) > 2*time.Minute || pod.LastSeenAt.After(now.Add(time.Minute)) {
			continue
		}
		candidates = append(candidates, pod)
	}
	if persistent {
		candidates = s.scheduler.nextScaleSourceCandidates(target, candidates)
	}
	for _, pod := range candidates {
		if err := ctx.Err(); err != nil {
			return target, nil, err
		}
		target.PodName, target.PodUID = pod.PodName, *pod.PodUID
		target.RequireCurrentTemplate = persistent
		verified, err := kube.InspectRuntimeScale(ctx, target)
		if err != nil {
			lastErr = err
			continue
		}
		if !persistent {
			return target, verified, nil
		}
		if verified.Option.DeploymentUID != configuration.Option.DeploymentUID || verified.ResourceVersion != configuration.ResourceVersion || verified.WorkspaceStorageIdentity != configuration.WorkspaceStorageIdentity || !apiequality.Semantic.DeepEqual(verified.Template, configuration.Template) {
			return target, nil, fmt.Errorf("Runtime configuration changed during approval")
		}
		if err := s.scheduler.verifyScaleSourceProtocol(ctx, client, pod); err != nil {
			lastErr = err
			continue
		}
		if err := repository.RecordRuntimeScaleApproval(ctx, sess, target.RuntimeType, target.Namespace, target.DeploymentName, configuration.Option.DeploymentUID, configuration.ApprovalDigest, target.PodUID, configuration.ApprovalConfiguration); err != nil {
			return target, nil, err
		}
		return target, configuration, nil
	}
	return target, nil, lastErr
}

func inspectRuntimeExpansion(ctx context.Context, sess db.Session, kube k8s.RuntimeScaleApprovalDeploymentService, target k8s.RuntimePodScaleTarget, snapshot *k8s.RuntimePodScaleSnapshot) (k8s.RuntimeExpansionObservation, error) {
	attemptID := snapshot.ExpansionAttemptID()
	uids, found, err := repository.RuntimeScaleStartupBaseline(ctx, sess, snapshot.Option.DeploymentUID, attemptID)
	if err != nil {
		return k8s.RuntimeExpansionObservation{}, err
	}
	if found {
		snapshot.ExpansionBaseline = &k8s.RuntimeExpansionBaseline{ID: attemptID, PodUIDs: uids}
	}
	return kube.InspectRuntimeExpansion(ctx, target, snapshot)
}

func prepareRuntimeExpansion(ctx context.Context, tx db.Session, snapshot *k8s.RuntimePodScaleSnapshot, observed k8s.RuntimeExpansionObservation) error {
	id := uuid.NewString()
	if err := repository.RecordRuntimeScaleStartupBaseline(ctx, tx, snapshot.Option.DeploymentUID, snapshot.ExpansionAttemptID(), id, observed.ExistingPodUIDs); err != nil {
		return err
	}
	snapshot.ExpansionWriteID = id
	return nil
}

func (s *RuntimePodMaintenanceService) ScaleOptions(ctx context.Context) (*RuntimePodScaleOptions, error) {
	kube, err := s.scaleKube()
	if err != nil {
		return nil, err
	}
	result := &RuntimePodScaleOptions{Items: []k8s.RuntimePodScaleOption{}}
	result.Limits.MinPods = 1
	result.Limits.MaxPods = 10
	for _, runtimeType := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		_, snapshot, err := s.defaultScaleSnapshot(ctx, s.sess, kube, runtimeType, scaleAllowApproval)
		if err != nil {
			result.Unavailable = append(result.Unavailable, RuntimePodScaleUnavailable{RuntimeType: runtimeType, Reason: err.Error()})
			continue
		}
		result.Items = append(result.Items, snapshot.Option)
	}
	return result, nil
}
func (s *RuntimePodMaintenanceService) Scale(ctx context.Context, req k8s.RuntimePodScaleRequest) (*k8s.RuntimePodScaleResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	kube, err := s.scaleKube()
	if err != nil {
		return nil, err
	}
	evidence := scaleHealthySource
	approvalKube, persistent := kube.(k8s.RuntimeScaleApprovalDeploymentService)
	if persistent {
		if _, _, err := s.defaultScaleSnapshot(ctx, s.sess, kube, req.RuntimeType, scaleAllowApproval); err != nil {
			return nil, err
		}
		evidence = scaleRequireApproval
	}
	var result *k8s.RuntimePodScaleResult
	var unconfirmedWrite error
	err = s.createAdmission(ctx, req.RuntimeType, func(tx db.Session) error {
		target, snapshot, err := s.defaultScaleSnapshot(ctx, tx, kube, req.RuntimeType, evidence)
		if err != nil {
			return err
		}
		if err := repository.CheckRuntimePodDeletionIntent(ctx, tx, target.Namespace, target.DeploymentName); err != nil {
			return err
		}
		if persistent {
			observed, err := inspectRuntimeExpansion(ctx, tx, approvalKube, target, snapshot)
			if err != nil {
				return err
			}
			if err := prepareRuntimeExpansion(ctx, tx, snapshot, observed); err != nil {
				return err
			}
		}
		result, err = kube.ExpandRuntimeScale(ctx, target, snapshot, req.PodCount)
		if persistent && errors.Is(err, k8s.ErrRuntimeScaleWriteUnconfirmed) {
			unconfirmedWrite = err
			return nil
		}
		return err
	})
	return result, errors.Join(err, unconfirmedWrite)
}
