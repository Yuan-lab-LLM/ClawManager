package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	"github.com/upper/db/v4"
)

const targetOpenClawUpgradeVersion = "2026.8.1"
const maxOpenClawUpgradeBatchSize = 8
const openClawUpgradeGatewayRestartTimeout = 5 * time.Minute
const (
	openClawLegacyBaselineDigest           = "sha256:c0905d813cdf22f5ed357d9bd6f61a6798020f1d099022f0aaec4a96c83df125"
	openClawLegacyBaselineRuntimeVersion   = "2026.7.1-2"
	RuntimeUpgradeStrategyLegacyRolling    = "legacy_rolling"
	RuntimeUpgradeStrategyOpenClawDataSafe = "openclaw_8plus_data_safe"
	RuntimeUpgradePhaseEmptyPoolReset      = "empty_pool_reset"
	openClawDataSafeImageStrategy          = "openclaw-sqlite-v1"
	openClawDataSafeProtocol               = "openclaw-upgrade-v3"
)

var openClawUpgradeRequiredCapabilities = []string{
	"openclaw.state.sqlite",
	"openclaw.automation.rpc",
	"openclaw.backup.sqlite",
	"openclaw.database.preflight",
	"openclaw.plugin.capability-consent",
	"openclaw.gateway.stop-confirm",
	"openclaw.workspace.writer-lease",
	"openclaw.session-sqlite-migrate-v1",
	"openclaw.session-sqlite-restore-v1",
	"openclaw.session-sqlite-preserve-v1",
	"openclaw.session-continuity-v1",
	"openclaw.runtime-standby-v1",
	"openclaw.upgrade-capsule-v3",
	"openclaw.upgrade-preflight-v4",
	"redis-team.group-hooks-v1",
}

type RuntimeUpgradePreflightRequest struct {
	TargetImageRef       string `json:"target_image_ref"`
	BatchSize            int    `json:"batch_size"`
	MaxUnavailable       int    `json:"max_unavailable"`
	AutoRollback         bool   `json:"auto_rollback"`
	ActorUserID          *int   `json:"-"`
	UpgradeLabRunID      *int64 `json:"-"`
	CandidateInstanceIDs []int  `json:"-"`
	ExcludeInstanceIDs   []int  `json:"exclude_instance_ids,omitempty"`
	RecoveryMode         string `json:"-"`
	RecoveryParentID     int64  `json:"-"`
	RecoveryKey          string `json:"-"`
	RecoveryRequestHash  string `json:"-"`
}

type runtimeUpgradeScope struct {
	UpgradeLabRunID      *int64
	CandidateInstanceIDs []int
	ExcludeInstanceIDs   []int
	RecoveryMode         string
	RecoveryParentID     int64
}

type RuntimeUpgradePreflightResult struct {
	Rollout                 *models.RuntimeRollout      `json:"rollout,omitempty"`
	Strategy                string                      `json:"strategy"`
	TargetImageRef          string                      `json:"target_image_ref"`
	TargetRuntimeVersion    string                      `json:"target_runtime_version,omitempty"`
	TargetUpgradeProtocol   string                      `json:"target_upgrade_protocol,omitempty"`
	Passed                  bool                        `json:"passed"`
	Blockers                []string                    `json:"blockers"`
	Warnings                []string                    `json:"warnings"`
	InstanceCount           int                         `json:"instance_count"`
	TeamCount               int                         `json:"team_count"`
	OpenClawTeamMemberCount int                         `json:"openclaw_team_member_count"`
	HermesTeamMemberCount   int                         `json:"hermes_team_member_count"`
	RequiredCapabilities    []string                    `json:"required_capabilities"`
	EmptyPoolReset          bool                        `json:"empty_pool_reset,omitempty"`
	Unavailable             []RuntimeUpgradeUnavailable `json:"unavailable,omitempty"`
	ExcludedInstanceIDs     []int                       `json:"excluded_instance_ids,omitempty"`
	Isolated                []RuntimeUpgradeUnavailable `json:"isolated,omitempty"`
}

type RuntimeUpgradeUnavailable struct {
	InstanceID int    `json:"instance_id"`
	TeamID     *int   `json:"team_id,omitempty"`
	Reason     string `json:"reason"`
}

type OpenClawTargetClassification struct {
	Strategy       string
	ImageRef       string
	ImageDigest    string
	RuntimeVersion string
	Protocol       string
	EmptyPoolReset bool
	SourceImages   map[string]string
}

type RuntimeUpgradeDetails struct {
	Rollout   *models.RuntimeRollout         `json:"rollout"`
	Items     []models.RuntimeUpgradeItem    `json:"items"`
	Audits    []models.RuntimeUpgradeAudit   `json:"audits"`
	Execution *runtimeUpgradeExecutionPolicy `json:"execution,omitempty"`
}

func newRuntimeUpgradePreflightResult() *RuntimeUpgradePreflightResult {
	return &RuntimeUpgradePreflightResult{
		Blockers:             make([]string, 0),
		Warnings:             make([]string, 0),
		RequiredCapabilities: make([]string, 0),
	}
}

func newRuntimeUpgradeDetails(rollout *models.RuntimeRollout, items []models.RuntimeUpgradeItem, audits []models.RuntimeUpgradeAudit) *RuntimeUpgradeDetails {
	if items == nil {
		items = make([]models.RuntimeUpgradeItem, 0)
	}
	if audits == nil {
		audits = make([]models.RuntimeUpgradeAudit, 0)
	}
	return &RuntimeUpgradeDetails{Rollout: rollout, Items: items, Audits: audits}
}

type runtimeUpgradeCandidate struct {
	InstanceID        int
	UserID            int
	InstanceStatus    string
	RuntimeGeneration int
	RuntimePodID      *int64
	GatewayID         string
	Generation        int
	BindingState      string
	WorkspacePath     string
	TeamID            *int
	TeamMemberID      *int
	MemberKey         string
	Role              string
	RuntimeType       string
	Availability      string
	MemberStatus      string
	SourceVersion     string
	SourcePodUID      string
	SourceDeployment  string
	SourceImageDigest string
	AgentEndpoint     string
	Capabilities      []string
	Blockers          []string
}

type runtimeWorkspaceInventory struct {
	FileCount         int64  `json:"file_count"`
	DirectoryCount    int64  `json:"directory_count"`
	SymlinkCount      int64  `json:"symlink_count"`
	TotalBytes        int64  `json:"total_bytes"`
	SQLiteFileCount   int    `json:"sqlite_file_count"`
	SQLiteHeaderValid bool   `json:"sqlite_header_valid"`
	ManifestSHA256    string `json:"manifest_sha256"`
}

type RuntimeUpgradeService struct {
	sess             db.Session
	rollouts         repository.RuntimeRolloutRepository
	pods             repository.RuntimePodRepository
	bindings         repository.InstanceRuntimeBindingRepository
	agent            RuntimeAgentClient
	workspaceRoot    string
	redisURL         string
	teamMaintenance  TeamUpgradeMaintenanceController
	deployments      RuntimeDeploymentInventoryProvider
	gatewayRestarter OpenClawUpgradeGatewayRestarter
}

type TeamUpgradeMaintenanceController interface {
	SetRuntimeUpgradeMaintenance(ctx context.Context, teamIDs []int, rolloutID int64, enabled bool) error
}

type RuntimeDeploymentInventoryProvider interface {
	RuntimeDeploymentPods(ctx context.Context, runtimeType string) ([]models.RuntimePod, error)
}

type RuntimeDeploymentScopedInventoryProvider interface {
	RuntimeDeploymentPodsFor(ctx context.Context, runtimeType string, refs []k8s.RuntimeDeploymentRef) ([]models.RuntimePod, error)
}

type OpenClawUpgradeGatewayRestarter interface {
	EnsureUpgradeGateway(ctx context.Context, rollout *models.RuntimeRollout, instanceID int, targetPods []models.RuntimePod) error
}

func (s *RuntimeUpgradeService) SetTeamMaintenanceController(controller TeamUpgradeMaintenanceController) {
	s.teamMaintenance = controller
}

func (s *RuntimeUpgradeService) SetDeploymentInventoryProvider(provider RuntimeDeploymentInventoryProvider) {
	s.deployments = provider
}

func (s *RuntimeUpgradeService) SetUpgradeGatewayRestarter(restarter OpenClawUpgradeGatewayRestarter) {
	s.gatewayRestarter = restarter
}

func NewRuntimeUpgradeService(sess db.Session, rollouts repository.RuntimeRolloutRepository, pods repository.RuntimePodRepository, bindings repository.InstanceRuntimeBindingRepository, agent RuntimeAgentClient, workspaceRoot, redisURL string) *RuntimeUpgradeService {
	return &RuntimeUpgradeService{
		sess:          sess,
		rollouts:      rollouts,
		pods:          pods,
		bindings:      bindings,
		agent:         agent,
		workspaceRoot: filepath.Clean(strings.TrimSpace(workspaceRoot)),
		redisURL:      strings.TrimSpace(redisURL),
	}
}

func (s *RuntimeUpgradeService) Preflight(ctx context.Context, req RuntimeUpgradePreflightRequest) (*RuntimeUpgradePreflightResult, error) {
	if s == nil || s.sess == nil || s.rollouts == nil {
		return nil, fmt.Errorf("runtime upgrade service is not configured")
	}
	target := strings.TrimSpace(req.TargetImageRef)
	// API collection fields are part of the wire contract: an empty collection
	// must be encoded as [] rather than null. Legacy and empty-pool preflights
	// can return before the normal de-duplication path below, so initialise every
	// collection eagerly.
	result := newRuntimeUpgradePreflightResult()
	if target == "" {
		result.Blockers = append(result.Blockers, "target_image_ref is required")
	}
	if target != "" {
		classification, classifyErr := s.ClassifyOpenClawTarget(ctx, target)
		if classifyErr != nil {
			result.Blockers = append(result.Blockers, classifyErr.Error())
		} else {
			result.Strategy = classification.Strategy
			result.TargetImageRef = classification.ImageRef
			result.TargetRuntimeVersion = classification.RuntimeVersion
			result.TargetUpgradeProtocol = classification.Protocol
			result.EmptyPoolReset = classification.EmptyPoolReset
			target = classification.ImageRef
		}
	}
	if len(result.Blockers) > 0 {
		result.Blockers = uniqueSortedStrings(result.Blockers)
		return result, nil
	}
	if result.Strategy == RuntimeUpgradeStrategyLegacyRolling {
		result.Passed = true
		if result.EmptyPoolReset {
			result.Warnings = []string{"The ordinary OpenClaw Lite pool is empty and has no user state to migrate; only the Runtime image will be rolled to the pinned target digest"}
		} else {
			result.Warnings = []string{"The target does not declare the OpenClaw 2026.8.1 data-safe upgrade contract and will use the unchanged legacy Lite rolling update path"}
		}
		return result, nil
	}
	if req.BatchSize > maxOpenClawUpgradeBatchSize {
		result.Blockers = append(result.Blockers, fmt.Sprintf("batch_size must not exceed %d", maxOpenClawUpgradeBatchSize))
	}
	result.RequiredCapabilities = append([]string(nil), openClawUpgradeRequiredCapabilities...)
	if cap := upgradeRecoveryCapability(req.RecoveryMode); cap != "" {
		result.RequiredCapabilities = append(result.RequiredCapabilities, cap)
	}

	if req.RecoveryMode == "" && req.UpgradeLabRunID == nil {
		var err error
		result.Isolated, err = s.isolatedUpgradeInstances(ctx)
		if err != nil {
			return nil, err
		}
		result.Isolated, err = s.expandIsolatedUpgradeTeams(ctx, result.Isolated)
		if err != nil {
			return nil, err
		}
		for _, item := range result.Isolated {
			req.ExcludeInstanceIDs = append(req.ExcludeInstanceIDs, item.InstanceID)
		}
	}
	scope := runtimeUpgradeScope{UpgradeLabRunID: req.UpgradeLabRunID, CandidateInstanceIDs: req.CandidateInstanceIDs, ExcludeInstanceIDs: req.ExcludeInstanceIDs, RecoveryMode: req.RecoveryMode, RecoveryParentID: req.RecoveryParentID}
	candidates, sourceImages, warnings, blockers, err := s.inspectCandidates(ctx, scope)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, warnings...)
	result.Blockers = append(result.Blockers, blockers...)
	digest := imageDigestFromReference(target)
	if digest == "" {
		result.Blockers = append(result.Blockers, "target image must resolve to an immutable sha256 digest")
	}
	result.Warnings = append(result.Warnings, fmt.Sprintf("OpenClaw session migration inputs will be scoped and validated by the standby %s Runtime; project files are not copied or inspected as databases", result.TargetRuntimeVersion))
	result.ExcludedInstanceIDs = normalizedPositiveIDs(req.ExcludeInstanceIDs)
	for _, c := range candidates {
		if len(c.Blockers) > 0 {
			result.Unavailable = append(result.Unavailable, RuntimeUpgradeUnavailable{InstanceID: c.InstanceID, TeamID: c.TeamID, Reason: strings.Join(c.Blockers, "; ")})
		}
	}
	deferredTeams := map[int]bool{}
	for _, u := range result.Unavailable {
		if u.TeamID != nil {
			deferredTeams[*u.TeamID] = true
		}
	}
	for _, c := range candidates {
		if c.TeamID != nil && deferredTeams[*c.TeamID] && len(c.Blockers) == 0 {
			result.Unavailable = append(result.Unavailable, RuntimeUpgradeUnavailable{InstanceID: c.InstanceID, TeamID: c.TeamID, Reason: "所属 Team 有成员暂缓，整个 Team 一并保护"})
		}
	}
	if len(sourceImages) == 0 {
		result.Blockers = append(result.Blockers, "no current OpenClaw deployment image is available for rollback")
	}
	if len(candidates) == 0 {
		result.Blockers = append(result.Blockers, "no OpenClaw Lite instance was discovered; refusing an empty data-safe rollout")
	}
	teamIDs := map[int]struct{}{}
	for _, candidate := range candidates {
		if candidate.TeamID != nil {
			teamIDs[*candidate.TeamID] = struct{}{}
			result.OpenClawTeamMemberCount++
		}
	}
	result.InstanceCount = len(candidates)
	result.TeamCount = len(teamIDs)
	hermesCount, teamBlockers, err := s.inspectTeamConsistency(ctx, teamIDs)
	if err != nil {
		return nil, err
	}
	result.HermesTeamMemberCount = hermesCount
	result.Blockers = append(result.Blockers, teamBlockers...)
	result.Blockers = uniqueSortedStrings(result.Blockers)
	result.Warnings = uniqueSortedStrings(result.Warnings)

	preflightID, err := randomUpgradeID()
	if err != nil {
		return nil, err
	}
	planFingerprint := runtimeUpgradeFingerprint(target, candidates, sourceImages)
	preflightSummary, _ := json.Marshal(map[string]any{
		"strategy":                   result.Strategy,
		"target_runtime_version":     result.TargetRuntimeVersion,
		"target_upgrade_protocol":    result.TargetUpgradeProtocol,
		"passed":                     len(result.Blockers) == 0,
		"blockers":                   result.Blockers,
		"warnings":                   result.Warnings,
		"instance_count":             result.InstanceCount,
		"team_count":                 result.TeamCount,
		"openclaw_team_member_count": result.OpenClawTeamMemberCount,
		"hermes_team_member_count":   result.HermesTeamMemberCount,
		"upgrade_lab_run_id":         req.UpgradeLabRunID,
		"candidate_instance_ids":     normalizedPositiveIDs(req.CandidateInstanceIDs),
		"exclude_instance_ids":       normalizedPositiveIDs(req.ExcludeInstanceIDs),
		"isolated":                   result.Isolated,
		"recovery_mode":              req.RecoveryMode,
		"recovery_parent_id":         req.RecoveryParentID,
		"recovery_request_hash":      req.RecoveryRequestHash,
		"execution":                  defaultRuntimeUpgradeExecutionPolicy(),
	})
	sourceImagesJSON, _ := json.Marshal(sourceImages)
	requiredJSON, _ := json.Marshal(result.RequiredCapabilities)
	fingerprint := planFingerprint
	preflight := preflightID
	preflightRaw := string(preflightSummary)
	sourceRaw := string(sourceImagesJSON)
	requiredRaw := string(requiredJSON)
	rollout := &models.RuntimeRollout{
		RuntimeType:              RuntimeTypeOpenClaw,
		TargetImageRef:           target,
		SourceImagesJSON:         &sourceRaw,
		TargetImageDigest:        upgradeStringPtrOrNil(digest),
		Status:                   map[bool]string{true: "preflight_passed", false: "preflight_blocked"}[len(result.Blockers) == 0],
		Phase:                    "preflight",
		PreflightID:              &preflight,
		PlanFingerprint:          &fingerprint,
		PreflightJSON:            &preflightRaw,
		RequiredCapabilitiesJSON: &requiredRaw,
		BatchSize:                minInt(maxInt(req.BatchSize, 1), maxOpenClawUpgradeBatchSize),
		MaxUnavailable:           0,
		StartedBy:                req.ActorUserID,
		AutoRollback:             req.AutoRollback,
		RecoveryKey:              upgradeStringPtrOrNil(req.RecoveryKey),
	}
	result.Passed = len(result.Blockers) == 0
	if !result.Passed {
		rollout.ErrorMessage = upgradeStringPtrOrNil(strings.Join(result.Blockers, "; "))
	}
	// Publish the idempotency key and the complete plan together. A duplicate
	// HTTP request must never observe a half-created, executable recovery.
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.NewRuntimeRolloutRepository(tx).Create(ctx, rollout); err != nil {
			return err
		}
		if err := s.insertUpgradeItems(ctx, tx, rollout.ID, candidates, result.TargetRuntimeVersion); err != nil {
			return err
		}
		return insertRuntimeUpgradeAudit(ctx, tx, &rollout.ID, req.ActorUserID, "preflight", "preflight", map[bool]string{true: "passed", false: "blocked"}[result.Passed], map[string]any{
			"plan_fingerprint":    planFingerprint,
			"target_image_digest": digest,
			"blocker_count":       len(result.Blockers),
			"warning_count":       len(result.Warnings),
			"instance_count":      len(candidates),
		})
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("persist runtime upgrade preflight audit: %w", err)
	}
	result.Rollout = rollout
	result.TargetImageRef = target
	return result, nil
}

