package handlers

import (
	"context"
	"errors"
	"net/http"

	"clawreef/internal/aigateway"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

func (h *AIGatewayHandler) ImageGenerations(c *gin.Context) {
	h.imageOperation(c, aigateway.ImageGenerate)
}
func (h *AIGatewayHandler) ImageEdits(c *gin.Context) { h.imageOperation(c, aigateway.ImageEdit) }

func (h *AIGatewayHandler) ImageModels(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, ok := gatewayUserID(c)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	catalog, ok := h.service.(interface {
		ImageCatalog(context.Context, int, []string) ([]aigateway.ImageCatalogModel, error)
	})
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Image catalog unavailable")
		return
	}
	_, groups := imageReadScope(c, userID)
	items, err := catalog.ImageCatalog(c.Request.Context(), userID, groups)
	if err != nil {
		h.writeImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": items})
}

func (h *AIGatewayHandler) imageOperation(c *gin.Context, operation aigateway.ImageOperation) {
	// OpenAI SDK honors this hint. The Gateway itself never retries paid images.
	c.Header("x-should-retry", "false")
	c.Header("Cache-Control", "no-store")
	resultMode := c.GetHeader("X-ClawManager-Image-Result")
	if resultMode != "" && resultMode != "references" {
		c.JSON(400, gin.H{"error": gin.H{"code": "invalid_image_result_mode", "message": "Use references or omit X-ClawManager-Image-Result"}})
		return
	}
	userID, ok := gatewayUserID(c)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	images, ok := h.service.(aigateway.ImagesService)
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Image service unavailable")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, aigateway.MaxVisionRequestBytes)
	req, err := aigateway.ParseImageOperationRequest(operation, c.GetHeader("Content-Type"), c.Request.Body)
	if err != nil {
		h.writeImageError(c, err)
		return
	}
	attribution := aigateway.ChatCompletionRequest{Model: req.Model()}
	attribution.ImageIdempotencyKey = c.GetHeader("Idempotency-Key")
	if !aigateway.ValidateImageIdempotencyKey(attribution.ImageIdempotencyKey) {
		c.JSON(400, gin.H{"error": gin.H{"code": "invalid_idempotency_key", "message": "Idempotency-Key must contain at most 200 visible ASCII characters"}})
		return
	}
	if !h.applyGatewayRequestContext(c, &attribution) {
		return
	}
	lease, ok := h.acquireGatewayLeaseWithEstimate(c, aigateway.EstimateImageRequestTokens(req))
	if !ok {
		return
	}
	h.attachGatewayUsageFinalizer(&attribution, lease)
	defer h.releaseGatewayKeyLease(lease)
	defer h.releaseGatewayBudget(&attribution)
	result, err := images.ExecuteImage(c.Request.Context(), userID, req, attribution)
	if err != nil {
		h.writeImageError(c, err)
		return
	}
	c.Header("X-Trace-ID", result.TraceID)
	if resultMode == "references" {
		c.JSON(http.StatusOK, result.ToolReferences())
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AIGatewayHandler) ImageArtifact(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	userID, ok := gatewayUserID(c)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	images, ok := h.service.(aigateway.ImagesService)
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Image service unavailable")
		return
	}
	owner, groups := imageReadScope(c, userID)
	artifact, err := images.LoadImageArtifact(c.Request.Context(), owner, c.Param("artifactID"), groups)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "image_artifact_unavailable", "message": "Image unavailable or expired"}})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=generated-image")
	c.Data(http.StatusOK, artifact.MediaType, artifact.Data)
}

func (h *AIGatewayHandler) ImageRequestStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, ok := gatewayUserID(c)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	jobs, ok := h.service.(interface {
		ImageRequestStatus(context.Context, services.ImageArtifactOwner, string, []string) (*aigateway.ImageJobStatus, error)
	})
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Image task service unavailable")
		return
	}
	owner, groups := imageReadScope(c, userID)
	status, err := jobs.ImageRequestStatus(c.Request.Context(), owner, c.Param("requestID"), groups)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "image_request_unavailable", "message": "Image request unavailable"}})
		return
	}
	c.JSON(http.StatusOK, status)
}

func imageReadScope(c *gin.Context, userID int) (services.ImageArtifactOwner, []string) {
	owner := services.ImageArtifactOwner{UserID: userID}
	if raw, ok := c.Get("instanceID"); ok {
		if id, ok := raw.(int); ok {
			owner.InstanceID = &id
		}
	}
	if raw, ok := c.Get("gatewayKeyDBID"); ok {
		if id, ok := raw.(int); ok {
			owner.GatewayKeyID = &id
		}
	}
	var groups []string
	if raw, ok := c.Get("gatewayAllowedModelGroups"); ok {
		groups, _ = raw.([]string)
	}
	return owner, groups
}

func (h *AIGatewayHandler) writeImageError(c *gin.Context, err error) {
	var requestErr *aigateway.ImageRequestError
	if errors.As(err, &requestErr) {
		c.Header("X-Trace-ID", requestErr.RequestID)
	}
	if errors.Is(err, services.ErrImageIdempotencyConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "idempotency_conflict", "message": "This idempotency key belongs to a different image request"}})
		return
	}
	var operationErr *aigateway.ImageExecutionError
	if errors.As(err, &operationErr) {
		c.JSON(operationErr.Status, gin.H{"error": gin.H{"code": operationErr.Code, "message": operationErr.Message, "type": "image_operation_error"}})
		return
	}
	if errors.Is(err, aigateway.ErrInvalidImageOperation) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_image_operation", "message": err.Error()}})
		return
	}
	if errors.Is(err, models.ErrModelUnavailable) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "model_unavailable", "message": "当前模型不可用，图片请求已中止"}})
		return
	}
	if errors.Is(err, services.ErrGatewayDeploymentUnavailable) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"code": "image_deployment_unavailable", "message": "Image deployment is busy or unavailable"}})
		return
	}
	if errors.Is(err, services.ErrGatewayKeyBudgetExceeded) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"code": "budget_exceeded", "message": "Gateway key budget exceeded"}})
		return
	}
	if errors.Is(err, services.ErrGatewayBudgetCurrencyMismatch) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "image_currency_unsupported", "message": "Image budget estimates currently require USD"}})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "image_request_failed", "message": "Image request could not complete; check the Gateway invocation record before retrying"}})
}
