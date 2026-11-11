package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/go-sql-driver/mysql"
)

const (
	PlatformLLMGatewayResourceKey  = "platform-llm-gateway"
	PlatformLLMGatewayResourceName = "Platform LLM Gateway"
)

var platformLLMGatewayAgentContent = json.RawMessage(`{
  "schemaVersion": 1,
  "kind": "agent",
  "format": "agent/platform-llm-gateway@v1",
  "dependsOn": [],
  "config": {
    "models": {
      "providers": {
        "clawmanager": {
          "type": "openai-compatible",
          "baseUrl": "${CLAWMANAGER_LLM_BASE_URL}",
          "apiKey": "${CLAWMANAGER_LLM_API_KEY}",
          "default": true
        }
      },
      "primary": "auto/auto"
    }
  }
}`)

func (s *openClawConfigService) EnsurePlatformLLMGatewayResource(userID int) (*models.OpenClawConfigResource, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user id is required")
	}

	existing, err := s.repo.GetResourceByUserTypeKey(userID, OpenClawConfigResourceTypeAgent, PlatformLLMGatewayResourceKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	description := "Built-in agent config that routes OpenClaw LLM calls through the ClawManager AI Gateway."
	now := time.Now()
	item := &models.OpenClawConfigResource{
		UserID:       userID,
		ResourceType: OpenClawConfigResourceTypeAgent,
		ResourceKey:  PlatformLLMGatewayResourceKey,
		Name:         PlatformLLMGatewayResourceName,
		Description:  &description,
		Enabled:      true,
		Version:      1,
		TagsJSON:     encodeStringArray([]string{"builtin", "llm-governance", "platform"}),
		ContentJSON:  string(platformLLMGatewayAgentContent),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateResource(item); err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			// Another API replica may have initialized the same built-in resource.
			if existing, readErr := s.repo.GetResourceByUserTypeKey(userID, OpenClawConfigResourceTypeAgent, PlatformLLMGatewayResourceKey); readErr == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return item, nil
}

func (s *openClawConfigService) CreateDefaultLLMGovernanceSnapshot(userID int, instance *models.Instance) (*models.OpenClawInjectionSnapshot, error) {
	if instance == nil || !supportsManagedRuntimeIntegration(instance.Type) {
		return nil, nil
	}

	resource, err := s.EnsurePlatformLLMGatewayResource(userID)
	if err != nil {
		return nil, err
	}
	if resource == nil || resource.ID <= 0 {
		return nil, fmt.Errorf("failed to provision platform llm gateway resource")
	}

	plan := &OpenClawConfigPlan{
		Mode:        OpenClawConfigPlanModeManual,
		ResourceIDs: []int{resource.ID},
	}
	return s.CreateSnapshotForInstance(userID, instance, plan)
}
