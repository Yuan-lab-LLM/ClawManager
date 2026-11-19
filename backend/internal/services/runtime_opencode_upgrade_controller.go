package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

type openCodeUpgradeInstance struct {
	HistoricalStop         bool                                `json:"historical_stop,omitempty"`
	PriorRolloutID         int64                               `json:"prior_rollout_id,omitempty"`
	ID                     int                                 `json:"id"`
	Owner                  int                                 `json:"owner"`
	Generation             int                                 `json:"generation"`
	Status                 string                              `json:"status"`
	Workspace              string                              `json:"workspace"`
	SourcePod              models.RuntimePod                   `json:"source_pod"`
	Assignment             k8s.RuntimeAssignment               `json:"assignment"`
	Binding                *models.InstanceRuntimeBinding      `json:"binding,omitempty"`
	State                  string                              `json:"state"`
	StopProof              json.RawMessage                     `json:"stop_proof,omitempty"`
	Compatibility          *RuntimeAgentUpgradeCompatibility   `json:"compatibility,omitempty"`
	RollbackGeneration     int                                 `json:"rollback_generation,omitempty"`
	RollbackFromGeneration int                                 `json:"rollback_from_generation,omitempty"`
	RollbackState          string                              `json:"rollback_state,omitempty"`
	TargetStopProof        json.RawMessage                     `json:"target_stop_proof,omitempty"`
	Restore                *RuntimeAgentSessionSQLiteRestore   `json:"restore,omitempty"`
	Capsule                *RuntimeAgentSessionSQLiteMigration `json:"capsule,omitempty"`
}
type openCodeUpgradePlan struct {
	ForceRetry             *openCodeForceAttempt        `json:"force_retry,omitempty"`
	TargetImage            string                       `json:"target_image"`
	PlannedUnavailable     int                          `json:"planned_unavailable_instances"`
	Kind                   string                       `json:"kind"`
	Version                int                          `json:"version"`
	Strategy               string                       `json:"strategy"`
	Contract               openCodeNativeContract       `json:"contract"`
	Sources                []*k8s.OpenCodeUpgradeSource `json:"sources"`
	SourceDeploymentProofs []k8s.RuntimeRolloutTarget   `json:"source_deployment_proofs"`
	TargetProofs           []*k8s.OpenCodeUpgradeSource `json:"target_proofs,omitempty"`
	SourcePods             []models.RuntimePod          `json:"source_pods"`
	Instances              []openCodeUpgradeInstance    `json:"instances"`
	Targets                []models.RuntimePod          `json:"targets,omitempty"`
	TargetControllers      []k8s.RuntimeRolloutTarget   `json:"target_controllers,omitempty"`
	Operation              string                       `json:"operation"`
	Failure                string                       `json:"failure,omitempty"`
	Stage                  string                       `json:"stage"`
	raw                    string
}

func isOpenCodeNativeRollout(r *models.RuntimeRollout) bool {
	if r == nil || r.RuntimeType != RuntimeTypeOpenCode || r.PreflightJSON == nil {
		return false
	}
	var p struct {
		Strategy string `json:"strategy"`
	}
	return json.Unmarshal([]byte(*r.PreflightJSON), &p) == nil && p.Strategy == RuntimeUpgradeStrategyOpenCodePreserve
}
func (s *RuntimeScheduler) openCodeUpgradeDependencies() (k8s.OpenCodeUpgradeDeploymentService, openCodeUpgradeSourceClient, error) {
	if s == nil || s.upgrade == nil || s.upgrade.sess == nil || s.rolloutRepo == nil || s.podRepo == nil || s.bindingRepo == nil || s.instanceRepo == nil || s.deployments == nil {
		return nil, nil, fmt.Errorf("OpenCode native upgrade controller is unavailable")
	}
	backend, ok := s.deployments.(k8s.OpenCodeUpgradeDeploymentService)
	if !ok {
		return nil, nil, fmt.Errorf("OpenCode native source/storage inspection is unavailable")
	}
	client, ok := s.agentClient.(openCodeUpgradeSourceClient)
	if !ok || !requiresConfirmedGatewayStop(s.agentClient) {
		return nil, nil, fmt.Errorf("OpenCode native upgrade requires persisted C1 container termination")
	}
	if _, ok = s.agentClient.(RuntimeUpgradeAgentClient); !ok {
		return nil, nil, fmt.Errorf("OpenCode native maintenance client is unavailable")
	}
	if _, ok = s.rolloutRepo.(interface {
		CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
	}); !ok {
		return nil, nil, fmt.Errorf("OpenCode checkpoint CAS is unavailable")
	}
	return backend, client, nil
}

