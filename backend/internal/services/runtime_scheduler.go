package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type RuntimeScheduler struct {
	instanceRepo repository.InstanceRepository
	podRepo      repository.RuntimePodRepository
	bindingRepo  repository.InstanceRuntimeBindingRepository
	rolloutRepo  repository.RuntimeRolloutRepository
	agentClient  RuntimeAgentClient
	events       RuntimeEventService
	leader       RuntimeLeaderService
	deployments  k8s.RuntimeDeploymentService
	envBuilder   RuntimeGatewayEnvBuilder
	upgrade      *RuntimeUpgradeService
	proxyPolicy  *RuntimeProxyPolicyCoordinator
	tick         time.Duration

	workspaceRoot             string
	runtimeNamespace          string
	gatewayPortStart          int
	gatewayPortEnd            int
	heartbeatTimeout          time.Duration
	maxGatewaysPerPod         int
	gatewayStartInFlightLimit int

	gatewayCreateLocksMu sync.Mutex
	gatewayCreateLocks   map[int64]*sync.Mutex
	lastPoolCleanupAt    time.Time
	configurationRetryMu sync.Mutex
	configurationRetryAt map[string]time.Time
}

var (
	errRuntimeScaleOutPending      = errors.New("runtime scale-out pending")
	errRuntimeGatewayStartPending  = errors.New("runtime gateway start pending")
	errRuntimeUpgradePending       = errors.New("runtime data-safe upgrade pending")
	errRuntimeControlPlanePending  = errors.New("runtime control plane temporarily unavailable")
	errRuntimeConfigurationPending = errors.New("runtime configuration recheck pending")
)

const (
	runtimeRolloutStaleWindowMultiplier     = 3
	runtimeHeartbeatFailoverMultiplier      = 3
	runtimeHeartbeatProbeTimeout            = 2 * time.Second
	defaultRuntimeGatewayStartInFlightLimit = 32
	runtimeSchedulerBatchLimit              = 1000
)

type RuntimeGatewayEnvBuilder func(*models.Instance) (map[string]string, error)

type RuntimeSchedulerOption func(*RuntimeScheduler)

func WithRuntimeSchedulerWorkspaceRoot(root string) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		if strings.TrimSpace(root) != "" {
			s.workspaceRoot = strings.TrimSpace(root)
		}
	}
}

func WithRuntimeSchedulerGatewayPortRange(start, end int) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		if start > 0 {
			s.gatewayPortStart = start
		}
		if end > 0 {
			s.gatewayPortEnd = end
		}
		if s.gatewayPortEnd < s.gatewayPortStart {
			s.gatewayPortEnd = s.gatewayPortStart
		}
	}
}

func WithRuntimeSchedulerHeartbeatTimeout(timeout time.Duration) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		s.heartbeatTimeout = timeout
	}
}

func WithRuntimeSchedulerNamespace(namespace string) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		if strings.TrimSpace(namespace) != "" {
			s.runtimeNamespace = strings.TrimSpace(namespace)
		}
	}
}

func WithRuntimeSchedulerMaxGatewaysPerPod(capacity int) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		if capacity > 0 {
			s.maxGatewaysPerPod = capacity
		}
	}
}

func WithRuntimeSchedulerGatewayStartInFlightLimit(limit int) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		if limit > 0 {
			s.gatewayStartInFlightLimit = limit
		}
	}
}

func WithRuntimeSchedulerGatewayEnvBuilder(builder RuntimeGatewayEnvBuilder) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		s.envBuilder = builder
	}
}

func WithRuntimeUpgradeService(service *RuntimeUpgradeService) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) { s.upgrade = service }
}

func NewRuntimeScheduler(
	instanceRepo repository.InstanceRepository,
	podRepo repository.RuntimePodRepository,
	bindingRepo repository.InstanceRuntimeBindingRepository,
	rolloutRepo repository.RuntimeRolloutRepository,
	agentClient RuntimeAgentClient,
	events RuntimeEventService,
	leader RuntimeLeaderService,
	deployments k8s.RuntimeDeploymentService,
	tick time.Duration,
	opts ...RuntimeSchedulerOption,
) *RuntimeScheduler {
	if tick <= 0 {
		tick = 2 * time.Second
	}
	s := &RuntimeScheduler{
		instanceRepo:              instanceRepo,
		podRepo:                   podRepo,
		bindingRepo:               bindingRepo,
		rolloutRepo:               rolloutRepo,
		agentClient:               agentClient,
		events:                    events,
		leader:                    leader,
		deployments:               deployments,
		tick:                      tick,
		workspaceRoot:             "/workspaces",
		runtimeNamespace:          "clawmanager-system",
		gatewayPortStart:          RuntimeGatewayPortStart,
		gatewayPortEnd:            RuntimeGatewayPortEnd,
		heartbeatTimeout:          heartbeat.Timeout,
		maxGatewaysPerPod:         RuntimePodCapacity,
		gatewayStartInFlightLimit: defaultRuntimeGatewayStartInFlightLimit,
		gatewayCreateLocks:        map[int64]*sync.Mutex{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *RuntimeScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	// Independent of long migration/reconciliation work: an idle Team and
	// Agent-supervised restart must not depend on a browser renewing trust.
	if s.proxyPolicy != nil {
		go s.runProxyPolicyRenewal(ctx)
	}
	go func() {
		ticker := time.NewTicker(s.tick)
		defer ticker.Stop()

		s.reconcileIfLeader(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcileIfLeader(ctx)
			}
		}
	}()
}

func (s *RuntimeScheduler) HeartbeatTimeout() time.Duration {
	if s == nil {
		return 0
	}
	return s.heartbeatTimeout
}

func (s *RuntimeScheduler) DrainPod(ctx context.Context, podID int64) error {
	if s == nil || s.podRepo == nil {
		return fmt.Errorf("runtime scheduler pod repository is not configured")
	}
	pod, err := s.podRepo.GetByID(ctx, podID)
	if err != nil {
		return err
	}
	if pod == nil {
		return fmt.Errorf("runtime pod %d not found", podID)
	}
	if pod.AgentEndpoint != nil && strings.TrimSpace(*pod.AgentEndpoint) != "" && s.agentClient != nil {
		if err := s.agentClient.Drain(ctx, strings.TrimSpace(*pod.AgentEndpoint)); err != nil {
			return err
		}
	}
	return s.podRepo.MarkState(ctx, podID, "draining", true)
}

func (s *RuntimeScheduler) FailoverPod(ctx context.Context, podID int64, reason string) error {
	if s == nil || s.podRepo == nil || s.bindingRepo == nil || s.instanceRepo == nil {
		return fmt.Errorf("runtime scheduler dependencies are not configured")
	}
	var errs []error
	if err := s.podRepo.MarkState(ctx, podID, "unhealthy", false); err != nil {
		errs = append(errs, err)
	}

	bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, podID)
	if err != nil {
		errs = append(errs, err)
		return errors.Join(errs...)
	}
	for _, binding := range bindings {
		if err := s.failoverBinding(ctx, podID, binding, reason); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) StartRollout(ctx context.Context, rolloutID int64) error {
	if s == nil || s.rolloutRepo == nil || s.podRepo == nil {
		return fmt.Errorf("runtime scheduler rollout dependencies are not configured")
	}
	rollout, err := s.rolloutRepo.GetByID(ctx, rolloutID)
	if err != nil {
		return err
	}
	if rollout == nil {
		return fmt.Errorf("runtime rollout %d not found", rolloutID)
	}
	// The legacy rollout changes a shared Deployment before migrating its
	// instances. It is not a data-preserving OpenCode upgrade path.
	if rollout.RuntimeType == "opencode" {
		message := models.ErrOpenCodeSafeUpgradeRequired.Error()
		if err := s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", nil, nil, &message); err != nil {
			return err
		}
		return models.ErrOpenCodeSafeUpgradeRequired
	}
	if rollout.Status != "pending" {
		return nil
	}
	if handled, err := s.startPoolRollout(ctx, rollout); handled {
		return err
	}
	if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil {
		if s.upgrade == nil {
			return fmt.Errorf("OpenClaw data-safe rollout service is not configured")
		}
		if err := s.upgrade.Prepare(ctx, rollout); err != nil {
			message := err.Error()
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", nil, nil, &message)
			return err
		}
	}
	emptyOpenClawPoolReset := isEmptyOpenClawPoolReset(rollout)
	if emptyOpenClawPoolReset {
		if s.upgrade == nil {
			return fmt.Errorf("OpenClaw empty-pool reset service is not configured")
		}
		if err := s.upgrade.ValidateEmptyOpenClawPoolReset(ctx, rollout.ID); err != nil {
			message := err.Error()
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", nil, nil, &message)
			return fmt.Errorf("OpenClaw empty-pool reset was cancelled before changing the image: %w", err)
		}
	}

	startedAt := time.Now().UTC()
	if err := s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "running", &startedAt, nil, nil); err != nil {
		return err
	}

	allPods, err := s.podRepo.List(ctx, rollout.RuntimeType)
	if err != nil {
		message := err.Error()
		_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", &startedAt, nil, &message)
		return err
	}
	var deploymentInventory []models.RuntimePod
	var inventoryErr error
	if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil {
		deploymentInventory, inventoryErr = s.RuntimeDeploymentPodsFor(ctx, rollout.RuntimeType, runtimeRolloutDeploymentRefs(rollout, false))
	} else {
		deploymentInventory, inventoryErr = s.RuntimeDeploymentPods(ctx, rollout.RuntimeType)
	}
	if inventoryErr != nil {
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil && runtimeUpgradeInfrastructureRetryable(inventoryErr) {
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "pending", nil, nil, nil)
			return nil
		}
		message := inventoryErr.Error()
		_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", &startedAt, nil, &message)
		return inventoryErr
	}
	allPods = annotateRuntimePods(allPods, deploymentInventory)
	if rollout.RuntimeType != RuntimeTypeOpenClaw || rollout.PreflightID == nil {
		allPods = filterOrdinaryRuntimePods(allPods)
	}
	currentPods := s.currentRuntimePods(allPods, time.Now().UTC())
	alreadyAtImage := runtimePodsAlreadyAtImage(currentPods, rollout.RuntimeType, rollout.TargetImageRef) && rollout.Phase != k8s.HermesWebRolloutPhase && !(rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil)
	if alreadyAtImage {
		if _, ok := s.deployments.(k8s.RuntimeRolloutInventory); ok {
			reason, err := s.runtimeRolloutCompletionReason(ctx, *rollout, nil)
			if err != nil {
				return err
			}
			alreadyAtImage = reason == ""
		}
	}
	if alreadyAtImage {
		finishedAt := time.Now().UTC()
		return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "finished", &startedAt, &finishedAt, nil)
	}
	batchSize := rollout.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	maxUnavailable := rollout.MaxUnavailable
	if maxUnavailable <= 0 && (rollout.RuntimeType != RuntimeTypeOpenClaw || rollout.PreflightID == nil) {
		maxUnavailable = 1
	}
	rolloutPods := allPods
	// Kubernetes is authoritative for which Deployments still exist. Runtime
	// Agent rows are retained for health history and can outlive a deleted
	// Deployment; never turn such rows back into mutation targets. Preserve the
	// historical fallback only when no deployment inventory is available.
	if len(deploymentInventory) > 0 {
		rolloutPods = deploymentInventory
	}
	if err := s.rolloutRuntimeDeployments(ctx, rollout, rolloutPods, maxUnavailable, batchSize); err != nil {
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil && runtimeUpgradeInfrastructureRetryable(err) {
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "pending", nil, nil, nil)
			return nil
		}
		message := err.Error()
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil && rollout.AutoRollback {
			if rollbackErr := s.rollbackOpenClawRollout(ctx, rollout, err); rollbackErr != nil {
				if runtimeUpgradeReconcileInterrupted(ctx, rollbackErr) || runtimeUpgradeInfrastructureRetryable(rollbackErr) {
					return nil
				}
				message = errors.Join(err, rollbackErr).Error()
			}
		}
		_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", &startedAt, nil, &message)
		return err
	}
	if emptyOpenClawPoolReset {
		// There are no gateways to drain. Let Kubernetes keep the old empty pod
		// Ready until the replacement image is Ready; draining here races the pod
		// replacement and adds no safety.
		return nil
	}
	unavailable := 0
	readyCandidates := 0
	for _, pod := range currentPods {
		if pod.Draining || pod.State != "ready" {
			unavailable++
			continue
		}
		readyCandidates++
	}
	remaining := maxUnavailable - unavailable
	if remaining <= 0 {
		return nil
	}
	drainLimit := minInt(batchSize, remaining)
	var errs []error
	drained := 0
	for _, pod := range currentPods {
		if drained >= drainLimit {
			break
		}
		if pod.State != "ready" || pod.Draining {
			continue
		}
		if err := s.DrainPod(ctx, pod.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		drained++
	}
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		message := joined.Error()
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil && rollout.AutoRollback {
			if rollbackErr := s.rollbackOpenClawRollout(ctx, rollout, joined); rollbackErr != nil {
				if runtimeUpgradeReconcileInterrupted(ctx, rollbackErr) || runtimeUpgradeInfrastructureRetryable(rollbackErr) {
					return nil
				}
				message = errors.Join(joined, rollbackErr).Error()
			}
		}
		_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", &startedAt, nil, &message)
		return joined
	}
	if drained == 0 && readyCandidates == 0 && unavailable == 0 && len(currentPods) > 0 {
		finishedAt := time.Now().UTC()
		return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "finished", &startedAt, &finishedAt, nil)
	}
	return nil
}

