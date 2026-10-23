package services

import (
	"fmt"
	"math"
	"strings"

	"clawreef/internal/models"
)

// Missing fields preserve saved choices for older clients. Discovery metadata
// alone must never grant permission to execute an image operation.
func applyModelOperations(model, current *models.LLMModel, req SaveLLMModelRequest) error {
	model.ModelKind = models.ModelKindChat
	if current != nil {
		model.ModelKind = models.EffectiveLLMModelKind(current)
		model.ImageGenerationEnabled = current.ImageGenerationEnabled
		model.ImageEditEnabled = current.ImageEditEnabled
		model.ImagePricePerImage = current.ImagePricePerImage
	}
	if req.ModelKind != nil {
		model.ModelKind = strings.ToLower(strings.TrimSpace(*req.ModelKind))
	}
	switch model.ModelKind {
	case models.ModelKindChat, models.ModelKindImage, models.ModelKindOther:
	default:
		return fmt.Errorf("%w: model_kind must be chat, image or other", models.ErrInvalidModelOperations)
	}
	if req.ImageGenerationEnabled != nil {
		model.ImageGenerationEnabled = *req.ImageGenerationEnabled
	}
	if req.ImageEditEnabled != nil {
		model.ImageEditEnabled = *req.ImageEditEnabled
	}
	if req.ResetImagePrice && req.ImagePricePerImage != nil {
		return fmt.Errorf("%w: cannot set and reset image price together", models.ErrInvalidModelOperations)
	}
	if req.ResetImagePrice {
		model.ImagePricePerImage = nil
	}
	if req.ImagePricePerImage != nil {
		price := *req.ImagePricePerImage
		if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 || price > 1000000 {
			return fmt.Errorf("%w: image price must be between 0 and 1000000", models.ErrInvalidModelOperations)
		}
		// Match the database's DECIMAL(18,8) scale without silently converting
		// a tiny positive estimate to the explicit-free sentinel (zero).
		price = math.Round(price*1e8) / 1e8
		if *req.ImagePricePerImage > 0 && price == 0 {
			return fmt.Errorf("%w: positive image price must be at least 0.00000001", models.ErrInvalidModelOperations)
		}
		model.ImagePricePerImage = &price
	}
	if model.ImageGenerationEnabled || model.ImageEditEnabled {
		if !models.LLMNativeImagesAvailable(model) {
			return fmt.Errorf("%w: image generation/editing requires model_kind=image and an OpenAI-compatible protocol; Anthropic Messages does not provide native image generation", models.ErrInvalidModelOperations)
		}
	}
	return nil
}
