package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
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
	tick         time.Duration

	workspaceRoot     string
	runtimeNamespace  string
	gatewayPortStart  int
	gatewayPortEnd    int
	heartbeatTimeout  time.Duration
	maxGatewaysPerPod int
}

var errRuntimeScaleOutPending = errors.New("runtime scale-out pending")

const runtimeRolloutStaleWindowMultiplier = 3

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

func WithRuntimeSchedulerGatewayEnvBuilder(builder RuntimeGatewayEnvBuilder) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) {
		s.envBuilder = builder
	}
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
		instanceRepo:      instanceRepo,
		podRepo:           podRepo,
		bindingRepo:       bindingRepo,
		rolloutRepo:       rolloutRepo,
		agentClient:       agentClient,
		events:            events,
		leader:            leader,
		deployments:       deployments,
		tick:              tick,
		workspaceRoot:     "/workspaces",
		runtimeNamespace:  "clawmanager-system",
		gatewayPortStart:  RuntimeGatewayPortStart,
		gatewayPortEnd:    RuntimeGatewayPortEnd,
		heartbeatTimeout:  10 * time.Second,
		maxGatewaysPerPod: RuntimePodCapacity,
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
		nextGeneration := binding.Generation + 1
		instance, err := s.instanceRepo.GetByID(binding.InstanceID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if instance != nil {
			nextGeneration = instance.RuntimeGeneration + 1
		}
		message := reason
		if err := s.instanceRepo.UpdateRuntimeState(ctx, binding.InstanceID, "creating", nextGeneration, &message); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.bindingRepo.DeleteByInstanceID(ctx, binding.InstanceID); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.podRepo.ReleaseSlot(ctx, podID); err != nil {
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
	currentPods := s.currentRuntimePods(allPods, time.Now().UTC())
	if runtimePodsAlreadyAtImage(currentPods, rollout.RuntimeType, rollout.TargetImageRef) {
		finishedAt := time.Now().UTC()
		return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "finished", &startedAt, &finishedAt, nil)
	}
	batchSize := rollout.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	maxUnavailable := rollout.MaxUnavailable
	if maxUnavailable <= 0 {
		maxUnavailable = 1
	}
	if err := s.rolloutRuntimeDeployments(ctx, rollout, allPods, maxUnavailable, batchSize); err != nil {
		message := err.Error()
		_ = s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "error", &startedAt, nil, &message)
		return err
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
	type deploymentRef struct {
		namespace string
		name      string
	}
	refs := map[deploymentRef]struct{}{}
	for _, pod := range pods {
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
		if err := s.deployments.RolloutImage(ctx, ref.namespace, ref.name, targetImage, maxUnavailable, maxSurge); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
			ID:             -int64(index + 1),
			RuntimeType:    pod.RuntimeType,
			Namespace:      pod.Namespace,
			PodName:        pod.PodName,
			PodIP:          pod.PodIP,
			NodeName:       pod.NodeName,
			DeploymentName: pod.DeploymentName,
			ImageRef:       pod.ImageRef,
			State:          runtimeDeploymentFallbackState(pod.State),
			Capacity:       s.maxGatewaysPerPod,
			UsedSlots:      0,
			Draining:       false,
		})
	}
	return result, nil
}