func (s *RuntimeScheduler) reconcileRollouts(ctx context.Context) error {
	if s == nil || s.rolloutRepo == nil {
		return nil
	}
	rollouts, err := s.rolloutRepo.ListActive(ctx, "")
	if err != nil {
		return fmt.Errorf("list active runtime rollouts: %w", err)
	}
	var errs []error
	for _, rollout := range rollouts {
		switch rollout.Status {
		case "pending":
			if err := s.StartRollout(ctx, rollout.ID); err != nil {
				errs = append(errs, fmt.Errorf("start runtime rollout %d: %w", rollout.ID, err))
			}
		case "running":
			if err := s.finishRolloutIfReady(ctx, rollout); err != nil {
				errs = append(errs, fmt.Errorf("finish runtime rollout %d: %w", rollout.ID, err))
			}
		}
	}
	if err := s.reconcileRestoredUpgradePools(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) reconcileRestoredUpgradePools(ctx context.Context) error {
	if s == nil || s.upgrade == nil || s.deployments == nil {
		return nil
	}
	now := time.Now().UTC()
	if !s.lastPoolCleanupAt.IsZero() && now.Sub(s.lastPoolCleanupAt) < 30*time.Second {
		return nil
	}
	s.lastPoolCleanupAt = now
	var partial []models.RuntimeRollout
	if s.upgrade.sess != nil {
		if err := s.upgrade.sess.Collection("runtime_rollouts").Find(db.Cond{"runtime_type": RuntimeTypeOpenClaw, "status IN": []string{"finished", "error"}}).All(&partial); err != nil {
			return err
		}
		for index := range partial {
			if s.upgrade.hasIsolatedUpgradeItems(ctx, partial[index].ID) {
				if err := s.upgrade.settleIsolatedUpgradeGroups(ctx, &partial[index]); err != nil {
					return err
				}
			}
		}
	}
	rollouts, err := s.upgrade.RollbackCleanupCandidates(ctx)
	if err != nil {
		return fmt.Errorf("list restored OpenClaw rollout cleanup candidates: %w", err)
	}
	var errs []error
	for index := range rollouts {
		rollout := &rollouts[index]
		cleanupFailed := false
		verified, verifyErr := s.upgrade.RollbackRecoveryVerified(ctx, rollout)
		if verifyErr != nil {
			if runtimeUpgradeInfrastructureRetryable(verifyErr) {
				continue
			}
			errs = append(errs, fmt.Errorf("verify rollback recovery %d: %w", rollout.ID, verifyErr))
			continue
		}
		if !verified {
			continue
		}
		var sourceImages map[string]string
		if rollout.SourceImagesJSON == nil || json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sourceImages) != nil || len(sourceImages) == 0 {
			errs = append(errs, fmt.Errorf("rollout %d cleanup has no source inventory", rollout.ID))
			continue
		}
		for deployment := range sourceImages {
			parts := strings.SplitN(deployment, "/", 2)
			if len(parts) != 2 {
				errs = append(errs, fmt.Errorf("rollout %d cleanup has invalid deployment %q", rollout.ID, deployment))
				cleanupFailed = true
				continue
			}
			targetName := runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID)
			hasBindings, bindingErr := s.runtimeDeploymentHasActiveBindings(ctx, parts[0], targetName)
			if bindingErr != nil {
				errs = append(errs, bindingErr)
				cleanupFailed = true
				continue
			}
			if hasBindings {
				cleanupFailed = true
				continue
			}
			if err := s.deployments.DeleteUpgradePool(ctx, parts[0], parts[1], targetName, strconv.FormatInt(rollout.ID, 10)); err != nil {
				errs = append(errs, fmt.Errorf("delete retained target %s/%s: %w", parts[0], targetName, err))
				cleanupFailed = true
			}
		}
		if !cleanupFailed {
			if err := s.upgrade.MarkRollbackPoolCleanupComplete(ctx, rollout); err != nil {
				errs = append(errs, fmt.Errorf("audit retained pool cleanup %d: %w", rollout.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) rolloutRuntimeDeployments(ctx context.Context, rollout *models.RuntimeRollout, pods []models.RuntimePod, maxUnavailable, maxSurge int) error {
	if s == nil || s.deployments == nil || rollout == nil {
		return nil
	}
	targetImage := strings.TrimSpace(rollout.TargetImageRef)
	if targetImage == "" {
		return nil
	}
	if isEmptyOpenClawPoolReset(rollout) {
		maxUnavailable = 0
		maxSurge = 1
	}
	type deploymentRef struct {
		namespace string
		name      string
	}
	refs := map[deploymentRef]struct{}{}
	usePersistedSources := rollout.RuntimeType == RuntimeTypeOpenClaw && (rollout.PreflightID != nil || isEmptyOpenClawPoolReset(rollout))
	if usePersistedSources {
		var sources map[string]string
		if rollout.SourceImagesJSON == nil || json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil || len(sources) == 0 {
			return fmt.Errorf("OpenClaw rollout has no immutable deployment inventory")
		}
		for key := range sources {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return fmt.Errorf("invalid OpenClaw deployment inventory entry %q", key)
			}
			refs[deploymentRef{namespace: parts[0], name: parts[1]}] = struct{}{}
		}
	}
	for _, pod := range pods {
		if usePersistedSources {
			continue
		}
		if !runtimePodEligibleForOrdinaryScheduling(pod) {
			continue
		}
		if pod.RuntimeType != rollout.RuntimeType {
			continue
		}
		namespace := strings.TrimSpace(pod.Namespace)
		name := strings.TrimSpace(pod.DeploymentName)
		if namespace == "" || name == "" {
			continue
		}
		refs[deploymentRef{namespace: namespace, name: name}] = struct{}{}
	}
	if len(refs) == 0 {
		name := defaultRuntimeDeploymentName(rollout.RuntimeType)
		if name == "" {
			return fmt.Errorf("no runtime deployment found for %s rollout", rollout.RuntimeType)
		}
		refs[deploymentRef{namespace: s.runtimeNamespace, name: name}] = struct{}{}
	}
	var errs []error
	for ref := range refs {
		upgradeID := ""
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil {
			upgradeID = strconv.FormatInt(rollout.ID, 10)
		}
		var err error
		if rollout.RuntimeType == RuntimeTypeHermes && rollout.Phase == k8s.HermesWebRolloutPhase {
			web, ok := s.deployments.(k8s.HermesWebDeploymentService)
			if !ok {
				return fmt.Errorf("Hermes Web deployment configuration is unavailable")
			}
			err = web.RolloutHermesWebImage(ctx, ref.namespace, ref.name, targetImage, maxUnavailable, maxSurge)
		} else if upgradeID != "" {
			err = s.deployments.EnsureUpgradePool(ctx, ref.namespace, ref.name, runtimeUpgradeTargetDeploymentName(ref.name, rollout.ID), targetImage, upgradeID)
		} else {
			err = s.deployments.RolloutImage(ctx, ref.namespace, ref.name, targetImage, "", maxUnavailable, maxSurge)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func runtimeUpgradeTargetDeploymentName(source string, rolloutID int64) string {
	suffix := fmt.Sprintf("-u%d", rolloutID)
	source = strings.Trim(strings.ToLower(strings.TrimSpace(source)), "-")
	maxSource := 63 - len(suffix)
	if len(source) > maxSource {
		source = strings.TrimRight(source[:maxSource], "-")
	}
	return source + suffix
}

func (s *RuntimeScheduler) RuntimeDeploymentPods(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	if s == nil || s.deployments == nil {
		return nil, nil
	}
	runtimeType = strings.TrimSpace(runtimeType)
	pods, err := s.deployments.ListPods(ctx, s.runtimeNamespace, runtimeType)
	if err != nil {
		return nil, err
	}
	result := make([]models.RuntimePod, 0, len(pods))
	for index, pod := range pods {
		result = append(result, models.RuntimePod{
			ID:                -int64(index + 1),
			RuntimeType:       pod.RuntimeType,
			Namespace:         pod.Namespace,
			PodName:           pod.PodName,
			PodUID:            stringPtrOrNil(pod.PodUID),
			PodIP:             pod.PodIP,
			NodeName:          pod.NodeName,
			DeploymentName:    pod.DeploymentName,
			ImageRef:          pod.ImageRef,
			ImageDigest:       stringPtrOrNil(pod.ImageDigest),
			State:             runtimeDeploymentFallbackState(pod.State),
			Capacity:          s.maxGatewaysPerPod,
			UsedSlots:         0,
			Draining:          false,
			PoolRole:          pod.PoolRole,
			PoolPurpose:       pod.PoolPurpose,
			UpgradeID:         pod.UpgradeID,
			SourceDeployment:  pod.SourceDeployment,
			SchedulingEnabled: pod.SchedulingEnabled,
			DesiredReplicas:   pod.DesiredReplicas,
			KubernetesReady:   pod.State == "ready",
		})
	}
	// Kubernetes is authoritative for deployment ownership and the running
	// image. Runtime Agent heartbeats are authoritative for capabilities,
	// OpenClaw version and gateway occupancy. Merge them by immutable pod
	// identity so downgrade decisions cannot silently treat an 8.1 pod as an
	// unknown legacy source.
	if s.podRepo != nil {
		reported, reportErr := s.podRepo.List(ctx, runtimeType)
		if reportErr != nil {
			return nil, fmt.Errorf("list Runtime Agent reports for %s deployments: %w", runtimeType, reportErr)
		}
		byIdentity := make(map[string]models.RuntimePod, len(reported))
		for _, pod := range reported {
			byIdentity[runtimePodIdentity(pod)] = pod
		}
		for index := range result {
			pod, ok := byIdentity[runtimePodIdentity(result[index])]
			if !ok {
				continue
			}
			result[index].OpenClawVersion = pod.OpenClawVersion
			result[index].AgentProtocolVersion = pod.AgentProtocolVersion
			result[index].TeamPluginVersion = pod.TeamPluginVersion
			result[index].SessionStore = pod.SessionStore
			result[index].CapabilitiesJSON = pod.CapabilitiesJSON
			result[index].AgentEndpoint = pod.AgentEndpoint
			result[index].UsedSlots = pod.UsedSlots
			result[index].LastSeenAt = pod.LastSeenAt
		}
	}
	return result, nil
}

func (s *RuntimeScheduler) RuntimeDeploymentPodsFor(ctx context.Context, runtimeType string, refs []k8s.RuntimeDeploymentRef) ([]models.RuntimePod, error) {
	if s == nil || s.deployments == nil {
		return nil, nil
	}
	pods, err := s.deployments.ListDeploymentPods(ctx, refs)
	if err != nil {
		return nil, err
	}
	result := make([]models.RuntimePod, 0, len(pods))
	for index, pod := range pods {
		if runtimeType != "" && !strings.EqualFold(strings.TrimSpace(pod.RuntimeType), strings.TrimSpace(runtimeType)) {
			continue
		}
		result = append(result, models.RuntimePod{
			ID: -int64(index + 1), RuntimeType: pod.RuntimeType, Namespace: pod.Namespace,
			PodName: pod.PodName, PodUID: stringPtrOrNil(pod.PodUID), PodIP: pod.PodIP, NodeName: pod.NodeName,
			DeploymentName: pod.DeploymentName, ImageRef: pod.ImageRef,
			ImageDigest: stringPtrOrNil(pod.ImageDigest), State: runtimeDeploymentFallbackState(pod.State),
			Capacity: s.maxGatewaysPerPod, PoolRole: pod.PoolRole, PoolPurpose: pod.PoolPurpose,
			UpgradeID: pod.UpgradeID, SourceDeployment: pod.SourceDeployment, SchedulingEnabled: pod.SchedulingEnabled,
			DesiredReplicas: pod.DesiredReplicas,
			KubernetesReady: pod.State == "ready",
		})
	}
	if s.podRepo == nil {
		return result, nil
	}
	reported, err := s.podRepo.List(ctx, runtimeType)
	if err != nil {
		return nil, fmt.Errorf("list Runtime Agent reports for selected %s deployments: %w", runtimeType, err)
	}
	byIdentity := make(map[string]models.RuntimePod, len(reported))
	for _, pod := range reported {
		byIdentity[runtimePodIdentity(pod)] = pod
	}
	for index := range result {
		pod, ok := byIdentity[runtimePodIdentity(result[index])]
		if !ok {
			continue
		}
		result[index].OpenClawVersion = pod.OpenClawVersion
		result[index].AgentProtocolVersion = pod.AgentProtocolVersion
		result[index].TeamPluginVersion = pod.TeamPluginVersion
		result[index].SessionStore = pod.SessionStore
		result[index].CapabilitiesJSON = pod.CapabilitiesJSON
		result[index].AgentEndpoint = pod.AgentEndpoint
		result[index].UsedSlots = pod.UsedSlots
		result[index].LastSeenAt = pod.LastSeenAt
	}
	return result, nil
}

func runtimeRolloutDeploymentRefs(rollout *models.RuntimeRollout, targets bool) []k8s.RuntimeDeploymentRef {
	if rollout == nil || rollout.SourceImagesJSON == nil {
		return nil
	}
	var sources map[string]string
	if json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil {
		return nil
	}
	refs := make([]k8s.RuntimeDeploymentRef, 0, len(sources))
	for key := range sources {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[1]
		if targets {
			name = runtimeUpgradeTargetDeploymentName(name, rollout.ID)
		}
		refs = append(refs, k8s.RuntimeDeploymentRef{Namespace: parts[0], Name: name})
	}
	return refs
}

func runtimePodIsUpgradeLab(pod models.RuntimePod) bool {
	return strings.EqualFold(strings.TrimSpace(pod.PoolPurpose), "openclaw-upgrade-lab") ||
		strings.EqualFold(strings.TrimSpace(pod.PoolRole), "upgrade-lab") ||
		isOpenClawUpgradeLabDeployment(pod.DeploymentName)
}

func runtimePodEligibleForOrdinaryScheduling(pod models.RuntimePod) bool {
	if runtimePodIsUpgradeLab(pod) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(pod.PoolRole), "upgrade-target") {
		return pod.SchedulingEnabled != nil && *pod.SchedulingEnabled
	}
	return pod.SchedulingEnabled == nil || *pod.SchedulingEnabled
}

func annotateRuntimePods(pods []models.RuntimePod, inventory []models.RuntimePod) []models.RuntimePod {
	metadata := make(map[string]models.RuntimePod, len(inventory))
	for _, pod := range inventory {
		metadata[strings.TrimSpace(pod.Namespace)+"/"+strings.TrimSpace(pod.DeploymentName)] = pod
	}
	for index := range pods {
		key := strings.TrimSpace(pods[index].Namespace) + "/" + strings.TrimSpace(pods[index].DeploymentName)
		if discovered, ok := metadata[key]; ok {
			pods[index].PoolRole = discovered.PoolRole
			pods[index].PoolPurpose = discovered.PoolPurpose
			pods[index].UpgradeID = discovered.UpgradeID
			pods[index].SourceDeployment = discovered.SourceDeployment
			pods[index].SchedulingEnabled = discovered.SchedulingEnabled
			pods[index].DesiredReplicas = discovered.DesiredReplicas
		}
	}
	return pods
}

func filterOrdinaryRuntimePods(pods []models.RuntimePod) []models.RuntimePod {
	filtered := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		if runtimePodEligibleForOrdinaryScheduling(pod) {
			filtered = append(filtered, pod)
		}
	}
	return filtered
}

func (s *RuntimeScheduler) annotateAndFilterOrdinaryRuntimePods(ctx context.Context, runtimeType string, pods []models.RuntimePod) ([]models.RuntimePod, error) {
	if s == nil || s.deployments == nil {
		return pods, nil
	}
	inventory, err := s.RuntimeDeploymentPods(ctx, runtimeType)
	if err != nil {
		return nil, err
	}
	return filterOrdinaryRuntimePods(annotateRuntimePods(pods, inventory)), nil
}

func defaultRuntimeDeploymentName(runtimeType string) string {
	switch strings.ToLower(strings.TrimSpace(runtimeType)) {
	case RuntimeTypeOpenClaw:
		return "openclaw-runtime"
	case RuntimeTypeHermes:
		return "hermes-runtime"
	case RuntimeTypeOpenCode:
		return "opencode-runtime"
	case RuntimeTypeDeepSeekHarness:
		return "deepseek-harness-runtime"
	default:
		return ""
	}
}

func runtimeDeploymentFallbackState(k8sState string) string {
	switch strings.TrimSpace(k8sState) {
	case "pending", "deleted":
		return strings.TrimSpace(k8sState)
	default:
		return "unhealthy"
	}
}

func (s *RuntimeScheduler) finishRolloutIfReady(ctx context.Context, rollout models.RuntimeRollout) error {
	if s == nil || s.podRepo == nil || s.rolloutRepo == nil {
		return nil
	}
	if rollout.RuntimeType != RuntimeTypeOpenClaw {
		p, err := LoadRuntimePoolProgress(ctx, s.rolloutRepo, rollout.ID)
		if err != nil {
			return err
		}
		if p != nil {
			return s.advancePoolRollout(ctx, rollout, p)
		}
	}
	targetImage := strings.TrimSpace(rollout.TargetImageRef)
	if targetImage == "" {
		return nil
	}
	dataSafeOpenClaw := rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil
	if !dataSafeOpenClaw {
		if _, ok := s.deployments.(k8s.RuntimeRolloutInventory); ok {
			return s.finishLiveRuntimeRollout(ctx, rollout, nil)
		}
	}
	if dataSafeOpenClaw && (strings.HasPrefix(strings.TrimSpace(rollout.Phase), "rollback_") || stringValue(rollout.RollbackStatus) == "starting" || stringValue(rollout.RollbackStatus) == "waiting") {
		cause := errors.New("OpenClaw upgrade failed before activation; resumable rollback was continued")
		if rollout.ErrorMessage != nil && strings.TrimSpace(*rollout.ErrorMessage) != "" {
			cause = errors.New(strings.TrimSpace(*rollout.ErrorMessage))
		}
		if err := s.rollbackOpenClawRollout(ctx, &rollout, cause); err != nil {
			if runtimeUpgradeReconcileInterrupted(ctx, err) || runtimeUpgradeInfrastructureRetryable(err) {
				return nil
			}
			return err
		}
		finishedAt := time.Now().UTC()
		message := cause.Error()
		return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, &finishedAt, &message)
	}
	pods, err := s.podRepo.List(ctx, rollout.RuntimeType)
	if err != nil {
		return err
	}
	var inventory []models.RuntimePod
	var inventoryErr error
	if dataSafeOpenClaw {
		inventory, inventoryErr = s.RuntimeDeploymentPodsFor(ctx, rollout.RuntimeType, runtimeRolloutDeploymentRefs(&rollout, true))
	} else {
		inventory, inventoryErr = s.RuntimeDeploymentPods(ctx, rollout.RuntimeType)
	}
	if inventoryErr != nil {
		if dataSafeOpenClaw && runtimeUpgradeInfrastructureRetryable(inventoryErr) {
			if s.upgrade != nil {
				return s.upgrade.waitOrHoldUpgrade(ctx, &rollout)
			}
			return nil
		}
		return inventoryErr
	}
	pods = annotateRuntimePods(pods, inventory)
	if dataSafeOpenClaw {
		if rollout.Phase == "image_rollout" {
			problem := upgradeTargetIdentityProblem(pods, inventory)
			previous := stringValue(rollout.ErrorMessage)
			if (previous == "" || strings.HasPrefix(previous, upgradeTargetIdentityMismatch)) && previous != problem {
				message := stringPtrOrNil(problem)
				if err := s.rolloutRepo.UpdateStatus(ctx, rollout.ID, rollout.Status, rollout.StartedAt, rollout.FinishedAt, message); err != nil {
					return err
				}
				rollout.ErrorMessage = message
			}
		}
		pods = upgradePodsWithLiveIdentity(pods, inventory)
	}
	pods = s.currentRuntimePods(pods, time.Now().UTC())
	if len(pods) == 0 {
		if dataSafeOpenClaw && !canAutoRollbackOpenClaw(rollout.Phase) && s.upgrade != nil {
			if err := s.upgrade.recordUnavailableUpgradeTargets(ctx, &rollout); err != nil {
				return err
			}
			return s.upgrade.waitOrHoldUpgrade(ctx, &rollout)
		}
		if !dataSafeOpenClaw {
			return s.waitForLegacyRollout(ctx, rollout, "no current Runtime Agent has registered")
		}
		if rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID != nil && rollout.StartedAt != nil && time.Since(rollout.StartedAt.UTC()) > 15*time.Minute {
			timeoutErr := fmt.Errorf("OpenClaw target runtime did not register within 15 minutes")
			if strings.HasPrefix(stringValue(rollout.ErrorMessage), upgradeTargetIdentityMismatch) {
				timeoutErr = fmt.Errorf("OpenClaw target runtime did not register within 15 minutes: %s", *rollout.ErrorMessage)
			}
			message := timeoutErr.Error()
			if rollout.AutoRollback && canAutoRollbackOpenClaw(rollout.Phase) {
				if rollbackErr := s.rollbackOpenClawRollout(ctx, &rollout, timeoutErr); rollbackErr != nil {
					if runtimeUpgradeReconcileInterrupted(ctx, rollbackErr) || runtimeUpgradeInfrastructureRetryable(rollbackErr) {
						return nil
					}
					message = errors.Join(timeoutErr, rollbackErr).Error()
				}
			}
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, nil, &message)
			return errors.New(message)
		}
		return nil
	}
	if !dataSafeOpenClaw {
		pods = filterOrdinaryRuntimePods(pods)
		if len(pods) == 0 {
			return s.waitForLegacyRollout(ctx, rollout, "no ordinary Runtime Agent has registered")
		}
		for _, pod := range pods {
			if pod.RuntimeType != rollout.RuntimeType {
				continue
			}
			if rollout.RuntimeType == RuntimeTypeHermes && rollout.Phase == k8s.HermesWebRolloutPhase {
				web, ok := s.deployments.(k8s.HermesWebDeploymentService)
				if !ok {
					return fmt.Errorf("Hermes Web deployment configuration is unavailable")
				}
				ready, err := web.HermesWebRolloutReady(ctx, pod.Namespace, pod.DeploymentName)
				if err != nil {
					return err
				}
				if !ready {
					return s.waitForLegacyRollout(ctx, rollout, "Hermes Web deployment configuration has not rolled out")
				}
			}
			if pod.State != "ready" || pod.Draining || strings.TrimSpace(pod.ImageRef) != targetImage {
				return s.waitForLegacyRollout(ctx, rollout, fmt.Sprintf("Runtime pod %s is not ready at the target image (state=%s, draining=%t)", pod.PodName, pod.State, pod.Draining))
			}
		}
	}
	if dataSafeOpenClaw {
		if s.upgrade == nil {
			return fmt.Errorf("OpenClaw data-safe rollout service is not configured")
		}
		ready, err := s.upgrade.ValidateTargetRuntime(ctx, &rollout, pods)
		if err != nil {
			if errors.Is(err, errUpgradeImageSuspect) {
				finished := time.Now().UTC()
				message := err.Error()
				return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, &finished, &message)
			}
			if errors.Is(err, errAllUpgradeGroupsIsolated) {
				finished := time.Now().UTC()
				message := err.Error()
				return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, &finished, &message)
			}
			// Losing the control-plane lease cancels this scheduler context. That
			// is an ownership hand-off, not evidence that the image or user data
			// failed validation. Persisted item states and Runtime receipts let the
			// next leader resume the same phase.
			if runtimeUpgradeReconcileInterrupted(ctx, err) {
				return nil
			}
			if runtimeUpgradeInfrastructureRetryable(err) {
				return s.upgrade.waitOrHoldUpgrade(ctx, &rollout)
			}
			if !rollout.AutoRollback {
				return s.upgrade.holdFailedUpgrade(ctx, &rollout, err.Error())
			}
			if !canAutoRollbackOpenClaw(rollout.Phase) {
				// Infrastructure retries were handled above. A deterministic failure
				// after activation requires a scoped forward recovery, never rollback.
				message := fmt.Sprintf("OpenClaw upgrade recovery required in %s: %v", rollout.Phase, err)
				if persistErr := s.upgrade.holdFailedUpgrade(ctx, &rollout, message); persistErr != nil {
					return errors.Join(err, persistErr)
				}
				return errors.New(message)
			}
			message := err.Error()
			if rollout.AutoRollback && canAutoRollbackOpenClaw(rollout.Phase) {
				if rollbackErr := s.rollbackOpenClawRollout(ctx, &rollout, err); rollbackErr != nil {
					if runtimeUpgradeReconcileInterrupted(ctx, rollbackErr) || runtimeUpgradeInfrastructureRetryable(rollbackErr) {
						return nil
					}
					message = errors.Join(err, rollbackErr).Error()
				}
			}
			_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, nil, &message)
			return errors.New(message)
		}
		if !ready {
			{
				problem, err := s.upgrade.upgradeWaitingFailure(ctx, &rollout)
				if err != nil {
					return err
				}
				if problem != "" {
					return s.upgrade.holdFailedUpgrade(ctx, &rollout, problem)
				}
			}
			if rollout.Phase == "image_rollout" && rollout.StartedAt != nil && time.Since(rollout.StartedAt.UTC()) > 15*time.Minute {
				timeoutErr := fmt.Errorf("OpenClaw standby target runtime did not register within 15 minutes")
				message := timeoutErr.Error()
				if rollout.AutoRollback {
					if rollbackErr := s.rollbackOpenClawRollout(ctx, &rollout, timeoutErr); rollbackErr != nil {
						if runtimeUpgradeReconcileInterrupted(ctx, rollbackErr) || runtimeUpgradeInfrastructureRetryable(rollbackErr) {
							return nil
						}
						message = errors.Join(timeoutErr, rollbackErr).Error()
					}
				}
				_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, nil, &message)
				return errors.New(message)
			}
			return nil
		}
		var sources map[string]string
		if rollout.SourceImagesJSON == nil || json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sources) != nil || len(sources) == 0 {
			return fmt.Errorf("OpenClaw rollout source deployment inventory is unavailable at commit")
		}
		for deployment := range sources {
			parts := strings.SplitN(deployment, "/", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid OpenClaw source deployment entry %q", deployment)
			}
			targetName := runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID)
			hasBindings, err := s.runtimeDeploymentHasActiveBindings(ctx, parts[0], parts[1])
			if err != nil {
				return err
			}
			if runtimeUpgradeScopeFromRollout(&rollout).scoped() || s.upgrade.hasIsolatedUpgradeItems(ctx, rollout.ID) {
				preserver, ok := s.deployments.(interface {
					SetUpgradePoolActivePreservingSource(context.Context, string, string, string, string) error
				})
				if !ok {
					return fmt.Errorf("scoped upgrade source preservation is unavailable")
				}
				if err := preserver.SetUpgradePoolActivePreservingSource(ctx, parts[0], parts[1], targetName, strconv.FormatInt(rollout.ID, 10)); err != nil {
					return err
				}
				// Retain even stopped excluded instances' pool. Their workspace has
				// deliberately not been migrated and restart must use its old image.
				continue
			}
			if err := s.deployments.SetUpgradePoolActive(ctx, parts[0], parts[1], targetName, strconv.FormatInt(rollout.ID, 10), true); err != nil {
				return fmt.Errorf("activate committed OpenClaw target pool for %s: %w", deployment, err)
			}
			if hasBindings {
				continue
			} // error/unknown bindings also retain the source
			if err := s.deployments.Scale(ctx, parts[0], parts[1], 0); err != nil {
				return fmt.Errorf("scale committed OpenClaw source pool %s to zero: %w", deployment, err)
			}
		}
	}
	finishedAt := time.Now().UTC()
	return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "finished", rollout.StartedAt, &finishedAt, rollout.ErrorMessage)
}