func (s *RuntimeUpgradeService) ConfirmPreflight(ctx context.Context, preflightID, targetImage string, actor *int) (*models.RuntimeRollout, error) {
	preflightID = strings.TrimSpace(preflightID)
	if preflightID == "" {
		return nil, fmt.Errorf("preflight_id is required for OpenClaw rollout")
	}
	var rollout models.RuntimeRollout
	if err := s.sess.Collection("runtime_rollouts").Find(db.Cond{"preflight_id": preflightID}).One(&rollout); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, fmt.Errorf("runtime upgrade preflight not found")
		}
		return nil, err
	}
	if rollout.Status != "preflight_passed" || rollout.Phase != "preflight" {
		return nil, fmt.Errorf("runtime upgrade preflight is not executable: %s/%s", rollout.Status, rollout.Phase)
	}
	if strings.TrimSpace(targetImage) != "" && strings.TrimSpace(targetImage) != strings.TrimSpace(rollout.TargetImageRef) {
		return nil, fmt.Errorf("target image changed after preflight")
	}
	if rollout.PlanFingerprint == nil || strings.TrimSpace(*rollout.PlanFingerprint) == "" {
		return nil, fmt.Errorf("runtime upgrade preflight fingerprint is missing")
	}
	items, err := s.listUpgradeItems(ctx, rollout.ID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("runtime upgrade preflight has no complete instance plan")
	}
	if runtimeUpgradeScopeFromRollout(&rollout).RecoveryMode == "" {
		isolated, err := s.isolatedUpgradeInstances(ctx)
		if err != nil {
			return nil, err
		}
		isolated, err = s.expandIsolatedUpgradeTeams(ctx, isolated)
		if err != nil {
			return nil, err
		}
		for _, held := range isolated {
			for _, item := range items {
				if held.InstanceID == item.InstanceID {
					return nil, fmt.Errorf("instance %d became isolated; refresh the healthy upgrade plan", item.InstanceID)
				}
			}
		}
	}
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.InstanceID)
	}
	release, err := s.lockWorkspaceOperations(ids)
	if err != nil {
		return nil, err
	}
	defer release()
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := s.lockUpgradeScope(ctx, tx, rollout.ID); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET status = 'pending', started_by = ?, updated_at = ? WHERE id = ? AND status = 'preflight_passed'`, actor, time.Now().UTC(), rollout.ID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("runtime upgrade preflight was already consumed")
		}
		return insertRuntimeUpgradeAudit(ctx, tx, &rollout.ID, actor, "confirm", "preflight", "accepted", map[string]any{"preflight_id": preflightID, "plan_fingerprint": *rollout.PlanFingerprint})
	}, nil)
	if err != nil {
		return nil, err
	}
	rollout.Status = "pending"
	rollout.StartedBy = actor
	return &rollout, nil
}

func (s *RuntimeUpgradeService) Details(ctx context.Context, rolloutID int64) (*RuntimeUpgradeDetails, error) {
	rollout, err := s.rollouts.GetByID(ctx, rolloutID)
	if err != nil || rollout == nil {
		return nil, err
	}
	items, err := s.listUpgradeItems(ctx, rolloutID)
	if err != nil {
		return nil, err
	}
	audits := make([]models.RuntimeUpgradeAudit, 0)
	if err := s.sess.Collection("runtime_upgrade_audits").Find(db.Cond{"rollout_id": rolloutID}).OrderBy("created_at", "id").All(&audits); err != nil {
		return nil, err
	}
	details := newRuntimeUpgradeDetails(rollout, items, audits)
	policy := runtimeUpgradePodPolicy(rollout, nil)
	expectedTargets, targetErr := rolloutExpectedTargetDeployments(rollout)
	if pods, err := s.pods.List(ctx, RuntimeTypeOpenClaw); err == nil && targetErr == nil {
		var targets []models.RuntimePod
		for _, pod := range pods {
			_, expected := expectedTargets[strings.TrimSpace(pod.Namespace)+"/"+strings.TrimSpace(pod.DeploymentName)]
			if expected && runtimePodMatchesImage(pod, rollout.TargetImageRef) && (pod.State == "standby" || pod.State == "ready") {
				targets = append(targets, pod)
			}
		}
		if len(targets) > 0 {
			policy = runtimeUpgradePodPolicy(rollout, targets)
		}
	}
	details.Execution = &policy
	return details, nil
}

func (s *RuntimeUpgradeService) Prepare(ctx context.Context, rollout *models.RuntimeRollout) error {
	if rollout == nil || rollout.PreflightID == nil || rollout.PlanFingerprint == nil {
		return fmt.Errorf("OpenClaw rollout requires a successful persisted preflight")
	}
	if rollout.Status != "preflight_passed" && rollout.Status != "pending" && rollout.Status != "running" {
		return fmt.Errorf("OpenClaw rollout preflight is not executable: %s", rollout.Status)
	}
	scope := runtimeUpgradeScopeFromRollout(rollout)
	candidates, sourceImages, _, blockers, err := s.inspectCandidates(ctx, scope)
	if err != nil {
		return err
	}
	teamIDs := map[int]struct{}{}
	for _, candidate := range candidates {
		if candidate.TeamID != nil {
			teamIDs[*candidate.TeamID] = struct{}{}
		}
	}
	_, teamBlockers, err := s.inspectTeamConsistency(ctx, teamIDs)
	if err != nil {
		return err
	}
	blockers = append(blockers, teamBlockers...)
	if len(blockers) > 0 {
		return fmt.Errorf("runtime upgrade state changed after preflight: %s", strings.Join(uniqueSortedStrings(blockers), "; "))
	}
	if got := runtimeUpgradeFingerprint(rollout.TargetImageRef, candidates, sourceImages); got != *rollout.PlanFingerprint {
		return fmt.Errorf("runtime upgrade inventory changed after preflight; run preflight again")
	}
	if err := s.updateRolloutPhase(ctx, rollout.ID, "maintenance"); err != nil {
		return err
	}
	if err := s.setTeamMaintenance(ctx, teamIDs, rollout.ID, true); err != nil {
		return err
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = s.setTeamMaintenance(context.Background(), teamIDs, rollout.ID, false)
		}
	}()
	// Close the preflight-to-maintenance race: once dispatch is fenced, re-read
	// every durable Team ledger before stopping a single gateway.
	if _, fencedBlockers, err := s.inspectTeamConsistency(ctx, teamIDs); err != nil {
		return err
	} else if len(fencedBlockers) > 0 {
		return fmt.Errorf("Team state changed while entering runtime maintenance: %s", strings.Join(uniqueSortedStrings(fencedBlockers), "; "))
	}

	// Persist only the immutable binding/path capsule. User projects, plugin
	// caches and the rest of the workspace remain in place and are never copied.
	now := time.Now().UTC()
	for _, candidate := range candidates {
		capsule, _ := json.Marshal(map[string]any{
			"schema_version": 2,
			"workspace_path": filepath.Clean(candidate.WorkspacePath),
			"runtime_pod_id": candidate.RuntimePodID,
			"gateway_id":     candidate.GatewayID,
			"generation":     candidate.Generation,
			"source_version": candidate.SourceVersion,
		})
		if _, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'prepared', snapshot_ref = NULL, workspace_manifest_sha256 = NULL, preflight_json = ?, started_at = COALESCE(started_at, ?), updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state IN ('pending','prepared')`, string(capsule), now, now, rollout.ID, candidate.InstanceID); err != nil {
			return err
		}
	}
	prepared = true
	if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "prepare", "capsule", "passed", map[string]any{"instance_count": len(candidates), "team_count": len(teamIDs), "workspace_snapshot": false}); err != nil {
		return err
	}
	return s.updateRolloutPhase(ctx, rollout.ID, "image_rollout")
}

func (s *RuntimeUpgradeService) ValidateTargetRuntime(ctx context.Context, rollout *models.RuntimeRollout, pods []models.RuntimePod) (bool, error) {
	if rollout == nil {
		return false, fmt.Errorf("rollout is required")
	}
	expectedVersion, expectedProtocol := rolloutTargetMetadata(rollout)
	if !openClawVersionAtLeast(expectedVersion, targetOpenClawUpgradeVersion) || expectedProtocol != openClawDataSafeProtocol {
		return false, fmt.Errorf("rollout target metadata is missing or unsupported")
	}
	var targetPods []models.RuntimePod
	targetCapacity := 0
	expectedTargets, err := rolloutExpectedTargetDeployments(rollout)
	if err != nil {
		return false, err
	}
	for _, pod := range pods {
		if _, expected := expectedTargets[strings.TrimSpace(pod.Namespace)+"/"+strings.TrimSpace(pod.DeploymentName)]; !expected {
			continue
		}
		if pod.RuntimeType != RuntimeTypeOpenClaw || pod.Draining || !runtimePodMatchesImage(pod, rollout.TargetImageRef) || (pod.State != "standby" && pod.State != "ready") {
			continue
		}
		targetPods = append(targetPods, pod)
		targetCapacity += maxInt(pod.Capacity, 0)
		if value := stringValue(pod.OpenClawVersion); value != expectedVersion {
			return false, fmt.Errorf("target pod %s reports OpenClaw %q, want %s", pod.PodName, value, expectedVersion)
		}
		if value := stringValue(pod.SessionStore); value != "sqlite" {
			return false, fmt.Errorf("target pod %s reports session store %q, want sqlite", pod.PodName, value)
		}
		if value := stringValue(pod.AgentProtocolVersion); value != expectedProtocol {
			return false, fmt.Errorf("target pod %s reports agent protocol %q, want %s", pod.PodName, value, expectedProtocol)
		}
		if rollout.TargetImageDigest != nil && stringValue(pod.ImageDigest) != strings.TrimSpace(*rollout.TargetImageDigest) {
			return false, fmt.Errorf("target pod %s reports image digest %q, want %s", pod.PodName, stringValue(pod.ImageDigest), strings.TrimSpace(*rollout.TargetImageDigest))
		}
		for _, capability := range openClawUpgradeRequiredCapabilities {
			if !containsString(pod.Capabilities(), capability) {
				return false, fmt.Errorf("target pod %s lacks required capability %s", pod.PodName, capability)
			}
		}
		if capability := upgradeRecoveryCapability(runtimeUpgradeScopeFromRollout(rollout).RecoveryMode); capability != "" && !containsString(pod.Capabilities(), capability) {
			return false, fmt.Errorf("target pod %s lacks data-preserving rebuild support; choose a compatible target image", pod.PodName)
		}
	}
	if len(targetPods) == 0 {
		if rollout.Phase == "gateway_restart" {
			return false, s.recordUnavailableUpgradeTargets(ctx, rollout)
		}
		return false, nil
	}
	items, err := s.listUpgradeItems(ctx, rollout.ID)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, fmt.Errorf("OpenClaw data-safe rollout has no persisted upgrade items")
	}
	if runtimeUpgradePolicy(rollout).Version >= 2 {
		if rollout.Phase == "compatibility_check" {
			open, e := s.upgradeImageCircuitOpen(ctx, rollout)
			if e != nil {
				return false, e
			}
			if open {
				return false, errUpgradeImageSuspect
			}
		}
		if err := s.settleIsolatedUpgradeGroups(ctx, rollout); err != nil {
			return false, err
		}
		items, err = s.listUpgradeItems(ctx, rollout.ID)
		if err != nil {
			return false, err
		}
		items = activeUpgradeItems(items)
		if len(items) == 0 {
			return false, errAllUpgradeGroupsIsolated
		}
	}
	if targetCapacity < len(items) {
		return false, nil
	}
	if rollout.Phase == "image_rollout" {
		if err := s.updateRolloutPhase(ctx, rollout.ID, "compatibility_check"); err != nil {
			return false, err
		}
		rollout.Phase = "compatibility_check"
	}
	if rollout.Phase == "compatibility_check" {
		upgradeAgent, ok := s.agent.(RuntimeUpgradeAgentClient)
		if !ok {
			return false, fmt.Errorf("target Runtime Agent does not implement the upgrade compatibility contract")
		}
		policy := runtimeUpgradePodPolicy(rollout, targetPods)
		batch := nextRuntimeUpgradeBatch(items, minInt(maxInt(rollout.BatchSize, 1), 4), "prepared")
		if policy.Version >= 1 {
			batch = boundedUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "prepared")
		}
		if policy.Version >= 2 && len(batch) > 1 {
			passed := false
			for _, i := range items {
				passed = passed || i.State == "compatibility_checked"
			}
			if !passed {
				batch = batch[:1]
			}
		}
		if err := s.runUpgradeItems(ctx, rollout.ID, "compatibility_check", batch, policy.Check, func(ctx context.Context, index int) error {
			item := batch[index]
			candidate, err := s.candidateForUpgradeItem(ctx, item)
			if err != nil {
				return err
			}
			endpoint := stringValue(targetPods[index%len(targetPods)].AgentEndpoint)
			request := RuntimeAgentWorkspaceRequest{RolloutID: strconv.FormatInt(rollout.ID, 10), UserID: candidate.UserID, InstanceID: item.InstanceID, Generation: candidate.Generation, UID: RuntimeLinuxID(item.InstanceID), GID: RuntimeLinuxID(item.InstanceID), RecoveryMode: runtimeUpgradeScopeFromRollout(rollout).RecoveryMode}
			compatibility, err := upgradeAgent.PreflightUpgradeCompatibility(ctx, endpoint, request)
			if err != nil {
				return fmt.Errorf("compatibility preflight instance %d: %w", item.InstanceID, err)
			}
			if compatibility != nil && compatibility.Status == "deferred" {
				return nil
			}
			if compatibility == nil || compatibility.Status != "compatible" || compatibility.ConfigOriginalSHA256 == "" || compatibility.ConfigTargetSHA256 == "" || !compatibility.ConfigValidated || !compatibility.DoctorValidated || !compatibility.SessionDryRunValid {
				return fmt.Errorf("compatibility preflight instance %d returned incomplete evidence", item.InstanceID)
			}
			if containsString(targetPods[index%len(targetPods)].Capabilities(), "openclaw.managed-plugin-convergence-v1") && compatibility.ManagedPluginPlanSHA256 == "" {
				return fmt.Errorf("target Runtime advertised plugin convergence but omitted its validated plan")
			}
			var capsule map[string]any
			if item.PreflightJSON != nil {
				_ = json.Unmarshal([]byte(*item.PreflightJSON), &capsule)
			}
			if capsule == nil {
				capsule = map[string]any{}
			}
			capsule["compatibility"] = compatibility
			raw, _ := json.Marshal(capsule)
			result, err := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'compatibility_checked', preflight_json = ?, updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'prepared'`, string(raw), time.Now().UTC(), rollout.ID, item.InstanceID)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return fmt.Errorf("instance %d compatibility state changed concurrently", item.InstanceID)
			}
			return nil
		}); err != nil {
			return false, err
		}
		if len(batch) > 0 {
			return false, nil
		}
		for _, item := range items {
			if item.State != "compatibility_checked" {
				return false, fmt.Errorf("instance %d has unexpected compatibility state %q", item.InstanceID, item.State)
			}
		}
		requiredBytes, availableBytes, err := validateOpenClawUpgradeAggregateCapacity(items)
		if err != nil {
			return false, err
		}
		if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "aggregate_capacity", "compatibility_check", "passed", map[string]any{"required_bytes": requiredBytes, "available_bytes": availableBytes, "instance_count": len(items)}); err != nil {
			return false, err
		}
		if err := s.updateRolloutPhase(ctx, rollout.ID, "session_migration"); err != nil {
			return false, err
		}
		rollout.Phase = "session_migration"
		items, err = s.listUpgradeItems(ctx, rollout.ID)
		if err != nil {
			return false, err
		}
		items = activeUpgradeItems(items)
	}
	if rollout.Phase == "session_migration" {
		upgradeAgent, ok := s.agent.(RuntimeUpgradeAgentClient)
		if !ok {
			return false, fmt.Errorf("target Runtime Agent does not implement the upgrade capsule contract")
		}
		for _, pod := range targetPods {
			if pod.State != "standby" {
				return false, fmt.Errorf("target pod %s became ready before all migrations completed", pod.PodName)
			}
			if stringValue(pod.AgentEndpoint) == "" {
				return false, nil
			}
		}
		batch := nextRuntimeUpgradeMigrationBatch(items, maxInt(rollout.BatchSize, 1))
		policy := runtimeUpgradePodPolicy(rollout, targetPods)
		if policy.Version >= 1 {
			batch = boundedUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "compatibility_checked", "quiescing", "quiesced", "migration_started")
		}
		var workers, leaders []models.RuntimeUpgradeItem
		for _, item := range batch {
			if item.IsTeamLeader {
				leaders = append(leaders, item)
			} else {
				workers = append(workers, item)
			}
		}
		for _, wave := range [][]models.RuntimeUpgradeItem{workers, leaders} {
			if err := s.runUpgradeItems(ctx, rollout.ID, "session_migration", wave, policy.Migrate, func(ctx context.Context, index int) error {
				endpoint := stringValue(targetPods[index%len(targetPods)].AgentEndpoint)
				return s.migrateUpgradeItem(ctx, rollout, wave[index], endpoint, upgradeAgent)
			}); err != nil {
				return false, err
			}
		}
		if len(batch) > 0 {
			return false, nil
		}
		for _, item := range items {
			if item.State != "migrated" {
				return false, fmt.Errorf("instance %d has unexpected pre-activation state %q", item.InstanceID, item.State)
			}
		}
		if err := s.drainSourceRuntimePods(ctx, rollout); err != nil {
			return false, err
		}
		rolloutID := strconv.FormatInt(rollout.ID, 10)
		for _, pod := range targetPods {
			if err := upgradeAgent.ActivateUpgrade(ctx, stringValue(pod.AgentEndpoint), rolloutID); err != nil {
				return false, fmt.Errorf("activate target pod %s: %w", pod.PodName, err)
			}
		}
		if err := s.updateRolloutPhase(ctx, rollout.ID, "gateway_restart"); err != nil {
			return false, err
		}
		rollout.Phase = "gateway_restart"
		// Activation changes the remote state, not this round's Pod snapshot.
		// Observe a fresh heartbeat/inventory on the next reconciliation.
		return false, nil
	}
	if rollout.Phase != "postflight" {
		allTargetsReady := true
		for _, pod := range targetPods {
			if pod.State != "ready" {
				allTargetsReady = false
			}
		}
		if !allTargetsReady {
			return false, s.waitForUpgradeTargetActivation(ctx, rollout, items)
		}
		if err := s.clearUnavailableUpgradeTargets(ctx, rollout.ID); err != nil {
			return false, err
		}
		items, err = s.listUpgradeItems(ctx, rollout.ID)
		if err != nil {
			return false, err
		}
		items = activeUpgradeItems(items)
		reopened, err := s.reopenMissingVerifiedUpgradeGateway(ctx, rollout, items, pods)
		if err != nil {
			return false, err
		}
		if reopened {
			// A target Deployment can be replaced during a forward repair after an
			// instance was already verified.  Re-run only the gateway placement
			// check; the session migration and runtime generation remain intact.
			return false, nil
		}
		activeBatch := nextRuntimeUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "restart_ready")
		if runtimeUpgradePolicy(rollout).Version >= 1 {
			activeBatch = boundedUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "restart_ready")
		}
		if len(activeBatch) == 0 {
			nextBatch := nextRuntimeUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "migrated")
			if runtimeUpgradePolicy(rollout).Version >= 1 {
				nextBatch = boundedUpgradeBatch(items, maxInt(rollout.BatchSize, 1), "migrated")
			}
			for _, item := range nextBatch {
				if item.RecoveryStatus != nil && *item.RecoveryStatus == "needs_attention" {
					continue
				}
				if err := s.markUpgradeItemRestartReady(ctx, rollout.ID, item.InstanceID); err != nil {
					return false, err
				}
			}
			if len(nextBatch) > 0 {
				return false, nil
			}
			for _, item := range items {
				if item.State != "gateway_verified" && item.State != "verified" {
					return false, fmt.Errorf("instance %d has unexpected terminal restart state %q", item.InstanceID, item.State)
				}
			}
			rollout.Phase = "postflight"
		}
		policy := runtimeUpgradePodPolicy(rollout, targetPods)
		if err := s.runUpgradeItems(ctx, rollout.ID, "gateway_restart", activeBatch, policy.Start, func(ctx context.Context, index int) error {
			item := activeBatch[index]
			if item.State != "restart_ready" {
				return fmt.Errorf("instance %d has unexpected restart state %q", item.InstanceID, item.State)
			}
			if s.gatewayRestarter == nil {
				return fmt.Errorf("OpenClaw upgrade gateway restarter is unavailable")
			}
			if err := s.initializeUpgradeRecovery(ctx, &item); err != nil {
				return err
			}
			// Observe success before enforcing deadlines. A response/heartbeat may
			// have arrived while the controller was offline; never restart it.
			binding, err := s.bindings.GetRunningByInstanceID(ctx, item.InstanceID)
			if err != nil {
				return err
			}
			if binding == nil {
				if err := s.observeUpgradeGateway(ctx, item, targetPods); err != nil {
					if runtimeUpgradeReconcileInterrupted(ctx, err) {
						return err
					}
					code, hold := upgradeRecoveryError(err)
					return s.recordUpgradeRecoveryFailure(ctx, rollout.ID, item, code, err.Error(), hold)
				}
				binding, err = s.bindings.GetRunningByInstanceID(ctx, item.InstanceID)
				if err != nil {
					return err
				}
			}
			if binding == nil {
				if item.RecoveryStatus != nil && *item.RecoveryStatus == "needs_attention" {
					return nil
				}
				if upgradeGatewayRestartExpired(item, time.Time{}, time.Now().UTC()) {
					return s.recordUpgradeRecoveryFailure(ctx, rollout.ID, item, "UPGRADE_GATEWAY_START_TIMEOUT", "Gateway startup deadline exceeded; reservation and writer fence retained", true)
				}
				if item.RecoveryStartedAt != nil && time.Since(*item.RecoveryStartedAt) >= 15*time.Minute {
					return s.recordUpgradeRecoveryFailure(ctx, rollout.ID, item, "UPGRADE_RECOVERY_DEADLINE", "Recovery exceeded its bounded window; inspection required", true)
				}
				if item.RestartNextRetryAt != nil && time.Now().UTC().Before(*item.RestartNextRetryAt) {
					return nil
				}
				if err := s.gatewayRestarter.EnsureUpgradeGateway(ctx, rollout, item.InstanceID, targetPods); err != nil {
					if runtimeUpgradeReconcileInterrupted(ctx, err) {
						return err
					}
					code, hold := upgradeRecoveryError(err)
					return s.recordUpgradeRecoveryFailure(ctx, rollout.ID, item, code, err.Error(), hold)
				}
				binding, err = s.bindings.GetRunningByInstanceID(ctx, item.InstanceID)
				if err != nil {
					return err
				}
				if binding == nil {
					return nil
				}
			}
			var expectedGeneration int
			row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT runtime_generation FROM instances WHERE id = ?`, item.InstanceID)
			if err != nil {
				return err
			}
			if err := row.Scan(&expectedGeneration); err != nil {
				return err
			}
			if binding.Generation != expectedGeneration {
				return fmt.Errorf("instance %d binding generation %d does not match %d", item.InstanceID, binding.Generation, expectedGeneration)
			}
			targetPod := false
			for _, pod := range pods {
				if pod.ID == binding.RuntimePodID && runtimePodMatchesImage(pod, rollout.TargetImageRef) && pod.State == "ready" && !pod.Draining {
					targetPod = true
					break
				}
			}
			if !targetPod {
				return fmt.Errorf("instance %d running binding is not on a ready target pod", item.InstanceID)
			}
			if err := s.markUpgradeItemGatewayVerified(ctx, rollout.ID, item.InstanceID); err != nil {
				return err
			}
			if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "instance_gateway_verified", "gateway_restart", "passed", map[string]any{"instance_id": item.InstanceID, "is_team_leader": item.IsTeamLeader}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return false, err
		}
		if len(activeBatch) > 0 {
			return false, nil
		}
	}
	if err := s.updateRolloutPhase(ctx, rollout.ID, "postflight"); err != nil {
		return false, err
	}
	teamIDs := map[int]struct{}{}
	for _, item := range items {
		if item.TeamID != nil {
			teamIDs[*item.TeamID] = struct{}{}
		}
	}
	if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "postflight", "postflight", "passed", map[string]any{"instance_count": len(items)}); err != nil {
		return false, err
	}
	if runtimeUpgradeScopeFromRollout(rollout).RecoveryMode != "" {
		for teamID := range teamIDs {
			var unresolved int
			row, e := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_members tm JOIN instances i ON i.id=tm.instance_id WHERE tm.team_id=? AND tm.status NOT IN ('deleted','deleting') AND (i.status NOT IN ('running','stopped') OR EXISTS (SELECT 1 FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=i.id AND r.id<>? AND ui.recovery_status='needs_attention' AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=i.id AND nr.id>r.id AND (nr.status='finished' OR (nr.id=? AND n.state IN ('verified','gateway_verified'))))))`, teamID, rollout.ID, rollout.ID)
			if e != nil {
				return false, e
			}
			if e = row.Scan(&unresolved); e != nil {
				return false, e
			}
			if unresolved > 0 {
				delete(teamIDs, teamID)
				if e = s.audit(ctx, &rollout.ID, rollout.StartedBy, "team_maintenance_retained", "postflight", "waiting", map[string]any{"team_id": teamID, "unresolved_members": unresolved}); e != nil {
					return false, e
				}
			}
		}
	}
	if err := s.setTeamMaintenance(ctx, teamIDs, rollout.ID, false); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'verified', finished_at = ?, updated_at = ? WHERE rollout_id = ? AND isolated_at IS NULL AND state IN('gateway_verified','verified')`, now, now, rollout.ID)
	if err != nil {
		return false, err
	}
	return s.finishConvergentUpgrade(ctx, rollout)
}

