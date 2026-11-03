package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// AIGatewayHandler exposes AI gateway endpoints.
type AIGatewayHandler struct {
	service                     aigateway.Service
	instanceService             services.InstanceService
	workspaceFileService        services.WorkspaceFileService
	runtimeWorkspaceFileService services.WorkspaceFileService
	rateLimitStore              services.GatewayRateLimitStore
	budgetService               services.GatewayBudgetService
}

// NewAIGatewayHandler creates a new AI gateway handler.
func NewAIGatewayHandler(service aigateway.Service, instanceService services.InstanceService, workspaceFileService, runtimeWorkspaceFileService services.WorkspaceFileService, rateLimitStore services.GatewayRateLimitStore, budgetServices ...services.GatewayBudgetService) *AIGatewayHandler {
	if rateLimitStore == nil {
		rateLimitStore = services.NewNoopGatewayRateLimitStore()
	}
	var budgetService services.GatewayBudgetService
	if len(budgetServices) > 0 {
		budgetService = budgetServices[0]
	}
	return &AIGatewayHandler{service: service, instanceService: instanceService, workspaceFileService: workspaceFileService, runtimeWorkspaceFileService: runtimeWorkspaceFileService, rateLimitStore: rateLimitStore, budgetService: budgetService}
}

// ListModels returns active models available to the current user.
func (h *AIGatewayHandler) ListModels(c *gin.Context) {
	userID, ok := gatewayUserID(c)
	if !ok {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	lister, ok := h.service.(interface {
		ListAvailableModelsForUser(int, []string) ([]aigateway.AvailableModel, error)
	})
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Model authorization unavailable")
		return
	}
	var groups []string
	if value, exists := c.Get("gatewayAllowedModelGroups"); exists {
		groups, _ = value.([]string)
	}
	items, err := lister.ListAvailableModelsForUser(userID, groups)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Available gateway models retrieved successfully", gin.H{
		"items": items,
	})
}