func defaultRuntimeDeploymentName(runtimeType string) string {
	switch strings.ToLower(strings.TrimSpace(runtimeType)) {
	case RuntimeTypeOpenClaw:
		return "openclaw-runtime"
	case RuntimeTypeHermes:
		return "hermes-runtime"
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
	targetImage := strings.TrimSpace(rollout.TargetImageRef)
	if targetImage == "" {
		return nil
	}
	pods, err := s.podRepo.List(ctx, rollout.RuntimeType)
	if err != nil {
		return err
	}
	pods = s.currentRuntimePods(pods, time.Now().UTC())
	if len(pods) == 0 {
		return nil
	}
	for _, pod := range pods {
		if pod.RuntimeType != rollout.RuntimeType {
			continue
		}
		if pod.State != "ready" || pod.Draining || strings.TrimSpace(pod.ImageRef) != targetImage {
			return nil
		}
	}
	finishedAt := time.Now().UTC()
	return s.rolloutRepo.UpdateStatus(ctx, rollout.ID, "finished", rollout.StartedAt, &finishedAt, nil)
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
	creating, err := s.instanceRepo.GetV2Creating(ctx, 100)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, instance := range creating {
			if !isSchedulerManagedV2Instance(instance) {
				continue
			}
			binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
			if err != nil {
				errs = append(errs, fmt.Errorf("get binding for creating instance %d: %w", instance.ID, err))
				continue
			}
			if binding != nil {
				if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
					errs = append(errs, fmt.Errorf("sync creating instance %d from binding: %w", instance.ID, err))
				}
				continue
			}
			if err := s.assignInstance(ctx, instance); err != nil {
				if errors.Is(err, errRuntimeScaleOutPending) {
					continue
				}
				errs = append(errs, fmt.Errorf("assign creating instance %d: %w", instance.ID, err))
				s.markInstanceError(ctx, instance, err, &errs)
			}
		}
	}

	desired, err := s.instanceRepo.GetV2DesiredRunning(ctx, 100)
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, instance := range desired {
			if !isSchedulerManagedV2Instance(instance) {
				continue
			}
			if isRuntimeErrorInstance(instance) && !isRecoverableRuntimeSchedulingError(instance) {
				continue
			}
			binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instance.ID)
			if err != nil {
				errs = append(errs, fmt.Errorf("get running binding for instance %d: %w", instance.ID, err))
				continue
			}
			if binding != nil {
				if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
					errs = append(errs, fmt.Errorf("sync desired instance %d from binding: %w", instance.ID, err))
				}
				continue
			}
			binding, err = s.bindingRepo.GetByInstanceID(ctx, instance.ID)
			if err != nil {
				errs = append(errs, fmt.Errorf("get binding for desired instance %d: %w", instance.ID, err))
				continue
			}
			if binding != nil {
				if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
					errs = append(errs, fmt.Errorf("sync desired instance %d from binding: %w", instance.ID, err))
				}
				continue
			}
			if assignErr := s.assignInstance(ctx, instance); assignErr != nil {
				if errors.Is(assignErr, errRuntimeScaleOutPending) {
					continue
				}
				errs = append(errs, fmt.Errorf("assign desired instance %d: %w", instance.ID, assignErr))
				s.markInstanceError(ctx, instance, assignErr, &errs)
			}
		}
	}
	return errors.Join(errs...)
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
	case "error", "failed":
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
	cutoff := time.Now().UTC().Add(-s.heartbeatTimeout)
	var errs []error
	for _, pod := range pods {
		if pod.State == "unhealthy" || pod.State == "pending" {
			continue
		}
		if pod.LastSeenAt == nil || !pod.LastSeenAt.Before(cutoff) {
			continue
		}
		reason := fmt.Sprintf("runtime pod heartbeat lost since %s", pod.LastSeenAt.UTC().Format(time.RFC3339))
		if err := s.FailoverPod(ctx, pod.ID, reason); err != nil {
			errs = append(errs, fmt.Errorf("failover stale runtime pod %d: %w", pod.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) assignInstance(ctx context.Context, instance models.Instance) error {
	if s == nil || s.podRepo == nil {
		return fmt.Errorf("runtime scheduler pod repository is not configured")
	}
	if !isSchedulerManagedV2Instance(instance) {
		return fmt.Errorf("instance %d is not scheduler-managed v2", instance.ID)
	}
	if s.bindingRepo != nil {
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
		if err != nil {
			return fmt.Errorf("failed to check existing binding: %w", err)
		}
		if binding != nil {
			return nil
		}
	}
	runtimeType, ok := schedulerRuntimeType(instance)
	if !ok {
		return fmt.Errorf("unsupported runtime type %q", instance.Type)
	}
	pods, err := s.podRepo.ListSchedulable(ctx, runtimeType)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		scaled, err := s.scaleOutIfAtCapacity(ctx, runtimeType)
		if err != nil {
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
		claimed, err := s.podRepo.TryClaimSlot(ctx, pod.ID)
		if err != nil {
			lastErr = err
			continue
		}
		if !claimed {
			continue
		}
		if err := s.createGatewayOnPod(ctx, instance, runtimeType, pod); err != nil {
			if releaseErr := s.podRepo.ReleaseSlot(ctx, pod.ID); releaseErr != nil {
				return errors.Join(err, releaseErr)
			}
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("no schedulable %s runtime pod: %w", runtimeType, lastErr)
	}
	return fmt.Errorf("no schedulable %s runtime pod", runtimeType)
}

type runtimeDeploymentCapacity struct {
	namespace string
	name      string
	active    int
	full      int
}

func (s *RuntimeScheduler) scaleOutIfAtCapacity(ctx context.Context, runtimeType string) (bool, error) {
	if s == nil || s.deployments == nil || s.podRepo == nil {
		return false, nil
	}
	pods, err := s.podRepo.List(ctx, runtimeType)
	if err != nil {
		return false, fmt.Errorf("list %s runtime pods for scale-out: %w", runtimeType, err)
	}
	groups := map[string]*runtimeDeploymentCapacity{}
	for _, pod := range pods {
		if pod.RuntimeType != runtimeType || pod.State != "ready" || pod.Draining || pod.Capacity <= 0 {
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
		group.active++
		if pod.UsedSlots >= pod.Capacity {
			group.full++
		}
	}
	var target *runtimeDeploymentCapacity
	for _, group := range groups {
		if group.active == 0 || group.active != group.full {
			continue
		}
		if target == nil || group.active > target.active {
			target = group
		}
	}
	if target == nil {
		return false, nil
	}
	replicas := int32(target.active + 1)
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

func (s *RuntimeScheduler) createGatewayOnPod(ctx context.Context, instance models.Instance, runtimeType string, pod models.RuntimePod) error {
	if s.agentClient == nil || s.bindingRepo == nil || s.instanceRepo == nil {
		return fmt.Errorf("runtime scheduler gateway dependencies are not configured")
	}
	endpoint := strings.TrimSpace(*pod.AgentEndpoint)
	workspacePath := RuntimeWorkspacePathWithRoot(s.workspaceRoot, runtimeType, instance.UserID, instance.ID)
	environment, err := s.gatewayEnvironment(&instance)
	if err != nil {
		return fmt.Errorf("build runtime gateway environment: %w", err)
	}
	uid, gid := runtimeGatewayLinuxIDs(instance.ID, environment)
	resp, err := s.agentClient.CreateGateway(ctx, endpoint, RuntimeAgentCreateGatewayRequest{
		InstanceID:    instance.ID,
		UserID:        instance.UserID,
		AgentType:     runtimeType,
		WorkspacePath: workspacePath,
		PortRange: RuntimeAgentPortRange{
			Start: s.gatewayPortStart,
			End:   s.gatewayPortEnd,
		},
		UID:         uid,
		GID:         gid,
		CPUCores:    instance.CPUCores,
		MemoryMB:    instance.MemoryGB * 1024,
		DiskQuotaMB: instance.DiskGB * 1024,
		Generation:  instance.RuntimeGeneration,
		Environment: environment,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("runtime agent returned empty gateway response")
	}

	now := time.Now().UTC()
	gatewayState := normalizeRuntimeGatewayCreateState(resp.Status)
	binding := &models.InstanceRuntimeBinding{
		InstanceID:    instance.ID,
		RuntimePodID:  pod.ID,
		RuntimeType:   runtimeType,
		GatewayID:     resp.GatewayID,
		GatewayPort:   resp.Port,
		GatewayPID:    resp.PID,
		WorkspacePath: workspacePath,
		State:         gatewayState,
		Generation:    instance.RuntimeGeneration,
	}
	if gatewayState == "running" {
		binding.LastHealthAt = &now
	}
	if err := s.bindingRepo.Create(ctx, binding); err != nil {
		return s.cleanupGatewayAfterAssignFailure(ctx, endpoint, instance.ID, resp.GatewayID, false, err)
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, workspacePath); err != nil {
		return s.cleanupGatewayAfterAssignFailure(ctx, endpoint, instance.ID, resp.GatewayID, true, err)
	}
	instanceState := "creating"
	var stateMessage *string
	if gatewayState == "running" {
		instanceState = "running"
	} else {
		message := "runtime gateway starting"
		stateMessage = &message
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, instanceState, instance.RuntimeGeneration, stateMessage); err != nil {
		return s.cleanupGatewayAfterAssignFailure(ctx, endpoint, instance.ID, resp.GatewayID, true, err)
	}
	if s.events != nil {
		eventType := "runtime.instance.starting"
		if gatewayState == "running" {
			eventType = "runtime.instance.running"
		}
		if err := s.events.Publish(ctx, eventType, map[string]any{
			"instance_id":    instance.ID,
			"runtime_type":   runtimeType,
			"runtime_pod_id": pod.ID,
			"gateway_id":     resp.GatewayID,
			"gateway_port":   resp.Port,
			"gateway_state":  gatewayState,
			"workspace_path": workspacePath,
			"generation":     instance.RuntimeGeneration,
		}); err != nil {
			log.Printf("runtime scheduler publish event failed: %v", err)
		}
	}
	return nil
}

func normalizeRuntimeGatewayCreateState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "ready", "healthy":
		return "running"
	default:
		return "starting"
	}
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
		if err := s.agentClient.DeleteGateway(ctx, endpoint, gatewayID); err != nil {
			errs = append(errs, fmt.Errorf("delete gateway %s: %w", gatewayID, err))
		}
	}
	if bindingCreated && s.bindingRepo != nil {
		if err := s.bindingRepo.DeleteByInstanceID(ctx, instanceID); err != nil {
			errs = append(errs, fmt.Errorf("delete binding for instance %d: %w", instanceID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) markInstanceError(ctx context.Context, instance models.Instance, cause error, errs *[]error) {
	message := cause.Error()
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", instance.RuntimeGeneration, &message); err != nil {
		*errs = append(*errs, fmt.Errorf("mark instance %d error: %w", instance.ID, err))
	}
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
	return message == fmt.Sprintf("no schedulable %s runtime pod", runtimeType)
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
