package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
)

const (
	deploymentPerformanceBand = 0.20
	deploymentHealthSampleTTL = 5 * time.Minute
)

type deploymentRouteScore struct {
	DeploymentID        int     `json:"deployment_id"`
	State               string  `json:"state"`
	Score               float64 `json:"score"`
	EWMATTFTMs          float64 `json:"ewma_ttft_ms"`
	EWMALatencyMs       float64 `json:"ewma_latency_ms"`
	EWMATokensPerSecond float64 `json:"ewma_tokens_per_second"`
	Inflight            int     `json:"inflight"`
	Samples             int64   `json:"samples"`
	Selected            bool    `json:"selected"`
}

type deploymentRoutingDetails struct {
	Policy     string                 `json:"policy"`
	ModelGroup string                 `json:"model_group"`
	Candidates []deploymentRouteScore `json:"candidates"`
	Fallback   string                 `json:"fallback,omitempty"`
}

type scoredDeployment struct {
	model    *models.LLMModel
	snapshot services.GatewayDeploymentSnapshot
	score    float64
}

type autoDeploymentGroup struct {
	key            string
	representative *models.LLMModel
	candidates     []*models.LLMModel
}

func (s *service) orderAutoDeployments(ctx context.Context, prepared *preparedChatRequest, candidates []*models.LLMModel) []*models.LLMModel {
	return s.orderAutoDeploymentsWithAudit(ctx, prepared, candidates, true)
}

func (s *service) orderAutoDeploymentsWithAudit(ctx context.Context, prepared *preparedChatRequest, candidates []*models.LLMModel, recordAudit bool) []*models.LLMModel {
	if len(candidates) == 0 || s == nil || s.deploymentStore == nil {
		return candidates
	}
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil {
			ids = append(ids, candidate.ID)
		}
	}
	snapshots, err := s.deploymentStore.Snapshots(ctx, ids)
	if err != nil {
		if recordAudit {
			s.recordDeploymentRouting(prepared, deploymentRoutingDetails{Policy: "adaptive-performance-v1", ModelGroup: routingModelGroup(prepared.resolvedModel), Fallback: "redis_unavailable_static_priority"})
		}
		return candidates
	}

	_, reservedOutput := routingTokenEstimate(prepared.req)
	expectedOutput := reservedOutput / 4
	if expectedOutput < 128 {
		expectedOutput = 128
	}
	if expectedOutput > 2048 {
		expectedOutput = 2048
	}
	scored := make([]scoredDeployment, 0, len(candidates))
	unknown := make([]int, 0)
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		snapshot := snapshots[candidate.ID]
		nowMs := snapshot.SnapshotAtMs
		if nowMs <= 0 {
			nowMs = time.Now().UTC().UnixMilli()
		}
		if snapshot.State == "cooldown" && snapshot.CooldownUntilMs > nowMs {
			continue
		}
		lastObservedMs := snapshot.LastSuccessMs
		if snapshot.LastFailureMs > lastObservedMs {
			lastObservedMs = snapshot.LastFailureMs
		}
		if (snapshot.State == "healthy" || snapshot.State == "degraded") &&
			(lastObservedMs == 0 || nowMs-lastObservedMs > deploymentHealthSampleTTL.Milliseconds()) {
			snapshot.State = "unknown"
			// Once the observation expires, its latency and throughput figures are
			// expired as well. Keeping those values would let an old, formerly-fast
			// deployment dominate routing indefinitely despite its unknown health.
			snapshot.EWMATTFTMs = 0
			snapshot.EWMALatencyMs = 0
			snapshot.EWMATokensPerSecond = 0
		}
		ttft := snapshot.EWMATTFTMs
		if ttft <= 0 {
			ttft = 1500
		}
		tps := snapshot.EWMATokensPerSecond
		if tps <= 0 {
			tps = 40
		}
		predicted := ttft + float64(expectedOutput)/tps*1000
		if snapshot.EWMALatencyMs > 0 {
			predicted = 0.7*predicted + 0.3*snapshot.EWMALatencyMs
		}
		loadPenalty := 1.0
		if candidate.MaxConcurrentRequests > 0 {
			loadPenalty += float64(snapshot.Inflight) / float64(candidate.MaxConcurrentRequests)
		} else {
			loadPenalty += math.Min(float64(snapshot.Inflight)*0.05, 1)
		}
		healthPenalty := 1.0
		switch snapshot.State {
		case "degraded":
			healthPenalty = 1.5 + float64(snapshot.ConsecutiveFailures)*0.15
		case "half_open":
			healthPenalty = 2
		case "unknown", "":
			healthPenalty = 1.10
		}
		scored = append(scored, scoredDeployment{model: candidate, snapshot: snapshot, score: predicted * loadPenalty * healthPenalty})
		if snapshot.Samples == 0 || snapshot.State == "unknown" || snapshot.State == "" {
			unknown = append(unknown, candidate.ID)
		}
	}
	if len(scored) == 0 {
		if recordAudit {
			s.recordDeploymentRouting(prepared, deploymentRoutingDetails{Policy: "adaptive-performance-v1", ModelGroup: routingModelGroup(prepared.resolvedModel), Fallback: "all_deployments_in_cooldown"})
		}
		return []*models.LLMModel{}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		return scored[i].model.DeploymentPriority < scored[j].model.DeploymentPriority
	})

	// Give cold/new deployments bounded exploration traffic so they can obtain
	// real TTFT/TPS samples instead of remaining permanently unknown.
	hash := deploymentRouteHash(prepared, time.Now().UTC().Unix()/600)
	selectedIndex := 0
	if len(unknown) > 0 && hash%20 == 0 {
		target := unknown[int((hash/20)%uint64(len(unknown)))]
		for index := range scored {
			if scored[index].model.ID == target {
				selectedIndex = index
				break
			}
		}
	} else {
		best := scored[0].score
		band := make([]int, 0, len(scored))
		for index := range scored {
			if scored[index].score <= best*(1+deploymentPerformanceBand) {
				band = append(band, index)
			}
		}
		if len(band) > 1 {
			first := band[int(hash%uint64(len(band)))]
			second := band[int((hash/uint64(len(band))+1)%uint64(len(band)))]
			if scored[second].score < scored[first].score {
				selectedIndex = second
			} else {
				selectedIndex = first
			}
		}
	}

	orderedScores := make([]scoredDeployment, 0, len(scored))
	orderedScores = append(orderedScores, scored[selectedIndex])
	for index := range scored {
		if index != selectedIndex {
			orderedScores = append(orderedScores, scored[index])
		}
	}
	ordered := make([]*models.LLMModel, 0, len(orderedScores))
	details := deploymentRoutingDetails{Policy: "adaptive-performance-v1", ModelGroup: routingModelGroup(prepared.resolvedModel)}
	for index, item := range orderedScores {
		ordered = append(ordered, item.model)
		details.Candidates = append(details.Candidates, deploymentRouteScore{
			DeploymentID: item.model.ID, State: item.snapshot.State, Score: item.score,
			EWMATTFTMs: item.snapshot.EWMATTFTMs, EWMALatencyMs: item.snapshot.EWMALatencyMs,
			EWMATokensPerSecond: item.snapshot.EWMATokensPerSecond, Inflight: item.snapshot.Inflight,
			Samples: item.snapshot.Samples, Selected: index == 0,
		})
	}
	if recordAudit {
		s.recordDeploymentRouting(prepared, details)
	}
	return ordered
}