// A failed/unregistered Agent must not leave a generic rollout running forever.
// Do not claim rollback or touch bindings: generic updates have no saved source
// inventory from which a safe rollback could be reconstructed.
func (s *RuntimeScheduler) waitForLegacyRollout(ctx context.Context, rollout models.RuntimeRollout, reason string) error {
	if rollout.StartedAt == nil || time.Since(rollout.StartedAt.UTC()) < 15*time.Minute {
		return nil
	}
	finished := time.Now().UTC()
	message := "Runtime rollout verification timed out after 15 minutes: " + reason + "; no automatic rollback was performed"
	return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", rollout.StartedAt, &finished, &message)
}

func runtimeUpgradeReconcileInterrupted(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return true
	}
	if parts := runtimeUpgradeJoinedErrors(err); len(parts) > 0 {
		for _, part := range parts {
			if !runtimeUpgradeReconcileInterrupted(ctx, part) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, context.Canceled)
}

// A parallel wave can contain both an I/O delay and a deterministic migration
// failure. A retryable sibling must not hide that failure or delay rollback.
func runtimeUpgradeJoinedErrors(err error) []error {
	for err != nil {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			return joined.Unwrap()
		}
		err = errors.Unwrap(err)
	}
	return nil
}

func runtimeUpgradeInfrastructureRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUpgradeSourceState) {
		return false
	}
	if parts := runtimeUpgradeJoinedErrors(err); len(parts) > 0 {
		for _, part := range parts {
			if !runtimeUpgradeInfrastructureRetryable(part) {
				return false
			}
		}
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrRuntimeUpgradeBusy) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsServiceUnavailable(err) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"etcdserver: request timed out", "client.timeout exceeded", "timeout awaiting response headers", "i/o timeout", "connection refused", "connection reset by peer", "transport is closing", "unexpected eof"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

