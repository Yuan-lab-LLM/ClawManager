package aigateway

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

const imageAutoPolicy = "image-priority-load-latency-v1"

func imageRoutingPolicy(name string) string {
	if isAutoModelRequest(name) {
		return imageAutoPolicy
	}
	return "explicit-image-model"
}

func imageBudgetPriceValid(m *models.LLMModel) bool {
	return m.ImagePricePerImage != nil && !math.IsNaN(*m.ImagePricePerImage) && !math.IsInf(*m.ImagePricePerImage, 0) && *m.ImagePricePerImage >= 0 && *m.ImagePricePerImage <= 1000000 && strings.EqualFold(strings.TrimSpace(m.Currency), services.GatewayBudgetCurrency)
}

func (s *service) resolveAutoImageModel(ctx context.Context, userID int, req *ImageOperationRequest, attr ChatCompletionRequest) (*models.LLMModel, error) {
	if s.deploymentStore == nil || s.riskDetector == nil {
		return nil, imageError(503, "image_governance_unavailable", "Image routing dependencies are unavailable")
	}
	checker := s.modelAccessChecker
	if checker == nil {
		checker, _ = s.modelRepo.(repository.ModelAccessChecker)
	}
	if checker == nil {
		return nil, imageError(503, "image_authorization_unavailable", "Image authorization unavailable")
	}
	items, err := repository.ForModelUser(s.modelRepo, userID).ListActive()
	if err != nil {
		return nil, err
	}
	requireSecure := s.riskDetector.AnalyzeText(req.prompt).HighestAction == models.RiskActionRouteSecureModel
	candidates := []*models.LLMModel{}
	ids := []int{}
	for i := range items {
		m := &items[i]
		if !m.IsActive || req.ValidateModel(m) != nil || !imageBudgetPriceValid(m) || !modelAllowedForGatewayKey(m, attr.AllowedModelGroups) || (requireSecure && !m.IsSecure) {
			continue
		}
		if err := checker.CheckModelAccess(ctx, userID, m.ID); err != nil {
			if errors.Is(err, models.ErrModelUnavailable) {
				continue
			}
			return nil, err
		}
		candidates = append(candidates, m)
		ids = append(ids, m.ID)
	}
	if len(candidates) == 0 {
		return nil, imageError(503, "no_eligible_image_model", "No authorized model is available for this image operation")
	}
	snapshots, err := s.deploymentStore.Snapshots(ctx, ids)
	if err != nil {
		return nil, imageError(503, "image_routing_unavailable", "Image load/health state is unavailable; no provider request was sent")
	}
	ordered := rankImageDeployments(candidates, snapshots, attr.RequestID)
	if len(ordered) == 0 {
		return nil, imageError(503, "no_eligible_image_model", "Image models are busy or cooling down; no provider request was sent")
	}
	return ordered[0], nil
}

// Priority is an administrator preference. Within a priority use image latency
// and capacity, not chat TTFT/TPS or text difficulty. Spread comparable options
// deterministically using the server-issued request ID, without exposing tenants.
func rankImageDeployments(candidates []*models.LLMModel, snapshots map[int]services.GatewayDeploymentSnapshot, requestID *string) []*models.LLMModel {
	type scored struct {
		model *models.LLMModel
		score float64
		tie   uint64
	}
	items := []scored{}
	for _, m := range candidates {
		snap := snapshots[m.ID]
		now := snap.SnapshotAtMs
		if now <= 0 {
			now = time.Now().UTC().UnixMilli()
		}
		if snap.State == "cooldown" && snap.CooldownUntilMs > now {
			continue
		}
		if (snap.State == "half_open" || (snap.State == "cooldown" && snap.CooldownUntilMs <= now)) && snap.Inflight > 0 {
			continue
		}
		if m.MaxConcurrentRequests > 0 && snap.Inflight >= m.MaxConcurrentRequests {
			continue
		}
		latency := 30000.0 // Unknown sample, not a claimed provider SLA.
		last := snap.LastSuccessMs
		if snap.LastFailureMs > last {
			last = snap.LastFailureMs
		}
		fresh := last > 0 && now-last <= deploymentHealthSampleTTL.Milliseconds()
		if fresh && snap.EWMALatencyMs > 0 && !math.IsNaN(snap.EWMALatencyMs) && !math.IsInf(snap.EWMALatencyMs, 0) {
			latency = snap.EWMALatencyMs
		}
		load := 1.0 + float64(max(snap.Inflight, 0))*.05
		if m.MaxConcurrentRequests > 0 {
			load = 1 + float64(max(snap.Inflight, 0))/float64(m.MaxConcurrentRequests)
		}
		if fresh && snap.State == "degraded" {
			load *= 1.5
		}
		if snap.State == "half_open" || snap.State == "cooldown" {
			load *= 2
		}
		hash := fnv.New64a()
		if requestID != nil {
			_, _ = hash.Write([]byte(*requestID))
		}
		_, _ = hash.Write([]byte("/" + m.DisplayName))
		items = append(items, scored{m, latency * load, hash.Sum64()})
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.model.DeploymentPriority != b.model.DeploymentPriority {
			return a.model.DeploymentPriority < b.model.DeploymentPriority
		}
		if a.score != b.score {
			return a.score < b.score
		}
		if *a.model.ImagePricePerImage != *b.model.ImagePricePerImage {
			return *a.model.ImagePricePerImage < *b.model.ImagePricePerImage
		}
		if a.tie != b.tie {
			return a.tie < b.tie
		}
		return a.model.ID < b.model.ID
	})
	result := make([]*models.LLMModel, 0, len(items))
	for _, item := range items {
		result = append(result, item.model)
	}
	return result
}
