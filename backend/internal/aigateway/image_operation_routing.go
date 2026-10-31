package aigateway

import (
	"context"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// ValidateModel is a capability gate, not an authorization or billing check.
func (r *ImageOperationRequest) ValidateModel(model *models.LLMModel) error {
	if r == nil || !models.LLMNativeImagesAvailable(model) {
		return fmt.Errorf("%w: selected model does not support native image operations", ErrInvalidImageOperation)
	}
	if (r.operation == ImageGenerate && model.ImageGenerationEnabled) || (r.operation == ImageEdit && model.ImageEditEnabled) {
		return nil
	}
	return fmt.Errorf("%w: requested image capability is not enabled on this model", ErrInvalidImageOperation)
}

// Images use their own capability/load policy, never the chat classifier.
func (s *service) resolveImageOperationModel(ctx context.Context, userID int, req *ImageOperationRequest, attribution ChatCompletionRequest) (*models.LLMModel, error) {
	if req == nil || userID <= 0 {
		return nil, models.ErrModelUnavailable
	}
	if err := s.checkRuntimeTask(ctx, attribution); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Model())
	if name == "" {
		return nil, ErrInvalidImageOperation
	}
	if isAutoModelRequest(name) {
		return s.resolveAutoImageModel(ctx, userID, req, attribution)
	}
	model, err := repository.ForModelUser(s.modelRepo, userID).GetByDisplayName(name)
	if err != nil {
		return nil, err
	}
	if model == nil || !model.IsActive || !modelAllowedForGatewayKey(model, attribution.AllowedModelGroups) {
		return nil, models.ErrModelUnavailable
	}
	if err := req.ValidateModel(model); err != nil {
		return nil, err
	}
	return model, nil
}