const openClawUpgradeLabDeploymentPrefix = "openclaw-upgrade-lab-"

func isOpenClawUpgradeLabDeployment(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), openClawUpgradeLabDeploymentPrefix)
}

func isEmptyOpenClawPoolReset(rollout *models.RuntimeRollout) bool {
	return rollout != nil && rollout.RuntimeType == RuntimeTypeOpenClaw && rollout.PreflightID == nil && strings.EqualFold(strings.TrimSpace(rollout.Phase), RuntimeUpgradePhaseEmptyPoolReset)
}

func canAutoRollbackOpenClaw(phase string) bool {
	switch strings.TrimSpace(phase) {
	case "maintenance", "image_rollout", "compatibility_check", "session_migration", "rollback_image", "rollback_restore":
		return true
	default:
		// Once target gateways are released, new SQLite-only writes may exist.
		// A blind downgrade would hide them from 7.1, so failures after activation
		// are held for forward repair instead of destructive automatic rollback.
		return false
	}
}

func (s *RuntimeScheduler) rollbackOpenClawRollout(ctx context.Context, rollout *models.RuntimeRollout, cause error) error {
	if rollout == nil || s.deployments == nil || s.upgrade == nil {
		return fmt.Errorf("OpenClaw rollback dependencies are not configured")
	}
	var sourceImages map[string]string
	if rollout.SourceImagesJSON == nil || json.Unmarshal([]byte(*rollout.SourceImagesJSON), &sourceImages) != nil || len(sourceImages) == 0 {
		return fmt.Errorf("OpenClaw rollback source images are unavailable")
	}
	if err := s.upgrade.BeginRollback(ctx, rollout.ID, cause); err != nil {
		return err
	}
	var errs []error
	// Restore OpenClaw's official migration archives while the 8.1 target
	// agents are still alive. Switching the Deployment image first would remove
	// the only runtime that understands the transactional restore format.
	if err := s.upgrade.Rollback(ctx, rollout); err != nil {
		errs = append(errs, err)
	}
	for deployment := range sourceImages {
		if len(errs) > 0 {
			break
		}
		parts := strings.SplitN(deployment, "/", 2)
		if len(parts) != 2 {
			errs = append(errs, fmt.Errorf("invalid rollback deployment entry %q", deployment))
			continue
		}
		if err := s.deployments.Scale(ctx, parts[0], runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID), 0); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.deployments.SetUpgradePoolActive(ctx, parts[0], parts[1], runtimeUpgradeTargetDeploymentName(parts[1], rollout.ID), strconv.FormatInt(rollout.ID, 10), false); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		undrainer, canUndrain := s.agentClient.(interface {
			Undrain(context.Context, string) error
		})
		if !canUndrain {
			errs = append(errs, fmt.Errorf("source runtime agent cannot be released from drain"))
		} else {
			livePods, listErr := s.RuntimeDeploymentPodsFor(ctx, RuntimeTypeOpenClaw, runtimeRolloutDeploymentRefs(rollout, false))
			if listErr != nil {
				errs = append(errs, listErr)
			} else {
				seenDeployments := map[string]bool{}
				for _, pod := range livePods {
					key := pod.Namespace + "/" + pod.DeploymentName
					sourceImage, source := sourceImages[key]
					if !source || !runtimePodMatchesImage(pod, sourceImage) {
						continue
					}
					seenDeployments[key] = true
					endpoint := runtimeAgentEndpoint(pod)
					if endpoint == "" {
						errs = append(errs, fmt.Errorf("live source runtime pod %s has no agent endpoint", pod.PodName))
						continue
					}
					if err := undrainer.Undrain(ctx, endpoint); err != nil {
						errs = append(errs, fmt.Errorf("release live source runtime pod %s from drain: %w", pod.PodName, err))
					}
				}
				for key := range sourceImages {
					if !seenDeployments[key] {
						errs = append(errs, fmt.Errorf("rollback source deployment %s has no live pod", key))
					}
				}
			}
		}
	}
	if len(errs) == 0 {
		if err := s.upgrade.CompleteRollback(ctx, rollout); err != nil {
			errs = append(errs, err)
		}
	}
	joined := errors.Join(errs...)
	if joined != nil && !runtimeUpgradeReconcileInterrupted(ctx, joined) && !runtimeUpgradeInfrastructureRetryable(joined) {
		s.upgrade.FailRollback(ctx, rollout.ID, joined)
	}
	return joined
}

func (s *RuntimeScheduler) runtimeDeploymentHasActiveBindings(ctx context.Context, namespace, deploymentName string) (bool, error) {
	if s == nil || s.podRepo == nil || s.bindingRepo == nil {
		return false, fmt.Errorf("runtime binding inventory is unavailable")
	}
	pods, err := s.podRepo.List(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return false, err
	}
	for _, pod := range pods {
		if strings.TrimSpace(pod.Namespace) != strings.TrimSpace(namespace) || strings.TrimSpace(pod.DeploymentName) != strings.TrimSpace(deploymentName) {
			continue
		}
		bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
		if err != nil {
			return false, err
		}
		for _, binding := range bindings {
			state := strings.ToLower(strings.TrimSpace(binding.State))
			if state != "stopped" && state != "deleted" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *RuntimeScheduler) currentRuntimePods(pods []models.RuntimePod, now time.Time) []models.RuntimePod {
	if s == nil || s.heartbeatTimeout <= 0 {
		return pods
	}
	cutoff := now.UTC().Add(-runtimeRolloutStaleWindowMultiplier * s.heartbeatTimeout)
	current := pods[:0]
	for _, pod := range pods {
		if pod.LastSeenAt != nil && pod.LastSeenAt.UTC().Before(cutoff) {
			continue
		}
		current = append(current, pod)
	}
	return current
}

func runtimePodsAlreadyAtImage(pods []models.RuntimePod, runtimeType, targetImage string) bool {
	targetImage = strings.TrimSpace(targetImage)
	if targetImage == "" {
		return false
	}
	matched := 0
	for _, pod := range pods {
		if pod.RuntimeType != runtimeType {
			continue
		}
		matched++
		if pod.State != "ready" || pod.Draining || strings.TrimSpace(pod.ImageRef) != targetImage {
			return false
		}
	}
	return matched > 0
}

func (s *RuntimeScheduler) reconcileIfLeader(ctx context.Context) {
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		return
	}
	if err := s.reconcile(ctx); err != nil {
		log.Printf("runtime scheduler reconcile failed: %v", err)
	}
}

func (s *RuntimeScheduler) reconcile(ctx context.Context) error {
	if s == nil || s.instanceRepo == nil || s.bindingRepo == nil || s.podRepo == nil {
		return fmt.Errorf("runtime scheduler dependencies are not configured")
	}
	var errs []error
	if err := s.failoverStalePods(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.reconcileRollouts(ctx); err != nil {
		errs = append(errs, err)
	}
	creating, err := s.instanceRepo.GetV2Creating(ctx, runtimeSchedulerBatchLimit)
	if err != nil {
		errs = append(errs, err)
	} else {
		errs = append(errs, s.reconcileCreatingInstances(ctx, creating)...)
	}

	desired, err := s.instanceRepo.GetV2DesiredRunning(ctx, runtimeSchedulerBatchLimit)
	if err != nil {
		errs = append(errs, err)
	} else {
		if err := s.scaleOutForPendingBacklog(ctx, desired); err != nil {
			errs = append(errs, err)
		}
		for _, instance := range desired {
			errs = append(errs, s.reconcileDesiredInstance(ctx, instance)...)
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) recoverLegacyFailedGatewayBinding(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding) (bool, error) {
	if s == nil || binding == nil || s.agentClient == nil || s.podRepo == nil || s.instanceRepo == nil {
		return false, nil
	}
	state := strings.ToLower(strings.TrimSpace(binding.State))
	if state != RuntimeGatewayBindingError && state != "failed" && state != RuntimeGatewayBindingStopped {
		return false, nil
	}
	if !isRuntimeErrorInstance(instance) || !isLegacyUnexpectedGatewayExitMessage(strings.TrimSpace(stringValue(instance.RuntimeErrorMessage))) {
		return false, nil
	}
	releaser, ok := s.bindingRepo.(repository.FailedGatewayBindingReleaser)
	if !ok {
		return false, nil
	}
	pod, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return false, err
	}
	if pod == nil || pod.State != "ready" || pod.Draining || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
		return false, nil
	}
	// DeleteGateway is the writer-fencing acknowledgement. Never release the
	// binding or schedule another process when the Agent cannot confirm that the
	// old PID is gone.
	if err := s.agentClient.DeleteGateway(ctx, strings.TrimSpace(*pod.AgentEndpoint), binding.GatewayID); err != nil && !errors.Is(err, ErrRuntimeAgentNotFound) {
		return false, fmt.Errorf("confirm failed Gateway %s stopped: %w", binding.GatewayID, err)
	}
	deleted, err := releaser.DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx, instance.ID, binding.RuntimePodID, binding.Generation)
	if err != nil || !deleted {
		return false, err
	}
	message := "recovering a confirmed legacy Gateway process exit"
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "creating", binding.Generation, &message); err != nil {
		return false, err
	}
	return true, nil
}

