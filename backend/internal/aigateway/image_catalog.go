package aigateway

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
)

// Separate from the chat catalog: image deployments must never become chat
// candidates. This projection deliberately contains no provider credentials/URL.
type ImageCatalogModel struct {
	ID                     string           `json:"id"`
	Operations             []ImageOperation `json:"operations"`
	EstimatedPricePerImage float64          `json:"estimated_price_per_image"`
	Currency               string           `json:"currency"`
}

func (s *service) ImageCatalog(ctx context.Context, userID int, groups []string) ([]ImageCatalogModel, error) {
	if userID <= 0 {
		return nil, models.ErrModelUnavailable
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
	result := []ImageCatalogModel{}
	for _, m := range items {
		if !m.IsActive || !models.LLMNativeImagesAvailable(&m) || !modelAllowedForGatewayKey(&m, groups) || (!m.ImageGenerationEnabled && !m.ImageEditEnabled) || m.ImagePricePerImage == nil || !strings.EqualFold(strings.TrimSpace(m.Currency), services.GatewayBudgetCurrency) {
			continue
		}
		price := *m.ImagePricePerImage
		if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price > 1000000 {
			continue
		}
		if err := checker.CheckModelAccess(ctx, userID, m.ID); err != nil {
			if errors.Is(err, models.ErrModelUnavailable) {
				continue
			}
			return nil, err
		}
		entry := ImageCatalogModel{ID: m.DisplayName, EstimatedPricePerImage: price, Currency: services.GatewayBudgetCurrency, Operations: []ImageOperation{}}
		if m.ImageGenerationEnabled {
			entry.Operations = append(entry.Operations, ImageGenerate)
		}
		if m.ImageEditEnabled {
			entry.Operations = append(entry.Operations, ImageEdit)
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Agents receive safe authenticated references rather than injecting megabytes
// of generated pixels and provider-rewritten prompts into the chat context.
func (r *ImageExecutionResponse) ToolReferences() map[string]interface{} {
	ids := make([]string, 0, len(r.Data))
	for _, item := range r.Data {
		ids = append(ids, item.ArtifactID)
	}
	return map[string]interface{}{"request_id": r.TraceID, "artifact_ids": ids, "created": r.Created, "clawmanager": r.Accounting}
}