func gatewayRestartNotBefore(rollout models.RuntimeRollout, targetPods []models.RuntimePod) time.Time {
	startedAt := rollout.UpdatedAt.UTC()
	for _, pod := range targetPods {
		createdAt := pod.CreatedAt.UTC()
		if createdAt.After(startedAt) {
			startedAt = createdAt
		}
	}
	return startedAt
}

func upgradeGatewayRestartExpired(item models.RuntimeUpgradeItem, notBefore, now time.Time) bool {
	// Neither generic updates nor replacement pod creation may reset a deadline.
	return item.State == "restart_ready" && item.RestartDeadlineAt != nil && !now.Before(*item.RestartDeadlineAt)
}

func (s *RuntimeUpgradeService) reopenMissingVerifiedUpgradeGateway(ctx context.Context, rollout *models.RuntimeRollout, items []models.RuntimeUpgradeItem, pods []models.RuntimePod) (bool, error) {
	for _, item := range items {
		if item.State != "gateway_verified" {
			continue
		}
		binding, err := s.bindings.GetRunningByInstanceID(ctx, item.InstanceID)
		if err != nil {
			return false, err
		}
		reason := "verified target gateway binding is absent"
		if binding != nil {
			var expectedGeneration int
			row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT runtime_generation FROM instances WHERE id = ?`, item.InstanceID)
			if err != nil {
				return false, err
			}
			if err := row.Scan(&expectedGeneration); err != nil {
				return false, err
			}
			if verifiedUpgradeBindingOnTarget(binding, expectedGeneration, pods, rollout.TargetImageRef) {
				continue
			}
			reason = "verified gateway binding no longer belongs to the ready target"
		}

		now := time.Now().UTC()
		result, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'restart_ready', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'gateway_verified'`, now, rollout.ID, item.InstanceID)
		if err != nil {
			return false, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		if affected != 1 {
			continue
		}
		if _, err := s.sess.SQL().ExecContext(ctx, `UPDATE instances SET status = 'creating', runtime_error_message = NULL, updated_at = ? WHERE id = ? AND LOWER(TRIM(status)) <> 'deleting'`, now, item.InstanceID); err != nil {
			return false, err
		}
		if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "instance_gateway_revalidation_required", "gateway_restart", "pending", map[string]any{"instance_id": item.InstanceID, "reason": reason}); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func verifiedUpgradeBindingOnTarget(binding *models.InstanceRuntimeBinding, expectedGeneration int, pods []models.RuntimePod, targetImage string) bool {
	if binding == nil || binding.Generation != expectedGeneration {
		return false
	}
	for _, pod := range pods {
		if pod.ID == binding.RuntimePodID && runtimePodMatchesImage(pod, targetImage) && pod.State == "ready" && !pod.Draining {
			return true
		}
	}
	return false
}

// validateOpenClawUpgradeAggregateCapacity prevents hundreds of individually
// valid migrations from exhausting a shared NFS volume together. OpenClaw's
// upstream migration archives are renames, not full workspace copies; this
// budget covers the new SQLite database/WAL, the tiny config capsule, and a
// fixed operational reserve without reserving space for user project data.
func validateOpenClawUpgradeAggregateCapacity(items []models.RuntimeUpgradeItem) (uint64, uint64, error) {
	const reserve = uint64(1 << 30)
	const maxUint64 = ^uint64(0)
	required := reserve
	available := maxUint64
	for _, item := range items {
		var evidence struct {
			Compatibility RuntimeAgentUpgradeCompatibility `json:"compatibility"`
		}
		if item.PreflightJSON == nil || json.Unmarshal([]byte(*item.PreflightJSON), &evidence) != nil {
			return 0, 0, fmt.Errorf("instance %d has no valid compatibility capacity evidence", item.InstanceID)
		}
		compatibility := evidence.Compatibility
		if compatibility.Status != "compatible" || !compatibility.ConfigValidated || !compatibility.DoctorValidated || !compatibility.SessionDryRunValid || compatibility.SessionBytes < 0 || compatibility.StateBytes < 0 || compatibility.ConfigBytes < 0 || compatibility.AvailableBytes == 0 {
			return 0, 0, fmt.Errorf("instance %d has incomplete compatibility capacity evidence", item.InstanceID)
		}
		sessionBytes := uint64(compatibility.SessionBytes)
		configBytes := uint64(compatibility.ConfigBytes)
		stateBytes := uint64(compatibility.StateBytes)
		if sessionBytes > (maxUint64-required)/3 {
			return 0, 0, errors.New("aggregate session migration capacity overflow")
		}
		required += sessionBytes * 3
		if configBytes > (maxUint64-required)/2 {
			return 0, 0, errors.New("aggregate config migration capacity overflow")
		}
		required += configBytes * 2
		if stateBytes > (maxUint64-required)/2 {
			return 0, 0, errors.New("aggregate state capsule capacity overflow")
		}
		// One original control-state capsule plus the migrated target database.
		required += stateBytes * 2
		if compatibility.AvailableBytes < available {
			available = compatibility.AvailableBytes
		}
	}
	if len(items) == 0 || available == maxUint64 {
		return 0, 0, errors.New("aggregate migration capacity evidence is unavailable")
	}
	if required > available {
		return required, available, fmt.Errorf("insufficient aggregate session migration capacity: need %d bytes, available %d", required, available)
	}
	return required, available, nil
}

func (s *RuntimeUpgradeService) drainSourceRuntimePods(ctx context.Context, rollout *models.RuntimeRollout) error {
	if rollout == nil || rollout.SourceImagesJSON == nil {
		return errors.New("source runtime inventory is unavailable")
	}
	// Excluded instances keep serving on this shared source Agent.
	if runtimeUpgradeScopeFromRollout(rollout).scoped() || s.hasIsolatedUpgradeItems(ctx, rollout.ID) {
		return nil
	}
	var sourceImages map[string]string
	if err := json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sourceImages); err != nil || len(sourceImages) == 0 {
		return errors.New("source runtime inventory is invalid")
	}
	if s.deployments == nil {
		return errors.New("live source runtime inventory is unavailable")
	}
	refs := make([]k8s.RuntimeDeploymentRef, 0, len(sourceImages))
	for key := range sourceImages {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: parts[0], Name: parts[1]})
		}
	}
	livePods, err := runtimeDeploymentPodsFor(ctx, s.deployments, RuntimeTypeOpenClaw, refs)
	if err != nil {
		return err
	}
	databasePods, err := s.pods.List(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return err
	}
	databaseByIdentity := make(map[string]models.RuntimePod, len(databasePods))
	for _, pod := range databasePods {
		key := runtimePodIdentity(pod)
		if current, exists := databaseByIdentity[key]; !exists || pod.ID > current.ID {
			databaseByIdentity[key] = pod
		}
	}
	seenDeployments := map[string]bool{}
	for _, pod := range livePods {
		key := pod.Namespace + "/" + pod.DeploymentName
		sourceImage, ok := sourceImages[key]
		if !ok || !runtimePodMatchesImage(pod, sourceImage) {
			continue
		}
		seenDeployments[key] = true
		databasePod, registered := databaseByIdentity[runtimePodIdentity(pod)]
		endpoint := runtimeAgentEndpoint(pod)
		if registered && stringValue(databasePod.AgentEndpoint) != "" {
			endpoint = stringValue(databasePod.AgentEndpoint)
		}
		if endpoint == "" {
			return fmt.Errorf("source runtime pod %s has no agent endpoint", pod.PodName)
		}
		if err := s.agent.Drain(ctx, endpoint); err != nil {
			return fmt.Errorf("drain source runtime pod %s: %w", pod.PodName, err)
		}
		if registered {
			if err := s.pods.MarkState(ctx, databasePod.ID, "draining", true); err != nil {
				return err
			}
		}
	}
	for key := range sourceImages {
		if !seenDeployments[key] {
			return fmt.Errorf("source runtime deployment %s has no live pod at the rollback image", key)
		}
	}
	return nil
}

func runtimePodIdentity(pod models.RuntimePod) string {
	return strings.TrimSpace(pod.Namespace) + "/" + strings.TrimSpace(pod.DeploymentName) + "/" + strings.TrimSpace(pod.PodName)
}

func runtimeAgentEndpoint(pod models.RuntimePod) string {
	if endpoint := strings.TrimSpace(stringValue(pod.AgentEndpoint)); endpoint != "" {
		return endpoint
	}
	if pod.PodIP == nil || strings.TrimSpace(*pod.PodIP) == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(strings.TrimSpace(*pod.PodIP), "19090")
}

func (s *RuntimeUpgradeService) migrateUpgradeItem(ctx context.Context, rollout *models.RuntimeRollout, item models.RuntimeUpgradeItem, targetEndpoint string, upgradeAgent RuntimeUpgradeAgentClient) error {
	candidate, err := s.candidateForUpgradeItem(ctx, item)
	if err != nil {
		return err
	}
	state := strings.ToLower(strings.TrimSpace(item.State))
	if state == "compatibility_checked" {
		transitioned, transitionErr := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'quiescing', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'compatibility_checked'`, time.Now().UTC(), rollout.ID, item.InstanceID)
		if transitionErr != nil {
			return transitionErr
		}
		if affected, _ := transitioned.RowsAffected(); affected != 1 {
			return fmt.Errorf("instance %d could not enter quiescing", item.InstanceID)
		}
		state = "quiescing"
	}
	if state == "quiescing" {
		stopped, stopErr := s.stopCandidateGateway(ctx, candidate)
		if stopErr != nil {
			retryState := "compatibility_checked"
			if stopped {
				// The process is confirmed absent even if releasing its stale binding
				// failed. Resume from the stopped state instead of starting a second writer.
				retryState = "quiesced"
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, cleanupErr := s.execClaimedUpgradeState(cleanupCtx, `UPDATE runtime_upgrade_items SET state = ?, error_message = ?, updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'quiescing'`, retryState, stopErr.Error(), time.Now().UTC(), rollout.ID, item.InstanceID)
			cleanupCancel()
			if cleanupErr != nil {
				return errors.Join(stopErr, cleanupErr)
			}
			return fmt.Errorf("stop instance %d before session migration: %w", item.InstanceID, stopErr)
		}
		if _, err := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'quiesced', error_message = NULL, updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'quiescing'`, time.Now().UTC(), rollout.ID, item.InstanceID); err != nil {
			return err
		}
		state = "quiesced"
	}
	if state != "quiesced" && state != "migration_started" {
		return fmt.Errorf("instance %d has unsupported resumable migration state %q", item.InstanceID, item.State)
	}
	var userID, generation int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT user_id, runtime_generation FROM instances WHERE id = ?`, item.InstanceID)
	if err != nil {
		return err
	}
	if err := row.Scan(&userID, &generation); err != nil {
		return err
	}
	leaseToken := runtimeUpgradeLeaseToken(rollout.ID, item.InstanceID)
	request := RuntimeAgentWorkspaceRequest{RolloutID: strconv.FormatInt(rollout.ID, 10), UserID: userID, InstanceID: item.InstanceID, Generation: generation, UID: RuntimeLinuxID(item.InstanceID), GID: RuntimeLinuxID(item.InstanceID), LeaseToken: leaseToken, RecoveryMode: runtimeUpgradeScopeFromRollout(rollout).RecoveryMode}
	request.ManagedPluginPlanSHA256 = managedPluginPlanFromItem(item)
	s.attachRecoveryMigrationReceipt(ctx, rollout, item, candidate, &request)
	lease := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: request, Token: leaseToken, TTLSeconds: 3600}
	if err := upgradeAgent.AcquireWriterLease(ctx, targetEndpoint, lease); err != nil {
		return fmt.Errorf("acquire migration writer lease for instance %d: %w", item.InstanceID, err)
	}
	leaseHeld := true
	defer func() {
		if leaseHeld {
			_ = upgradeAgent.ReleaseWriterLease(context.Background(), targetEndpoint, lease)
		}
	}()
	inventory, preflightErr := upgradeAgent.PreflightWorkspace(ctx, targetEndpoint, request)
	if preflightErr != nil {
		return fmt.Errorf("preflight instance %d session migration: %w", item.InstanceID, preflightErr)
	}
	if state == "quiesced" {
		result, transitionErr := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'migration_started', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'quiesced'`, time.Now().UTC(), rollout.ID, item.InstanceID)
		if transitionErr != nil {
			return transitionErr
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("instance %d could not enter migration_started", item.InstanceID)
		}
	}
	migration, migrateErr := upgradeAgent.MigrateSessionSQLite(ctx, targetEndpoint, request)
	if migrateErr != nil && shouldReconcileUpgradeReceipt(ctx, migrateErr) {
		statusCtx, cancel := context.WithTimeout(ctx, runtimeAgentControlTimeout)
		persisted, statusErr := upgradeAgent.SessionSQLiteMigrationStatus(statusCtx, targetEndpoint, request)
		cancel()
		if statusErr == nil && validSessionSQLiteMigration(persisted) {
			migration = persisted
			migrateErr = nil
		}
	}
	releaseErr := upgradeAgent.ReleaseWriterLease(ctx, targetEndpoint, lease)
	leaseHeld = releaseErr != nil
	if migrateErr != nil {
		return fmt.Errorf("migrate instance %d session store: %w", item.InstanceID, migrateErr)
	}
	if releaseErr != nil {
		return fmt.Errorf("release migration writer lease for instance %d: %w", item.InstanceID, releaseErr)
	}
	if !validSessionSQLiteMigration(migration) {
		return fmt.Errorf("instance %d session migration did not return validated evidence", item.InstanceID)
	}
	if request.RecoveryMode == "rebuild" && (!migration.DataPreserved || migration.PreservationSHA256 == "") {
		return errors.New("rebuild did not return verified original-data preservation evidence")
	}
	postflight, _ := json.Marshal(map[string]any{"migration": migration, "session_inventory": inventory, "target_agent_endpoint": targetEndpoint})
	result, err := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'migrated', postflight_json = ?, snapshot_ref = ?, error_message = NULL, updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'migration_started'`, string(postflight), "official-session-archive", time.Now().UTC(), rollout.ID, item.InstanceID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("instance %d migration state changed concurrently", item.InstanceID)
	}
	return nil
}

func validSessionSQLiteMigration(migration *RuntimeAgentSessionSQLiteMigration) bool {
	return migration != nil && migration.Status == "validated" && strings.TrimSpace(migration.OutputSHA256) != "" && migration.SessionCount >= 0 && strings.TrimSpace(migration.SessionCatalogSHA256) != ""
}

func validSessionSQLiteRestore(restored *RuntimeAgentSessionSQLiteRestore, preserveSessionSQLite bool) bool {
	return restored != nil && restored.Status == "restored" && restored.ConfigRestored && restored.StateRestored && restored.PreservedSessionSQLite == preserveSessionSQLite
}

func shouldReconcileUpgradeReceipt(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && !errors.Is(err, ErrRuntimeAgentConflict) && !errors.Is(err, ErrRuntimeAgentNotFound) && !errors.Is(err, ErrRuntimeAgentUnsupported)
}

func runtimeUpgradeLeaseToken(rolloutID int64, instanceID int) string {
	return fmt.Sprintf("rollout-%d-instance-%d", rolloutID, instanceID)
}

func (s *RuntimeUpgradeService) candidateForUpgradeItem(ctx context.Context, item models.RuntimeUpgradeItem) (runtimeUpgradeCandidate, error) {
	return s.candidateForUpgradeItemMode(ctx, item, true)
}

func (s *RuntimeUpgradeService) candidateForUpgradeItemMode(ctx context.Context, item models.RuntimeUpgradeItem, requireStable bool) (runtimeUpgradeCandidate, error) {
	var userID, generation int
	var instanceStatus string
	var workspace sql.NullString
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT user_id, runtime_generation, workspace_path, status FROM instances WHERE id = ?`, item.InstanceID)
	if err != nil {
		return runtimeUpgradeCandidate{}, err
	}
	if err := row.Scan(&userID, &generation, &workspace, &instanceStatus); err != nil {
		return runtimeUpgradeCandidate{}, err
	}
	expected := RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenClaw, userID, item.InstanceID)
	actual := filepath.Clean(strings.TrimSpace(workspace.String))
	if actual == "." || actual == "" {
		actual = expected
	}
	if !sameCleanPath(actual, expected) || !pathWithin(s.workspaceRoot, actual) {
		return runtimeUpgradeCandidate{}, fmt.Errorf("instance %d workspace left its fixed managed path", item.InstanceID)
	}
	instanceStatus = strings.ToLower(strings.TrimSpace(instanceStatus))
	candidate := runtimeUpgradeCandidate{InstanceID: item.InstanceID, UserID: userID, InstanceStatus: instanceStatus, RuntimeGeneration: generation, Generation: generation, WorkspacePath: actual, TeamID: item.TeamID, TeamMemberID: item.TeamMemberID}
	binding, err := s.bindings.GetByInstanceID(ctx, item.InstanceID)
	if err != nil {
		return runtimeUpgradeCandidate{}, err
	}
	if binding == nil {
		recoveryStopped := false
		if instanceStatus != "stopped" {
			scope := s.upgradeItemRecovery(ctx, item)
			if scope.allowsRecovery(item.InstanceID) && scope.RecoveryParentID > 0 {
				prior, err := s.recoveryStoppedSource(ctx, scope.RecoveryParentID, item.InstanceID, generation)
				if err != nil {
					return candidate, err
				}
				if err := s.verifyRecoverySourceStopped(ctx, prior); err != nil {
					return candidate, err
				}
				item = prior
				recoveryStopped = true
			}
		}
		if instanceStatus != "stopped" {
			if requireStable && !recoveryStopped && !upgradeItemOwnsConfirmedStoppedGateway(item) {
				return runtimeUpgradeCandidate{}, fmt.Errorf("%w: instance %d is %s without a Runtime binding", ErrUpgradeSourceState, item.InstanceID, instanceStatus)
			}
			if !upgradeItemOwnsConfirmedStoppedGateway(item) {
				return runtimeUpgradeCandidate{}, fmt.Errorf("OPENCLAW_ROLLBACK_SOURCE_IDENTITY_MISSING: instance %d has no confirmed persisted source gateway identity; automatic rollback is safely held", item.InstanceID)
			}
			candidate.RuntimePodID = item.RuntimePodID
			candidate.GatewayID = stringValue(item.SourceGatewayID)
			candidate.Generation = intValue(item.SourceGeneration, generation)
			candidate.SourceVersion = stringValue(item.SourceRuntimeVersion)
			candidate.SourcePodUID = stringValue(item.SourcePodUID)
			candidate.SourceDeployment = stringValue(item.SourceDeploymentName)
			candidate.SourceImageDigest = stringValue(item.SourceImageDigest)
			candidate.BindingState = "stopped"
			if item.RuntimePodID != nil {
				pod, podErr := s.pods.GetByID(ctx, *item.RuntimePodID)
				if podErr != nil {
					return runtimeUpgradeCandidate{}, podErr
				}
				if pod != nil && persistedSourcePodMatches(*pod, item) {
					candidate.AgentEndpoint = stringValue(pod.AgentEndpoint)
					candidate.Capabilities = pod.Capabilities()
				} else {
					// A missing or replaced source Pod UID proves the persisted
					// process cannot still own this gateway.  Keep GatewayID empty
					// so rollback restores data without contacting an unrelated Pod.
					candidate.GatewayID = ""
					candidate.RuntimePodID = nil
				}
			}
		}
		return candidate, nil
	}
	if requireStable && instanceStatus != "running" && instanceStatus != "stopped" && !(instanceStatus == "error" && s.upgradeItemRecovery(ctx, item).allowsRecovery(item.InstanceID)) {
		return runtimeUpgradeCandidate{}, fmt.Errorf("instance %d runtime state %s is not stable for migration", item.InstanceID, instanceStatus)
	}
	candidate.RuntimePodID = &binding.RuntimePodID
	candidate.GatewayID = strings.TrimSpace(binding.GatewayID)
	candidate.Generation = binding.Generation
	candidate.BindingState = strings.ToLower(strings.TrimSpace(binding.State))
	pod, err := s.pods.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return runtimeUpgradeCandidate{}, err
	}
	if pod == nil || pod.State != "ready" || pod.Draining || stringValue(pod.AgentEndpoint) == "" {
		return runtimeUpgradeCandidate{}, fmt.Errorf("instance %d source Runtime pod is unavailable", item.InstanceID)
	}
	candidate.AgentEndpoint = stringValue(pod.AgentEndpoint)
	candidate.Capabilities = pod.Capabilities()
	candidate.SourceVersion = resolvedSourceOpenClawVersion(*pod)
	candidate.SourcePodUID = stringValue(pod.PodUID)
	candidate.SourceDeployment = pod.DeploymentName
	candidate.SourceImageDigest = stringValue(pod.ImageDigest)
	return candidate, nil
}