func (s *RuntimeScheduler) reconcileCreatingInstances(ctx context.Context, instances []models.Instance) []error {
	if len(instances) == 0 {
		return nil
	}
	concurrency := s.gatewayStartInFlightLimit
	if concurrency <= 0 {
		concurrency = 1
	}
	concurrency = minInt(concurrency, len(instances))

	jobs := make(chan models.Instance)
	var wg sync.WaitGroup
	var errsMu sync.Mutex
	var errs []error
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for instance := range jobs {
				instanceErrs := s.reconcileCreatingInstance(ctx, instance)
				if len(instanceErrs) == 0 {
					continue
				}
				errsMu.Lock()
				errs = append(errs, instanceErrs...)
				errsMu.Unlock()
			}
		}()
	}
	for _, instance := range instances {
		if isSchedulerManagedV2Instance(instance) {
			jobs <- instance
		}
	}
	close(jobs)
	wg.Wait()
	return errs
}

func (s *RuntimeScheduler) reconcileCreatingInstance(ctx context.Context, instance models.Instance) []error {
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, instance.ID)
	if err != nil {
		return schedulerGuardErrors(err)
	}
	defer release()
	current, err := currentLifecycleSnapshot(s.instanceRepo, instance)
	if err != nil {
		return []error{err}
	}
	if current == nil || current.Status != "creating" {
		return nil
	}
	return s.reconcileCreatingInstanceLocked(ctx, *current)
}

func (s *RuntimeScheduler) reconcileCreatingInstanceLocked(ctx context.Context, instance models.Instance) []error {
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return []error{fmt.Errorf("get binding for creating instance %d: %w", instance.ID, err)}
	}
	if binding != nil {
		stale, staleErr := s.cleanupStaleInstanceBinding(ctx, instance, binding)
		if staleErr != nil {
			return []error{fmt.Errorf("clean stale binding for creating instance %d: %w", instance.ID, staleErr)}
		}
		if !stale {
			recovered, recoverErr := s.recoverLegacyFailedGatewayBinding(ctx, instance, binding)
			if recoverErr != nil {
				return []error{fmt.Errorf("recover failed Gateway binding for instance %d: %w", instance.ID, recoverErr)}
			}
			if recovered {
				stale = true
				instance.Status = "creating"
				instance.RuntimeErrorMessage = nil
			}
		}
		if !stale {
			if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
				return []error{fmt.Errorf("sync creating instance %d from binding: %w", instance.ID, err)}
			}
			return nil
		}
	}
	if err := s.assignInstance(ctx, instance); err != nil {
		if isRuntimeAssignmentPending(err) {
			return nil
		}
		errs := []error{fmt.Errorf("assign creating instance %d: %w", instance.ID, err)}
		s.markInstanceError(ctx, instance, err, &errs)
		return errs
	}
	return nil
}

// cleanupStaleInstanceBinding removes only a binding from an older runtime
// generation. A newer binding may have been created after the instance snapshot
// was read and must never be removed by this reconciliation pass.
func (s *RuntimeScheduler) cleanupStaleInstanceBinding(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding) (bool, error) {
	if binding == nil || binding.Generation >= instance.RuntimeGeneration {
		return false, nil
	}
	if s.bindingRepo == nil {
		return false, fmt.Errorf("runtime binding repository is not configured")
	}
	if s.podRepo != nil && s.agentClient != nil && strings.TrimSpace(binding.GatewayID) != "" {
		pod, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
		if err != nil {
			return false, fmt.Errorf("get runtime pod %d: %w", binding.RuntimePodID, err)
		}
		if pod != nil && pod.AgentEndpoint != nil && strings.TrimSpace(*pod.AgentEndpoint) != "" {
			if err := s.agentClient.DeleteGateway(ctx, strings.TrimSpace(*pod.AgentEndpoint), binding.GatewayID); err != nil && !errors.Is(err, ErrRuntimeAgentNotFound) {
				return false, fmt.Errorf("delete stale gateway %q: %w", binding.GatewayID, err)
			}
		}
	}
	if err := s.bindingRepo.DeleteByInstanceIDAndReleaseSlot(ctx, instance.ID, binding.RuntimePodID); err != nil {
		return false, fmt.Errorf("delete stale binding and release slot: %w", err)
	}
	return true, nil
}

func (s *RuntimeScheduler) syncInstanceStateFromBinding(ctx context.Context, instance models.Instance, binding *models.InstanceRuntimeBinding) error {
	if s == nil || s.instanceRepo == nil || binding == nil {
		return nil
	}
	state := strings.ToLower(strings.TrimSpace(binding.State))
	switch state {
	case "running", "ready", "healthy":
		if strings.EqualFold(strings.TrimSpace(instance.Status), "running") && instance.RuntimeErrorMessage == nil {
			return nil
		}
		return s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "running", maxInt(instance.RuntimeGeneration, binding.Generation), nil)
	case "error", "failed", RuntimeGatewayBindingUnhealthy:
		message := "runtime gateway failed"
		if binding.ErrorMessage != nil && strings.TrimSpace(*binding.ErrorMessage) != "" {
			message = strings.TrimSpace(*binding.ErrorMessage)
		}
		if strings.EqualFold(strings.TrimSpace(instance.Status), "error") &&
			instance.RuntimeErrorMessage != nil &&
			strings.TrimSpace(*instance.RuntimeErrorMessage) == message {
			return nil
		}
		return s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", maxInt(instance.RuntimeGeneration, binding.Generation), &message)
	default:
		return nil
	}
}

func (s *RuntimeScheduler) failoverStalePods(ctx context.Context) error {
	if s.heartbeatTimeout <= 0 {
		return nil
	}
	pods, err := s.podRepo.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list runtime pods for heartbeat failover: %w", err)
	}
	// A heartbeat timeout is only a suspicion threshold. Destructive failover
	// requires a longer continuous absence plus independent Kubernetes and
	// Runtime Agent evidence. This protects bindings while MySQL, etcd or the
	// control-plane Service is temporarily slow.
	cutoff := time.Now().UTC().Add(-time.Duration(runtimeHeartbeatFailoverMultiplier) * s.heartbeatTimeout)
	kubernetesPodsByType := map[string][]k8s.RuntimeDeploymentPod{}
	var errs []error
	for _, pod := range pods {
		if pod.State == "unhealthy" || pod.State == "pending" {
			continue
		}
		if pod.LastSeenAt == nil || !pod.LastSeenAt.Before(cutoff) {
			continue
		}
		if s.deployments == nil || s.agentClient == nil || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
			// Without independent evidence, retaining a possibly live writer is
			// safer than deleting its binding and starting a duplicate.
			continue
		}
		runtimeType := strings.TrimSpace(pod.RuntimeType)
		inventory, loaded := kubernetesPodsByType[runtimeType]
		if !loaded {
			inventory, err = s.deployments.ListPods(ctx, s.runtimeNamespace, runtimeType)
			if err != nil {
				return fmt.Errorf("confirm stale %s runtime pods with Kubernetes: %w", runtimeType, err)
			}
			kubernetesPodsByType[runtimeType] = inventory
		}
		if runtimePodStillReadyInKubernetes(pod, inventory) {
			continue
		}
		probeTimeout := runtimeHeartbeatProbeTimeout
		if s.heartbeatTimeout > 0 && s.heartbeatTimeout/2 < probeTimeout {
			probeTimeout = s.heartbeatTimeout / 2
		}
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		probeErr := s.agentClient.Health(probeCtx, strings.TrimSpace(*pod.AgentEndpoint))
		cancel()
		if probeErr == nil {
			continue
		}
		reason := fmt.Sprintf("runtime pod heartbeat lost since %s", pod.LastSeenAt.UTC().Format(time.RFC3339))
		if err := s.FailoverPod(ctx, pod.ID, reason); err != nil {
			errs = append(errs, fmt.Errorf("failover stale runtime pod %d: %w", pod.ID, err))
		}
	}
	return errors.Join(errs...)
}

func runtimePodStillReadyInKubernetes(reported models.RuntimePod, inventory []k8s.RuntimeDeploymentPod) bool {
	for _, live := range inventory {
		if strings.TrimSpace(live.Namespace) != strings.TrimSpace(reported.Namespace) || strings.TrimSpace(live.PodName) != strings.TrimSpace(reported.PodName) {
			continue
		}
		if reported.PodUID != nil && strings.TrimSpace(*reported.PodUID) != "" && strings.TrimSpace(live.PodUID) != strings.TrimSpace(*reported.PodUID) {
			return false
		}
		return strings.EqualFold(strings.TrimSpace(live.State), "ready")
	}
	return false
}

func (s *RuntimeScheduler) lockGatewayCreateForPod(podID int64) func() {
	if s == nil {
		return func() {}
	}
	s.gatewayCreateLocksMu.Lock()
	if s.gatewayCreateLocks == nil {
		s.gatewayCreateLocks = map[int64]*sync.Mutex{}
	}
	lock := s.gatewayCreateLocks[podID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.gatewayCreateLocks[podID] = lock
	}
	s.gatewayCreateLocksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (s *RuntimeScheduler) podCanStartGateway(ctx context.Context, podID int64) (bool, error) {
	if s == nil || s.bindingRepo == nil || s.gatewayStartInFlightLimit <= 0 {
		return true, nil
	}
	bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, podID)
	if err != nil {
		return false, fmt.Errorf("list runtime pod %d bindings: %w", podID, err)
	}
	starting := 0
	for _, binding := range bindings {
		switch strings.ToLower(strings.TrimSpace(binding.State)) {
		case "creating", "starting":
			starting++
		}
	}
	return starting < s.gatewayStartInFlightLimit, nil
}