// ChatCompletions proxies a governed chat completion request.
func (h *AIGatewayHandler) ChatCompletions(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, aigateway.MaxVisionRequestBytes)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.ValidationError(c, err)
		return
	}

	var req aigateway.ChatCompletionRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	req.RawBody = rawBody
	if !attachRuntimeTask(c, &req) {
		return
	}
	if !h.validateGatewayInstanceAttribution(c, &req) {
		return
	}
	if sessionKey := strings.TrimSpace(c.GetHeader("x-openclaw-session-key")); sessionKey != "" {
		req.OpenClawSessionKey = &sessionKey
		if req.SessionID == nil {
			req.SessionID = &sessionKey
		}
	}
	if req.TraceID == nil {
		if runID := strings.TrimSpace(c.GetHeader("x-openclaw-run-id")); runID != "" {
			req.TraceID = &runID
		}
	}
	applyGatewayAllowedModelGroups(c, &req)
	h.applyGatewayBudgetContext(c, &req)
	lease, ok := h.acquireGatewayKeyLease(c, &req)
	if !ok {
		return
	}
	h.attachGatewayUsageFinalizer(&req, lease)
	defer h.releaseGatewayKeyLease(lease)
	defer h.releaseGatewayBudget(&req)
	gatewayAuthType, _ := c.Get("gatewayAuthType")
	instanceType, _ := c.Get("instanceType")
	aigateway.ApplyManagedInstanceSessionDefaults(
		&req,
		stringValue(gatewayAuthType),
		stringValue(instanceType),
	)

	userID, exists := c.Get("userID")
	if !exists {
		utils.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if instanceID, exists := c.Get("instanceID"); exists {
		switch value := instanceID.(type) {
		case int:
			if req.InstanceID == nil {
				req.InstanceID = &value
			} else if *req.InstanceID != value {
				utils.Error(c, http.StatusForbidden, "Gateway token does not match requested instance")
				return
			}
		}
	}
	if !setStringMetadata(c, &req.InstanceMode, "instanceMode") ||
		!setStringMetadata(c, &req.RuntimeType, "runtimeType") ||
		!setStringMetadata(c, &req.GatewayID, "gatewayID") ||
		!setInt64Metadata(c, &req.RuntimePodID, "runtimePodID") {
		utils.Error(c, http.StatusForbidden, "Gateway token metadata does not match request")
		return
	}

	if req.Stream {
		traceID, err := h.service.StreamChatCompletions(c.Request.Context(), userID.(int), req, c.Writer)
		if traceID != "" {
			c.Header("X-Trace-ID", traceID)
		}
		if err != nil {
			if !c.Writer.Written() {
				utils.HandleError(c, err)
			}
		}
		return
	}

	response, traceID, err := h.service.ChatCompletions(c.Request.Context(), userID.(int), req)
	if err != nil {
		c.Header("X-Trace-ID", traceID)
		utils.HandleError(c, err)
		return
	}

	c.Header("X-Trace-ID", traceID)
	for key, values := range response.Headers {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Status(response.StatusCode)
	_, _ = c.Writer.Write(response.Body)
}

func (h *AIGatewayHandler) applyGatewayBudgetContext(c *gin.Context, req *aigateway.ChatCompletionRequest) {
	if req == nil {
		return
	}
	raw, ok := c.Get("gatewayKeyDBID")
	if !ok {
		return
	}
	keyID, ok := raw.(int)
	if !ok || keyID <= 0 {
		return
	}
	req.GatewayKeyID = &keyID
	req.BudgetController = services.NewGatewayBudgetController(h.budgetService, keyID)
}

func (h *AIGatewayHandler) releaseGatewayBudget(req *aigateway.ChatCompletionRequest) {
	if req == nil || req.BudgetController == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = req.BudgetController.Release(ctx)
}

func (h *AIGatewayHandler) acquireGatewayKeyLease(c *gin.Context, req *aigateway.ChatCompletionRequest) (*services.GatewayRateLimitLease, bool) {
	if err := aigateway.NormalizeVisionRequest(req); err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	if err := aigateway.NormalizeGenerationLimits(req); err != nil {
		utils.ValidationError(c, err)
		return nil, false
	}
	estimate := 0
	if req != nil {
		estimate = aigateway.EstimateRequestTokens(*req)
	}
	return h.acquireGatewayLeaseWithEstimate(c, estimate)
}

func (h *AIGatewayHandler) acquireGatewayLeaseWithEstimate(c *gin.Context, estimatedTokens int) (*services.GatewayRateLimitLease, bool) {
	keyID, hasKey := c.Get("gatewayKeyID")
	if !hasKey {
		return nil, true
	}
	key, _ := keyID.(string)
	rpm, _ := c.Get("gatewayKeyRPMLimit")
	tpm, _ := c.Get("gatewayKeyTPMLimit")
	concurrent, _ := c.Get("gatewayKeyConcurrencyLimit")
	rpmValue, _ := rpm.(int)
	tpmValue, _ := tpm.(int)
	concurrentValue, _ := concurrent.(int)
	requestID, err := newGatewayRateLimitRequestID()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "Unable to allocate a gateway request identity", "type": "server_error", "code": "request_identity_unavailable"}})
		return nil, false
	}
	lease, err := h.rateLimitStore.Acquire(c.Request.Context(), services.GatewayRateLimitInput{KeyID: key, RPMLimit: rpmValue, TPMLimit: tpmValue, ConcurrentRequestsLimit: concurrentValue, EstimatedTokens: estimatedTokens, RequestID: requestID})
	if err == nil {
		return lease, true
	}
	if errors.Is(err, services.ErrGatewayKeyRPMLimitExceeded) || errors.Is(err, services.ErrGatewayKeyTPMLimitExceeded) || errors.Is(err, services.ErrGatewayKeyConcurrencyExceeded) {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": err.Error(), "type": "rate_limit_error", "code": "rate_limit_exceeded"}})
		return nil, false
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "Gateway rate-limit service is temporarily unavailable", "type": "server_error", "code": "rate_limit_store_unavailable"}})
	return nil, false
}

func newGatewayRateLimitRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "grl_" + hex.EncodeToString(value[:]), nil
}

func (h *AIGatewayHandler) attachGatewayUsageFinalizer(req *aigateway.ChatCompletionRequest, lease *services.GatewayRateLimitLease) {
	if req == nil || lease == nil {
		return
	}
	req.UsageFinalizer = func(actualTokens int) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return h.rateLimitStore.Finalize(ctx, lease, actualTokens)
	}
}

func (h *AIGatewayHandler) releaseGatewayKeyLease(lease *services.GatewayRateLimitLease) {
	if lease == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = h.rateLimitStore.Release(ctx, lease)
}

func applyGatewayAllowedModelGroups(c *gin.Context, req *aigateway.ChatCompletionRequest) {
	if req == nil {
		return
	}
	if value, exists := c.Get("gatewayAllowedModelGroups"); exists {
		if groups, ok := value.([]string); ok {
			req.AllowedModelGroups = append([]string(nil), groups...)
		}
	}
}

func setStringMetadata(c *gin.Context, field **string, key string) bool {
	raw, exists := c.Get(key)
	if !exists {
		return true
	}
	value, ok := raw.(string)
	if !ok {
		return true
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if *field == nil {
		*field = &value
		return true
	}
	return strings.TrimSpace(**field) == value
}

func setInt64Metadata(c *gin.Context, field **int64, key string) bool {
	raw, exists := c.Get(key)
	if !exists {
		return true
	}
	var value int64
	switch typed := raw.(type) {
	case int64:
		value = typed
	case int:
		value = int64(typed)
	default:
		return true
	}
	if value <= 0 {
		return true
	}
	if *field == nil {
		*field = &value
		return true
	}
	return **field == value
}

func stringValue(raw interface{}) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