func upgradeItemOwnsConfirmedStoppedGateway(item models.RuntimeUpgradeItem) bool {
	if item.RuntimePodID == nil || item.SourceGatewayID == nil || strings.TrimSpace(*item.SourceGatewayID) == "" || item.SourceGeneration == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.State)) {
	case "quiesced", "migration_started", "migrated", "restart_ready", "gateway_verified", "restored":
		return true
	default:
		return false
	}
}

func persistedSourcePodMatches(pod models.RuntimePod, item models.RuntimeUpgradeItem) bool {
	if item.SourcePodUID != nil && strings.TrimSpace(*item.SourcePodUID) != "" && strings.TrimSpace(stringValue(pod.PodUID)) != strings.TrimSpace(*item.SourcePodUID) {
		return false
	}
	if item.SourceDeploymentName != nil && strings.TrimSpace(*item.SourceDeploymentName) != "" && strings.TrimSpace(pod.DeploymentName) != strings.TrimSpace(*item.SourceDeploymentName) {
		return false
	}
	if item.SourceImageDigest != nil && strings.TrimSpace(*item.SourceImageDigest) != "" && strings.TrimSpace(stringValue(pod.ImageDigest)) != strings.TrimSpace(*item.SourceImageDigest) {
		return false
	}
	return true
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func nextRuntimeUpgradeBatch(items []models.RuntimeUpgradeItem, limit int, state string) []models.RuntimeUpgradeItem {
	if limit <= 0 {
		limit = 1
	}
	var batch []models.RuntimeUpgradeItem
	for index := 0; index < len(items); {
		item := items[index]
		end := index + 1
		if item.TeamID != nil {
			for end < len(items) && items[end].TeamID != nil && *items[end].TeamID == *item.TeamID {
				end++
			}
		}
		var group []models.RuntimeUpgradeItem
		for _, candidate := range items[index:end] {
			if candidate.State == state {
				group = append(group, candidate)
			}
		}
		if len(group) > 0 {
			if len(batch) > 0 && len(batch)+len(group) > limit {
				break
			}
			batch = append(batch, group...)
			if len(batch) >= limit {
				break
			}
		}
		index = end
	}
	return batch
}

func nextRuntimeUpgradeMigrationBatch(items []models.RuntimeUpgradeItem, limit int) []models.RuntimeUpgradeItem {
	if limit <= 0 {
		limit = 1
	}
	allowed := map[string]bool{
		"compatibility_checked": true,
		"quiescing":             true,
		"quiesced":              true,
		"migration_started":     true,
	}
	var batch []models.RuntimeUpgradeItem
	for index := 0; index < len(items); {
		item := items[index]
		end := index + 1
		if item.TeamID != nil {
			for end < len(items) && items[end].TeamID != nil && *items[end].TeamID == *item.TeamID {
				end++
			}
		}
		var group []models.RuntimeUpgradeItem
		for _, candidate := range items[index:end] {
			if allowed[strings.ToLower(strings.TrimSpace(candidate.State))] {
				group = append(group, candidate)
			}
		}
		if len(group) > 0 {
			if len(batch) > 0 && len(batch)+len(group) > limit {
				break
			}
			batch = append(batch, group...)
			if len(batch) >= limit {
				break
			}
		}
		index = end
	}
	return batch
}

func (s *RuntimeUpgradeService) workspaceForInstance(instanceID int) (string, error) {
	var userID int
	row, err := s.sess.SQL().QueryRow(`SELECT user_id FROM instances WHERE id = ?`, instanceID)
	if err != nil {
		return "", err
	}
	if err := row.Scan(&userID); err != nil {
		return "", err
	}
	return RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenClaw, userID, instanceID), nil
}

