package handlers

import (
	"net/http"
	"strconv"

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
	ID                int     `json:"id,omitempty"`
	DisplayName       string  `json:"display_name" binding:"required"`
	Description       *string `json:"description,omitempty"`
	ProviderType      string  `json:"provider_type" binding:"required"`
	BaseURL           string  `json:"base_url" binding:"required"`
	ProviderModelName string  `json:"provider_model_name" binding:"required"`
	APIKey            *string `json:"api_key,omitempty"`
	APIKeySecretRef   *string `json:"api_key_secret_ref,omitempty"`
	IsSecure          bool    `json:"is_secure"`
	IsActive          bool    `json:"is_active"`
	InputPrice        float64 `json:"input_price"`
	OutputPrice       float64 `json:"output_price"`
	Currency          string  `json:"currency,omitempty"`
}

// DiscoverLLMModelsRequest defines fields needed to fetch provider models.
type DiscoverLLMModelsRequest struct {
	ProviderType    string  `json:"provider_type" binding:"required"`
	BaseURL         string  `json:"base_url" binding:"required"`
	APIKey          *string `json:"api_key,omitempty"`
	APIKeySecretRef *string `json:"api_key_secret_ref,omitempty"`
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
		ID:                req.ID,
		DisplayName:       req.DisplayName,
		Description:       req.Description,
		ProviderType:      req.ProviderType,
		BaseURL:           req.BaseURL,
		ProviderModelName: req.ProviderModelName,
		APIKey:            req.APIKey,
		APIKeySecretRef:   req.APIKeySecretRef,
		IsSecure:          req.IsSecure,
		IsActive:          req.IsActive,
		InputPrice:        req.InputPrice,
		OutputPrice:       req.OutputPrice,
		Currency:          req.Currency,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "LLM model saved successfully", item)
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
