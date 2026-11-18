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
	aliases := map[int][]string{}
	if reader, ok := s.modelRepo.(repository.RuntimeModelAliasCatalog); ok {
		ids := make([]int, 0, len(allowed))
		for _, m := range allowed {
			ids = append(ids, m.ID)
		}
		aliases, err = reader.RuntimeModelAliases(ids)
		if err != nil {
			// A failed history read is not evidence that old session models vanished.
			return nil, fmt.Errorf("runtime model alias catalog unavailable: %w", err)
		}
	}
	minimum := allowed[0].ContextWindowTokens
	providerRefs := map[int][]models.RuntimeProviderRef{}
	if reader, ok := s.modelRepo.(repository.RuntimeProviderRefCatalog); ok {
		ids := make([]int, 0, len(allowed))
		for _, m := range allowed {
			ids = append(ids, m.ID)
		}
		providerRefs, err = reader.RuntimeProviderRefs(ids)
		if err != nil {
			return nil, fmt.Errorf("runtime provider references unavailable: %w", err)
		}
	}
	for _, m := range allowed {
		if m.ContextWindowTokens < minimum {
			minimum = m.ContextWindowTokens
		}
	}
	result := []models.RuntimeCatalogModel{{ID: "auto", ContextWindow: minimum, Tools: true, ProviderRefs: []models.RuntimeProviderRef{{ProviderID: "auto", ModelID: "auto"}}}}
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
		legacyNameUnavailable := false
		if _, authoritative := s.modelRepo.(repository.RuntimeModelAliasCatalog); authoritative && m.RuntimeModelID != "" {
			legacyNameUnavailable = true
			for _, alias := range aliases[m.ID] {
				if strings.EqualFold(strings.TrimSpace(alias), name) {
					legacyNameUnavailable = false
					break
				}
			}
		}
		result = append(result, models.RuntimeCatalogModel{ID: name, StableID: m.RuntimeModelID, LegacyNameUnavailable: legacyNameUnavailable, Aliases: aliases[m.ID], ProviderRefs: providerRefs[m.ID], ContextWindow: window, Reasoning: m.SupportsReasoning && m.ReasoningEnabled, ReasoningControl: m.ReasoningControl, Tools: tools, Vision: models.LLMVisionInputAvailable(&m)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