func (s *RuntimeUpgradeService) Rollback(ctx context.Context, rollout *models.RuntimeRollout) error {
	if rollout == nil {
		return nil
	}
	if upgradeGroupScope(ctx) == nil {
		_ = s.updateRolloutPhase(ctx, rollout.ID, "rollback_restore")
	}
	items, err := s.listUpgradeItems(ctx, rollout.ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("rollback refused: rollout %d has no persisted upgrade items", rollout.ID)
	}
	upgradeAgent, ok := s.agent.(RuntimeUpgradeAgentClient)
	if !ok {
		return fmt.Errorf("target Runtime Agent does not implement official session restore")
	}
	var errs []error
	restoreConcurrency := 1
	var fallbackEndpoints []string
	if runtimePods, listErr := s.pods.List(ctx, RuntimeTypeOpenClaw); listErr == nil {
		restorePods := []models.RuntimePod{}
		for _, pod := range runtimePods {
			endpoint := stringValue(pod.AgentEndpoint)
			if strings.TrimSpace(pod.ImageRef) == strings.TrimSpace(rollout.TargetImageRef) && endpoint != "" && (pod.State == "standby" || pod.State == "ready") {
				fallbackEndpoints = append(fallbackEndpoints, endpoint)
				restorePods = append(restorePods, pod)
			}
		}
		if len(restorePods) > 0 {
			restoreConcurrency = runtimeUpgradePodPolicy(rollout, restorePods).Restore
		}
	}
	if err := s.runUpgradeItems(ctx, rollout.ID, "rollback_restore", items, restoreConcurrency, func(ctx context.Context, index int) error {
		var errs []error
		for _, item := range []models.RuntimeUpgradeItem{items[index]} {
			if item.State == "prepared" || item.State == "compatibility_checked" || item.State == "pending" {
				continue
			}
			// Rollback is deliberately resumable. Once an item was restored (or was
			// already released for a source restart), a new leader must not replay the
			// official restore against the same archive.
			if item.State == "restored" || item.State == "restart_ready" {
				continue
			}
			if item.State == "quiesced" {
				// The source gateway was stopped, but no target-side mutation began.
				// CompleteRollback must still explicitly recreate it on the source.
				result, err := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'restored', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state='quiesced'`, time.Now().UTC(), rollout.ID, item.InstanceID)
				if err != nil {
					return err
				}
				if n, err := result.RowsAffected(); err != nil || n != 1 {
					return errors.Join(ErrRuntimeUpgradeBusy, err)
				}
				continue
			}
			var evidence struct {
				Migration           RuntimeAgentSessionSQLiteMigration `json:"migration"`
				TargetAgentEndpoint string                             `json:"target_agent_endpoint"`
			}
			if item.PostflightJSON != nil && strings.TrimSpace(*item.PostflightJSON) != "" {
				if err := json.Unmarshal([]byte(*item.PostflightJSON), &evidence); err != nil {
					errs = append(errs, fmt.Errorf("restore instance %d: invalid migration evidence", item.InstanceID))
					continue
				}
			} else if item.State != "migration_started" {
				errs = append(errs, fmt.Errorf("restore instance %d: migration evidence is missing", item.InstanceID))
				continue
			}
			var userID, generation int
			row, queryErr := s.sess.SQL().QueryRowContext(ctx, `SELECT user_id, runtime_generation FROM instances WHERE id = ?`, item.InstanceID)
			if queryErr != nil || row.Scan(&userID, &generation) != nil {
				errs = append(errs, fmt.Errorf("restore instance %d: instance identity is unavailable", item.InstanceID))
				continue
			}
			if item.SourceRuntimeVersion == nil || strings.TrimSpace(*item.SourceRuntimeVersion) == "" {
				errs = append(errs, fmt.Errorf("restore instance %d: source OpenClaw version is unavailable; refusing to guess the session rollback format", item.InstanceID))
				continue
			}
			preserveSessionSQLite := openClawVersionAtLeast(*item.SourceRuntimeVersion, targetOpenClawUpgradeVersion)
			sourceCandidate, candidateErr := s.candidateForUpgradeItemMode(ctx, item, false)
			if candidateErr != nil {
				errs = append(errs, fmt.Errorf("restore instance %d cannot quiesce its source gateway: %w", item.InstanceID, candidateErr))
				continue
			}
			stopped, stopErr := s.stopCandidateGateway(ctx, sourceCandidate)
			if stopErr != nil || !stopped {
				if stopErr == nil {
					stopErr = errors.New("source gateway stop was not confirmed")
				}
				errs = append(errs, fmt.Errorf("restore instance %d cannot quiesce its source gateway: %w", item.InstanceID, stopErr))
				continue
			}
			leaseToken := runtimeUpgradeLeaseToken(rollout.ID, item.InstanceID)
			request := RuntimeAgentWorkspaceRequest{RolloutID: strconv.FormatInt(rollout.ID, 10), UserID: userID, InstanceID: item.InstanceID, Generation: generation, UID: RuntimeLinuxID(item.InstanceID), GID: RuntimeLinuxID(item.InstanceID), LeaseToken: leaseToken, PreserveSessionSQLite: preserveSessionSQLite}
			lease := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: request, Token: leaseToken, TTLSeconds: 3600}
			endpoints := uniqueSortedStrings(append([]string{strings.TrimSpace(evidence.TargetAgentEndpoint)}, fallbackEndpoints...))
			if len(endpoints) == 0 {
				errs = append(errs, fmt.Errorf("restore instance %d: target agent endpoint is missing", item.InstanceID))
				continue
			}
			endpoint := ""
			var acquireErrors []error
			for _, candidateEndpoint := range endpoints {
				if err := upgradeAgent.AcquireWriterLease(ctx, candidateEndpoint, lease); err == nil {
					endpoint = candidateEndpoint
					break
				} else {
					acquireErrors = append(acquireErrors, err)
				}
			}
			if endpoint == "" {
				errs = append(errs, fmt.Errorf("restore instance %d acquire writer lease: %w", item.InstanceID, errors.Join(acquireErrors...)))
				continue
			}
			restored, restoreErr := upgradeAgent.RestoreSessionSQLite(ctx, endpoint, request)
			if restoreErr != nil && shouldReconcileUpgradeReceipt(ctx, restoreErr) {
				statusCtx, cancel := context.WithTimeout(ctx, runtimeAgentControlTimeout)
				persisted, statusErr := upgradeAgent.SessionSQLiteRestoreStatus(statusCtx, endpoint, request)
				cancel()
				if statusErr == nil && validSessionSQLiteRestore(persisted, preserveSessionSQLite) {
					restored = persisted
					restoreErr = nil
				}
			}
			releaseErr := upgradeAgent.ReleaseWriterLease(ctx, endpoint, lease)
			if restoreErr != nil {
				errs = append(errs, fmt.Errorf("restore instance %d official session archive: %w", item.InstanceID, restoreErr))
				continue
			}
			if releaseErr != nil {
				errs = append(errs, fmt.Errorf("restore instance %d release writer lease: %w", item.InstanceID, releaseErr))
				continue
			}
			if !validSessionSQLiteRestore(restored, preserveSessionSQLite) {
				errs = append(errs, fmt.Errorf("restore instance %d returned no verified evidence", item.InstanceID))
				continue
			}
			result, err := s.execClaimedUpgradeState(ctx, `UPDATE runtime_upgrade_items SET state = 'restored', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state=?`, time.Now().UTC(), rollout.ID, item.InstanceID, item.State)
			if err != nil {
				return err
			}
			if n, err := result.RowsAffected(); err != nil || n != 1 {
				return errors.Join(ErrRuntimeUpgradeBusy, err)
			}
		}
		return errors.Join(errs...)
	}); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "rollback_restore", "rollback_restore", "passed", map[string]any{"instance_count": len(items), "workspace_snapshot": false}); err != nil {
			return err
		}
		if upgradeGroupScope(ctx) == nil {
			_, _ = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status = 'waiting', rollback_error = NULL, updated_at = ? WHERE id = ?`, time.Now().UTC(), rollout.ID)
		}
		return nil
	}
	message := errors.Join(errs...).Error()
	if upgradeGroupScope(ctx) == nil {
		_, _ = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status = 'error', rollback_error = ?, updated_at = ? WHERE id = ?`, message, time.Now().UTC(), rollout.ID)
	}
	return errors.Join(errs...)
}

func (s *RuntimeUpgradeService) CompleteRollback(ctx context.Context, rollout *models.RuntimeRollout) error {
	if rollout == nil {
		return nil
	}
	items, err := s.listUpgradeItems(ctx, rollout.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.State != "restored" {
			continue
		}
		binding, bindingErr := s.bindings.GetByInstanceID(ctx, item.InstanceID)
		if bindingErr != nil {
			return fmt.Errorf("inspect restored instance %d binding: %w", item.InstanceID, bindingErr)
		}
		if binding != nil {
			if err := s.bindings.DeleteByInstanceIDAndReleaseSlot(ctx, item.InstanceID, binding.RuntimePodID); err != nil {
				return fmt.Errorf("release restored instance %d stale binding: %w", item.InstanceID, err)
			}
		}
	}
	if err := s.sess.TxContext(ctx, func(tx db.Session) error {
		now := time.Now().UTC()
		for _, item := range items {
			if item.State != "restored" {
				continue
			}
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE instances SET status = 'creating', runtime_generation = runtime_generation + 1, runtime_error_message = NULL, updated_at = ? WHERE id = ?`, now, item.InstanceID); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'restart_ready', recovery_started_at = COALESCE(recovery_started_at, ?), recovery_status = 'pending', updated_at = ? WHERE rollout_id = ? AND instance_id = ?`, now, now, rollout.ID, item.InstanceID); err != nil {
				return err
			}
		}
		return nil
	}, nil); err != nil {
		return err
	}
	if err := s.audit(ctx, &rollout.ID, rollout.StartedBy, "rollback_source_restart_requested", "rollback_image", "pending", map[string]any{"instance_count": len(items)}); err != nil {
		return err
	}
	// The archive/config restore is durable, but the failed target pool is not
	// yet disposable. Keep it in waiting until the scheduler proves every
	// instance is running again on its immutable source deployment.
	if upgradeGroupScope(ctx) != nil {
		return nil
	}
	changed := false
	for _, item := range items {
		if item.State != "pending" && item.State != "prepared" && item.State != "compatibility_checked" {
			changed = true
		}
	}
	if !changed {
		_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status='not_needed',finished_at=COALESCE(finished_at,UTC_TIMESTAMP(6)),updated_at=UTC_TIMESTAMP(6) WHERE id=?`, rollout.ID)
		return err
	}
	_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status = 'waiting', rollback_error = NULL, updated_at = ? WHERE id = ?`, time.Now().UTC(), rollout.ID)
	return err
}

func (s *RuntimeUpgradeService) FailRollback(ctx context.Context, rolloutID int64, rollbackErr error) {
	if s == nil || s.sess == nil || rollbackErr == nil {
		return
	}
	message := rollbackErr.Error()
	if len(message) > 4096 {
		message = message[:4096]
	}
	now := time.Now().UTC()
	// A deterministic rollback failure must not be reconciled forever, and an
	// instance whose writer binding has already been released must not fall back
	// into ordinary scheduling against partially migrated state.  Hold only the
	// unbound instances; a sibling that still has a running binding (for example
	// one not yet quiesced in the same batch) remains untouched.
	if err := s.sess.TxContext(ctx, func(tx db.Session) error {
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE runtime_rollouts
			SET status = 'error', finished_at = COALESCE(finished_at, ?),
			    rollback_status = 'error', rollback_error = ?, updated_at = ?
			WHERE id = ?
		`, now, message, now, rolloutID); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(ctx, `
			UPDATE instances instance_row
			JOIN runtime_upgrade_items upgrade_item ON upgrade_item.instance_id = instance_row.id
			LEFT JOIN instance_runtime_bindings binding
			  ON binding.instance_id = instance_row.id AND binding.state = 'running'
			SET instance_row.status = 'error',
			    instance_row.runtime_error_message = ?,
			    instance_row.updated_at = ?
			WHERE upgrade_item.rollout_id = ?
			  AND binding.id IS NULL
			  AND LOWER(TRIM(instance_row.status)) NOT IN ('deleting', 'stopped')
		`, "OpenClaw upgrade rollback is safely held: "+message, now, rolloutID)
		return err
	}, nil); err != nil {
		// Preserve the original failure even if the stronger atomic hold could
		// not be committed; the next control-plane pass can retry the hold.
		_, _ = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status = 'error', rollback_error = ?, updated_at = ? WHERE id = ?`, message, now, rolloutID)
	}
}

func (s *RuntimeUpgradeService) RollbackCleanupCandidates(ctx context.Context) ([]models.RuntimeRollout, error) {
	if s == nil || s.sess == nil {
		return nil, nil
	}
	var rollouts []models.RuntimeRollout
	err := s.sess.Collection("runtime_rollouts").Find(db.Cond{
		"runtime_type":       RuntimeTypeOpenClaw,
		"status":             "error",
		"rollback_status IN": []string{"waiting", "restored", "needs_attention"},
	}).OrderBy("id").All(&rollouts)
	if err != nil {
		return nil, err
	}
	completed := map[int64]struct{}{}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT DISTINCT rollout_id FROM runtime_upgrade_audits WHERE action = 'upgrade_pool_cleanup' AND outcome = 'passed' AND rollout_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rolloutID int64
		if err := rows.Scan(&rolloutID); err != nil {
			return nil, err
		}
		completed[rolloutID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	filtered := make([]models.RuntimeRollout, 0, len(rollouts))
	for _, rollout := range rollouts {
		if _, ok := completed[rollout.ID]; !ok {
			filtered = append(filtered, rollout)
		}
	}
	return filtered, nil
}

func (s *RuntimeUpgradeService) MarkRollbackPoolCleanupComplete(ctx context.Context, rollout *models.RuntimeRollout) error {
	if rollout == nil {
		return nil
	}
	return s.audit(ctx, &rollout.ID, rollout.StartedBy, "upgrade_pool_cleanup", "rollback_restore", "passed", map[string]any{"data_deleted": false})
}

// RollbackRecoveryVerified proves every candidate is serving from the
// immutable source pool before a failed target Deployment may be deleted.
// Workspaces, migration receipts and database records are never cleanup
// targets and remain independent of the Deployment lifecycle.
func (s *RuntimeUpgradeService) RollbackRecoveryVerified(ctx context.Context, rollout *models.RuntimeRollout) (bool, error) {
	if s == nil || rollout == nil || rollout.SourceImagesJSON == nil {
		return false, nil
	}
	var sources map[string]string
	if json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil || len(sources) == 0 {
		return false, fmt.Errorf("rollback source inventory is unavailable")
	}
	if s.deployments == nil {
		return false, fmt.Errorf("live rollback source inventory is unavailable")
	}
	refs := make([]k8s.RuntimeDeploymentRef, 0, len(sources))
	for key := range sources {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			return false, fmt.Errorf("invalid rollback source identity")
		}
		refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: parts[0], Name: parts[1]})
	}
	livePods, err := runtimeDeploymentPodsFor(ctx, s.deployments, RuntimeTypeOpenClaw, refs)
	if err != nil {
		return false, err
	}
	liveByUID := make(map[string]models.RuntimePod, len(livePods))
	for _, p := range livePods {
		if stringValue(p.PodUID) != "" {
			liveByUID[p.Namespace+"/"+stringValue(p.PodUID)] = p
		}
	}
	type observed struct {
		podUID                                                                   sql.NullString
		lastSeen, lastHealth                                                     sql.NullTime
		instanceID                                                               int
		teamID                                                                   sql.NullInt64
		instanceStatus                                                           string
		bindingState, namespace, deploymentName, imageRef, imageDigest, podState sql.NullString
		draining                                                                 sql.NullBool
		recoveryStarted                                                          sql.NullTime
		generation                                                               int
		bindingGeneration                                                        sql.NullInt64
		podName, podIP, endpoint                                                 sql.NullString
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `
		SELECT i.id, ui.team_id, i.status, b.state, p.namespace, p.deployment_name,
		       p.image_ref, p.image_digest, p.state, p.draining,
		       ui.recovery_started_at, i.runtime_generation, b.generation,
		       p.pod_uid, p.last_seen_at, b.last_health_at,p.pod_name,p.pod_ip,p.agent_endpoint
		FROM runtime_upgrade_items ui
		JOIN instances i ON i.id = ui.instance_id
		LEFT JOIN instance_runtime_bindings b ON b.instance_id = i.id AND b.state = 'running'
		LEFT JOIN runtime_pods p ON p.id = b.runtime_pod_id
		WHERE ui.rollout_id = ?
	`, rollout.ID)
	if err != nil {
		return false, err
	}
	var observations []observed
	for rows.Next() {
		var o observed
		if err := rows.Scan(&o.instanceID, &o.teamID, &o.instanceStatus, &o.bindingState, &o.namespace, &o.deploymentName, &o.imageRef, &o.imageDigest, &o.podState, &o.draining, &o.recoveryStarted, &o.generation, &o.bindingGeneration, &o.podUID, &o.lastSeen, &o.lastHealth, &o.podName, &o.podIP, &o.endpoint); err != nil {
			rows.Close()
			return false, err
		}
		observations = append(observations, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(observations) == 0 {
		return false, nil
	}
	now := time.Now().UTC()
	allReady, needsAttention := true, false
	teams := map[int]bool{}
	for _, o := range observations {
		key := strings.TrimSpace(o.namespace.String) + "/" + strings.TrimSpace(o.deploymentName.String)
		expected, known := sources[key]
		pod := models.RuntimePod{ImageRef: o.imageRef.String, ImageDigest: upgradeStringPtrOrNil(o.imageDigest.String)}
		ready := strings.EqualFold(strings.TrimSpace(o.instanceStatus), "running") && o.bindingState.String == "running" && o.podState.String == "ready" && !o.draining.Bool && o.bindingGeneration.Valid && int(o.bindingGeneration.Int64) == o.generation && known && runtimePodMatchesImage(pod, expected)
		live, exists := liveByUID[o.namespace.String+"/"+o.podUID.String]
		if !o.podUID.Valid || o.podUID.String == "" {
			legacy := models.RuntimePod{Namespace: o.namespace.String, DeploymentName: o.deploymentName.String, PodName: o.podName.String, PodIP: upgradeStringPtrOrNil(o.podIP.String), AgentEndpoint: upgradeStringPtrOrNil(o.endpoint.String)}
			matched := upgradePodsWithLiveIdentity([]models.RuntimePod{legacy}, livePods)
			if len(matched) == 1 {
				live, exists = liveByUID[o.namespace.String+"/"+stringValue(matched[0].KubernetesPodUID)]
			}
		}
		ready = ready && exists && live.DeploymentName == o.deploymentName.String && (live.KubernetesReady || live.State == "ready") && runtimePodMatchesImage(live, expected) && upgradeRecoveryObservationFresh(now, o.lastSeen, o.lastHealth)
		if o.teamID.Valid {
			id := int(o.teamID.Int64)
			previous, exists := teams[id]
			teams[id] = ready && (!exists || previous)
		}
		status := "verified"
		var code, message any
		if !ready {
			allReady = false
			status = "pending"
			if strings.EqualFold(o.instanceStatus, "error") || (o.recoveryStarted.Valid && now.Sub(o.recoveryStarted.Time) >= 15*time.Minute) {
				needsAttention = true
				status = "needs_attention"
				code = "UPGRADE_ROLLBACK_SOURCE_UNAVAILABLE"
				message = fmt.Sprintf("Instance %d data restore completed but source Gateway recovery is unverified; retained data and Team isolation require inspection", o.instanceID)
			}
		}
		if _, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_started_at=COALESCE(recovery_started_at, ?), recovery_status=?, recovery_error_code=?, error_message=? WHERE rollout_id=? AND instance_id=?`, now, status, code, message, rollout.ID, o.instanceID); err != nil {
			return false, err
		}
	}
	recoveredTeams := map[int]struct{}{}
	for id, ready := range teams {
		if ready {
			recoveredTeams[id] = struct{}{}
		}
	}
	if len(recoveredTeams) > 0 {
		if err := s.setTeamMaintenance(ctx, recoveredTeams, rollout.ID, false); err != nil {
			return false, err
		}
	}
	if allReady {
		marker := "restored"
		items, e := s.listUpgradeItems(ctx, rollout.ID)
		if e != nil {
			return false, e
		}
		changed := false
		for _, i := range items {
			changed = changed || (i.State != "pending" && i.State != "prepared" && i.State != "compatibility_checked")
		}
		if !changed {
			marker = "not_needed"
		}
		_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status=?, rollback_error=NULL, finished_at=COALESCE(finished_at, ?), updated_at=? WHERE id=?`, marker, now, now, rollout.ID)
		return err == nil, err
	}
	if needsAttention && stringValue(rollout.RollbackStatus) != "needs_attention" {
		_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET rollback_status='needs_attention', rollback_error='Data restoration is complete; one or more source Gateways did not recover. No automatic data restore will be replayed.', finished_at=COALESCE(finished_at, ?), updated_at=? WHERE id=?`, now, now, rollout.ID)
		if err != nil {
			return false, err
		}
	}
	return false, nil
}
func (s *RuntimeUpgradeService) BeginRollback(ctx context.Context, rolloutID int64, cause error) error {
	var message any
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = strings.TrimSpace(cause.Error())
	}
	_, err := s.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_rollouts
		SET phase = CASE WHEN phase = 'rollback_restore' THEN phase ELSE 'rollback_image' END,
		    rollback_status = CASE WHEN rollback_status = 'waiting' THEN rollback_status ELSE 'starting' END,
		    error_message = COALESCE(error_message, ?),
		    updated_at = ?
		WHERE id = ?
	`, message, time.Now().UTC(), rolloutID)
	return err
}

func runtimeDeploymentPodsFor(ctx context.Context, provider RuntimeDeploymentInventoryProvider, runtimeType string, refs []k8s.RuntimeDeploymentRef) ([]models.RuntimePod, error) {
	if scoped, ok := provider.(RuntimeDeploymentScopedInventoryProvider); ok && len(refs) > 0 {
		return scoped.RuntimeDeploymentPodsFor(ctx, runtimeType, refs)
	}
	pods, err := provider.RuntimeDeploymentPods(ctx, runtimeType)
	if err != nil || len(refs) == 0 {
		return pods, err
	}
	allowed := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		allowed[strings.TrimSpace(ref.Namespace)+"/"+strings.TrimSpace(ref.Name)] = struct{}{}
	}
	filtered := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		if _, ok := allowed[strings.TrimSpace(pod.Namespace)+"/"+strings.TrimSpace(pod.DeploymentName)]; ok {
			filtered = append(filtered, pod)
		}
	}
	return filtered, nil
}

func (s *RuntimeUpgradeService) InstanceBlocked(ctx context.Context, runtimeType string, instanceID int) bool {
	normalized, ok := NormalizeV2RuntimeType(runtimeType)
	if !ok || normalized != RuntimeTypeOpenClaw {
		return false
	}
	if s == nil || s.sess == nil || instanceID <= 0 {
		return true
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `
		SELECT r.phase,
		       COALESCE(r.rollback_status, ''),
		       COALESCE(r.preflight_json, ''),
		       i.id IS NOT NULL,
		       COALESCE(i.state, ''),i.isolated_at IS NOT NULL,COALESCE(i.recovery_status,'')
		FROM runtime_rollouts r
		LEFT JOIN runtime_upgrade_items i ON i.rollout_id = r.id AND i.instance_id = ?
		WHERE r.runtime_type = 'openclaw' AND (r.status IN ('pending','running') OR ((r.status='error' AND i.recovery_status='needs_attention' OR r.status='finished' AND i.isolated_at IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=i.instance_id AND nr.id>r.id AND nr.status IN('pending','running','finished','error'))))
	`, instanceID)
	if err != nil {
		return true
	}
	defer rows.Close()
	for rows.Next() {
		var phase, rollbackStatus, preflightJSON, itemState string
		var itemExists bool
		var isolated bool
		var recovery string
		if err := rows.Scan(&phase, &rollbackStatus, &preflightJSON, &itemExists, &itemState, &isolated, &recovery); err != nil {
			return true
		}
		if isolated && (itemState == "pending" || itemState == "prepared" || itemState == "compatibility_checked" || recovery == "source_restore_ready" || recovery == "source_restored") {
			continue
		}
		if activeOpenClawRolloutBlocksInstance(phase, rollbackStatus, preflightJSON, itemExists, itemState) {
			return true
		}
	}
	return rows.Err() != nil
}

func activeOpenClawRolloutBlocksInstance(phase, rollbackStatus, preflightJSON string, itemExists bool, itemState string) bool {
	preflightJSON = strings.TrimSpace(preflightJSON)
	var preflight *string
	if preflightJSON != "" {
		preflight = &preflightJSON
	}
	scope := runtimeUpgradeScopeFromRollout(&models.RuntimeRollout{PreflightJSON: preflight})
	// An upgrade-lab rollout owns only its persisted candidate items. It must
	// never pause an ordinary OpenClaw instance, much less another Runtime.
	if (scope.UpgradeLabRunID != nil || scope.scoped()) && !itemExists {
		return false
	}
	phase = strings.ToLower(strings.TrimSpace(phase))
	rollbackStatus = strings.ToLower(strings.TrimSpace(rollbackStatus))
	itemState = strings.ToLower(strings.TrimSpace(itemState))
	if rollbackStatus == "starting" || rollbackStatus == "waiting" {
		return true
	}
	switch phase {
	case RuntimeUpgradePhaseEmptyPoolReset, "maintenance", "image_rollout", "compatibility_check", "session_migration", "postflight", "rollback_restore":
		return true
	case "gateway_restart":
		// The upgrade reconciler owns restart_ready placement and targets the
		// still-isolated rollout pool explicitly. Releasing it to ordinary
		// scheduling creates a race and cannot work while that pool correctly
		// remains scheduling-disabled until postflight commits.
		return true
	default:
		return false
	}
}

func (s *RuntimeUpgradeService) ValidateInstanceDeletion(ctx context.Context, instanceID int) error {
	if s == nil || s.sess == nil || instanceID <= 0 {
		return fmt.Errorf("runtime upgrade deletion guard is unavailable")
	}
	var count int
	row, err := s.sess.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM runtime_upgrade_items i
		JOIN runtime_rollouts r ON r.id = i.rollout_id
		WHERE i.instance_id = ? AND r.status IN ('pending','running')
	`, instanceID)
	if err == nil {
		err = row.Scan(&count)
	}
	if err != nil {
		return fmt.Errorf("check active runtime rollout for instance %d: %w", instanceID, err)
	}
	if count > 0 {
		return fmt.Errorf("instance %d is held by an active data-safe runtime rollout", instanceID)
	}
	return nil
}

// ValidateEmptyOpenClawPoolReset proves that the ordinary OpenClaw Lite pool
// contains no user lifecycle state before an 8.1+ deployment is allowed to
// rejoin the pre-8.1 rolling path. Upgrade-lab deployments are intentionally
// excluded because they are isolated by their Kubernetes ownership labels.
// The rollout row itself is excluded during the execution-time recheck.
func (s *RuntimeUpgradeService) ValidateEmptyOpenClawPoolReset(ctx context.Context, excludeRolloutID int64) error {
	if s == nil || s.sess == nil || s.pods == nil || s.bindings == nil || s.rollouts == nil || s.deployments == nil {
		return fmt.Errorf("empty OpenClaw pool reset safety dependencies are unavailable")
	}
	var blockers []string
	var instanceCount int
	var firstInstance sql.NullInt64
	row, err := s.sess.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(i.id)
		FROM instances i
		WHERE LOWER(TRIM(i.type)) = 'openclaw'
		  AND (CASE
		    WHEN LOWER(TRIM(i.instance_mode)) IN ('lite','pro') THEN LOWER(TRIM(i.instance_mode))
		    WHEN LOWER(TRIM(i.runtime_type)) = 'gateway' THEN 'lite'
		    ELSE 'pro'
		  END) = 'lite'
		  AND LOWER(TRIM(COALESCE(i.description, ''))) NOT LIKE 'openclaw-upgrade-lab:%'
	`)
	if err != nil {
		return fmt.Errorf("inspect ordinary OpenClaw Lite instances: %w", err)
	}
	if err := row.Scan(&instanceCount, &firstInstance); err != nil {
		return fmt.Errorf("inspect ordinary OpenClaw Lite instances: %w", err)
	}
	if instanceCount > 0 {
		blockers = append(blockers, fmt.Sprintf("%d ordinary OpenClaw Lite instance record(s) remain (first instance %d)", instanceCount, firstInstance.Int64))
	}

	var memberCount int
	var firstMember sql.NullInt64
	row, err = s.sess.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(tm.id)
		FROM team_members tm
		JOIN instances i ON i.id = tm.instance_id
		WHERE tm.status NOT IN ('deleted','deleting')
		  AND LOWER(TRIM(COALESCE(tm.runtime_type, 'openclaw'))) = 'openclaw'
		  AND LOWER(TRIM(i.type)) = 'openclaw'
		  AND (CASE
		    WHEN LOWER(TRIM(i.instance_mode)) IN ('lite','pro') THEN LOWER(TRIM(i.instance_mode))
		    WHEN LOWER(TRIM(i.runtime_type)) = 'gateway' THEN 'lite'
		    ELSE 'pro'
		  END) = 'lite'
		  AND LOWER(TRIM(COALESCE(i.description, ''))) NOT LIKE 'openclaw-upgrade-lab:%'
	`)
	if err != nil {
		return fmt.Errorf("inspect OpenClaw Team references: %w", err)
	}
	if err := row.Scan(&memberCount, &firstMember); err != nil {
		return fmt.Errorf("inspect OpenClaw Team references: %w", err)
	}
	if memberCount > 0 {
		blockers = append(blockers, fmt.Sprintf("%d active Team member reference(s) remain (first member %d)", memberCount, firstMember.Int64))
	}

	activeRollouts, err := s.rollouts.ListActive(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return fmt.Errorf("inspect active OpenClaw rollouts: %w", err)
	}
	for _, rollout := range activeRollouts {
		if rollout.ID == excludeRolloutID || runtimeUpgradeScopeFromRollout(&rollout).UpgradeLabRunID != nil {
			continue
		}
		blockers = append(blockers, fmt.Sprintf("OpenClaw rollout %d is still %s", rollout.ID, strings.TrimSpace(rollout.Status)))
	}

	livePods, err := s.deployments.RuntimeDeploymentPods(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return fmt.Errorf("inspect live OpenClaw deployments for empty reset: %w", err)
	}
	liveOrdinaryPods := map[string]models.RuntimePod{}
	for _, pod := range livePods {
		if !runtimePodEligibleForOrdinaryScheduling(pod) {
			continue
		}
		liveOrdinaryPods[runtimePodIdentity(pod)] = pod
	}
	if len(liveOrdinaryPods) == 0 {
		blockers = append(blockers, "no active ordinary OpenClaw Runtime pod is available for an image-only reset")
	}
	reportedPods, err := s.pods.List(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return fmt.Errorf("inspect Runtime Agent reports for empty reset: %w", err)
	}
	reportedByIdentity := make(map[string]models.RuntimePod, len(reportedPods))
	for _, pod := range reportedPods {
		reportedByIdentity[runtimePodIdentity(pod)] = pod
	}
	for identity, livePod := range liveOrdinaryPods {
		reported, ok := reportedByIdentity[identity]
		if !ok || reported.LastSeenAt == nil || time.Since(reported.LastSeenAt.UTC()) > 2*time.Minute {
			blockers = append(blockers, fmt.Sprintf("Runtime pod %s has no current agent report", strings.TrimSpace(livePod.PodName)))
			continue
		}
		if reported.UsedSlots != 0 {
			blockers = append(blockers, fmt.Sprintf("Runtime pod %s still reports %d used gateway slot(s)", strings.TrimSpace(livePod.PodName), reported.UsedSlots))
		}
		bindings, bindingErr := s.bindings.ListByRuntimePodID(ctx, reported.ID)
		if bindingErr != nil {
			return fmt.Errorf("inspect Runtime pod %s bindings: %w", strings.TrimSpace(livePod.PodName), bindingErr)
		}
		if len(bindings) > 0 {
			blockers = append(blockers, fmt.Sprintf("Runtime pod %s still has %d gateway binding(s)", strings.TrimSpace(livePod.PodName), len(bindings)))
		}
	}
	blockers = uniqueSortedStrings(blockers)
	if len(blockers) > 0 {
		return errors.New(strings.Join(blockers, "; "))
	}
	return nil
}

