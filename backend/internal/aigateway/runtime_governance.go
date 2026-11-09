package aigateway

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func WithRuntimeTasks(tasks repository.GatewayTaskRepository) ServiceOption {
	return func(s *service) { s.runtimeTasks = tasks }
}
func (s *service) checkRuntimeTask(ctx context.Context, req ChatCompletionRequest) error {
	if req.RuntimeTask == nil {
		return nil
	}
	if s.runtimeTasks == nil {
		return fmt.Errorf("runtime task authorization unavailable")
	}
	state, err := s.runtimeTasks.Check(ctx, *req.RuntimeTask)
	if err != nil {
		return err
	}
	if state.Revoked || state.Finished {
		return models.ErrModelUnavailable
	}
	return nil
}
func (s *service) bindRuntimeTask(ctx context.Context, req ChatCompletionRequest, modelID int) error {
	if req.RuntimeTask == nil {
		return nil
	}
	if s.runtimeTasks == nil {
		return fmt.Errorf("runtime task authorization unavailable")
	}
	return s.runtimeTasks.Bind(ctx, *req.RuntimeTask, modelID)
}

type taskAccessChecker struct {
	service *service
	req     ChatCompletionRequest
}

func (c taskAccessChecker) CheckModelAccess(ctx context.Context, userID, modelID int) error {
	if err := c.service.checkRuntimeTask(ctx, c.req); err != nil {
		return err
	}
	if c.service.modelAccessChecker == nil {
		return fmt.Errorf("model authorization unavailable")
	}
	return c.service.modelAccessChecker.CheckModelAccess(ctx, userID, modelID)
}
func (s *service) taskAccessChecker(req ChatCompletionRequest) repository.ModelAccessChecker {
	if req.RuntimeTask == nil {
		return s.modelAccessChecker
	}
	return taskAccessChecker{s, req}
}

// RuntimeCatalog never exposes provider URLs, credentials or grant lists.
func (s *service) RuntimeCatalog(userID int, groups []string) ([]models.RuntimeCatalogModel, error) {
	items, err := repository.ForModelUser(s.modelRepo, userID).ListActive()
	if errors.Is(err, models.ErrModelUnavailable) {
		return []models.RuntimeCatalogModel{}, nil
	}
	if err != nil {
		return nil, err
	}
	allowed := make([]models.LLMModel, 0, len(items))
	for _, m := range items {
		if models.LLMChatAvailable(&m) && modelAllowedForGatewayKey(&m, groups) {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) == 0 {
		return []models.RuntimeCatalogModel{}, nil
	}
	minimum := allowed[0].ContextWindowTokens
	for _, m := range allowed {
		if m.ContextWindowTokens < minimum {
			minimum = m.ContextWindowTokens
		}
	}
	result := []models.RuntimeCatalogModel{{ID: "auto", ContextWindow: minimum, Tools: true}}
	for _, model := range allowed {
		result[0].Vision = result[0].Vision || models.LLMVisionInputAvailable(&model)
	}
	seen := map[string]bool{"auto": true}
	for _, m := range allowed {
		name := strings.TrimSpace(m.DisplayName)
		if name == "" {
			name = m.ProviderModelName
		}
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		models.PopulateLLMReasoningCapability(&m)
		tools := m.SupportsTools
		if m.ToolsSupportOverride != nil {
			tools = *m.ToolsSupportOverride
		}
		window := m.ContextWindowTokens
		// Explicit display names can fail over within their logical group.
		if m.ModelGroup != "" {
			for _, other := range allowed {
				if strings.EqualFold(m.ModelGroup, other.ModelGroup) && other.ContextWindowTokens < window {
					window = other.ContextWindowTokens
				}
			}
		}
		result = append(result, models.RuntimeCatalogModel{ID: name, ContextWindow: window, Reasoning: m.SupportsReasoning && m.ReasoningEnabled, Tools: tools, Vision: models.LLMVisionInputAvailable(&m)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