// Submit freezes the complete native/storage/source identity before publishing
// work. Request handlers never dispatch maintenance or mutate Deployments.
func (s *RuntimeScheduler) SubmitOpenCodeUpgrade(ctx context.Context, target string, actor *int) (*models.RuntimeRollout, error) {
	backend, client, err := s.openCodeUpgradeDependencies()
	if err != nil {
		return nil, err
	}
	inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
	if !ok {
		return nil, fmt.Errorf("OpenCode live source inventory is unavailable")
	}
	sources, err := inventory.RolloutTargets(ctx, s.runtimeNamespace, RuntimeTypeOpenCode)
	if err != nil {
		return nil, err
	}
	if len(sources) != 1 {
		return nil, fmt.Errorf("OpenCode native preservation currently requires one proven source Deployment")
	}
	p := openCodeUpgradePlan{TargetImage: target, Kind: "opencode-native-preserve", Version: 1, Strategy: RuntimeUpgradeStrategyOpenCodePreserve, Stage: "standby", Instances: []openCodeUpgradeInstance{}, Targets: []models.RuntimePod{}}
	sourceImages := map[string]string{}
	for _, source := range sources {
		if !IsImmutableRuntimeImage(source.Image) {
			return nil, fmt.Errorf("OpenCode source image must be immutable")
		}
		snapshot, e := backend.CaptureOpenCodeUpgradeSource(ctx, source)
		if e != nil {
			return nil, e
		}
		for _, container := range snapshot.Template.Spec.Containers {
			env := map[string]string{}
			for _, entry := range container.Env {
				if entry.ValueFrom != nil && (strings.HasPrefix(entry.Name, "OPENCODE_") || strings.HasPrefix(entry.Name, "XDG_")) {
					return nil, fmt.Errorf("OpenCode native path environment cannot be indirect")
				}
				env[entry.Name] = entry.Value
			}
			if e = validateOpenCodeNativeEnvironment(env); e != nil {
				return nil, e
			}
		}
		if snapshot.Replicas != 1 {
			return nil, fmt.Errorf("OpenCode native preservation currently requires one source replica")
		}
		p.Sources = append(p.Sources, snapshot)
		p.SourceDeploymentProofs = append(p.SourceDeploymentProofs, source)
		sourceImages[source.Namespace+"/"+source.Name] = source.Image
	}
	p.Contract, err = inspectOpenCodeRegistryContract(ctx, target, sourceImages)
	if err != nil {
		return nil, err
	}
	live, err := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return nil, err
	}
	reported, err := s.podRepo.List(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return nil, err
	}
	p.SourcePods = upgradePodsWithLiveIdentity(reported, live)
	byUID := map[string]models.RuntimePod{}
	for _, pod := range p.SourcePods {
		if _, ok := sourceImages[pod.Namespace+"/"+pod.DeploymentName]; ok && pod.PodUID != nil && runtimePodMatchesImage(pod, sourceImages[pod.Namespace+"/"+pod.DeploymentName]) {
			byUID[*pod.PodUID] = pod
		}
	}
	p.SourcePods = nil
	for _, pod := range byUID {
		p.SourcePods = append(p.SourcePods, pod)
	}
	for _, source := range sources {
		found := false
		for _, pod := range byUID {
			if pod.Namespace == source.Namespace && pod.DeploymentName == source.Name {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("OpenCode source has no exact live Agent/Pod identity")
		}
	}
	rows, err := s.upgrade.sess.SQL().QueryContext(ctx, `SELECT id,user_id,runtime_generation,status,COALESCE(workspace_path,'') FROM instances WHERE type='opencode' AND instance_mode='lite' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item openCodeUpgradeInstance
		if err = rows.Scan(&item.ID, &item.Owner, &item.Generation, &item.Status, &item.Workspace); err != nil {
			rows.Close()
			return nil, err
		}
		p.Instances = append(p.Instances, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(p.Instances) > 8 {
		return nil, fmt.Errorf("OpenCode native preservation currently supports one eight-slot pool")
	}
	releases := []func(){}
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	locker, ok := s.instanceRepo.(interface {
		AcquireLifecycleLock(context.Context, int) (func(), error)
	})
	if !ok && len(p.Instances) != 0 {
		return nil, fmt.Errorf("OpenCode lifecycle source lock is unavailable")
	}
	for _, item := range p.Instances {
		release, e := locker.AcquireLifecycleLock(ctx, item.ID)
		if e != nil {
			return nil, e
		}
		releases = append(releases, release)
	}
	for i := range p.Instances {
		item := &p.Instances[i]
		if item.Status != "running" && item.Status != "stopped" {
			return nil, fmt.Errorf("OpenCode instance %d has unresolved state %s", item.ID, item.Status)
		}
		expected := RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenCode, item.Owner, item.ID)
		if item.Workspace == "" {
			item.Workspace = expected
		}
		if filepath.Clean(item.Workspace) != filepath.Clean(expected) {
			return nil, fmt.Errorf("OpenCode instance %d workspace ownership changed", item.ID)
		}
		assignment, state, e := client.openCodeSourceAssignment(ctx, item.ID)
		if e != nil {
			return nil, e
		}
		if assignment.InstanceID != item.ID || assignment.UserID != item.Owner || assignment.Generation != item.Generation || assignment.RuntimeType != RuntimeTypeOpenCode || assignment.GatewayID != fmt.Sprintf("gw-%d-%d", item.ID, item.Generation) || state != "running" && state != "stopped" {
			return nil, fmt.Errorf("OpenCode instance %d source assignment differs from frozen SQL identity", item.ID)
		}
		if e = validateOpenCodeNativeEnvironment(assignment.Environment); e != nil {
			return nil, e
		}
		pod, currentSource := byUID[assignment.PodUID]
		if !currentSource || assignment.Endpoint != stringValue(pod.AgentEndpoint) {
			if item.Status != "stopped" || state != "stopped" {
				return nil, fmt.Errorf("OpenCode instance %d source Pod is unavailable or changed", item.ID)
			}
			historical, priorID, e := s.completedOpenCodeStoppedSource(ctx, *item, assignment, p.Sources[0])
			if e != nil {
				return nil, e
			}
			if _, e = client.openCodeHistoricalSourceStopProof(ctx, assignment); e != nil {
				return nil, e
			}
			pod = historical
			item.HistoricalStop = true
			item.PriorRolloutID = priorID
		}
		binding, e := s.bindingRepo.GetByInstanceID(ctx, item.ID)
		if e != nil {
			return nil, e
		}
		if item.Status == "running" && (binding == nil || binding.RuntimePodID != pod.ID || binding.Generation != item.Generation || binding.GatewayID != assignment.GatewayID || binding.State != "running") {
			return nil, fmt.Errorf("OpenCode instance %d running binding is unresolved", item.ID)
		}
		if item.Status == "stopped" {
			if binding != nil {
				return nil, fmt.Errorf("stopped OpenCode instance %d still has a binding", item.ID)
			}
			if item.HistoricalStop {
				_, e = client.openCodeHistoricalSourceStopProof(ctx, assignment)
			} else {
				_, e = client.openCodeSourceStopProof(ctx, assignment)
			}
			if e != nil {
				return nil, e
			}
		}
		assignment.Environment = nil
		assignment.Request = nil
		item.Assignment = assignment
		item.SourcePod = pod
		item.Binding = binding
		item.State = "pending"
	}
	if len(p.Instances) == 0 {
		for _, pod := range p.SourcePods {
			if _, ok := sourceImages[pod.Namespace+"/"+pod.DeploymentName]; ok {
				if err = client.openCodePoolIdle(ctx, pod); err != nil {
					return nil, err
				}
			}
		}
	}
	p.PlannedUnavailable = openCodePlannedUnavailability(p.Instances)
	nonce, err := randomUpgradeID()
	if err != nil {
		return nil, err
	}
	p.Operation = nonce[:32]
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	fingerprint := openCodePreflightFingerprint(raw)
	rawString := string(raw)
	sourceRaw, _ := json.Marshal(sourceImages)
	sourceString := string(sourceRaw)
	capsRaw, _ := json.Marshal(openCodeUpgradeCapabilities)
	capsString := string(capsRaw)
	digest := imageDigestFromReference(target)
	r := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenCode, TargetImageRef: target, TargetImageDigest: &digest, SourceImagesJSON: &sourceString, PlanFingerprint: &fingerprint, PreflightID: &nonce, PreflightJSON: &rawString, RequiredCapabilitiesJSON: &capsString, Status: "pending", Phase: "opencode_native_preserve", BatchSize: 1, MaxUnavailable: p.PlannedUnavailable, AutoRollback: true, StartedBy: actor}
	attempted := false
	err = s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeOrphanLifecycleAdmission(ctx, tx); e != nil {
			return e
		}
		if e := repository.CheckRuntimeManagementRolloutAdmission(ctx, tx, RuntimeTypeOpenCode); e != nil {
			return e
		}
		var count int
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_rollouts WHERE runtime_type='opencode' AND status IN ('pending','running')`)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return fmt.Errorf("OpenCode has an unfinished native upgrade; resume its existing operation")
		}
		row, e = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances WHERE type='opencode' AND instance_mode='lite'`)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count != len(p.Instances) {
			return fmt.Errorf("OpenCode instance inventory changed during submission")
		}
		ids := []int{}
		for _, item := range p.Instances {
			ids = append(ids, item.ID)
		}
		if e := repository.CheckRuntimeOrphanLifecycleReservations(ctx, tx, ids); e != nil {
			return e
		}
		for _, item := range p.Instances {
			var owner, generation int
			var status string
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,status FROM instances WHERE id=? AND type='opencode' FOR UPDATE`, item.ID)
			if e != nil {
				return e
			}
			if e = row.Scan(&owner, &generation, &status); e != nil {
				return e
			}
			if owner != item.Owner || generation != item.Generation || status != item.Status {
				return models.ErrRuntimeUpgradeConflict
			}
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM instance_commands WHERE instance_id=? AND status NOT IN ('succeeded','failed'))+(SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?)`, item.ID, item.ID)
			if e != nil {
				return e
			}
			if e = row.Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				return models.ErrRuntimeUpgradeWritersActive
			}
			if e = repository.RequireNoInstanceTeamWorkspaceWriters(ctx, tx, item.ID); e != nil {
				return e
			}
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_lifecycle_manual_guards WHERE instance_id=?`, item.ID)
			if e != nil {
				return e
			}
			if e = row.Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				return models.ErrInstanceOperationInProgress
			}
			var desired string
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE((SELECT desired_power_state FROM instance_desired_state WHERE instance_id=?),'')`, item.ID)
			if e != nil {
				return e
			}
			if e = row.Scan(&desired); e != nil {
				return e
			}
			if desired != "" && desired != item.Status {
				return models.ErrRuntimeUpgradeConflict
			}
			if _, e = tx.SQL().ExecContext(ctx, `INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(?,?,UTC_TIMESTAMP(6))`, item.ID, "opencode_upgrade_"+p.Operation); e != nil {
				return e
			}
			var prior models.RuntimeUpgrade
			e = tx.Collection("gateway_runtime_upgrades").Find("instance_id", item.ID).One(&prior)
			if e != nil && e != db.ErrNoMoreRows {
				return e
			}
			if e == nil && prior.Active() {
				return models.ErrRuntimeUpgradeInProgress
			}
			if _, e = tx.SQL().ExecContext(ctx, `INSERT INTO gateway_runtime_upgrades(instance_id,owner_user_id,generation,operation_id,state,created_at,updated_at) VALUES(?,?,?,?,'reserved',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE owner_user_id=VALUES(owner_user_id),generation=VALUES(generation),operation_id=VALUES(operation_id),state='reserved',created_at=VALUES(created_at),updated_at=VALUES(updated_at)`, item.ID, item.Owner, item.Generation, p.Operation); e != nil {
				return e
			}
		}
		attempted = true
		r.Status = "preflight_passed"
		if e := repository.NewRuntimeRolloutRepository(tx).Create(ctx, r); e != nil {
			return e
		}
		r.Status = "pending"
		if e := repository.NewRuntimeRolloutRepository(tx).UpdateStatus(ctx, r.ID, "pending", nil, nil, nil); e != nil {
			return e
		}
		if e := repository.ConsumeRuntimeRolloutPreparation(ctx, tx, r.ID); e != nil {
			return e
		}
		for _, item := range p.Instances {
			candidate := runtimeUpgradeCandidate{InstanceID: item.ID, UserID: item.Owner, Generation: item.Generation, RuntimePodID: &item.SourcePod.ID, GatewayID: item.Assignment.GatewayID, SourceVersion: openCodeNativeVersion, SourcePodUID: item.Assignment.PodUID, SourceDeployment: item.SourcePod.DeploymentName, SourceImageDigest: stringValue(item.SourcePod.ImageDigest)}
			if e := s.upgrade.insertUpgradeItems(ctx, tx, r.ID, []runtimeUpgradeCandidate{candidate}, openCodeNativeVersion); e != nil {
				return e
			}
		}
		return repository.NewRuntimeRolloutRepository(tx).(interface {
			CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
		}).CompareAndSavePoolCheckpoint(ctx, r.ID, "", rawString)
	}, nil)
	if err != nil && attempted {
		return r, fmt.Errorf("%w: OpenCode rollout %d: %v", ErrRuntimeUpgradeCommitUncertain, r.ID, err)
	}
	return r, err
}
func (s *RuntimeScheduler) loadOpenCodePlan(ctx context.Context, r *models.RuntimeRollout) (*openCodeUpgradePlan, error) {
	if !isOpenCodeNativeRollout(r) {
		return nil, models.ErrOpenCodeSafeUpgradeRequired
	}
	store, ok := s.rolloutRepo.(rolloutCheckpointStore)
	if !ok {
		return nil, fmt.Errorf("OpenCode checkpoint storage unavailable")
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	var p openCodeUpgradePlan
	if json.Unmarshal([]byte(raw), &p) != nil || p.Kind != "opencode-native-preserve" || p.Version != 1 || p.Strategy != RuntimeUpgradeStrategyOpenCodePreserve || !models.ValidRuntimeOperation(p.Operation) || len(p.Sources) == 0 || len(p.SourcePods) == 0 {
		return nil, fmt.Errorf("OpenCode native upgrade checkpoint is incomplete")
	}
	var frozen openCodeUpgradePlan
	if json.Unmarshal([]byte(*r.PreflightJSON), &frozen) != nil {
		return nil, fmt.Errorf("OpenCode immutable preflight is invalid")
	}
	if r.PlanFingerprint == nil || *r.PlanFingerprint != openCodePreflightFingerprint([]byte(*r.PreflightJSON)) || p.TargetImage != r.TargetImageRef || stringValue(r.TargetImageDigest) != imageDigestFromReference(r.TargetImageRef) || openCodeImmutablePlanDigest(p) != openCodeImmutablePlanDigest(frozen) {
		return nil, fmt.Errorf("OpenCode immutable target/source/native plan identity changed")
	}
	if p.Contract.Version != openCodeNativeVersion || p.Contract.Revision != openCodeNativeRevision || p.Contract.BinarySHA256 != openCodeNativeBinarySHA256 || p.Contract.JournalSHA256 != openCodeNativeJournalSHA256 || p.Contract.BaseImage != openCodeLegacyImage {
		return nil, fmt.Errorf("OpenCode native contract is outside the audited same-version boundary")
	}
	p.raw = raw
	return &p, nil
}
func (s *RuntimeScheduler) saveOpenCodePlan(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if string(raw) == p.raw {
		return nil
	}
	err = s.rolloutRepo.(interface {
		CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
	}).CompareAndSavePoolCheckpoint(ctx, r.ID, p.raw, string(raw))
	if err == nil {
		p.raw = string(raw)
		// Successful progress supersedes the captured prior refusal only at
		// this exact active checkpoint/phase. Guards and immutable results stay.
		if stringValue(r.ErrorMessage) == openCodeScopedPayloadMissing || p.ForceRetry != nil && r.Phase == "opencode_force_retry" && stringValue(r.ErrorMessage) != "" {
			_, err = s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id SET r.error_message=NULL,r.updated_at=UTC_TIMESTAMP(6) WHERE r.id=? AND r.status IN ('pending','running') AND BINARY c.state_json=BINARY ? AND r.phase=? AND BINARY r.error_message=BINARY ?`, r.ID, p.raw, r.Phase, stringValue(r.ErrorMessage))
		}
	}
	return err
}
func (s *RuntimeScheduler) advanceOpenCodeUpgrade(ctx context.Context, r *models.RuntimeRollout) (advanceErr error) {
	var observedCheckpoint string
	defer func() {
		if advanceErr != nil && observedCheckpoint != "" && s != nil && s.upgrade != nil && s.upgrade.sess != nil && ctx.Err() == nil {
			_, _ = s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id SET r.error_message=?,r.updated_at=UTC_TIMESTAMP(6) WHERE r.id=? AND r.status IN ('pending','running') AND r.phase=? AND BINARY c.state_json=BINARY ?`, advanceErr.Error(), r.ID, r.Phase, observedCheckpoint)
		}
	}()

	backend, client, err := s.openCodeUpgradeDependencies()
	if err != nil {
		return err
	}
	unlock, err := repository.LockOpenCodeUpgradeController(ctx, s.upgrade.sess)
	if err != nil {
		return err
	}
	defer unlock()
	fresh, err := s.rolloutRepo.GetByID(ctx, r.ID)
	if err != nil {
		return err
	}
	if fresh == nil || fresh.Status != "pending" && fresh.Status != "running" {
		return nil
	}
	r = fresh
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		return ErrRuntimeUpgradeBusy
	}
	p, err := s.loadOpenCodePlan(ctx, r)
	if err != nil {
		return err
	}
	observedCheckpoint = p.raw
	active, err := s.rolloutRepo.ListActive(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	for _, other := range active {
		if other.ID < r.ID {
			return nil
		}
	}
	if r.Status == "pending" {
		now := time.Now().UTC()
		if err = s.rolloutRepo.UpdateStatus(ctx, r.ID, "running", &now, nil, nil); err != nil {
			return err
		}
		r.Status = "running"
		r.StartedAt = &now
	}
	for _, source := range p.Sources {
		if p.Stage == "retire" || p.Stage == "verify" {
			continue
		}
		if err = backend.ValidateOpenCodeUpgradeSource(ctx, source); err != nil {
			return err
		}
	}
	if strings.HasPrefix(p.Stage, "force_") {
		return s.advanceOpenCodeForceRetry(ctx, r, p, backend)
	}
	for _, proof := range p.TargetProofs {
		if p.Stage == "rollback_retire" || p.Stage == "rollback_verify" {
			continue
		}
		if err = backend.ValidateOpenCodeUpgradeSource(ctx, proof); err != nil {
			return err
		}
	}
	if len(p.TargetProofs) > 0 {
		if len(p.TargetProofs) != len(p.Sources) {
			return fmt.Errorf("OpenCode scheduling proof inventory is incomplete")
		}
		for i, source := range p.Sources {
			if err = backend.ValidateOpenCodeUpgradeScheduling(ctx, source, p.TargetProofs[i], fmt.Sprintf("%d", r.ID), p.Stage); err != nil {
				return err
			}
		}
	}
	if len(p.Targets) > 0 && p.Stage != "rollback_retire" && p.Stage != "rollback_verify" {
		if err = s.verifyFrozenOpenCodeTargetPods(ctx, p); err != nil {
			return err
		}
	}
	if strings.HasPrefix(p.Stage, "rollback_") {
		return s.advanceOpenCodeRollback(ctx, r, p, backend, client)
	}
	upgrade := s.agentClient.(RuntimeUpgradeAgentClient)
	id := strconv.FormatInt(r.ID, 10)
	switch p.Stage {
	case "standby":
		p.TargetControllers = nil
		for _, source := range p.Sources {
			target, e := backend.EnsureOpenCodeUpgradePool(ctx, source, runtimeUpgradeTargetDeploymentName(source.Target.Name, r.ID), r.TargetImageRef, id)
			if e != nil {
				return e
			}
			p.TargetControllers = append(p.TargetControllers, target)
		}
		p.Stage = "standby_ready"
	case "standby_ready":
		frozenTargets := append([]k8s.RuntimeRolloutTarget(nil), p.TargetControllers...)
		if len(frozenTargets) != len(p.Sources) {
			return fmt.Errorf("OpenCode frozen target UID inventory is incomplete")
		}
		p.TargetProofs = nil
		for index, source := range p.Sources {
			target, e := backend.InspectOpenCodeUpgradeTarget(ctx, source.Target.Namespace, runtimeUpgradeTargetDeploymentName(source.Target.Name, r.ID), r.TargetImageRef, id)
			if e != nil {
				return e
			}
			if target.UID != frozenTargets[index].UID || target.Name != frozenTargets[index].Name || target.Namespace != frozenTargets[index].Namespace {
				return fmt.Errorf("OpenCode target UID changed after standby creation")
			}
			if e = backend.ValidateOpenCodeUpgradeTarget(ctx, source, target, id); e != nil {
				return e
			}
			proof, e := backend.CaptureOpenCodeUpgradeSource(ctx, target)
			if e != nil {
				return e
			}
			if proof.StorageIdentity != source.StorageIdentity {
				return fmt.Errorf("OpenCode standby does not mount the frozen original storage")
			}
			p.TargetProofs = append(p.TargetProofs, proof)
		}
		reports, e := s.podRepo.List(ctx, RuntimeTypeOpenCode)
		if e != nil {
			return e
		}
		inventory, e := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
		if e != nil {
			return e
		}
		p.Targets = nil
		for _, target := range p.TargetControllers {
			found := false
			for _, pod := range upgradePodsWithLiveIdentity(reports, inventory) {
				if pod.Namespace != target.Namespace || pod.DeploymentName != target.Name || !runtimePodMatchesImage(pod, r.TargetImageRef) {
					continue
				}
				if err := validateOpenCodeTargetCapabilities(pod); err != nil {
					return err
				}
				ready, e := backend.ColdRuntimeStandbyReady(ctx, target, stringValue(pod.PodUID))
				if e != nil {
					return e
				}
				if !ready {
					return nil
				}
				p.Targets = append(p.Targets, pod)
				found = true
			}
			if !found {
				return nil
			}
		}
		if len(p.Targets) != len(p.TargetControllers) {
			return fmt.Errorf("OpenCode standby Pod identity is ambiguous")
		}
		p.Stage = "quiesce"
	case "quiesce":
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.State != "pending" {
				continue
			}
			if err = repository.NewWorkspaceWriteFenceRepository(s.upgrade.sess).Fence(ctx, item.ID, item.Owner, item.Generation, item.Assignment.PodUID); err != nil {
				return err
			}
			if item.Status == "running" {
				if err = s.agentClient.DeleteGateway(ctx, item.Assignment.Endpoint, item.Assignment.GatewayID); err != nil {
					return err
				}
			}
			if item.HistoricalStop {
				item.StopProof, err = client.openCodeHistoricalSourceStopProof(ctx, item.Assignment)
			} else {
				item.StopProof, err = client.openCodeSourceStopProof(ctx, item.Assignment)
			}
			if err != nil {
				return err
			}
			if err = repository.ReleaseOpenCodeUpgradeSource(ctx, s.upgrade.sess, r.ID, p.Operation, item.ID, item.Owner, item.Generation, item.SourcePod, item.Assignment.GatewayID, item.StopProof); err != nil {
				return err
			}
			item.State = "quiesced"
			if err = s.saveOpenCodePlan(ctx, r, p); err != nil {
				return err
			}
			return nil
		}
		for _, pod := range p.SourcePods {
			if err = client.openCodePoolIdle(ctx, pod); err != nil {
				return err
			}
		}
		p.Stage = "preserve"
	case "preserve":
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.State != "quiesced" {
				continue
			}
			target := p.Targets[0]
			request := RuntimeAgentWorkspaceRequest{ExpectedPodUID: stringValue(target.PodUID), RolloutID: id, SnapshotID: "opencode-" + id, UserID: item.Owner, InstanceID: item.ID, Generation: item.Generation, LeaseToken: runtimeUpgradeLeaseToken(r.ID, item.ID), PreserveSessionSQLite: true, OfficialDBCheck: true}
			if len(item.StopProof) == 0 {
				return fmt.Errorf("OpenCode C1 stop proof is missing")
			}
			compat, e := upgrade.PreflightUpgradeCompatibility(ctx, stringValue(target.AgentEndpoint), request)
			if e != nil {
				return e
			}
			if e = validateOpenCodeCompatibilityReceipt(request, compat); e != nil {
				return e
			}
			item.Compatibility = compat
			lease := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: request, Token: request.LeaseToken, TTLSeconds: 3600}
			if p.ForceRetry != nil {
				if e = s.probeOpenCodeForceLeases(ctx, p); e != nil {
					return e
				}
			}
			if e = upgrade.AcquireWriterLease(ctx, stringValue(target.AgentEndpoint), lease); e != nil {
				return e
			}
			result, e := upgrade.MigrateSessionSQLite(ctx, stringValue(target.AgentEndpoint), request)
			if e == nil {
				e = validateOpenCodeCapsuleReceipt(request, result, compat)
			}
			if e == nil {
				migrationResult := result
				result, e = upgrade.SessionSQLiteMigrationStatus(ctx, stringValue(target.AgentEndpoint), request)
				if e == nil && !sameOpenCodeCapsuleProof(migrationResult, result) {
					e = fmt.Errorf("OpenCode migration/status receipts disagree")
				}
				if e == nil {
					e = validateOpenCodeCapsuleReceipt(request, result, compat)
				}
			}
			// Unknown maintenance outcome retains its fixed-token lease and
			// durable fences until the exact receipt/status is fully verified.
			if e != nil {
				return e
			}
			if e = upgrade.ReleaseWriterLease(ctx, stringValue(target.AgentEndpoint), lease); e != nil {
				return e
			}
			item.Capsule = result
			item.State = "migrated"
			post, _ := json.Marshal(result)
			if _, err = s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='migrated',snapshot_ref=?,workspace_manifest_sha256=?,postflight_json=?,updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=?`, request.SnapshotID, result.PreservationSHA256, string(post), r.ID, item.ID); err != nil {
				return err
			}
			return s.saveOpenCodePlan(ctx, r, p)
		}
		p.Stage = "activate"
	case "activate":
		for _, item := range p.Instances {
			if item.State != "migrated" || validateOpenCodeCapsule(item.ID, item.Capsule) != nil {
				return fmt.Errorf("OpenCode activation lacks a validated full-state capsule")
			}
		}
		for _, pod := range p.Targets {
			if err = client.openCodeActivateUpgrade(ctx, pod, id); err != nil {
				return err
			}
		}
		if err = s.setOpenCodePhase(ctx, r, p, "gateway_restart", ""); err != nil {
			return err
		}
		r.Phase = "gateway_restart"
		p.Stage = "restart"
	case "restart":
		r.Phase = "gateway_restart"
		reports, e := s.podRepo.List(ctx, RuntimeTypeOpenCode)
		if e != nil {
			return e
		}
		live, e := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
		if e != nil {
			return e
		}
		targets := upgradePodsWithLiveIdentity(reports, live)
		ready := []models.RuntimePod{}
		for _, pod := range targets {
			for _, pinned := range p.Targets {
				if pod.ID == pinned.ID && stringValue(pod.PodUID) == stringValue(pinned.PodUID) && pod.State == "ready" && !pod.Draining && runtimePodMatchesImage(pod, r.TargetImageRef) {
					ready = append(ready, pod)
				}
			}
		}
		if len(ready) != len(p.Targets) {
			return nil
		}
		for i := range p.Instances {
			item := &p.Instances[i]
			if item.State == "verified" {
				continue
			}
			if item.State == "migrated" {
				if err = s.prepareOpenCodeRestart(ctx, r, p, item); err != nil {
					return err
				}
				item.State = "restart_ready"
				if err = s.saveOpenCodePlan(ctx, r, p); err != nil {
					return err
				}
			}
			if item.Status == "running" {
				if err = s.EnsureUpgradeGateway(ctx, r, item.ID, ready); err != nil {
					if errors.Is(err, errOpenCodeTargetExecutionFailed) || errors.Is(err, repository.ErrUpgradeRecoveryHeld) {
						return s.beginOpenCodeRollback(ctx, r, p, err)
					}
					return err
				}
				binding, e := s.bindingRepo.GetByInstanceID(ctx, item.ID)
				if e != nil {
					return e
				}
				if binding == nil || binding.Generation != item.Generation+1 || binding.State != "running" {
					return nil
				}
				state, e := upgrade.GatewayState(ctx, stringValue(ready[0].AgentEndpoint), binding.GatewayID)
				if e != nil {
					return e
				}
				if state != nil && state.InstanceID == item.ID && state.Generation == item.Generation+1 && (state.State == "error" || state.State == "stopped") {
					return s.beginOpenCodeRollback(ctx, r, p, errOpenCodeTargetExecutionFailed)
				}
				if state == nil || state.InstanceID != item.ID || state.Generation != item.Generation+1 || !NormalizeRuntimeGatewayLifecycle(state.State, nil).Running {
					return nil
				}
			}
			item.State = "verified"
			if _, err = s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='gateway_verified',updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=?`, r.ID, item.ID); err != nil {
				return err
			}
			return s.saveOpenCodePlan(ctx, r, p)
		}
		p.Stage = "switch"
	case "switch":
		for index, source := range p.Sources {
			if err = backend.SetOpenCodeUpgradePoolActive(ctx, source, p.TargetProofs[index], id, true); err != nil {
				return err
			}
		}
		p.Stage = "retire"
	case "retire":
		for _, source := range p.Sources {
			retired, e := backend.OpenCodeUpgradeSourceRetired(ctx, source, id)
			if e == nil {
				if !retired {
					return nil
				}
				continue
			}
			// Recheck exact physical emptiness and SQL occupancy immediately
			// before the retirement CAS; an observation error grants no permit.
			for _, pod := range p.SourcePods {
				if pod.Namespace == source.Target.Namespace && pod.DeploymentName == source.Target.Name {
					if e = client.openCodePoolIdle(ctx, pod); e != nil {
						return e
					}
				}
			}
			var count int
			row, e := s.upgrade.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings b JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE p.namespace=? AND p.deployment_name=?`, source.Target.Namespace, source.Target.Name)
			if e != nil {
				return e
			}
			if e = row.Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				return fmt.Errorf("OpenCode source still has SQL bindings")
			}
			if e = backend.RetireOpenCodeUpgradeSource(ctx, source, id); e != nil {
				return e
			}
		}
		p.Stage = "verify"
	case "verify":
		for _, source := range p.Sources {
			retired, e := backend.OpenCodeUpgradeSourceRetired(ctx, source, id)
			if e != nil {
				return e
			}
			if !retired {
				return nil
			}
		}
		for _, item := range p.Instances {
			if item.State != "verified" {
				return fmt.Errorf("OpenCode instance verification is incomplete")
			}
		}
		for _, target := range p.TargetControllers {
			observed, e := backend.InspectOpenCodeUpgradeTarget(ctx, target.Namespace, target.Name, r.TargetImageRef, id)
			if e != nil {
				return e
			}
			if observed.UID != target.UID {
				return fmt.Errorf("OpenCode target controller UID changed")
			}
			if !observed.Ready {
				return nil
			}
		}
		if err = s.commitOpenCodeUpgrade(ctx, r, p); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown OpenCode native preservation stage %q", p.Stage)
	}
	return s.saveOpenCodePlan(ctx, r, p)
}
func (s *RuntimeScheduler) prepareOpenCodeRestart(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, item *openCodeUpgradeInstance) error {
	return s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		var generation int
		var status string
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT runtime_generation,status FROM instances WHERE id=? AND user_id=? AND type='opencode' FOR UPDATE`, item.ID, item.Owner)
		if e != nil {
			return e
		}
		if e = row.Scan(&generation, &status); e != nil {
			return e
		}
		if item.Status == "stopped" {
			if generation != item.Generation || status != "stopped" {
				return models.ErrRuntimeUpgradeConflict
			}
			return nil
		}
		if generation == item.Generation+1 && (status == "creating" || status == "running") {
			return nil
		}
		if generation != item.Generation || status != "running" {
			return models.ErrRuntimeUpgradeConflict
		}
		var count int
		row, e = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND operation_id=? AND state='process_group_confirmed'`, item.ID, p.Operation)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return models.ErrRuntimeUpgradeConflict
		}
		if _, e = tx.SQL().ExecContext(ctx, `UPDATE instances SET runtime_generation=runtime_generation+1,status='creating' WHERE id=? AND runtime_generation=?`, item.ID, item.Generation); e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state='restart_ready',updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=? AND state='migrated'`, r.ID, item.ID)
		return e
	}, nil)
}
func (s *RuntimeScheduler) commitOpenCodeUpgrade(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan) error {
	return s.upgrade.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
			return e
		}
		for _, item := range p.Instances {
			expected := item.Generation
			if item.Status == "running" {
				expected++
			}
			var generation int
			var status string
			row, e := tx.SQL().QueryRowContext(ctx, `SELECT runtime_generation,status FROM instances WHERE id=? AND user_id=? AND type='opencode' FOR UPDATE`, item.ID, item.Owner)
			if e != nil {
				return e
			}
			if e = row.Scan(&generation, &status); e != nil {
				return e
			}
			if generation != expected || status != item.Status {
				return models.ErrRuntimeUpgradeConflict
			}
			result, e := tx.SQL().ExecContext(ctx, `UPDATE gateway_runtime_upgrades SET state='completed',updated_at=UTC_TIMESTAMP(6) WHERE instance_id=? AND operation_id=? AND state='process_group_confirmed'`, item.ID, p.Operation)
			if e != nil {
				return e
			}
			if n, e := result.RowsAffected(); e != nil {
				return e
			} else if n != 1 {
				return models.ErrRuntimeUpgradeConflict
			}
		}
		for _, item := range p.Instances {
			if _, e := tx.SQL().ExecContext(ctx, `DELETE FROM instance_lifecycle_manual_guards WHERE instance_id=? AND token=?`, item.ID, "opencode_upgrade_"+p.Operation); e != nil {
				return e
			}
		}
		now := time.Now().UTC()
		return repository.NewRuntimeRolloutRepository(tx).(interface {
			UpdatePoolReplacementStatus(context.Context, int64, string, string, *time.Time, *time.Time, *string) error
		}).UpdatePoolReplacementStatus(ctx, r.ID, p.raw, "finished", r.StartedAt, &now, nil)
	}, nil)
}

func openCodeImmutablePlanDigest(p openCodeUpgradePlan) string {
	p.Stage = ""
	p.Failure = ""
	p.Targets = nil
	p.TargetControllers = nil
	p.TargetProofs = nil
	p.Instances = append([]openCodeUpgradeInstance(nil), p.Instances...)
	for i := range p.Instances {
		p.Instances[i].State = ""
		p.Instances[i].StopProof = nil
		p.Instances[i].Compatibility = nil
		p.Instances[i].Capsule = nil
		p.Instances[i].RollbackGeneration = 0
		p.Instances[i].RollbackFromGeneration = 0
		p.Instances[i].RollbackState = ""
		p.Instances[i].TargetStopProof = nil
		p.Instances[i].Restore = nil
	}
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func openCodePreflightFingerprint(raw []byte) string {
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&object) != nil {
		return ""
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

func (s *RuntimeScheduler) setOpenCodePhase(ctx context.Context, r *models.RuntimeRollout, p *openCodeUpgradePlan, phase, rollback string) error {
	result, err := s.upgrade.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id SET r.phase=?,r.rollback_status=IF(?='',r.rollback_status,?),r.updated_at=UTC_TIMESTAMP(6) WHERE r.id=? AND r.status='running' AND BINARY c.state_json=BINARY ?`, phase, rollback, rollback, r.ID, p.raw)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		// MySQL may report zero for an already identical phase. Adopt only
		// the exact same running checkpoint and requested phase/status.
		var count int
		row, e := s.upgrade.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id WHERE r.id=? AND r.status='running' AND r.phase=? AND (?='' OR r.rollback_status=?) AND BINARY c.state_json=BINARY ?`, r.ID, phase, rollback, rollback, p.raw)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrRuntimeUpgradeBusy
		}
	}
	return nil
}

// Source legacy Agents require C1 only; this contract applies exclusively to
// the target, before quiesce and again before every protected maintenance step.
func validateOpenCodeTargetCapabilities(pod models.RuntimePod) error {
	if stringValue(pod.AgentProtocolVersion) != openCodeNativeProtocol {
		return fmt.Errorf("OpenCode target protocol does not match frozen native contract")
	}
	for _, capability := range openCodeUpgradeCapabilities {
		if !containsString(pod.Capabilities(), capability) {
			return fmt.Errorf("OpenCode target lacks %s", capability)
		}
	}
	return nil
}

func (s *RuntimeScheduler) verifyFrozenOpenCodeTargetPods(ctx context.Context, p *openCodeUpgradePlan) error {
	live, err := s.RuntimeDeploymentPods(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	reported, err := s.podRepo.List(ctx, RuntimeTypeOpenCode)
	if err != nil {
		return err
	}
	verifiedReports := upgradePodsWithLiveIdentity(reported, live)
	for _, expected := range p.Targets {
		found := false
		for _, physical := range live {
			if physical.Namespace != expected.Namespace || physical.PodName != expected.PodName || physical.DeploymentName != expected.DeploymentName || stringValue(physical.KubernetesPodUID) != stringValue(expected.PodUID) || stringValue(physical.PodIP) != stringValue(expected.PodIP) || !runtimePodMatchesImage(physical, p.TargetImage) {
				continue
			}
			for _, actual := range verifiedReports {
				// Native maintenance never inherits the legacy nil-UID fallback.
				if actual.ID != expected.ID || actual.ID <= 0 || stringValue(actual.PodUID) == "" || stringValue(actual.PodUID) != stringValue(expected.PodUID) || actual.Namespace != physical.Namespace || actual.PodName != physical.PodName || actual.DeploymentName != physical.DeploymentName || stringValue(actual.PodIP) != stringValue(expected.PodIP) || !runtimePodMatchesImage(actual, p.TargetImage) {
					continue
				}
				if err := validateOpenCodeTargetCapabilities(actual); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("OpenCode frozen target Pod UID/registration/IP/image changed; original operation remains protected")
		}
	}
	return nil
}

func openCodePlannedUnavailability(instances []openCodeUpgradeInstance) int {
	count := 0
	for _, instance := range instances {
		if instance.Status == "running" {
			count++
		}
	}
	return count
}