func (s *RuntimeScheduler) scaleOutForPendingBacklog(ctx context.Context, instances []models.Instance) error {
	if s == nil || s.bindingRepo == nil || s.podRepo == nil || s.deployments == nil || len(instances) == 0 {
		return nil
	}
	backlogByType := map[string]int{}
	for _, instance := range instances {
		if !isSchedulerManagedV2Instance(instance) {
			continue
		}
		if isRuntimeErrorInstance(instance) && !isRecoverableRuntimeSchedulingError(instance) {
			continue
		}
		runtimeType, ok := schedulerRuntimeType(instance)
		if !ok {
			continue
		}
		// A data-safe OpenClaw rollout must not create ordinary OpenClaw
		// capacity for instances that are intentionally waiting. Other Runtime
		// types always bypass the OpenClaw guard.
		if s.upgrade != nil && s.upgrade.InstanceBlocked(ctx, runtimeType, instance.ID) {
			continue
		}
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
		if err != nil {
			return fmt.Errorf("get binding for backlog instance %d: %w", instance.ID, err)
		}
		if binding == nil {
			if _, configErr := s.gatewayEnvironment(&instance); configErr != nil {
				// Invalid configuration is not capacity demand. The normal assign
				// path records the exact error without scaling the pool.
				continue
			}
			backlogByType[runtimeType]++
		}
	}
	for runtimeType, backlog := range backlogByType {
		if backlog <= 0 {
			continue
		}
		if _, err := s.scaleOutIfBacklogExceedsCapacity(ctx, runtimeType, backlog); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeScheduler) scaleOutIfBacklogExceedsCapacity(ctx context.Context, runtimeType string, backlog int) (bool, error) {
	if s == nil || s.deployments == nil || s.podRepo == nil || backlog <= 0 {
		return false, nil
	}
	pods, err := s.podRepo.List(ctx, runtimeType)
	if err != nil {
		return false, fmt.Errorf("list %s runtime pods for backlog scale-out: %w", runtimeType, err)
	}
	pods, err = s.annotateAndFilterOrdinaryRuntimePods(ctx, runtimeType, pods)
	if err != nil {
		return false, fmt.Errorf("classify %s runtime pods for backlog scale-out: %w", runtimeType, err)
	}
	groups := map[string]*runtimeDeploymentCapacity{}
	for _, pod := range pods {
		if pod.RuntimeType != runtimeType || pod.Capacity <= 0 {
			continue
		}
		namespace := strings.TrimSpace(pod.Namespace)
		name := strings.TrimSpace(pod.DeploymentName)
		if namespace == "" || name == "" {
			continue
		}
		key := namespace + "/" + name
		group := groups[key]
		if group == nil {
			group = &runtimeDeploymentCapacity{namespace: namespace, name: name}
			groups[key] = group
		}
		group.observed++
		group.desiredReplicas = maxInt(group.desiredReplicas, int(pod.DesiredReplicas))
		group.capacityPerPod = maxInt(group.capacityPerPod, pod.Capacity)
		if pod.Draining {
			group.draining++
		}
		if pod.State != "ready" || pod.Draining {
			continue
		}
		group.active++
		group.capacityTotal += pod.Capacity
		group.usedSlots += minInt(pod.UsedSlots, pod.Capacity)
	}
	var target *runtimeDeploymentCapacity
	for _, group := range groups {
		if (group.active == 0 && group.draining == 0) || group.capacityPerPod <= 0 {
			continue
		}
		if target == nil || group.active > target.active {
			target = group
		}
	}
	if target == nil {
		return false, nil
	}
	if target.desiredReplicas > target.observed {
		return true, nil
	}
	desiredSlots := target.usedSlots + backlog
	if desiredSlots <= target.capacityTotal {
		return false, nil
	}
	capacityPerPod := target.capacityPerPod
	if target.active > 0 && target.capacityTotal > 0 {
		capacityPerPod = target.capacityTotal / target.active
	}
	if capacityPerPod <= 0 {
		capacityPerPod = s.maxGatewaysPerPod
	}
	if capacityPerPod <= 0 {
		capacityPerPod = RuntimePodCapacity
	}
	targetReplicas := (desiredSlots + capacityPerPod - 1) / capacityPerPod
	baselineReplicas := maxInt(target.observed, target.desiredReplicas)
	if targetReplicas <= baselineReplicas {
		targetReplicas = baselineReplicas + 1
	}
	// Scale one replica at a time so a large batch cannot stampede the node.
	replicas := int32(minInt(targetReplicas, baselineReplicas+1))
	if err := s.deployments.Scale(ctx, target.namespace, target.name, replicas); err != nil {
		return false, err
	}
	if s.events != nil {
		if err := s.events.Publish(ctx, "runtime.pool.scaleout", map[string]any{
			"runtime_type": runtimeType,
			"namespace":    target.namespace,
			"deployment":   target.name,
			"replicas":     replicas,
			"reason":       "pending_backlog",
			"backlog":      backlog,
		}); err != nil {
			log.Printf("runtime scheduler publish backlog scale-out event failed: %v", err)
		}
	}
	return true, nil
}
func (s *RuntimeScheduler) assignInstance(ctx context.Context, instance models.Instance) error {
	if s == nil || s.podRepo == nil {
		return fmt.Errorf("runtime scheduler pod repository is not configured")
	}
	if !isSchedulerManagedV2Instance(instance) {
		return fmt.Errorf("instance %d is not scheduler-managed v2", instance.ID)
	}
	runtimeType, ok := schedulerRuntimeType(instance)
	if !ok {
		return fmt.Errorf("unsupported runtime type %q", instance.Type)
	}
	if s.upgrade != nil && s.upgrade.InstanceBlocked(ctx, runtimeType, instance.ID) {
		return fmt.Errorf("%w: instance %d is held by an active data-safe runtime rollout", errRuntimeUpgradePending, instance.ID)
	}
	if s.bindingRepo != nil {
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
		if err != nil {
			if runtimeUpgradeInfrastructureRetryable(err) {
				return fmt.Errorf("%w: check existing binding: %v", errRuntimeControlPlanePending, err)
			}
			return fmt.Errorf("failed to check existing binding: %w", err)
		}
		if binding != nil {
			return nil
		}
	}
	if s.configurationRetryPending(instance) {
		return errRuntimeConfigurationPending
	}
	// Configuration errors cannot be resolved by trying another Pod. Check
	// before discovering capacity, scaling or claiming a slot. The start path
	// still revalidates immediately before constructing the gateway request.
	if _, err := s.gatewayEnvironment(&instance); err != nil {
		return fmt.Errorf("prepare instance configuration: %w", err)
	}
	pods, err := s.podRepo.ListSchedulable(ctx, runtimeType)
	if err != nil {
		if runtimeUpgradeInfrastructureRetryable(err) {
			return fmt.Errorf("%w: list schedulable runtime pods: %v", errRuntimeControlPlanePending, err)
		}
		return err
	}
	pods, err = s.annotateAndFilterOrdinaryRuntimePods(ctx, runtimeType, pods)
	if err != nil {
		if runtimeUpgradeInfrastructureRetryable(err) {
			return fmt.Errorf("%w: classify schedulable runtime pods: %v", errRuntimeControlPlanePending, err)
		}
		return fmt.Errorf("classify schedulable %s runtime pods: %w", runtimeType, err)
	}
	if runtimeType == RuntimeTypeOpenClaw && s.upgrade != nil {
		pods, err = s.upgrade.filterUpgradePlacement(ctx, instance.ID, pods)
		if err != nil {
			return err
		}
	}
	if len(pods) == 0 {
		scaled, err := s.scaleOutIfAtCapacity(ctx, runtimeType)
		if err != nil {
			if runtimeUpgradeInfrastructureRetryable(err) {
				return fmt.Errorf("%w: scale out runtime deployment: %v", errRuntimeControlPlanePending, err)
			}
			return fmt.Errorf("scale out %s runtime deployment: %w", runtimeType, err)
		}
		if scaled {
			return errRuntimeScaleOutPending
		}
	}
	var lastErr error
	for _, pod := range pods {
		if pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
			continue
		}
		unlock := s.lockGatewayCreateForPod(pod.ID)
		canStart, err := s.podCanStartGateway(ctx, pod.ID)
		if err != nil {
			unlock()
			lastErr = err
			continue
		}
		if !canStart {
			unlock()
			lastErr = fmt.Errorf("runtime pod %d has gateway start in flight: %w", pod.ID, errRuntimeGatewayStartPending)
			continue
		}
		claimed, err := s.podRepo.TryClaimSlot(ctx, pod.ID)
		if err != nil {
			unlock()
			lastErr = err
			continue
		}
		if !claimed {
			unlock()
			continue
		}
		start, err := s.prepareGatewayStart(ctx, instance, runtimeType, pod)
		unlock()
		if err != nil {
			if releaseErr := s.podRepo.ReleaseSlot(ctx, pod.ID); releaseErr != nil {
				return errors.Join(err, releaseErr)
			}
			if errors.Is(err, ErrModelConfigurationRequired) {
				return err
			}
			lastErr = err
			continue
		}
		if err := s.createGatewayOnPodWithPortFallback(ctx, instance, runtimeType, pod, start); err != nil {
			if isRuntimeAssignmentPending(err) {
				return err
			}
			if releaseErr := s.podRepo.ReleaseSlot(ctx, pod.ID); releaseErr != nil {
				return errors.Join(err, releaseErr)
			}
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		if runtimeFailureCode(lastErr) == "PROXY_POLICY_NOT_READY" {
			return lastErr
		}
		if runtimeUpgradeInfrastructureRetryable(lastErr) {
			return fmt.Errorf("%w: runtime assignment probe: %v", errRuntimeControlPlanePending, lastErr)
		}
		return &runtimeFailure{code: "GATEWAY_START_FAILED", cause: fmt.Errorf("prepare or start %s gateway: %w", runtimeType, lastErr)}
	}
	code := "RUNTIME_POOL_UNAVAILABLE"
	if len(pods) > 0 {
		code = "RUNTIME_CAPACITY_EXHAUSTED"
	}
	return &runtimeFailure{code: code, cause: fmt.Errorf("no schedulable %s runtime pod", runtimeType)}
}

type runtimeDeploymentCapacity struct {
	namespace       string
	name            string
	active          int
	observed        int
	draining        int
	full            int
	capacityTotal   int
	capacityPerPod  int
	usedSlots       int
	desiredReplicas int
}

func (s *RuntimeScheduler) scaleOutIfAtCapacity(ctx context.Context, runtimeType string) (bool, error) {
	if s == nil || s.deployments == nil || s.podRepo == nil {
		return false, nil
	}
	pods, err := s.podRepo.List(ctx, runtimeType)
	if err != nil {
		return false, fmt.Errorf("list %s runtime pods for scale-out: %w", runtimeType, err)
	}
	pods, err = s.annotateAndFilterOrdinaryRuntimePods(ctx, runtimeType, pods)
	if err != nil {
		return false, fmt.Errorf("classify %s runtime pods for scale-out: %w", runtimeType, err)
	}
	groups := map[string]*runtimeDeploymentCapacity{}
	for _, pod := range pods {
		if pod.RuntimeType != runtimeType || pod.Capacity <= 0 {
			continue
		}
		namespace := strings.TrimSpace(pod.Namespace)
		name := strings.TrimSpace(pod.DeploymentName)
		if namespace == "" || name == "" {
			continue
		}
		key := namespace + "/" + name
		group := groups[key]
		if group == nil {
			group = &runtimeDeploymentCapacity{namespace: namespace, name: name}
			groups[key] = group
		}
		group.observed++
		group.desiredReplicas = maxInt(group.desiredReplicas, int(pod.DesiredReplicas))
		group.capacityPerPod = maxInt(group.capacityPerPod, pod.Capacity)
		if pod.Draining {
			group.draining++
		}
		if pod.State != "ready" || pod.Draining {
			continue
		}
		group.active++
		if pod.UsedSlots >= pod.Capacity {
			group.full++
		}
	}
	var target *runtimeDeploymentCapacity
	for _, group := range groups {
		if group.active == 0 && group.draining == 0 {
			continue
		}
		if group.active > 0 && group.active != group.full {
			continue
		}
		if target == nil || group.active > target.active {
			target = group
		}
	}
	if target == nil {
		return false, nil
	}
	if target.desiredReplicas > target.observed {
		return true, nil
	}
	replicas := int32(maxInt(target.observed, target.desiredReplicas) + 1)
	if err := s.deployments.Scale(ctx, target.namespace, target.name, replicas); err != nil {
		return false, err
	}
	if s.events != nil {
		if err := s.events.Publish(ctx, "runtime.pool.scaleout", map[string]any{
			"runtime_type": runtimeType,
			"namespace":    target.namespace,
			"deployment":   target.name,
			"replicas":     replicas,
		}); err != nil {
			log.Printf("runtime scheduler publish scale-out event failed: %v", err)
		}
	}
	return true, nil
}

type runtimeGatewayStart struct {
	endpoint        string
	workspacePath   string
	environment     map[string]string
	uid             int
	gid             int
	reservedBinding *models.InstanceRuntimeBinding
	upgradeID       string
}

// EnsureUpgradeGateway starts a migrated OpenClaw instance only on a pod that
// belongs to the rollout target pool. Both upgrade-lab and ordinary data-safe
// rollouts use this path, so a standby target never needs to be exposed to the
// ordinary scheduler before postflight has committed the rollout.
func (s *RuntimeScheduler) EnsureUpgradeGateway(ctx context.Context, rollout *models.RuntimeRollout, instanceID int, targetPods []models.RuntimePod) error {
	if s == nil || rollout == nil || rollout.RuntimeType != RuntimeTypeOpenClaw || rollout.PreflightID == nil {
		return fmt.Errorf("data-safe OpenClaw rollout is required")
	}
	if s.instanceRepo == nil || s.podRepo == nil || s.bindingRepo == nil || s.agentClient == nil {
		return fmt.Errorf("upgrade gateway dependencies are not configured")
	}
	if instanceID <= 0 || len(targetPods) == 0 {
		return fmt.Errorf("upgrade target pod is unavailable")
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("upgrade instance %d not found", instanceID)
	}
	runtimeType, ok := schedulerRuntimeType(*instance)
	if !ok || runtimeType != RuntimeTypeOpenClaw {
		return fmt.Errorf("upgrade instance %d is not OpenClaw Lite", instanceID)
	}
	targetByID := make(map[int64]models.RuntimePod, len(targetPods))
	for _, pod := range targetPods {
		if pod.RuntimeType == RuntimeTypeOpenClaw && pod.State == "ready" && !pod.Draining && runtimePodMatchesImage(pod, rollout.TargetImageRef) {
			targetByID[pod.ID] = pod
		}
	}
	if len(targetByID) == 0 {
		return fmt.Errorf("no ready pod belongs to rollout %d target pool", rollout.ID)
	}
	if binding, bindingErr := s.bindingRepo.GetByInstanceID(ctx, instanceID); bindingErr != nil {
		return bindingErr
	} else if binding != nil {
		if targetPod, expected := targetByID[binding.RuntimePodID]; expected && binding.Generation == instance.RuntimeGeneration {
			lifecycle := NormalizeRuntimeGatewayLifecycle(binding.State, binding.ErrorMessage)
			if lifecycle.Running {
				return nil
			}
			if lifecycle.BindingState != RuntimeGatewayBindingError && lifecycle.BindingState != RuntimeGatewayBindingStopped && lifecycle.BindingState != RuntimeGatewayBindingCreating {
				return fmt.Errorf("instance %d target binding has unresolved state %q", instanceID, binding.State)
			}
			if lifecycle.BindingState != RuntimeGatewayBindingCreating && strings.TrimSpace(binding.GatewayID) != "" {
				endpoint := strings.TrimSpace(stringValue(targetPod.AgentEndpoint))
				if endpoint == "" {
					return fmt.Errorf("instance %d failed target binding cannot be confirmed without its Runtime Agent", instanceID)
				}
				observer, ok := s.agentClient.(interface {
					GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
				})
				if !ok {
					return fmt.Errorf("upgrade Gateway observer unavailable")
				}
				state, stateErr := observer.GatewayState(ctx, endpoint, binding.GatewayID)
				if stateErr != nil && !errors.Is(stateErr, ErrRuntimeAgentNotFound) {
					return stateErr
				}
				if state != nil && (state.InstanceID != instanceID || state.Generation != instance.RuntimeGeneration) {
					return repository.ErrUpgradeReservationDenied
				}
				if state != nil && NormalizeRuntimeGatewayLifecycle(state.State, nil).Running {
					return s.bindingRepo.UpdateState(ctx, instanceID, instance.RuntimeGeneration, "running", nil)
				}
				if state != nil && NormalizeRuntimeGatewayLifecycle(state.State, nil).BindingState == RuntimeGatewayBindingCreating {
					return errRuntimeGatewayStartPending
				}
				if deleteErr := s.agentClient.DeleteGateway(ctx, endpoint, binding.GatewayID); deleteErr != nil && !errors.Is(deleteErr, ErrRuntimeAgentNotFound) {
					return fmt.Errorf("stop failed target gateway for instance %d: %w", instanceID, deleteErr)
				}
				releaser, ok := s.bindingRepo.(repository.FailedGatewayBindingReleaser)
				if !ok {
					return fmt.Errorf("upgrade conditional binding releaser unavailable")
				}
				if _, deleteErr := releaser.DeleteFailedByInstanceIDGenerationAndReleaseSlot(ctx, instanceID, binding.RuntimePodID, binding.Generation); deleteErr != nil {
					return fmt.Errorf("release failed target binding for instance %d: %w", instanceID, deleteErr)
				}
			}
		} else {
			return fmt.Errorf("instance %d already has a binding outside rollout %d target pool", instanceID, rollout.ID)
		}
	} else {
		// Absence in SQL alone is not proof of absence on Runtime (e.g. a
		// previous controller lost its DB write). Never choose a second writer.
		observer, ok := s.agentClient.(interface {
			GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error)
		})
		if !ok {
			return fmt.Errorf("upgrade Gateway observer unavailable")
		}
		for _, target := range targetByID {
			state, err := observer.GatewayState(ctx, stringValue(target.AgentEndpoint), fmt.Sprintf("gw-%d-%d", instanceID, instance.RuntimeGeneration))
			if err != nil && !errors.Is(err, ErrRuntimeAgentNotFound) {
				return err
			}
			if state != nil {
				return fmt.Errorf("%w: target Gateway exists without its binding; await authoritative reconciliation", repository.ErrUpgradeRecoveryHeld)
			}
		}
	}

	ordered := make([]models.RuntimePod, 0, len(targetByID))
	for _, pod := range targetPods {
		if candidate, ok := targetByID[pod.ID]; ok {
			ordered = append(ordered, candidate)
		}
	}
	var lastErr error
	for _, pod := range ordered {
		if pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
			continue
		}
		unlock := s.lockGatewayCreateForPod(pod.ID)
		canStart, startErr := s.podCanStartGateway(ctx, pod.ID)
		if existing, lookupErr := s.bindingRepo.GetByInstanceID(ctx, instanceID); lookupErr != nil {
			unlock()
			return lookupErr
		} else if existing != nil && existing.RuntimePodID == pod.ID && existing.Generation == instance.RuntimeGeneration {
			// Replaying our existing reservation does not add an in-flight start.
			canStart = true
		}
		if startErr != nil || !canStart {
			unlock()
			if startErr != nil {
				lastErr = startErr
			} else {
				lastErr = errRuntimeGatewayStartPending
			}
			continue
		}
		start, prepareErr := s.prepareGatewayStartForUpgrade(ctx, *instance, runtimeType, pod, rollout)
		unlock()
		if prepareErr != nil {
			lastErr = prepareErr
			continue
		}
		start.upgradeID = strconv.FormatInt(rollout.ID, 10)
		if createErr := s.createUpgradeGateway(ctx, *instance, pod, start); createErr != nil {
			// Once reserved, never try a different pod without authoritative
			// writer reconciliation. Even a lost response can mean success.
			return createErr
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("no rollout %d target pod accepted instance %d: %w", rollout.ID, instanceID, lastErr)
	}
	return fmt.Errorf("no rollout %d target pod accepted instance %d", rollout.ID, instanceID)
}

