package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"clawreef/internal/aigateway"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// AIGatewayHandler exposes AI gateway endpoints.
type AIGatewayHandler struct {
	service aigateway.Service
}

// NewAIGatewayHandler creates a new AI gateway handler.
func NewAIGatewayHandler(service aigateway.Service) *AIGatewayHandler {
	return &AIGatewayHandler{service: service}
}

// ListModels returns active models available to the current user.
func (h *AIGatewayHandler) ListModels(c *gin.Context) {
	items, err := h.service.ListAvailableModels()
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
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var req aigateway.ChatCompletionRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	req.RawBody = rawBody
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
