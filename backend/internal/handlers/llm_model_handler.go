package handlers

import (
	"net/http"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// LLMModelHandler handles admin model catalog requests.
type LLMModelHandler struct {
	service services.LLMModelService
}

// UpsertLLMModelRequest defines editable fields for model catalog entries.
type UpsertLLMModelRequest struct {
	VisibilityScope            *string                   `json:"visibility_scope,omitempty"`
	AllowedUserIDs             *[]int                    `json:"allowed_user_ids,omitempty"`
	AccessPolicyVersion        *int                      `json:"access_policy_version,omitempty"`
	ID                         int                       `json:"id,omitempty"`
	DisplayName                string                    `json:"display_name" binding:"required"`
	Description                *string                   `json:"description,omitempty"`
	ProviderType               string                    `json:"provider_type" binding:"required"`
	ProtocolType               string                    `json:"protocol_type,omitempty"`
	ModelKind                  *string                   `json:"model_kind,omitempty"`
	ImageGenerationEnabled     *bool                     `json:"image_generation_enabled,omitempty"`
	ImageEditEnabled           *bool                     `json:"image_edit_enabled,omitempty"`
	ImagePricePerImage         *float64                  `json:"image_price_per_image,omitempty"`
	ResetImagePrice            bool                      `json:"reset_image_price,omitempty"`
	ModelGroup                 string                    `json:"model_group,omitempty"`
	DeploymentPriority         int                       `json:"deployment_priority,omitempty"`
	MaxConcurrentRequests      int                       `json:"max_concurrent_requests,omitempty"`
	ContextWindowTokens        int                       `json:"context_window_tokens,omitempty"`
	QualityTier                *int                      `json:"quality_tier,omitempty"`
	BaseURL                    string                    `json:"base_url" binding:"required"`
	ProviderModelName          string                    `json:"provider_model_name" binding:"required"`
	ReasoningEnabled           *bool                     `json:"reasoning_enabled,omitempty"`
	ReasoningControlMode       *string                   `json:"reasoning_control_mode,omitempty"`
	APIKey                     *string                   `json:"api_key,omitempty"`
	APIKeySecretRef            *string                   `json:"api_key_secret_ref,omitempty"`
	IsSecure                   bool                      `json:"is_secure"`
	IsActive                   bool                      `json:"is_active"`
	InputPrice                 float64                   `json:"input_price"`
	OutputPrice                float64                   `json:"output_price"`
	Currency                   string                    `json:"currency,omitempty"`
	SupportsTools              bool                      `json:"supports_tools,omitempty"`
	ToolsSupportOverride       *bool                     `json:"tools_support_override,omitempty"`
	SupportsVision             bool                      `json:"supports_vision,omitempty"`
	VisionSupportOverride      *bool                     `json:"vision_support_override,omitempty"`
	ResetVisionSupportOverride bool                      `json:"reset_vision_support_override,omitempty"`
	CapabilitySource           string                    `json:"capability_source,omitempty"`
	ProviderModels             []models.LLMProviderModel `json:"provider_models,omitempty"`
}

// DiscoverLLMModelsRequest defines fields needed to fetch provider models.
type DiscoverLLMModelsRequest struct {
	ProviderType    string  `json:"provider_type" binding:"required"`
	ProtocolType    string  `json:"protocol_type,omitempty"`
	BaseURL         string  `json:"base_url" binding:"required"`
	APIKey          *string `json:"api_key,omitempty"`
	APIKeySecretRef *string `json:"api_key_secret_ref,omitempty"`
}

type BatchUpsertLLMModelsRequest struct {
	Models []UpsertLLMModelRequest `json:"models" binding:"required,min=1,max=200"`
}

// NewLLMModelHandler creates a new model catalog handler.
func NewLLMModelHandler(service services.LLMModelService) *LLMModelHandler {
	return &LLMModelHandler{service: service}
}

// ListModels returns all configured models for admin management.
func (h *LLMModelHandler) ListModels(c *gin.Context) {
	items, err := h.service.ListModels()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM models retrieved successfully", gin.H{
		"items": items,
	})
}

