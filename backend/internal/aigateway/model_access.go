package aigateway

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

func (s *service) ListAvailableModelsForUser(userID int, groups []string) ([]AvailableModel, error) {
	items, err := repository.ForModelUser(s.modelRepo, userID).ListActive()
	if errors.Is(err, models.ErrModelUnavailable) {
		return []AvailableModel{}, nil
	}
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if models.LLMChatAvailable(&item) && modelAllowedForGatewayKey(&item, groups) {
			return []AvailableModel{{ID: 0, DisplayName: "Auto", Provider: "gateway"}}, nil
		}
	}
	return []AvailableModel{}, nil
}

// guardModelCall interrupts an upstream request when access is revoked. It does
// not claim to cancel tools or an Agent task: those need runtime acknowledgment.
func guardModelCall(ctx context.Context, checker repository.ModelAccessChecker, userID, modelID int) (context.Context, func(), error) {
	if checker == nil {
		return ctx, func() {}, nil
	}
	changes, unsubscribe := repository.SubscribeModelAccessChanges()
	callCtx, cancel := context.WithCancelCause(ctx)
	check := func() error {
		checkCtx, checkCancel := context.WithTimeout(callCtx, 2*time.Second)
		defer checkCancel()
		return checker.CheckModelAccess(checkCtx, userID, modelID)
	}
	if err := check(); err != nil {
		cancel(err)
		unsubscribe()
		return ctx, func() {}, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-callCtx.Done():
				return
			case <-changes:
			case <-ticker.C:
			}
			if err := check(); err != nil {
				cancel(err)
				return
			}
		}
	}()
	return callCtx, func() { cancel(nil); <-done; unsubscribe() }, nil
}

func (s *service) recordAccessInterruption(p *preparedChatRequest, err error) {
	if s.auditEventService == nil || p == nil {
		return
	}
	_ = s.auditEventService.RecordEvent(&models.AuditEvent{
		TraceID: p.traceID, RequestID: p.requestIDPtr, SessionID: p.sessionIDPtr,
		InstanceID: p.req.InstanceID, UserID: p.userIDPtr, GatewayKeyID: p.req.GatewayKeyID,
		EventType: "gateway.model_access.interrupted", TrafficClass: models.TrafficClassLLM,
		Severity: models.AuditSeverityWarn,
		Message:  "Model request stopped; runtime task cancellation is unconfirmed: " + err.Error(),
	})
}

func writeModelAccessStreamError(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":{\"code\":\"model_unavailable\",\"type\":\"permission_error\",\"message\":\"当前模型不可用，模型请求已中止\"}}\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
