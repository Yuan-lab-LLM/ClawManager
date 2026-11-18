package aigateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
)

type ModelActivityRecorder interface {
	BeginModelActivity(context.Context, int, int, int, string) (string, error)
	RenewModelActivity(context.Context, string) error
	FinishModelActivity(context.Context, string) error
}

func WithModelActivity(recorder ModelActivityRecorder) ServiceOption {
	return func(s *service) { s.modelActivity = recorder }
}
func (s *service) beginModelActivity(ctx context.Context, userID int, req ChatCompletionRequest) (context.Context, func(), error) {
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
			return ctx, noop, models.ErrInstanceStopping
		}
		return ctx, noop, nil
	}
	if s.modelActivity == nil || req.InstanceID == nil || *req.InstanceID <= 0 {
		return ctx, noop, nil
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
			return ctx, noop, models.ErrInstanceStopping
		}
		n, err := strconv.Atoi(strings.TrimPrefix(gatewayID, prefix))
		if err != nil || n < 1 || (generation > 0 && generation != n) {
			return ctx, noop, models.ErrInstanceStopping
		}
		generation = n
	}
	started := time.Now()
	key, err := s.modelActivity.BeginModelActivity(ctx, userID, *req.InstanceID, generation, gatewayID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceStopping) {
			return ctx, noop, err
		}
		return ctx, noop, fmt.Errorf("%w: activity admission failed", models.ErrActivityUnavailable)
	}
	if key == "" {
		return ctx, noop, nil
	}
	return s.renewModelActivityScope(ctx, key, *req.InstanceID, started)
}
func (s *service) beginChatModelActivity(ctx context.Context, userID int, req *ChatCompletionRequest) (context.Context, func(), error) {
	if s.modelActivity != nil && req.InstanceID != nil {
		if err := NormalizeVisionRequest(req); err != nil {
			return ctx, func() {}, err
		}
		if err := NormalizeGenerationLimits(req); err != nil {
			return ctx, func() {}, err
		}
		if strings.TrimSpace(req.Model) == "" || len(req.Messages) == 0 {
			return ctx, func() {}, errors.New("model and messages are required")
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
	ctx, finish, err := s.beginModelActivity(ctx, userID, req)
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
