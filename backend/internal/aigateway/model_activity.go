package aigateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
)

type ModelActivityRecorder interface {
	BeginModelActivity(context.Context, int, int, int, string) (string, error)
	FinishModelActivity(context.Context, string) error
}

func WithModelActivity(recorder ModelActivityRecorder) ServiceOption {
	return func(s *service) { s.modelActivity = recorder }
}
func (s *service) beginModelActivity(ctx context.Context, userID int, req ChatCompletionRequest) (func(), error) {
	noop := func() {}
	if scope, ok := ctx.Value(modelActivityScopeKey{}).(modelActivityScope); ok {
		id := 0
		if req.InstanceID != nil {
			id = *req.InstanceID
		}
		gateway := ""
		if req.GatewayID != nil {
			gateway = *req.GatewayID
		}
		generation := 0
		if req.RuntimeTask != nil {
			generation = req.RuntimeTask.Generation
		}
		if scope.userID != userID || scope.instanceID != id || scope.gatewayID != gateway || scope.generation != generation {
			return noop, models.ErrInstanceStopping
		}
		return noop, nil
	}
	if s.modelActivity == nil || req.InstanceID == nil || *req.InstanceID <= 0 {
		return noop, nil
	}
	generation := 0
	if req.RuntimeTask != nil {
		generation = req.RuntimeTask.Generation
	}
	gatewayID := ""
	if req.GatewayID != nil {
		gatewayID = strings.TrimSpace(*req.GatewayID)
	}
	if gatewayID != "" {
		prefix := fmt.Sprintf("gw-%d-", *req.InstanceID)
		if !strings.HasPrefix(gatewayID, prefix) {
			return noop, models.ErrInstanceStopping
		}
		n, err := strconv.Atoi(strings.TrimPrefix(gatewayID, prefix))
		if err != nil || n < 1 || (generation > 0 && generation != n) {
			return noop, models.ErrInstanceStopping
		}
		generation = n
	}
	key, err := s.modelActivity.BeginModelActivity(ctx, userID, *req.InstanceID, generation, gatewayID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceStopping) {
			return noop, err
		}
		return noop, fmt.Errorf("%w: activity admission failed", models.ErrActivityUnavailable)
	}
	return func() {
		if key == "" {
			return
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.modelActivity.FinishModelActivity(finishCtx, key); err != nil {
			// The durable unfinished scope remains a stop blocker. Never release it
			// merely because a client disconnected or this process is restarting.
			log.Printf("model activity completion pending for instance %d: %T", *req.InstanceID, err)
		}
	}, nil
}
func (s *service) beginChatModelActivity(ctx context.Context, userID int, req *ChatCompletionRequest) (func(), error) {
	if s.modelActivity != nil && req.InstanceID != nil {
		if err := NormalizeVisionRequest(req); err != nil {
			return func() {}, err
		}
		if err := NormalizeGenerationLimits(req); err != nil {
			return func() {}, err
		}
		if strings.TrimSpace(req.Model) == "" || len(req.Messages) == 0 {
			return func() {}, errors.New("model and messages are required")
		}
	}
	return s.beginModelActivity(ctx, userID, *req)
}

type modelActivityScopeKey struct{}
type modelActivityScope struct {
	userID, instanceID, generation int
	gatewayID                      string
}

// BeginModelActivityScope lets validated HTTP requests count before quota
// rejection while inference reuses that exact scope without double recording.
func (s *service) BeginModelActivityScope(ctx context.Context, userID int, req ChatCompletionRequest) (context.Context, func(), error) {
	finish, err := s.beginModelActivity(ctx, userID, req)
	if err != nil {
		return ctx, finish, err
	}
	scope := modelActivityScope{userID: userID}
	if req.InstanceID != nil {
		scope.instanceID = *req.InstanceID
	}
	if req.GatewayID != nil {
		scope.gatewayID = *req.GatewayID
	}
	if req.RuntimeTask != nil {
		scope.generation = req.RuntimeTask.Generation
	}
	return context.WithValue(ctx, modelActivityScopeKey{}, scope), finish, nil
}