// UpsertModel creates or updates a model catalog entry.
func (h *LLMModelHandler) UpsertModel(c *gin.Context) {
	var req UpsertLLMModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	item, err := h.service.SaveModel(services.SaveLLMModelRequest{
		VisibilityScope: req.VisibilityScope, AllowedUserIDs: req.AllowedUserIDs, AccessPolicyVersion: req.AccessPolicyVersion,
		ID:                         req.ID,
		DisplayName:                req.DisplayName,
		Description:                req.Description,
		ProviderType:               req.ProviderType,
		ProtocolType:               req.ProtocolType,
		ModelKind:                  req.ModelKind,
		ImageGenerationEnabled:     req.ImageGenerationEnabled,
		ImageEditEnabled:           req.ImageEditEnabled,
		ImagePricePerImage:         req.ImagePricePerImage,
		ResetImagePrice:            req.ResetImagePrice,
		ModelGroup:                 req.ModelGroup,
		DeploymentPriority:         req.DeploymentPriority,
		MaxConcurrentRequests:      req.MaxConcurrentRequests,
		ContextWindowTokens:        req.ContextWindowTokens,
		QualityTier:                req.QualityTier,
		BaseURL:                    req.BaseURL,
		ProviderModelName:          req.ProviderModelName,
		ReasoningEnabled:           req.ReasoningEnabled,
		ReasoningControlMode:       req.ReasoningControlMode,
		APIKey:                     req.APIKey,
		APIKeySecretRef:            req.APIKeySecretRef,
		IsSecure:                   req.IsSecure,
		IsActive:                   req.IsActive,
		InputPrice:                 req.InputPrice,
		OutputPrice:                req.OutputPrice,
		Currency:                   req.Currency,
		SupportsTools:              req.SupportsTools,
		ToolsSupportOverride:       req.ToolsSupportOverride,
		SupportsVision:             req.SupportsVision,
		VisionSupportOverride:      req.VisionSupportOverride,
		ResetVisionSupportOverride: req.ResetVisionSupportOverride,
		CapabilitySource:           req.CapabilitySource,
		ProviderModels:             req.ProviderModels,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM model saved successfully", item)
}

// BatchUpsertModels saves a discovered model plan as one admin operation.
// Individual validation errors are returned without aborting successful items.
func (h *LLMModelHandler) BatchUpsertModels(c *gin.Context) {
	var req BatchUpsertLLMModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	created := make([]*models.LLMModel, 0, len(req.Models))
	failed := make([]gin.H, 0)
	for _, item := range req.Models {
		saved, err := h.service.SaveModel(services.SaveLLMModelRequest{
			VisibilityScope: item.VisibilityScope, AllowedUserIDs: item.AllowedUserIDs, AccessPolicyVersion: item.AccessPolicyVersion,
			ID: item.ID, DisplayName: item.DisplayName, Description: item.Description,
			ProviderType: item.ProviderType, ProtocolType: item.ProtocolType, ModelGroup: item.ModelGroup,
			ModelKind: item.ModelKind, ImageGenerationEnabled: item.ImageGenerationEnabled, ImageEditEnabled: item.ImageEditEnabled,
			ImagePricePerImage: item.ImagePricePerImage, ResetImagePrice: item.ResetImagePrice,
			DeploymentPriority: item.DeploymentPriority, MaxConcurrentRequests: item.MaxConcurrentRequests,
			ContextWindowTokens: item.ContextWindowTokens, QualityTier: item.QualityTier,
			BaseURL: item.BaseURL, ProviderModelName: item.ProviderModelName,
			ReasoningEnabled: item.ReasoningEnabled, APIKey: item.APIKey, APIKeySecretRef: item.APIKeySecretRef,
			ReasoningControlMode: item.ReasoningControlMode,
			IsSecure:             item.IsSecure, IsActive: item.IsActive, InputPrice: item.InputPrice,
			OutputPrice: item.OutputPrice, Currency: item.Currency,
			SupportsTools: item.SupportsTools, SupportsVision: item.SupportsVision,
			ToolsSupportOverride:       item.ToolsSupportOverride,
			VisionSupportOverride:      item.VisionSupportOverride,
			ResetVisionSupportOverride: item.ResetVisionSupportOverride,
			CapabilitySource:           item.CapabilitySource,
		})
		if err != nil {
			failed = append(failed, gin.H{"display_name": item.DisplayName, "provider_model_name": item.ProviderModelName, "error": err.Error()})
			continue
		}
		created = append(created, saved)
	}
	utils.Success(c, http.StatusOK, "LLM models batch saved", gin.H{"saved": created, "failed": failed, "saved_count": len(created), "failed_count": len(failed)})
}

// DiscoverModels fetches available models from the configured provider.
func (h *LLMModelHandler) DiscoverModels(c *gin.Context) {
	var req DiscoverLLMModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	items, err := h.service.DiscoverProviderModels(services.DiscoverLLMModelsRequest{
		ProviderType:    req.ProviderType,
		ProtocolType:    req.ProtocolType,
		BaseURL:         req.BaseURL,
		APIKey:          req.APIKey,
		APIKeySecretRef: req.APIKeySecretRef,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM provider models retrieved successfully", gin.H{
		"items": items,
	})
}

// DeleteModel removes a configured model.
func (h *LLMModelHandler) DeleteModel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid model ID")
		return
	}

	if err := h.service.DeleteModel(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM model deleted successfully", nil)
}