func (s *RuntimeUpgradeService) inspectCandidates(ctx context.Context, scope runtimeUpgradeScope) ([]runtimeUpgradeCandidate, map[string]string, []string, []string, error) {
	pods, err := s.pods.List(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	podByID := map[int64]models.RuntimePod{}
	liveImages := map[string]string{}
	fallbackSourceImages := map[string]string{}
	var candidates []runtimeUpgradeCandidate
	var warnings, blockers []string
	for _, pod := range pods {
		podByID[pod.ID] = pod
	}
	if s.deployments == nil {
		return nil, nil, nil, nil, fmt.Errorf("live runtime deployment inventory is not configured")
	}
	livePods, err := s.deployments.RuntimeDeploymentPods(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("inspect live OpenClaw deployments: %w", err)
	}
	for _, pod := range livePods {
		if strings.TrimSpace(pod.DeploymentName) == "" || strings.TrimSpace(pod.Namespace) == "" {
			continue
		}
		if !runtimeDeploymentInUpgradeScope(pod.DeploymentName, scope) {
			continue
		}
		key := strings.TrimSpace(pod.Namespace) + "/" + strings.TrimSpace(pod.DeploymentName)
		pinned, pinErr := immutableRuntimeImage(pod.ImageRef, stringValue(pod.ImageDigest))
		if pinErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("deployment %s rollback image is not immutable: %w", key, pinErr)
		}
		if prior, exists := liveImages[key]; exists && prior != pinned {
			return nil, nil, nil, nil, fmt.Errorf("deployment %s has pods with conflicting image digests", key)
		}
		liveImages[key] = pinned
		if runtimePodEligibleForOrdinaryScheduling(pod) {
			fallbackSourceImages[key] = pinned
		}
	}
	selectedIDs := make(map[int]struct{})
	for _, id := range normalizedPositiveIDs(scope.CandidateInstanceIDs) {
		selectedIDs[id] = struct{}{}
	}
	excluded := map[int]bool{}
	for _, id := range normalizedPositiveIDs(scope.ExcludeInstanceIDs) {
		excluded[id] = true
	}
	excludedTeams := map[int]bool{}
	rows, err := s.sess.SQL().QueryContext(ctx, `
		SELECT i.id, i.user_id, i.workspace_path, i.description, i.status, i.runtime_generation,
		       b.runtime_pod_id, b.gateway_id, b.generation, b.state,
		       tm.id, tm.team_id, tm.member_key, tm.role, tm.runtime_type, tm.availability, tm.status
		FROM instances i
		LEFT JOIN instance_runtime_bindings b ON b.instance_id = i.id
		LEFT JOIN team_members tm ON tm.instance_id = i.id AND tm.status NOT IN ('deleted','deleting')
		WHERE LOWER(TRIM(i.type)) = 'openclaw'
		  AND LOWER(TRIM(i.status)) <> 'deleting'
		  AND (CASE
		    WHEN LOWER(TRIM(i.instance_mode)) IN ('lite','pro') THEN LOWER(TRIM(i.instance_mode))
		    WHEN LOWER(TRIM(i.runtime_type)) = 'gateway' THEN 'lite'
		    ELSE 'pro'
		  END) = 'lite'
		ORDER BY COALESCE(tm.team_id, 0), i.id
	`)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()
	seen := map[int]struct{}{}
	for rows.Next() {
		var instanceID, userID int
		var workspace, description sql.NullString
		var instanceStatus string
		var runtimeGeneration int
		var podID sql.NullInt64
		var gatewayID sql.NullString
		var generation sql.NullInt64
		var bindingState sql.NullString
		var memberID, teamID sql.NullInt64
		var memberKey, role, runtimeType, availability, status sql.NullString
		if err := rows.Scan(&instanceID, &userID, &workspace, &description, &instanceStatus, &runtimeGeneration, &podID, &gatewayID, &generation, &bindingState, &memberID, &teamID, &memberKey, &role, &runtimeType, &availability, &status); err != nil {
			return nil, nil, nil, nil, err
		}
		labInstance := strings.HasPrefix(strings.ToLower(strings.TrimSpace(description.String)), "openclaw-upgrade-lab:")
		if scope.UpgradeLabRunID == nil && labInstance {
			continue
		}
		if scope.UpgradeLabRunID != nil && !labInstance {
			continue
		}
		if excluded[instanceID] {
			if teamID.Valid {
				excludedTeams[int(teamID.Int64)] = true
			}
			continue
		}
		if _, duplicate := seen[instanceID]; duplicate {
			blockers = append(blockers, fmt.Sprintf("instance %d belongs to more than one active Team", instanceID))
			continue
		}
		if len(selectedIDs) > 0 {
			if _, selected := selectedIDs[instanceID]; !selected {
				continue
			}
		}
		seen[instanceID] = struct{}{}
		blockerStart := len(blockers)
		expected := RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenClaw, userID, instanceID)
		actual := filepath.Clean(strings.TrimSpace(workspace.String))
		if actual == "." || actual == "" {
			actual = expected
		}
		if !sameCleanPath(actual, expected) || !pathWithin(s.workspaceRoot, actual) {
			blockers = append(blockers, fmt.Sprintf("instance %d workspace is outside the fixed managed path", instanceID))
		}
		if _, err := os.Lstat(actual); err != nil {
			blockers = append(blockers, fmt.Sprintf("instance %d workspace is unavailable: %v", instanceID, err))
		}
		candidate := runtimeUpgradeCandidate{InstanceID: instanceID, UserID: userID, InstanceStatus: strings.ToLower(strings.TrimSpace(instanceStatus)), RuntimeGeneration: runtimeGeneration, GatewayID: strings.TrimSpace(gatewayID.String), Generation: runtimeGeneration, BindingState: strings.ToLower(strings.TrimSpace(bindingState.String)), WorkspacePath: actual, MemberKey: memberKey.String, Role: role.String, RuntimeType: runtimeType.String, Availability: availability.String, MemberStatus: status.String}
		if podID.Valid {
			value := podID.Int64
			candidate.RuntimePodID = &value
			candidate.Generation = int(generation.Int64)
			if pod, ok := podByID[value]; ok {
				if scope.UpgradeLabRunID != nil {
					expectedPrefix := fmt.Sprintf("%sr%d-", openClawUpgradeLabDeploymentPrefix, *scope.UpgradeLabRunID)
					if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(pod.DeploymentName)), expectedPrefix) {
						blockers = append(blockers, fmt.Sprintf("instance %d is not bound to upgrade lab run %d", instanceID, *scope.UpgradeLabRunID))
					}
				} else if isOpenClawUpgradeLabDeployment(pod.DeploymentName) {
					blockers = append(blockers, fmt.Sprintf("upgrade lab instance %d cannot enter a production rollout", instanceID))
				}
				stateReady := pod.State == "ready"
				if scope.UpgradeLabRunID != nil {
					stateReady = stateReady || pod.State == "standby"
				}
				if (!stateReady || pod.Draining) && !scope.allowsRecovery(instanceID) {
					blockers = append(blockers, fmt.Sprintf("instance %d source Runtime pod is not ready", instanceID))
				}
				candidate.SourceVersion = resolvedSourceOpenClawVersion(pod)
				candidate.SourcePodUID = stringValue(pod.PodUID)
				candidate.SourceDeployment = strings.TrimSpace(pod.DeploymentName)
				candidate.SourceImageDigest = strings.TrimSpace(stringValue(pod.ImageDigest))
				candidate.AgentEndpoint = stringValue(pod.AgentEndpoint)
				candidate.Capabilities = pod.Capabilities()
				if candidate.SourceVersion == "" {
					blockers = append(blockers, fmt.Sprintf("instance %d source OpenClaw version is unavailable; publish runtime metadata or use the pinned 7.1 baseline image", instanceID))
				}
			}
		}
		if !podID.Valid && candidate.InstanceStatus != "stopped" && !(candidate.InstanceStatus == "error" && scope.allowsRecovery(instanceID)) {
			blockers = append(blockers, fmt.Sprintf("instance %d is %s without a Runtime binding; wait for runtime reconciliation before upgrading", instanceID, candidate.InstanceStatus))
		}
		if !podID.Valid && candidate.InstanceStatus == "error" && scope.allowsRecovery(instanceID) && scope.RecoveryParentID > 0 {
			prior, err := s.recoveryStoppedSource(ctx, scope.RecoveryParentID, instanceID, runtimeGeneration)
			if err != nil {
				blockers = append(blockers, err.Error())
			} else {
				candidate.RuntimePodID, candidate.GatewayID, candidate.Generation = prior.RuntimePodID, stringValue(prior.SourceGatewayID), intValue(prior.SourceGeneration, runtimeGeneration)
				candidate.SourceVersion, candidate.SourcePodUID = stringValue(prior.SourceRuntimeVersion), stringValue(prior.SourcePodUID)
				candidate.SourceDeployment, candidate.SourceImageDigest = stringValue(prior.SourceDeploymentName), stringValue(prior.SourceImageDigest)
				candidate.BindingState = "stopped"
				if pod, ok := podByID[*prior.RuntimePodID]; ok && persistedSourcePodMatches(pod, prior) {
					candidate.AgentEndpoint, candidate.Capabilities = stringValue(pod.AgentEndpoint), pod.Capabilities()
				}
			}
		}
		if podID.Valid && candidate.InstanceStatus != "running" && candidate.InstanceStatus != "stopped" && !(candidate.InstanceStatus == "error" && scope.allowsRecovery(instanceID)) {
			blockers = append(blockers, fmt.Sprintf("instance %d runtime state %s is not stable for upgrade", instanceID, candidate.InstanceStatus))
		}
		if memberID.Valid {
			value := int(memberID.Int64)
			candidate.TeamMemberID = &value
		}
		if teamID.Valid {
			value := int(teamID.Int64)
			candidate.TeamID = &value
			if strings.EqualFold(candidate.Availability, models.TeamMemberAvailabilityBusy) || strings.EqualFold(candidate.MemberStatus, models.TeamMemberStatusBusy) {
				blockers = append(blockers, fmt.Sprintf("Team %d OpenClaw member %d is busy", value, candidate.TeamMemberIDValue()))
			}
			if !strings.EqualFold(strings.TrimSpace(candidate.RuntimeType), RuntimeTypeOpenClaw) {
				blockers = append(blockers, fmt.Sprintf("Team %d member %d runtime type %q conflicts with its OpenClaw instance", value, candidate.TeamMemberIDValue(), candidate.RuntimeType))
			}
		}
		if candidate.GatewayID != "" {
			if candidate.AgentEndpoint == "" {
				blockers = append(blockers, fmt.Sprintf("instance %d has a running gateway but no reachable Runtime Agent", instanceID))
			} else if !containsString(candidate.Capabilities, "openclaw.gateway.stop-confirm") {
				warnings = append(warnings, fmt.Sprintf("instance %d uses the legacy 7.1 synchronous stop path; its Gateway stop will be confirmed immediately before migration", instanceID))
			}
		}
		candidate.Blockers = append([]string(nil), blockers[blockerStart:]...)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, nil, err
	}
	for id := range selectedIDs {
		if _, found := seen[id]; !found {
			blockers = append(blockers, fmt.Sprintf("selected instance %d is unavailable", id))
		}
	}
	for _, c := range candidates {
		if c.TeamID != nil && excludedTeams[*c.TeamID] && scope.RecoveryMode == "" {
			blockers = append(blockers, fmt.Sprintf("Team %d cannot be partially excluded; defer the whole Team", *c.TeamID))
		}
	}
	sourceImages := map[string]string{}
	for _, candidate := range candidates {
		if candidate.RuntimePodID == nil {
			continue
		}
		pod, ok := podByID[*candidate.RuntimePodID]
		if !ok {
			continue
		}
		key := strings.TrimSpace(pod.Namespace) + "/" + strings.TrimSpace(pod.DeploymentName)
		if image := liveImages[key]; image != "" {
			sourceImages[key] = image
		}
	}
	if len(sourceImages) == 0 {
		sourceImages = fallbackSourceImages
	}
	return candidates, sourceImages, warnings, blockers, nil
}

func normalizedPositiveIDs(ids []int) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Ints(result)
	return result
}

func runtimeDeploymentInUpgradeScope(name string, scope runtimeUpgradeScope) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if scope.UpgradeLabRunID == nil {
		return !isOpenClawUpgradeLabDeployment(name)
	}
	expectedPrefix := fmt.Sprintf("%sr%d-", openClawUpgradeLabDeploymentPrefix, *scope.UpgradeLabRunID)
	return strings.HasPrefix(name, expectedPrefix)
}

func runtimeUpgradeScopeFromRollout(rollout *models.RuntimeRollout) runtimeUpgradeScope {
	var values struct {
		UpgradeLabRunID      *int64 `json:"upgrade_lab_run_id"`
		CandidateInstanceIDs []int  `json:"candidate_instance_ids"`
		ExcludeInstanceIDs   []int  `json:"exclude_instance_ids"`
		RecoveryMode         string `json:"recovery_mode"`
		RecoveryParentID     int64  `json:"recovery_parent_id"`
	}
	if rollout != nil && rollout.PreflightJSON != nil {
		_ = json.Unmarshal([]byte(*rollout.PreflightJSON), &values)
	}
	return runtimeUpgradeScope{UpgradeLabRunID: values.UpgradeLabRunID, CandidateInstanceIDs: normalizedPositiveIDs(values.CandidateInstanceIDs), ExcludeInstanceIDs: normalizedPositiveIDs(values.ExcludeInstanceIDs), RecoveryMode: values.RecoveryMode, RecoveryParentID: values.RecoveryParentID}
}

func (s runtimeUpgradeScope) allowsRecovery(id int) bool {
	if s.RecoveryMode != "retry" && s.RecoveryMode != "rebuild" {
		return false
	}
	for _, candidate := range s.CandidateInstanceIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func (s runtimeUpgradeScope) scoped() bool {
	return len(s.CandidateInstanceIDs) > 0 || len(s.ExcludeInstanceIDs) > 0
}

func rolloutExpectedTargetDeployments(rollout *models.RuntimeRollout) (map[string]struct{}, error) {
	if rollout == nil || rollout.SourceImagesJSON == nil {
		return nil, fmt.Errorf("OpenClaw rollout source deployment inventory is unavailable")
	}
	var sources map[string]string
	if json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil || len(sources) == 0 {
		return nil, fmt.Errorf("OpenClaw rollout source deployment inventory is unavailable")
	}
	result := make(map[string]struct{}, len(sources))
	for key := range sources {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid OpenClaw source deployment entry %q", key)
		}
		result[parts[0]+"/"+runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID)] = struct{}{}
	}
	return result, nil
}

func (c runtimeUpgradeCandidate) TeamMemberIDValue() int {
	if c.TeamMemberID == nil {
		return 0
	}
	return *c.TeamMemberID
}

func sortRuntimeUpgradeCandidates(candidates []runtimeUpgradeCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		leftTeam, rightTeam := 0, 0
		if candidates[i].TeamID != nil {
			leftTeam = *candidates[i].TeamID
		}
		if candidates[j].TeamID != nil {
			rightTeam = *candidates[j].TeamID
		}
		if leftTeam != rightTeam {
			return leftTeam < rightTeam
		}
		leftLeader := isTeamLeaderRole(candidates[i].Role)
		rightLeader := isTeamLeaderRole(candidates[j].Role)
		if leftLeader != rightLeader {
			return !leftLeader
		}
		return candidates[i].InstanceID < candidates[j].InstanceID
	})
}