func (s *RuntimeScheduler) prepareGatewayStart(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod) (*runtimeGatewayStart, error) {
	return s.prepareGatewayStartExcludingPorts(ctx, instance, runtimeType, pod, nil)
}

func (s *RuntimeScheduler) prepareGatewayStartExcludingPorts(
	ctx context.Context,
	instance models.Instance,
	runtimeType string,
	pod models.RuntimePod,
	excludedPorts map[int]struct{},
) (*runtimeGatewayStart, error) {
	return s.prepareGatewayStartOwned(ctx, instance, runtimeType, pod, excludedPorts, nil)
}

func (s *RuntimeScheduler) prepareGatewayStartForUpgrade(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod, rollout *models.RuntimeRollout) (*runtimeGatewayStart, error) {
	return s.prepareGatewayStartOwned(ctx, instance, runtimeType, pod, nil, rollout)
}

func (s *RuntimeScheduler) prepareGatewayStartOwned(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod, excludedPorts map[int]struct{}, rollout *models.RuntimeRollout) (*runtimeGatewayStart, error) {
	if s.agentClient == nil || s.bindingRepo == nil || s.instanceRepo == nil {
		return nil, fmt.Errorf("runtime scheduler gateway dependencies are not configured")
	}
	// Shared by ordinary creation, restart and upgrade recovery. Publish before
	// writing a workspace, reserving ports, or starting any Gateway process.
	if runtimeType == RuntimeTypeOpenClaw || runtimeType == RuntimeTypeHermes {
		if err := s.proxyPolicy.Ensure(ctx, &pod); err != nil {
			return nil, &runtimeFailure{code: "PROXY_POLICY_NOT_READY", cause: fmt.Errorf("prepare Gateway proxy policy: %w", err)}
		}
	}
	endpoint := strings.TrimSpace(*pod.AgentEndpoint)
	workspacePath := RuntimeWorkspacePathWithRoot(s.workspaceRoot, runtimeType, instance.UserID, instance.ID)
	environment, err := s.gatewayEnvironment(&instance)
	if err != nil {
		return nil, fmt.Errorf("build runtime gateway environment: %w", err)
	}
	if runtimeType == RuntimeTypeHermes {
		if web, ok := s.deployments.(interface {
			HermesGatewayEnvironment(context.Context, string, string) (map[string]string, error)
		}); ok {
			trusted, err := web.HermesGatewayEnvironment(ctx, pod.Namespace, pod.DeploymentName)
			if err != nil {
				return nil, fmt.Errorf("prepare Hermes Web trust: %w", err)
			}
			if environment == nil {
				environment = map[string]string{}
			}
			for key, value := range trusted {
				environment[key] = value
			}
		}
	}
	uid, gid := runtimeGatewayLinuxIDs(instance.ID, environment)
	if runtimeType == RuntimeTypeOpenClaw {
		if err := ensureOpenClawPluginLayoutCompatibility(workspacePath, uid, gid); err != nil {
			return nil, fmt.Errorf("prepare OpenClaw workspace compatibility: %w", err)
		}
	}
	var reservedBinding *models.InstanceRuntimeBinding
	if rollout != nil {
		reserver, ok := s.bindingRepo.(repository.UpgradeGatewayReserver)
		if !ok {
			return nil, fmt.Errorf("upgrade reservation repository unavailable")
		}
		reservedBinding, err = reserver.ReserveUpgradeGateway(ctx, repository.UpgradeGatewayReservation{RolloutID: rollout.ID, InstanceID: instance.ID, Generation: instance.RuntimeGeneration, Pod: pod, WorkspacePath: workspacePath, PortStart: s.gatewayPortStart, PortEnd: s.gatewayPortEnd, PortBlockSize: RuntimeGatewayPortBlockSize(runtimeType)})
	} else {
		reservedBinding, err = s.reserveGatewayPortExcludingPorts(ctx, instance, runtimeType, pod, workspacePath, excludedPorts)
	}
	if err != nil {
		return nil, err
	}
	return &runtimeGatewayStart{
		endpoint:        endpoint,
		workspacePath:   workspacePath,
		environment:     environment,
		uid:             uid,
		gid:             gid,
		reservedBinding: reservedBinding,
	}, nil
}

func (s *RuntimeScheduler) createGatewayOnPod(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod, start *runtimeGatewayStart) error {
	if start == nil || start.reservedBinding == nil {
		return fmt.Errorf("runtime gateway start is not prepared")
	}
	resp, err := s.agentClient.CreateGateway(ctx, start.endpoint, RuntimeAgentCreateGatewayRequest{
		InstanceID:          instance.ID,
		UserID:              instance.UserID,
		AgentType:           runtimeType,
		WorkspacePath:       start.workspacePath,
		ProjectRelativePath: strings.TrimSpace(start.environment["CLAWMANAGER_DEFAULT_PROJECT_RELATIVE_PATH"]),
		GatewayPort:         start.reservedBinding.GatewayPort,
		PortRange: RuntimeAgentPortRange{
			Start: s.gatewayPortStart,
			End:   s.gatewayPortEnd,
		},
		UID:         start.uid,
		GID:         start.gid,
		CPUCores:    instance.CPUCores,
		MemoryMB:    instance.MemoryGB * 1024,
		DiskQuotaMB: instance.DiskGB * 1024,
		Generation:  instance.RuntimeGeneration,
		UpgradeID:   start.upgradeID,
		Environment: start.environment,
	})
	if err != nil {
		if runtimeGatewayCreateOutcomeUncertain(err) {
			return s.preservePendingGatewayStart(ctx, instance.ID, instance.RuntimeGeneration, err)
		}
		return s.cleanupGatewayAfterAssignFailure(ctx, start.endpoint, instance.ID, "", true, err)
	}
	if resp == nil {
		return s.preservePendingGatewayStart(ctx, instance.ID, instance.RuntimeGeneration, fmt.Errorf("runtime agent returned empty gateway response"))
	}
	if resp.Port != start.reservedBinding.GatewayPort {
		cause := fmt.Errorf("runtime agent returned gateway port %d, want control-plane allocation %d", resp.Port, start.reservedBinding.GatewayPort)
		return s.cleanupGatewayAfterAssignFailure(ctx, start.endpoint, instance.ID, resp.GatewayID, true, cause)
	}

	now := time.Now().UTC()
	lifecycle := NormalizeRuntimeGatewayLifecycle(resp.Status, nil)
	var lastHealthAt *time.Time
	if lifecycle.Running {
		lastHealthAt = &now
	}
	if err := s.bindingRepo.UpdateGatewayAssignment(ctx, instance.ID, instance.RuntimeGeneration, resp.GatewayID, resp.PID, lifecycle.BindingState, lastHealthAt); err != nil {
		return s.preservePendingGatewayStart(ctx, instance.ID, instance.RuntimeGeneration, fmt.Errorf("persist runtime gateway assignment: %w", err))
	}
	if !lifecycle.CreateAccepted() {
		cause := fmt.Errorf("runtime gateway %s returned status %q", resp.GatewayID, strings.TrimSpace(resp.Status))
		message := cause.Error()
		var errs []error
		if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", instance.RuntimeGeneration, &message); err != nil {
			errs = append(errs, fmt.Errorf("mark instance %d error: %w", instance.ID, err))
		}
		errs = append(errs, s.cleanupGatewayAfterAssignFailure(ctx, start.endpoint, instance.ID, resp.GatewayID, true, cause))
		return errors.Join(errs...)
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, start.workspacePath); err != nil {
		return s.preservePendingGatewayStart(ctx, instance.ID, instance.RuntimeGeneration, fmt.Errorf("persist runtime workspace: %w", err))
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, lifecycle.InstanceState, instance.RuntimeGeneration, lifecycle.Message); err != nil {
		return s.preservePendingGatewayStart(ctx, instance.ID, instance.RuntimeGeneration, fmt.Errorf("persist runtime instance state: %w", err))
	}
	if s.events != nil {
		if err := s.events.Publish(ctx, lifecycle.EventType, map[string]any{
			"instance_id":    instance.ID,
			"runtime_type":   runtimeType,
			"runtime_pod_id": pod.ID,
			"gateway_id":     resp.GatewayID,
			"gateway_port":   resp.Port,
			"gateway_state":  lifecycle.BindingState,
			"workspace_path": start.workspacePath,
			"generation":     instance.RuntimeGeneration,
		}); err != nil {
			log.Printf("runtime scheduler publish event failed: %v", err)
		}
	}
	return nil
}