// orderAutoDeploymentGroups preserves content/cost selection as the primary
// group, then appends other policy-compatible groups as availability fallbacks.
// Load-aware ordering remains within each logical/security boundary.
func (s *service) orderAutoDeploymentGroups(ctx context.Context, prepared *preparedChatRequest, routing *routingDecision, primary []*models.LLMModel) []*models.LLMModel {
	if s == nil || prepared == nil || routing == nil || prepared.resolvedModel == nil {
		return s.orderAutoDeployments(ctx, prepared, primary)
	}
	items, err := s.modelRepo.ListActive()
	if err != nil {
		return s.orderAutoDeployments(ctx, prepared, primary)
	}
	selectedKey := autoDeploymentGroupKey(prepared.resolvedModel)
	groupsByKey := make(map[string]*autoDeploymentGroup)
	groups := make([]*autoDeploymentGroup, 0)
	orderedItems := orderModelDeployments(items)
	for index := range orderedItems {
		item := orderedItems[index]
		if effectiveQualityTier(&item) < routing.MinimumTier || !routingCandidateAllowed(&item, prepared.req, *routing) {
			continue
		}
		key := autoDeploymentGroupKey(&item)
		group := groupsByKey[key]
		if group == nil {
			candidate := item
			group = &autoDeploymentGroup{key: key, representative: &candidate}
			groupsByKey[key] = group
			groups = append(groups, group)
		}
		candidate := item
		group.candidates = append(group.candidates, &candidate)
	}
	if len(groups) == 0 {
		return []*models.LLMModel{}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].key == selectedKey {
			return true
		}
		if groups[j].key == selectedKey {
			return false
		}
		if routingCurrency(groups[i].representative) != routingCurrency(groups[j].representative) {
			if groups[i].representative.DeploymentPriority != groups[j].representative.DeploymentPriority {
				return groups[i].representative.DeploymentPriority < groups[j].representative.DeploymentPriority
			}
			return groups[i].key < groups[j].key
		}
		left := calculateEstimatedCost(groups[i].representative, routing.PromptTokens, routing.OutputTokens)
		right := calculateEstimatedCost(groups[j].representative, routing.PromptTokens, routing.OutputTokens)
		if left != right {
			return left < right
		}
		return groups[i].key < groups[j].key
	})
	ordered := make([]*models.LLMModel, 0)
	for _, group := range groups {
		recordAudit := group.key == selectedKey
		ordered = append(ordered, s.orderAutoDeploymentsWithAudit(ctx, prepared, group.candidates, recordAudit)...)
	}
	return ordered
}

