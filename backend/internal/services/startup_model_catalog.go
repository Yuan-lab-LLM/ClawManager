package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// The authenticated runtime launch envelope carries this snapshot before an
// isolated Pod's local model proxy/network namespace has been created. It is
// rebuilt on every start and never contains upstream credentials or URLs.
func (s *instanceService) buildStartupModelCatalog(instance *models.Instance, token string) (string, error) {
	items, err := repository.ForModelUser(s.llmModelRepo, instance.UserID).ListActive()
	if errors.Is(err, models.ErrModelUnavailable) {
		items = []models.LLMModel{}
		err = nil
	}
	if err != nil {
		return "", err
	}
	var groups []string
	if s.gatewayKeyRepo != nil {
		key, err := s.gatewayKeyRepo.GetByToken(token)
		if err != nil {
			return "", err
		}
		if key != nil && key.AllowedModelGroups != nil {
			if err := json.Unmarshal([]byte(*key.AllowedModelGroups), &groups); err != nil {
				return "", err
			}
		}
	}
	allowed := []models.LLMModel{}
	for _, item := range items {
		if !models.LLMChatAvailable(&item) {
			continue
		}
		group := strings.TrimSpace(item.ModelGroup)
		if group == "" {
			group = strings.TrimSpace(item.DisplayName)
		}
		match := len(groups) == 0
		for _, name := range groups {
			match = match || strings.EqualFold(group, strings.TrimSpace(name))
		}
		if match {
			allowed = append(allowed, item)
		}
	}
	entries := []models.RuntimeCatalogModel{}
	if len(allowed) > 0 {
		entries = append(entries, models.RuntimeCatalogModel{ID: "auto", ContextWindow: safeContextWindow(allowed), Tools: true})
	}
	seen := map[string]bool{"auto": true}
	for _, item := range allowed {
		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = strings.TrimSpace(item.ProviderModelName)
		}
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		// Compatibility repositories without migrated identities use the live
		// endpoint instead of publishing a misleading identity-less snapshot.
		if strings.TrimSpace(item.RuntimeModelID) == "" {
			return "", nil
		}
		models.PopulateLLMReasoningCapability(&item)
		tools := item.SupportsTools
		if item.ToolsSupportOverride != nil {
			tools = *item.ToolsSupportOverride
		}
		vision := models.LLMVisionInputAvailable(&item)
		window := item.ContextWindowTokens
		if strings.TrimSpace(item.ModelGroup) != "" {
			for _, other := range allowed {
				if strings.EqualFold(item.ModelGroup, other.ModelGroup) && other.ContextWindowTokens < window {
					window = other.ContextWindowTokens
				}
			}
		}
		entries[0].Vision = entries[0].Vision || vision
		entries = append(entries, models.RuntimeCatalogModel{ID: name, StableID: item.RuntimeModelID, ContextWindow: window, Tools: tools, Vision: vision, Reasoning: item.SupportsReasoning && item.ReasoningEnabled, ReasoningControl: item.ReasoningControl})
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	doc, err := json.Marshal(map[string]any{"models": entries, "generation": instance.RuntimeGeneration, "owner_user_id": instance.UserID, "complete": true, "catalog_protocol": 2, "revision": hex.EncodeToString(hash[:])})
	return string(doc), err
}