func (s *RuntimeScheduler) createGatewayOnPodWithPortFallback(
	ctx context.Context,
	instance models.Instance,
	runtimeType string,
	pod models.RuntimePod,
	start *runtimeGatewayStart,
) error {
	if start == nil || start.reservedBinding == nil {
		return fmt.Errorf("runtime gateway start is not prepared")
	}
	excludedPorts := map[int]struct{}{}
	blockSize := RuntimeGatewayPortBlockSize(runtimeType)
	maxAttempts := (s.gatewayPortEnd - s.gatewayPortStart + 1) / maxInt(blockSize, 1)
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := s.createGatewayOnPod(ctx, instance, runtimeType, pod, start)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRuntimeAgentGatewayPortConflict(err) {
			return err
		}
		excludedPorts[start.reservedBinding.GatewayPort] = struct{}{}
		if attempt+1 >= maxAttempts {
			break
		}

		unlock := s.lockGatewayCreateForPod(pod.ID)
		nextStart, prepareErr := s.prepareGatewayStartExcludingPorts(ctx, instance, runtimeType, pod, excludedPorts)
		unlock()
		if prepareErr != nil {
			return errors.Join(lastErr, prepareErr)
		}
		start = nextStart
	}
	return lastErr
}

func isRuntimeAgentGatewayPortConflict(err error) bool {
	if !errors.Is(err, ErrRuntimeAgentConflict) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "requested gateway port") &&
		(strings.Contains(message, "unavailable") || strings.Contains(message, "no free port"))
}

func (s *RuntimeScheduler) reserveGatewayPort(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod, workspacePath string) (*models.InstanceRuntimeBinding, error) {
	return s.reserveGatewayPortExcludingPorts(ctx, instance, runtimeType, pod, workspacePath, nil)
}

func (s *RuntimeScheduler) reserveGatewayPortExcludingPorts(
	ctx context.Context,
	instance models.Instance,
	runtimeType string,
	pod models.RuntimePod,
	workspacePath string,
	excludedPorts map[int]struct{},
) (*models.InstanceRuntimeBinding, error) {
	if s == nil || s.bindingRepo == nil {
		return nil, fmt.Errorf("runtime scheduler binding repository is not configured")
	}
	blockSize := RuntimeGatewayPortBlockSize(runtimeType)
	if blockSize <= 0 {
		blockSize = 1
	}
	existingBindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	if err != nil {
		return nil, fmt.Errorf("list runtime pod %d bindings for gateway port allocation: %w", pod.ID, err)
	}
	for port := s.gatewayPortStart; port+blockSize-1 <= s.gatewayPortEnd; port += blockSize {
		if _, excluded := excludedPorts[port]; excluded {
			continue
		}
		if runtimeGatewayPortBlockOccupied(existingBindings, port, blockSize) {
			deleted, err := s.bindingRepo.DeleteErrorByRuntimePodIDAndGatewayPort(ctx, pod.ID, port)
			if err != nil {
				return nil, fmt.Errorf("clear stale gateway port %d on runtime pod %d: %w", port, pod.ID, err)
			}
			if deleted == 0 {
				continue
			}
		}
		binding := &models.InstanceRuntimeBinding{
			InstanceID:    instance.ID,
			RuntimePodID:  pod.ID,
			RuntimeType:   runtimeType,
			GatewayID:     fmt.Sprintf("pending-%d-%d", instance.ID, instance.RuntimeGeneration),
			GatewayPort:   port,
			WorkspacePath: workspacePath,
			State:         "creating",
			Generation:    instance.RuntimeGeneration,
		}
		if err := s.createRuntimeBindingWithStalePortRecovery(ctx, binding); err != nil {
			return nil, fmt.Errorf("reserve gateway port %d on runtime pod %d: %w", port, pod.ID, err)
		}
		return binding, nil
	}
	return nil, fmt.Errorf("no free gateway port block on runtime pod %d in range %d-%d", pod.ID, s.gatewayPortStart, s.gatewayPortEnd)
}

func runtimeGatewayPortBlockOccupied(bindings []models.InstanceRuntimeBinding, requestedPort, blockSize int) bool {
	requestedEnd := requestedPort + blockSize - 1
	for _, binding := range bindings {
		existingEnd := binding.GatewayPort + blockSize - 1
		if requestedPort <= existingEnd && binding.GatewayPort <= requestedEnd {
			return true
		}
	}
	return false
}

func (s *RuntimeScheduler) createRuntimeBindingWithStalePortRecovery(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	if err := s.bindingRepo.Create(ctx, binding); err != nil {
		deleted, deleteErr := s.bindingRepo.DeleteErrorByRuntimePodIDAndGatewayPort(ctx, binding.RuntimePodID, binding.GatewayPort)
		if deleteErr != nil {
			return errors.Join(err, deleteErr)
		}
		if deleted == 0 {
			return err
		}
		if retryErr := s.bindingRepo.Create(ctx, binding); retryErr != nil {
			return errors.Join(err, retryErr)
		}
	}
	return nil
}
func runtimeGatewayLinuxIDs(instanceID int, environment map[string]string) (int, int) {
	linuxID := RuntimeLinuxID(instanceID)
	if !strings.EqualFold(strings.TrimSpace(environment["CLAWMANAGER_TEAM_ENABLED"]), "true") {
		return linuxID, linuxID
	}
	sharedGID, err := strconv.Atoi(strings.TrimSpace(environment["CLAWMANAGER_TEAM_SHARED_GID"]))
	if err != nil || sharedGID <= 0 {
		return linuxID, linuxID
	}
	return linuxID, sharedGID
}

func (s *RuntimeScheduler) gatewayEnvironment(instance *models.Instance) (map[string]string, error) {
	if s == nil || s.envBuilder == nil {
		return nil, nil
	}
	env, err := s.envBuilder(instance)
	if err != nil {
		return nil, err
	}
	if len(env) == 0 {
		return nil, nil
	}
	copied := make(map[string]string, len(env))
	for key, value := range env {
		if strings.TrimSpace(key) != "" {
			copied[key] = value
		}
	}
	if len(copied) == 0 {
		return nil, nil
	}
	return copied, nil
}

func (s *RuntimeScheduler) cleanupGatewayAfterAssignFailure(ctx context.Context, endpoint string, instanceID int, gatewayID string, bindingCreated bool, cause error) error {
	errs := []error{cause}
	if gatewayID != "" && s.agentClient != nil {
		// The request context may already have expired. Only release the
		// reservation after the Agent confirms that the writer is gone; an
		// unconfirmed delete must remain instance-local and be reconciled by a
		// later authoritative Gateway snapshot.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), runtimeHeartbeatProbeTimeout)
		deleteErr := s.agentClient.DeleteGateway(cleanupCtx, endpoint, gatewayID)
		cancel()
		if deleteErr != nil && !errors.Is(deleteErr, ErrRuntimeAgentNotFound) {
			errs = append(errs, fmt.Errorf("delete gateway %s was not confirmed: %w", gatewayID, deleteErr))
			return s.preservePendingGatewayStart(ctx, instanceID, 0, errors.Join(errs...))
		}
	}
	if bindingCreated && s.bindingRepo != nil {
		if err := s.bindingRepo.DeleteByInstanceID(ctx, instanceID); err != nil {
			errs = append(errs, fmt.Errorf("delete binding for instance %d: %w", instanceID, err))
		}
	}
	return errors.Join(errs...)
}

func runtimeGatewayCreateOutcomeUncertain(err error) bool {
	if err == nil {
		return false
	}
	if runtimeUpgradeInfrastructureRetryable(err) || errors.Is(err, context.Canceled) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "runtime agent status 5") || strings.Contains(message, "runtime agent status 429")
}

func (s *RuntimeScheduler) preservePendingGatewayStart(ctx context.Context, instanceID, generation int, cause error) error {
	if cause == nil {
		cause = errors.New("runtime gateway start outcome is unknown")
	}
	message := cause.Error()
	if s != nil && s.bindingRepo != nil && generation > 0 {
		if err := s.bindingRepo.UpdateState(ctx, instanceID, generation, RuntimeGatewayBindingCreating, &message); err != nil && !errors.Is(err, repository.ErrStaleRuntimeGeneration) {
			cause = errors.Join(cause, fmt.Errorf("preserve pending binding for instance %d: %w", instanceID, err))
		}
	}
	return fmt.Errorf("instance %d Gateway start awaits Agent reconciliation: %w", instanceID, errors.Join(errRuntimeGatewayStartPending, cause))
}

func (s *RuntimeScheduler) markInstanceError(ctx context.Context, instance models.Instance, cause error, errs *[]error) {
	message := cause.Error()
	if errors.Is(cause, ErrModelConfigurationRequired) {
		s.configurationRetryMu.Lock()
		if s.configurationRetryAt == nil {
			s.configurationRetryAt = map[string]time.Time{}
		}
		for key, deadline := range s.configurationRetryAt {
			if time.Now().After(deadline) {
				delete(s.configurationRetryAt, key)
			}
		}
		s.configurationRetryAt[fmt.Sprintf("%d/%d", instance.ID, instance.RuntimeGeneration)] = time.Now().Add(30 * time.Second)
		s.configurationRetryMu.Unlock()
	}
	if repo, ok := s.instanceRepo.(interface {
		UpdateRuntimeFailure(context.Context, int, int, string, string) error
	}); ok {
		if err := repo.UpdateRuntimeFailure(ctx, instance.ID, instance.RuntimeGeneration, message, runtimeFailureCode(cause)); err != nil {
			*errs = append(*errs, fmt.Errorf("mark instance %d error: %w", instance.ID, err))
		}
		return
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", instance.RuntimeGeneration, &message); err != nil {
		*errs = append(*errs, fmt.Errorf("mark instance %d error: %w", instance.ID, err))
	}
}

func (s *RuntimeScheduler) configurationRetryPending(instance models.Instance) bool {
	s.configurationRetryMu.Lock()
	defer s.configurationRetryMu.Unlock()
	return time.Now().Before(s.configurationRetryAt[fmt.Sprintf("%d/%d", instance.ID, instance.RuntimeGeneration)])
}

func schedulerRuntimeType(instance models.Instance) (string, bool) {
	return NormalizeV2RuntimeType(instance.Type)
}

func isSchedulerManagedV2Instance(instance models.Instance) bool {
	if _, ok := schedulerRuntimeType(instance); !ok {
		return false
	}
	if normalizeInstanceRuntimeType(instance.RuntimeType) != RuntimeBackendGateway {
		return false
	}
	if mode, ok := NormalizeInstanceMode(instance.InstanceMode); ok && mode != InstanceModeLite {
		return false
	}
	return instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != ""
}

func isRuntimeErrorInstance(instance models.Instance) bool {
	return strings.EqualFold(strings.TrimSpace(instance.Status), "error")
}

func isRecoverableRuntimeSchedulingError(instance models.Instance) bool {
	if !isRuntimeErrorInstance(instance) || instance.RuntimeErrorMessage == nil {
		return false
	}
	runtimeType, ok := schedulerRuntimeType(instance)
	if !ok {
		return false
	}
	message := strings.TrimSpace(*instance.RuntimeErrorMessage)
	if instance.RuntimeErrorCode != nil {
		switch *instance.RuntimeErrorCode {
		case "MODEL_CONFIGURATION_REQUIRED":
			// Bound retries across controller replicas/restarts without parsing
			// prose. Configuration restoration is retried within thirty seconds.
			return time.Since(instance.UpdatedAt) >= 30*time.Second
		case "RUNTIME_POOL_UNAVAILABLE", "RUNTIME_CAPACITY_EXHAUSTED", "CONTROL_PLANE_UNAVAILABLE":
			return true
		case "GATEWAY_START_FAILED", "PROXY_POLICY_NOT_READY":
			return time.Since(instance.UpdatedAt) >= 30*time.Second
		}
	}
	return message == fmt.Sprintf("no schedulable %s runtime pod", runtimeType) ||
		strings.Contains(message, fmt.Sprintf("no schedulable %s runtime pod:", runtimeType)) ||
		message == fmt.Sprintf("instance %d is held by an active data-safe runtime rollout", instance.ID) ||
		message == "gateway start failed: exit status 1" ||
		isLegacyUnexpectedGatewayExitMessage(message)
}

func isLegacyUnexpectedGatewayExitMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	return normalized == "exit status 1" ||
		strings.HasPrefix(normalized, "gateway process exited unexpectedly") ||
		strings.HasPrefix(normalized, "gateway exited unexpectedly")
}

func isRuntimeAssignmentPending(err error) bool {
	return errors.Is(err, errRuntimeScaleOutPending) ||
		errors.Is(err, errRuntimeGatewayStartPending) ||
		errors.Is(err, errRuntimeUpgradePending) ||
		errors.Is(err, errRuntimeConfigurationPending) ||
		errors.Is(err, errRuntimeControlPlanePending)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