func autoDeploymentGroupKey(model *models.LLMModel) string {
	if model == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(routingModelGroup(model))) + "|" + strconv.FormatBool(model.IsSecure)
}

func deploymentRouteHash(prepared *preparedChatRequest, epoch int64) uint64 {
	hasher := fnv.New64a()
	if prepared != nil {
		_, _ = hasher.Write([]byte(prepared.sessionID))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(strconv.Itoa(prepared.userID)))
		_, _ = hasher.Write([]byte{0})
	}
	// Do not include the request ID: the same user/session should keep a soft
	// affinity to one similarly-performing deployment during the time bucket.
	_, _ = hasher.Write([]byte(strconv.FormatInt(epoch, 10)))
	return hasher.Sum64()
}

func (s *service) recordDeploymentRouting(prepared *preparedChatRequest, details deploymentRoutingDetails) {
	if s == nil || s.auditEventService == nil || prepared == nil {
		return
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return
	}
	text := string(raw)
	_ = s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID: prepared.traceID, SessionID: prepared.sessionIDPtr, RequestID: prepared.requestIDPtr,
		UserID: prepared.userIDPtr, InstanceID: prepared.req.InstanceID, GatewayKeyID: prepared.req.GatewayKeyID,
		EventType: "gateway.deployment.selected", TrafficClass: models.TrafficClassLLM,
		Severity: models.AuditSeverityInfo, Message: "Selected a healthy model deployment using load-aware routing.", Details: &text,
	})
}

func (s *service) beginDeploymentAttempt(ctx context.Context, prepared *preparedChatRequest, candidate *models.LLMModel) (*services.GatewayDeploymentLease, error) {
	prepared.attemptNumber++
	prepared.attemptTTFTMs = 0
	prepared.attemptGenerationMs = 0
	prepared.attemptTokensPerSecond = 0
	prepared.attemptStatusCode = 0
	prepared.attemptFailureResponse = nil
	prepared.attemptSucceeded = false
	prepared.attemptDispatched = false
	prepared.attemptCompletionTokens = 0
	prepared.attemptPromptTokens, _ = routingTokenEstimate(prepared.req)
	if candidate == nil {
		return nil, nil
	}
	if s == nil || s.deploymentStore == nil {
		if candidate.MaxConcurrentRequests > 0 {
			return nil, services.ErrGatewayDeploymentUnavailable
		}
		return nil, nil
	}
	lease, err := s.deploymentStore.Acquire(ctx, services.GatewayDeploymentAcquireInput{
		DeploymentID: candidate.ID,
		RequestID:    "gdl_" + randomHex(16),
		MaxInflight:  candidate.MaxConcurrentRequests,
	})
	if err != nil && !errors.Is(err, services.ErrGatewayDeploymentUnavailable) {
		// Performance metrics may degrade to static routing, but an explicitly
		// configured capacity limit must never be bypassed on a store failure.
		if candidate.MaxConcurrentRequests > 0 {
			return nil, services.ErrGatewayDeploymentUnavailable
		}
		return nil, nil
	}
	return lease, err
}