func (s *RuntimeUpgradeService) inspectTeamConsistency(ctx context.Context, teamIDs map[int]struct{}) (int, []string, error) {
	hermesCount := 0
	var blockers []string
	for teamID := range teamIDs {
		var activeTasks int
		row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_tasks WHERE team_id = ? AND status IN ('pending','dispatched','running','waiting')`, teamID)
		if err != nil {
			return 0, nil, err
		}
		if err := row.Scan(&activeTasks); err != nil {
			return 0, nil, err
		}
		if activeTasks > 0 {
			blockers = append(blockers, fmt.Sprintf("Team %d has %d active task(s)", teamID, activeTasks))
		}
		var activeWorkItems int
		row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_work_items WHERE team_id = ? AND status IN ('dispatched','running','waiting')`, teamID)
		if err != nil {
			return 0, nil, err
		}
		if err := row.Scan(&activeWorkItems); err != nil {
			return 0, nil, err
		}
		if activeWorkItems > 0 {
			blockers = append(blockers, fmt.Sprintf("Team %d has %d active assignment(s)", teamID, activeWorkItems))
		}
		var pendingOutbox int
		row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_event_outbox WHERE team_id = ? AND status <> 'delivered'`, teamID)
		if err != nil {
			return 0, nil, err
		}
		if err := row.Scan(&pendingOutbox); err != nil {
			return 0, nil, err
		}
		if pendingOutbox > 0 {
			blockers = append(blockers, fmt.Sprintf("Team %d has %d pending outbox event(s)", teamID, pendingOutbox))
		}
		rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id, runtime_type, role, availability, status FROM team_members WHERE team_id = ? AND status NOT IN ('deleted','deleting')`, teamID)
		if err != nil {
			return 0, nil, err
		}
		leaderCount := 0
		openClawLeaderCount := 0
		for rows.Next() {
			var id int
			var runtimeType, role, availability, status string
			if err := rows.Scan(&id, &runtimeType, &role, &availability, &status); err != nil {
				rows.Close()
				return 0, nil, err
			}
			if isTeamLeaderRole(role) {
				leaderCount++
				if strings.EqualFold(runtimeType, RuntimeTypeOpenClaw) {
					openClawLeaderCount++
				}
			}
			if strings.EqualFold(runtimeType, RuntimeTypeHermes) {
				hermesCount++
				if strings.EqualFold(availability, models.TeamMemberAvailabilityBusy) || strings.EqualFold(status, models.TeamMemberStatusBusy) {
					blockers = append(blockers, fmt.Sprintf("Team %d Hermes member %d is busy", teamID, id))
				}
			}
		}
		rows.Close()
		if leaderCount != 1 {
			blockers = append(blockers, fmt.Sprintf("Team %d must have exactly one Leader", teamID))
		} else if openClawLeaderCount != 1 {
			blockers = append(blockers, fmt.Sprintf("Team %d Leader must remain an OpenClaw intermediary", teamID))
		}
	}
	return hermesCount, blockers, nil
}

// stopCandidateGateway returns stopped=true only after the Runtime Agent has
// confirmed the gateway process is absent. This distinction is required for a
// safe rollback when process stop succeeds but the database binding update
// fails: that instance must be recreated, while an unconfirmed stop must not
// risk a second writer.
func (s *RuntimeUpgradeService) stopCandidateGateway(ctx context.Context, candidate runtimeUpgradeCandidate) (bool, error) {
	if candidate.GatewayID == "" {
		if candidate.RuntimePodID != nil && candidate.BindingState != "running" {
			return true, s.bindings.DeleteByInstanceIDAndReleaseSlot(ctx, candidate.InstanceID, *candidate.RuntimePodID)
		}
		return true, nil
	}
	if candidate.AgentEndpoint == "" || s.agent == nil {
		return false, fmt.Errorf("running gateway cannot be stopped because its Runtime Agent is unavailable")
	}
	deleteErr := s.agent.DeleteGateway(ctx, candidate.AgentEndpoint, candidate.GatewayID)
	reader, canConfirm := s.agent.(gatewayStateReader)
	confirmed := errors.Is(deleteErr, ErrRuntimeAgentNotFound)
	// A 7.1 source agent may return a stop conflict after its process watcher
	// has already recorded the same gateway as stopped.  Query that terminal
	// state whenever Delete reported an error, even if the legacy capability
	// report predates openclaw.gateway.stop-confirm.  New agents retain the
	// stricter confirmation on every stop.
	if canConfirm && (deleteErr != nil || containsString(candidate.Capabilities, "openclaw.gateway.stop-confirm")) {
		var confirmErr error
		confirmed, confirmErr = waitForGatewayStopped(ctx, reader, candidate.AgentEndpoint, candidate.GatewayID, 5*time.Second)
		if !confirmed {
			if deleteErr != nil && !errors.Is(deleteErr, ErrRuntimeAgentNotFound) {
				return false, errors.Join(deleteErr, confirmErr)
			}
			return false, confirmErr
		}
	}
	if deleteErr != nil && !errors.Is(deleteErr, ErrRuntimeAgentNotFound) && !confirmed {
		return false, deleteErr
	}
	if candidate.RuntimePodID != nil {
		if candidate.BindingState == "running" {
			deleted, err := s.bindings.DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx, candidate.InstanceID, *candidate.RuntimePodID, candidate.Generation)
			if err != nil {
				return true, fmt.Errorf("release stopped runtime binding: %w", err)
			}
			if !deleted {
				current, currentErr := s.bindings.GetByInstanceID(ctx, candidate.InstanceID)
				if currentErr != nil {
					return true, fmt.Errorf("inspect concurrently stopped gateway binding: %w", currentErr)
				}
				if current == nil {
					return true, nil
				}
				if current.RuntimePodID != *candidate.RuntimePodID || current.Generation != candidate.Generation || strings.TrimSpace(current.GatewayID) != candidate.GatewayID {
					return true, fmt.Errorf("stopped gateway binding changed identity concurrently")
				}
				if err := s.bindings.DeleteByInstanceIDAndReleaseSlot(ctx, candidate.InstanceID, *candidate.RuntimePodID); err != nil {
					return true, fmt.Errorf("release concurrently stopped gateway binding: %w", err)
				}
			}
		} else if err := s.bindings.DeleteByInstanceIDAndReleaseSlot(ctx, candidate.InstanceID, *candidate.RuntimePodID); err != nil {
			return true, fmt.Errorf("release inactive runtime binding: %w", err)
		}
	}
	return true, nil
}

type gatewayStateReader interface {
	GatewayState(ctx context.Context, endpoint, gatewayID string) (*RuntimeAgentGatewayState, error)
}

// waitForGatewayStopped accepts both forms used by supported Runtime Agents:
// newer agents remove the record after a confirmed stop, while legacy 7.1
// agents can leave an idempotent stopped record behind after their process
// watcher wins the stop race.  Neither state can own a SQLite writer.
func waitForGatewayStopped(ctx context.Context, reader gatewayStateReader, endpoint, gatewayID string, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	lastState := "unknown"
	for {
		state, err := reader.GatewayState(ctx, endpoint, gatewayID)
		if errors.Is(err, ErrRuntimeAgentNotFound) {
			return true, nil
		}
		if err == nil && state != nil {
			lastState = strings.ToLower(strings.TrimSpace(state.State))
			if lastState == "stopped" {
				return true, nil
			}
		} else if err != nil {
			lastState = "query_error: " + err.Error()
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("gateway stop was not confirmed within %s (last state %s)", timeout, lastState)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *RuntimeUpgradeService) snapshotCandidate(ctx context.Context, rolloutID int64, candidate runtimeUpgradeCandidate) (string, runtimeWorkspaceInventory, error) {
	if fullWorkspaceSnapshotsDisabled() {
		return "", runtimeWorkspaceInventory{}, errors.New("full workspace snapshots are disabled; use the OpenClaw session migration capsule")
	}
	localInventory, localInventoryErr := inspectRuntimeWorkspace(candidate.WorkspacePath)
	if localInventoryErr != nil {
		return "", localInventory, localInventoryErr
	}
	if upgradeAgent, ok := s.agent.(RuntimeUpgradeAgentClient); ok && candidate.AgentEndpoint != "" && containsString(candidate.Capabilities, "openclaw.workspace.snapshot-v1") {
		if !containsString(candidate.Capabilities, "openclaw.workspace.writer-lease") {
			return "", localInventory, fmt.Errorf("Runtime Agent advertises snapshots without the required writer lease capability")
		}
		leaseToken, err := randomUpgradeID()
		if err != nil {
			return "", localInventory, err
		}
		request := RuntimeAgentWorkspaceRequest{
			RolloutID: strconv.FormatInt(rolloutID, 10), SnapshotID: fmt.Sprintf("instance-%d-generation-%d", candidate.InstanceID, candidate.Generation),
			UserID: candidate.UserID, InstanceID: candidate.InstanceID, Generation: candidate.Generation,
			LeaseToken: leaseToken, OfficialDBCheck: true,
		}
		leaseRequest := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: request, Token: leaseToken, TTLSeconds: 3600}
		if err := upgradeAgent.AcquireWriterLease(ctx, candidate.AgentEndpoint, leaseRequest); err != nil {
			return "", localInventory, fmt.Errorf("acquire workspace writer lease: %w", err)
		}
		release := func() error { return upgradeAgent.ReleaseWriterLease(ctx, candidate.AgentEndpoint, leaseRequest) }
		if _, err := upgradeAgent.PreflightWorkspace(ctx, candidate.AgentEndpoint, request); err != nil {
			_ = release()
			return "", localInventory, fmt.Errorf("Runtime Agent workspace preflight: %w", err)
		}
		snapshot, err := upgradeAgent.CreateWorkspaceSnapshot(ctx, candidate.AgentEndpoint, RuntimeAgentSnapshotRequest{RuntimeAgentWorkspaceRequest: request})
		if err != nil {
			_ = release()
			return "", localInventory, fmt.Errorf("Runtime Agent workspace snapshot: %w", err)
		}
		verifyRequest := request
		verifyRequest.SnapshotID = snapshot.SnapshotID
		if err := upgradeAgent.VerifyWorkspaceSnapshot(ctx, candidate.AgentEndpoint, RuntimeAgentSnapshotVerifyRequest{RuntimeAgentWorkspaceRequest: verifyRequest}); err != nil {
			_ = release()
			return "", localInventory, fmt.Errorf("verify Runtime Agent workspace snapshot: %w", err)
		}
		if !pathWithin(filepath.Join(s.workspaceRoot, ".clawmanager-upgrades"), snapshot.ArchivePath) {
			_ = release()
			return "", localInventory, fmt.Errorf("Runtime Agent snapshot path is outside the shared managed upgrade root")
		}
		if _, err := os.Stat(snapshot.ArchivePath); err != nil {
			_ = release()
			return "", localInventory, fmt.Errorf("Runtime Agent snapshot is not visible to the controller: %w", err)
		}
		if err := release(); err != nil {
			return "", localInventory, fmt.Errorf("release workspace writer lease: %w", err)
		}
		return "local:" + snapshot.ArchivePath, localInventory, nil
	}
	return s.createLocalSnapshot(rolloutID, candidate)
}

func (s *RuntimeUpgradeService) createLocalSnapshot(rolloutID int64, candidate runtimeUpgradeCandidate) (string, runtimeWorkspaceInventory, error) {
	if fullWorkspaceSnapshotsDisabled() {
		return "", runtimeWorkspaceInventory{}, errors.New("full workspace snapshots are disabled; use the OpenClaw session migration capsule")
	}
	inventory, err := inspectRuntimeWorkspace(candidate.WorkspacePath)
	if err != nil {
		return "", inventory, err
	}
	destination := filepath.Join(s.workspaceRoot, ".clawmanager-upgrades", fmt.Sprintf("rollout-%d", rolloutID))
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", inventory, err
	}
	archivePath := filepath.Join(destination, fmt.Sprintf("controller-instance-%d.tar.gz", candidate.InstanceID))
	tmpPath := archivePath + ".tmp"
	if _, err := os.Lstat(tmpPath); err == nil {
		return "", inventory, fmt.Errorf("stale snapshot temp file exists")
	}
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", inventory, err
	}
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(tmpPath)
		}
	}()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	err = filepath.Walk(candidate.WorkspacePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(candidate.WorkspacePath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, source)
		closeErr := source.Close()
		return errors.Join(copyErr, closeErr)
	})
	if closeErr := tw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Sync(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", inventory, err
	}
	after, err := inspectRuntimeWorkspace(candidate.WorkspacePath)
	if err != nil {
		return "", inventory, err
	}
	if after.ManifestSHA256 != inventory.ManifestSHA256 {
		return "", inventory, fmt.Errorf("workspace changed while snapshot was being created")
	}
	if err := os.Rename(tmpPath, archivePath); err != nil {
		return "", inventory, err
	}
	failed = false
	return "local:" + archivePath, inventory, nil
}

func (s *RuntimeUpgradeService) restoreLocalSnapshot(instanceID int, snapshotRef string) error {
	if fullWorkspaceSnapshotsDisabled() {
		return errors.New("full workspace restore is disabled; use the OpenClaw session migration restore")
	}
	if strings.HasPrefix(snapshotRef, "agent:") {
		return fmt.Errorf("agent snapshot restore requires the original runtime agent and is not available through local fallback")
	}
	archivePath := strings.TrimPrefix(snapshotRef, "local:")
	if !pathWithin(filepath.Join(s.workspaceRoot, ".clawmanager-upgrades"), archivePath) {
		return fmt.Errorf("snapshot path is outside the managed upgrade root")
	}
	workspace, err := s.workspaceForInstance(instanceID)
	if err != nil {
		return err
	}
	staging := workspace + fmt.Sprintf(".restore-%d", time.Now().UTC().UnixNano())
	preserved := workspace + fmt.Sprintf(".preserved-%d", time.Now().UTC().UnixNano())
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(staging, filepath.FromSlash(header.Name))
		if !pathWithin(staging, target) {
			return fmt.Errorf("snapshot contains an unsafe path")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) {
				return fmt.Errorf("snapshot contains an absolute symlink")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
		default:
			return fmt.Errorf("snapshot contains unsupported entry type")
		}
	}
	if err := os.Rename(workspace, preserved); err != nil {
		return err
	}
	if err := os.Rename(staging, workspace); err != nil {
		_ = os.Rename(preserved, workspace)
		return err
	}
	return nil
}

func fullWorkspaceSnapshotsDisabled() bool {
	return true
}

func verifyLocalSnapshotArchive(workspaceRoot, snapshotRef string) error {
	if strings.HasPrefix(snapshotRef, "agent:") {
		return fmt.Errorf("agent-only snapshot reference cannot be verified locally")
	}
	archivePath := strings.TrimPrefix(strings.TrimSpace(snapshotRef), "local:")
	if !pathWithin(filepath.Join(workspaceRoot, ".clawmanager-upgrades"), archivePath) {
		return fmt.Errorf("snapshot path is outside the managed upgrade root")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("snapshot contains an unsafe path")
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			if _, err := io.CopyN(io.Discard, reader, header.Size); err != nil {
				return err
			}
		}
	}
}