func (s *service) finishDeploymentAttempt(prepared *preparedChatRequest, candidate *models.LLMModel, lease *services.GatewayDeploymentLease, latency time.Duration, success, penalize bool) {
	if s == nil || s.deploymentStore == nil || prepared == nil || candidate == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Free scarce capacity first; metrics persistence must not keep a finished
	// request counted as in-flight when Redis is slow.
	_ = s.deploymentStore.Release(ctx, lease)
	if !success && prepared.attemptDispatched {
		prepared.priorAttemptTokens += prepared.attemptPromptTokens + prepared.attemptCompletionTokens
		prepared.priorAttemptCost += calculateEstimatedCost(candidate, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
		s.recordFailedAttemptCost(prepared, candidate)
	}
	// A non-retryable request response, or a 2xx response interrupted only while
	// writing downstream, proves the provider is reachable. Recover the circuit
	// without feeding request-error latency into the performance EWMA.
	healthSuccess := success || (!penalize && prepared.attemptStatusCode >= 200 && prepared.attemptStatusCode < 500)
	observation := services.GatewayDeploymentObservation{
		DeploymentID: candidate.ID, Success: healthSuccess, PenalizeFailure: penalize,
		ImmediateCooldown: penalize && prepared.attemptStatusCode == http.StatusForbidden,
	}
	if success {
		observation.TTFTMs = prepared.attemptTTFTMs
		observation.LatencyMs = int(latency.Milliseconds())
		observation.TokensPerSecond = prepared.attemptTokensPerSecond
	}
	_ = s.deploymentStore.Observe(ctx, observation)
}

func (s *service) recordFailedAttemptCost(prepared *preparedChatRequest, candidate *models.LLMModel) {
	if s == nil || s.costRecordService == nil || prepared == nil || candidate == nil {
		return
	}
	totalTokens := prepared.attemptPromptTokens + prepared.attemptCompletionTokens
	if totalTokens <= 0 {
		return
	}
	estimatedCost := calculateEstimatedCost(candidate, prepared.attemptPromptTokens, prepared.attemptCompletionTokens)
	internalCost := 0.0
	if candidate.IsSecure {
		internalCost = estimatedCost
	}
	if err := s.costRecordService.RecordCost(&models.CostRecord{
		TraceID: prepared.traceID, SessionID: prepared.sessionIDPtr, RequestID: prepared.requestIDPtr,
		UserID: prepared.userIDPtr, InstanceID: prepared.req.InstanceID, GatewayKeyID: prepared.req.GatewayKeyID,
		InstanceMode: runtimeAttributionString(prepared.req.InstanceMode), RuntimeType: runtimeAttributionString(prepared.req.RuntimeType),
		GatewayID: runtimeAttributionString(prepared.req.GatewayID), RuntimePodID: runtimeAttributionInt64(prepared.req.RuntimePodID),
		ModelID: intPtr(candidate.ID), ProviderType: candidate.ProviderType, ModelName: candidate.DisplayName,
		Currency: fallbackCurrency(candidate.Currency), PromptTokens: prepared.attemptPromptTokens,
		CompletionTokens: prepared.attemptCompletionTokens, TotalTokens: totalTokens,
		InputUnitPrice: candidate.InputPrice, OutputUnitPrice: candidate.OutputPrice,
		EstimatedCost: estimatedCost, InternalCost: internalCost,
	}); err != nil {
		logPersistenceError("record failed attempt cost", prepared.traceID, err)
	}
}

func (s *service) finalizeFailedAttemptAccounting(prepared *preparedChatRequest) {
	if prepared == nil || prepared.priorAttemptTokens <= 0 {
		return
	}
	if prepared.req.UsageFinalizer != nil {
		if err := prepared.req.UsageFinalizer(prepared.priorAttemptTokens); err != nil {
			logPersistenceError("finalize failed gateway key token usage", prepared.traceID, err)
		}
	}
	if prepared.req.BudgetController != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := prepared.req.BudgetController.Finalize(ctx, prepared.priorAttemptCost, services.GatewayBudgetCurrency); err != nil {
			logPersistenceError("finalize failed gateway key budget", prepared.traceID, err)
		}
		cancel()
	}
}

func shouldPenalizeDeployment(ctx context.Context, statusCode int, callErr error, responses ...*ProxyResponse) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	var downstreamErr *downstreamStreamWriteError
	if errors.As(callErr, &downstreamErr) {
		return false
	}
	if statusCode != 0 {
		if statusCode == http.StatusForbidden {
			return len(responses) > 0 && isProviderModelPermissionDenied(responses[0])
		}
		return statusCode == 401 || statusCode == 402 || statusCode == 404 || statusCode == 408 || statusCode == 409 || statusCode == 425 || statusCode == 429 || statusCode >= 500
	}
	return callErr != nil
}