func inspectRuntimeWorkspace(root string) (runtimeWorkspaceInventory, error) {
	var inventory runtimeWorkspaceInventory
	manifest := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			inventory.SymlinkCount++
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(manifest, "%s|symlink|%s\n", filepath.ToSlash(rel), link)
			return nil
		}
		if info.IsDir() {
			inventory.DirectoryCount++
			fmt.Fprintf(manifest, "%s|dir|%o\n", filepath.ToSlash(rel), info.Mode().Perm())
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported workspace entry %s", rel)
		}
		inventory.FileCount++
		inventory.TotalBytes += info.Size()
		fileHash := sha256.New()
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(fileHash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return err
		}
		fmt.Fprintf(manifest, "%s|file|%o|%d|%x\n", filepath.ToSlash(rel), info.Mode().Perm(), info.Size(), fileHash.Sum(nil))
		if isSQLiteMainDatabase(info.Name()) {
			inventory.SQLiteFileCount++
			if err := validateSQLiteHeader(path); err != nil {
				return fmt.Errorf("SQLite header check failed for %s", filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return inventory, err
	}
	inventory.SQLiteHeaderValid = true
	inventory.ManifestSHA256 = hex.EncodeToString(manifest.Sum(nil))
	return inventory, nil
}

func validateRuntimeWorkspaceSQLite(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || !info.Mode().IsRegular() || !isSQLiteMainDatabase(info.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if err := validateSQLiteHeader(path); err != nil {
			return fmt.Errorf("SQLite header check failed for %s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func isSQLiteMainDatabase(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, suffix := range []string{".sqlite-wal", ".sqlite-shm", ".sqlite-journal", ".sqlite3-wal", ".sqlite3-shm", ".sqlite3-journal", ".db-wal", ".db-shm", ".db-journal"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	for _, suffix := range []string{".lock.sqlite", ".lock.sqlite3", ".lock.db"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	return strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".sqlite3") || strings.HasSuffix(lower, ".db")
}

func validateSQLiteHeader(path string) error {
	dbFile, err := os.Open(path)
	if err != nil {
		return err
	}
	header := make([]byte, 16)
	_, readErr := io.ReadFull(dbFile, header)
	closeErr := dbFile.Close()
	if readErr != nil || closeErr != nil || string(header) != "SQLite format 3\x00" {
		return errors.Join(readErr, closeErr, fmt.Errorf("invalid SQLite header"))
	}
	return nil
}

func (s *RuntimeUpgradeService) setTeamMaintenance(ctx context.Context, teamIDs map[int]struct{}, rolloutID int64, enabled bool) error {
	if len(teamIDs) == 0 {
		return nil
	}
	ids := make([]int, 0, len(teamIDs))
	for teamID := range teamIDs {
		ids = append(ids, teamID)
	}
	sort.Ints(ids)
	if s.teamMaintenance != nil {
		return s.teamMaintenance.SetRuntimeUpgradeMaintenance(ctx, ids, rolloutID, enabled)
	}
	if s.redisURL == "" {
		return fmt.Errorf("Team maintenance requires PLATFORM_REDIS_URL/TEAM_REDIS_URL")
	}
	bus, err := newRedisBus(s.redisURL)
	if err != nil {
		return err
	}
	for teamID := range teamIDs {
		key := fmt.Sprintf("claw:team:%d:maintenance", teamID)
		if enabled {
			value, _ := json.Marshal(map[string]any{"enabled": true, "rolloutId": rolloutID, "reason": "openclaw_runtime_upgrade", "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)})
			if err := bus.Set(ctx, key, string(value), 0); err != nil {
				return err
			}
		} else {
			if _, err := bus.do(ctx, "EVAL", releaseUpgradeMaintenanceScript, "1", key, strconv.FormatInt(rolloutID, 10)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *RuntimeUpgradeService) insertUpgradeItems(ctx context.Context, session db.Session, rolloutID int64, candidates []runtimeUpgradeCandidate, targetVersion string) error {
	now := time.Now().UTC()
	for _, candidate := range candidates {
		leader := isTeamLeaderRole(candidate.Role)
		order := candidate.InstanceID
		item := &models.RuntimeUpgradeItem{RolloutID: rolloutID, TeamID: candidate.TeamID, TeamMemberID: candidate.TeamMemberID, InstanceID: candidate.InstanceID, RuntimePodID: candidate.RuntimePodID, SourceGatewayID: upgradeStringPtrOrNil(candidate.GatewayID), SourceGeneration: &candidate.Generation, SourcePodUID: upgradeStringPtrOrNil(candidate.SourcePodUID), SourceDeploymentName: upgradeStringPtrOrNil(candidate.SourceDeployment), SourceImageDigest: upgradeStringPtrOrNil(candidate.SourceImageDigest), MemberOrder: order, IsTeamLeader: leader, SourceRuntimeVersion: upgradeStringPtrOrNil(candidate.SourceVersion), TargetRuntimeVersion: upgradeStringPtrOrNil(targetVersion), State: "pending", CreatedAt: now, UpdatedAt: now}
		item.ErrorMessage = upgradeStringPtrOrNil(strings.Join(candidate.Blockers, "; "))
		if _, err := session.Collection("runtime_upgrade_items").Insert(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeUpgradeService) listUpgradeItems(ctx context.Context, rolloutID int64) ([]models.RuntimeUpgradeItem, error) {
	// A legacy rollout and the OpenClaw empty-pool reset intentionally have no
	// per-instance rows. Keep their JSON representation stable for all clients.
	items := make([]models.RuntimeUpgradeItem, 0)
	if err := s.sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": rolloutID}).OrderBy("team_id", "is_team_leader", "member_order", "id").All(&items); err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]models.RuntimeUpgradeItem, 0)
	}
	if ids := upgradeGroupScope(ctx); ids != nil {
		scoped := items[:0]
		for _, i := range items {
			if ids[i.InstanceID] {
				scoped = append(scoped, i)
			}
		}
		return scoped, nil
	}
	return items, nil
}

func (s *RuntimeUpgradeService) updateUpgradeItemSnapshot(ctx context.Context, rolloutID int64, instanceID int, snapshot string, inventory runtimeWorkspaceInventory) error {
	raw, _ := json.Marshal(inventory)
	now := time.Now().UTC()
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'snapshotted', snapshot_ref = ?, workspace_manifest_sha256 = ?, preflight_json = ?, started_at = COALESCE(started_at, ?), updated_at = ? WHERE rollout_id = ? AND instance_id = ?`, snapshot, inventory.ManifestSHA256, string(raw), now, now, rolloutID, instanceID)
	return err
}

func (s *RuntimeUpgradeService) markUpgradeItemRestartReady(ctx context.Context, rolloutID int64, instanceID int) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		now := time.Now().UTC()
		result, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'restart_ready', updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'migrated'`, now, rolloutID, instanceID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("instance %d could not enter restart_ready", instanceID)
		}
		result, err = tx.SQL().ExecContext(ctx, `UPDATE instances SET status = 'creating', runtime_generation = runtime_generation + 1, runtime_error_message = NULL, updated_at = ? WHERE id = ? AND LOWER(TRIM(status)) <> 'deleting'`, now, instanceID)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return fmt.Errorf("instance %d could not enter runtime recreation", instanceID)
		}
		return nil
	}, nil)
}

func (s *RuntimeUpgradeService) markUpgradeItemGatewayVerified(ctx context.Context, rolloutID int64, instanceID int) error {
	result, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET state = 'gateway_verified', recovery_status = 'verified', recovery_error_code = NULL, error_message = NULL, restart_next_retry_at = NULL, updated_at = ? WHERE rollout_id = ? AND instance_id = ? AND state = 'restart_ready'`, time.Now().UTC(), rolloutID, instanceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("instance %d gateway verification state changed concurrently", instanceID)
	}
	return nil
}

func (s *RuntimeUpgradeService) updateRolloutPhase(ctx context.Context, rolloutID int64, phase string) error {
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET phase = ?, updated_at = ? WHERE id = ?`, phase, time.Now().UTC(), rolloutID)
	return err
}

func (s *RuntimeUpgradeService) audit(ctx context.Context, rolloutID *int64, actor *int, action, phase, outcome string, detail map[string]any) error {
	return insertRuntimeUpgradeAudit(ctx, s.sess, rolloutID, actor, action, phase, outcome, detail)
}

func insertRuntimeUpgradeAudit(ctx context.Context, sess db.Session, rolloutID *int64, actor *int, action, phase, outcome string, detail map[string]any) error {
	raw, _ := json.Marshal(detail)
	_, err := sess.SQL().ExecContext(ctx, `INSERT INTO runtime_upgrade_audits (rollout_id, actor_user_id, action, phase, outcome, detail_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, rolloutID, actor, action, phase, outcome, string(raw), time.Now().UTC())
	return err
}

func imageDigestFromReference(value string) string {
	index := strings.LastIndex(strings.TrimSpace(value), "@sha256:")
	if index < 0 {
		return ""
	}
	digest := value[index+1:]
	hexPart := strings.TrimPrefix(digest, "sha256:")
	if len(hexPart) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return ""
	}
	return digest
}

// Runtime agents report both the Deployment image reference and the image ID
// resolved by the container runtime. A source Deployment commonly keeps its
// human-readable tag while the rollout journal stores the immutable digest;
// comparing the two raw strings leaves a successful rollback stuck forever.
func runtimePodMatchesImage(pod models.RuntimePod, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	if expectedDigest := imageDigestFromReference(expected); expectedDigest != "" {
		actualDigest := strings.TrimSpace(stringValue(pod.ImageDigest))
		if actualDigest == "" {
			actualDigest = imageDigestFromReference(pod.ImageRef)
		}
		return strings.EqualFold(actualDigest, expectedDigest)
	}
	return strings.TrimSpace(pod.ImageRef) == expected
}

type registryDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  *struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform,omitempty"`
}

type registryManifestDocument struct {
	MediaType string               `json:"mediaType"`
	Config    registryDescriptor   `json:"config"`
	Manifests []registryDescriptor `json:"manifests"`
}

type registryImageConfig struct {
	Config struct {
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"config"`
}

func (s *RuntimeUpgradeService) ClassifyOpenClawTarget(ctx context.Context, target string) (*OpenClawTargetClassification, error) {
	if s == nil || s.deployments == nil {
		return nil, fmt.Errorf("live runtime deployment inventory is not configured")
	}
	livePods, err := s.deployments.RuntimeDeploymentPods(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return nil, fmt.Errorf("inspect live OpenClaw deployments: %w", err)
	}
	sourceImages := make(map[string]string)
	sourceImagePinErrors := make(map[string]error)
	for _, pod := range livePods {
		if !runtimePodEligibleForOrdinaryScheduling(pod) {
			continue
		}
		if strings.TrimSpace(pod.ImageRef) != "" {
			key := strings.TrimSpace(pod.Namespace) + "/" + strings.TrimSpace(pod.DeploymentName)
			pinned, pinErr := immutableRuntimeImage(pod.ImageRef, stringValue(pod.ImageDigest))
			if pinErr != nil {
				sourceImages[key] = strings.TrimSpace(pod.ImageRef)
				sourceImagePinErrors[key] = pinErr
			} else {
				sourceImages[key] = pinned
			}
		}
	}
	if len(sourceImages) == 0 {
		return nil, fmt.Errorf("no current OpenClaw deployment image is available to authorize the target registry")
	}
	// With no ordinary Lite instances, Team references, Gateway occupancy,
	// bindings or active rollout, this is an image-only operation. Version and
	// migration-contract metadata are not needed because no user state moves.
	emptyPoolReset := s.ValidateEmptyOpenClawPoolReset(ctx, 0) == nil
	classification, err := inspectOpenClawRegistryImage(ctx, target, sourceImages, emptyPoolReset)
	if err != nil {
		return nil, err
	}
	classification.SourceImages = sourceImages
	if emptyPoolReset {
		if len(sourceImagePinErrors) > 0 {
			keys := make([]string, 0, len(sourceImagePinErrors))
			for key := range sourceImagePinErrors {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			return nil, fmt.Errorf("empty OpenClaw pool reset requires an immutable source image for %s: %w", keys[0], sourceImagePinErrors[keys[0]])
		}
		classification.Strategy = RuntimeUpgradeStrategyLegacyRolling
		classification.Protocol = ""
		classification.EmptyPoolReset = true
		return classification, nil
	}
	if classification.Strategy == RuntimeUpgradeStrategyLegacyRolling {
		sourceClassifications := map[string]*OpenClawTargetClassification{}
		active8Plus := false
		for _, pod := range livePods {
			if !runtimePodEligibleForOrdinaryScheduling(pod) {
				continue
			}
			if openClawVersionAtLeast(stringValue(pod.OpenClawVersion), targetOpenClawUpgradeVersion) {
				active8Plus = true
				continue
			}
			key := strings.TrimSpace(pod.Namespace) + "/" + strings.TrimSpace(pod.DeploymentName)
			source := sourceImages[key]
			if source == "" {
				continue
			}
			current, inspected := sourceClassifications[source]
			if !inspected {
				current, err = inspectOpenClawRegistryImage(ctx, source, sourceImages, false)
				if err != nil {
					return nil, fmt.Errorf("inspect current OpenClaw image %s before downgrade: %w", key, err)
				}
				sourceClassifications[source] = current
			}
			if openClawVersionAtLeast(current.RuntimeVersion, targetOpenClawUpgradeVersion) {
				active8Plus = true
			}
		}
		if active8Plus {
			if len(sourceImagePinErrors) > 0 {
				keys := make([]string, 0, len(sourceImagePinErrors))
				for key := range sourceImagePinErrors {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				return nil, fmt.Errorf("empty OpenClaw pool reset requires an immutable source image for %s: %w", keys[0], sourceImagePinErrors[keys[0]])
			}
			if err := s.ValidateEmptyOpenClawPoolReset(ctx, 0); err != nil {
				return nil, fmt.Errorf("downgrading an active OpenClaw 2026.8.1+ Runtime is allowed only after its ordinary Lite pool is empty: %w", err)
			}
			classification.EmptyPoolReset = true
		}
	}
	return classification, nil
}

func inspectOpenClawRegistryImage(ctx context.Context, target string, sourceImages map[string]string, imageOnlyReset bool) (*OpenClawTargetClassification, error) {
	host, repository, tag, err := splitRegistryImage(target)
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, source := range sourceImages {
		sourceHost, _, _, sourceErr := splitRegistryImage(source)
		if sourceErr == nil && strings.EqualFold(sourceHost, host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("registry host %q differs from the live OpenClaw registry", host)
	}
	reference := tag
	if digest := imageDigestFromReference(target); digest != "" {
		reference = digest
	}
	if reference == "" {
		return nil, fmt.Errorf("image tag or digest is required")
	}
	client, baseURL, err := safeRegistryClient(host, repository)
	if err != nil {
		return nil, err
	}
	manifest, manifestDigest, err := fetchRegistryManifest(ctx, client, baseURL, reference)
	if err != nil {
		return nil, err
	}
	rootDigest := manifestDigest
	if existing := imageDigestFromReference(target); existing != "" {
		rootDigest = existing
	}
	if len(manifest.Manifests) > 0 {
		selected := manifest.Manifests[0]
		for _, candidate := range manifest.Manifests {
			if candidate.Platform != nil && candidate.Platform.OS == "linux" && candidate.Platform.Architecture == "amd64" {
				selected = candidate
				break
			}
		}
		if imageDigestFromReference("image@"+selected.Digest) == "" {
			return nil, fmt.Errorf("registry image index contains an invalid platform digest")
		}
		manifest, _, err = fetchRegistryManifest(ctx, client, baseURL, selected.Digest)
		if err != nil {
			return nil, err
		}
	}
	if imageDigestFromReference("image@"+rootDigest) == "" || imageDigestFromReference("image@"+manifest.Config.Digest) == "" {
		return nil, fmt.Errorf("registry image manifest is missing a valid digest or config")
	}
	configURL := baseURL + "/blobs/" + url.PathEscape(manifest.Config.Digest)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("registry config returned status %d", response.StatusCode)
	}
	var config registryImageConfig
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode registry image config: %w", err)
	}
	env := make(map[string]string)
	for _, item := range config.Config.Env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	runtimeType := strings.TrimSpace(config.Config.Labels["io.clawmanager.runtime.type"])
	if runtimeType == "" {
		runtimeType = strings.TrimSpace(env["CLAWMANAGER_RUNTIME_TYPE"])
	}
	if !strings.EqualFold(runtimeType, RuntimeTypeOpenClaw) {
		return nil, fmt.Errorf("target image metadata identifies runtime type %q, want openclaw", runtimeType)
	}
	version := strings.TrimSpace(config.Config.Labels["io.clawmanager.openclaw.version"])
	if version == "" {
		version = strings.TrimSpace(env["CLAWMANAGER_OPENCLAW_VERSION"])
	}
	strategy := RuntimeUpgradeStrategyLegacyRolling
	protocol := ""
	imageStrategy := strings.TrimSpace(config.Config.Labels["io.clawmanager.upgrade.strategy"])
	imageProtocol := strings.TrimSpace(config.Config.Labels["io.clawmanager.upgrade.protocol"])
	_, versionOK := parseOpenClawNumericVersion(version)
	if !imageOnlyReset && !versionOK && (imageStrategy != "" || imageProtocol != "") {
		return nil, fmt.Errorf("target OpenClaw image publishes an incomplete data-safe upgrade contract without a valid runtime version")
	}
	if !imageOnlyReset && versionOK && openClawVersionAtLeast(version, targetOpenClawUpgradeVersion) {
		protocol = imageProtocol
		if imageStrategy != openClawDataSafeImageStrategy || protocol != openClawDataSafeProtocol {
			return nil, fmt.Errorf("OpenClaw %s image lacks the supported data-safe upgrade contract", version)
		}
		strategy = RuntimeUpgradeStrategyOpenClawDataSafe
	}
	return &OpenClawTargetClassification{
		Strategy: strategy, ImageRef: host + "/" + repository + "@" + rootDigest,
		ImageDigest: rootDigest, RuntimeVersion: version, Protocol: protocol,
	}, nil
}

func safeRegistryClient(host, repository string) (*http.Client, string, error) {
	scheme := "https"
	hostname := host
	if parsedHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		hostname = parsedHost
	}
	ip := net.ParseIP(strings.Trim(hostname, "[]"))
	if strings.EqualFold(hostname, "localhost") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback())) {
		scheme = "http"
	}
	parts := strings.Split(repository, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, scheme + "://" + host + "/v2/" + strings.Join(parts, "/"), nil
}

func fetchRegistryManifest(ctx context.Context, client *http.Client, baseURL, reference string) (registryManifestDocument, string, error) {
	var manifest registryManifestDocument
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/manifests/"+url.PathEscape(reference), nil)
	if err != nil {
		return manifest, "", err
	}
	request.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	response, err := client.Do(request)
	if err != nil {
		return manifest, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return manifest, "", fmt.Errorf("registry returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return manifest, "", err
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return manifest, "", fmt.Errorf("decode registry manifest: %w", err)
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if imageDigestFromReference("image@"+digest) == "" {
		sum := sha256.Sum256(body)
		digest = "sha256:" + hex.EncodeToString(sum[:])
	}
	return manifest, digest, nil
}

func parseOpenClawNumericVersion(raw string) ([3]int, bool) {
	var result [3]int
	raw = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(raw), "v"))
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	if len(parts) < 3 {
		return result, false
	}
	for index := 0; index < 3; index++ {
		value, err := strconv.Atoi(parts[index])
		if err != nil || value < 0 {
			return result, false
		}
		result[index] = value
	}
	return result, true
}

func openClawVersionAtLeast(current, required string) bool {
	left, leftOK := parseOpenClawNumericVersion(current)
	right, rightOK := parseOpenClawNumericVersion(required)
	if !leftOK || !rightOK {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return true
}

// resolvedSourceOpenClawVersion never infers a data format from an arbitrary
// tag. Legacy agents did not report a version, so only the operator-pinned 7.1
// baseline digest is safe to map. All 8.1+ images must publish their version in
// the Runtime heartbeat before they can enter the data-safe upgrade path.
func resolvedSourceOpenClawVersion(pod models.RuntimePod) string {
	if reported := strings.TrimSpace(stringValue(pod.OpenClawVersion)); reported != "" {
		return reported
	}
	digest := strings.TrimSpace(stringValue(pod.ImageDigest))
	if digest == "" {
		digest = imageDigestFromReference(pod.ImageRef)
	}
	if strings.EqualFold(digest, openClawLegacyBaselineDigest) {
		return openClawLegacyBaselineRuntimeVersion
	}
	return ""
}

func rolloutTargetMetadata(rollout *models.RuntimeRollout) (string, string) {
	if rollout == nil || rollout.PreflightJSON == nil {
		return "", ""
	}
	var values struct {
		TargetRuntimeVersion  string `json:"target_runtime_version"`
		TargetUpgradeProtocol string `json:"target_upgrade_protocol"`
	}
	if json.Unmarshal([]byte(*rollout.PreflightJSON), &values) != nil {
		return "", ""
	}
	return strings.TrimSpace(values.TargetRuntimeVersion), strings.TrimSpace(values.TargetUpgradeProtocol)
}

func resolveRuntimeImageReference(ctx context.Context, target string, sourceImages map[string]string) (string, error) {
	target = strings.TrimSpace(target)
	host, repository, tag, err := splitRegistryImage(target)
	if err != nil {
		return "", err
	}
	allowed := false
	for _, source := range sourceImages {
		sourceHost, _, _, sourceErr := splitRegistryImage(source)
		if sourceErr == nil && strings.EqualFold(sourceHost, host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("registry host %q differs from the live OpenClaw registry", host)
	}
	if digest := imageDigestFromReference(target); digest != "" {
		return host + "/" + repository + "@" + digest, nil
	}
	if tag == "" {
		return "", fmt.Errorf("image tag is required")
	}
	scheme := "https"
	hostname := host
	if parsedHost, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		hostname = parsedHost
	}
	ip := net.ParseIP(strings.Trim(hostname, "[]"))
	if strings.EqualFold(hostname, "localhost") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback())) {
		scheme = "http"
	}
	pathParts := strings.Split(repository, "/")
	for index := range pathParts {
		pathParts[index] = url.PathEscape(pathParts[index])
	}
	manifestURL := scheme + "://" + host + "/v2/" + strings.Join(pathParts, "/") + "/manifests/" + url.PathEscape(tag)
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("registry returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if imageDigestFromReference("image@"+digest) == "" {
		sum := sha256.Sum256(body)
		digest = "sha256:" + hex.EncodeToString(sum[:])
	}
	return host + "/" + repository + "@" + digest, nil
}

func splitRegistryImage(value string) (host, repository, tag string, err error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") || strings.ContainsAny(value, " \t\r\n") {
		return "", "", "", fmt.Errorf("invalid registry image reference")
	}
	slash := strings.Index(value, "/")
	if slash <= 0 || slash == len(value)-1 {
		return "", "", "", fmt.Errorf("registry host and repository are required")
	}
	host = value[:slash]
	remainder := value[slash+1:]
	if strings.Contains(host, "@") || strings.Contains(host, "\\") || strings.Contains(remainder, "\\") || strings.Contains(remainder, "..") {
		return "", "", "", fmt.Errorf("invalid registry image reference")
	}
	if at := strings.LastIndex(remainder, "@sha256:"); at >= 0 {
		repository = remainder[:at]
		return host, repository, "", nil
	}
	lastSlash := strings.LastIndex(remainder, "/")
	if colon := strings.LastIndex(remainder, ":"); colon > lastSlash {
		repository, tag = remainder[:colon], remainder[colon+1:]
	} else {
		repository = remainder
	}
	if repository == "" {
		return "", "", "", fmt.Errorf("repository is required")
	}
	return host, repository, tag, nil
}

func immutableRuntimeImage(imageRef, digest string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	digest = strings.TrimSpace(digest)
	if imageRef == "" {
		return "", fmt.Errorf("image reference is empty")
	}
	if existing := imageDigestFromReference(imageRef); existing != "" {
		return imageRef, nil
	}
	if !strings.HasPrefix(digest, "sha256:") || imageDigestFromReference("image@"+digest) == "" {
		return "", fmt.Errorf("runtime did not report a valid sha256 image digest")
	}
	if at := strings.Index(imageRef, "@"); at >= 0 {
		imageRef = imageRef[:at]
	}
	lastSlash := strings.LastIndex(imageRef, "/")
	if colon := strings.LastIndex(imageRef, ":"); colon > lastSlash {
		imageRef = imageRef[:colon]
	}
	if strings.TrimSpace(imageRef) == "" {
		return "", fmt.Errorf("image repository is empty")
	}
	return imageRef + "@" + digest, nil
}

func runtimeUpgradeFingerprint(target string, candidates []runtimeUpgradeCandidate, sourceImages map[string]string) string {
	type fingerprintCandidate struct {
		InstanceID int
		UserID     int
		Workspace  string
		TeamID     int
		Member     string
		Role       string
		Generation int
		Binding    string
	}
	values := make([]fingerprintCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		teamID := 0
		if candidate.TeamID != nil {
			teamID = *candidate.TeamID
		}
		values = append(values, fingerprintCandidate{candidate.InstanceID, candidate.UserID, filepath.Clean(candidate.WorkspacePath), teamID, candidate.MemberKey, candidate.Role, candidate.Generation, candidate.BindingState})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].InstanceID < values[j].InstanceID })
	payload, _ := json.Marshal(map[string]any{"target": strings.TrimSpace(target), "candidates": values, "sources": sourceImages})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func randomUpgradeID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func sameCleanPath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
func upgradeStringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
