package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type AgentStateReportRequest struct {
	AgentID    string                 `json:"agent_id" binding:"required"`
	ReportedAt *time.Time             `json:"reported_at,omitempty"`
	Runtime    AgentRuntimePayload    `json:"runtime"`
	SystemInfo map[string]interface{} `json:"system_info"`
	Health     map[string]interface{} `json:"health"`
}

type AgentRuntimePayload struct {
	OpenClawStatus          string  `json:"openclaw_status"`
	OpenClawPID             *int    `json:"openclaw_pid,omitempty"`
	OpenClawVersion         string  `json:"openclaw_version"`
	CurrentConfigRevisionID *int    `json:"current_config_revision_id,omitempty"`
	LLMConfigFingerprint    *string `json:"llm_config_fingerprint,omitempty"`
	LLMConfigStatus         *string `json:"llm_config_status,omitempty"`
	LLMProviderBaseURL      *string `json:"llm_provider_base_url,omitempty"`
}

type InstanceRuntimeStatusPayload struct {
	InstanceID              int                    `json:"instance_id"`
	InfraStatus             string                 `json:"infra_status"`
	AgentStatus             string                 `json:"agent_status"`
	OpenClawStatus          string                 `json:"openclaw_status"`
	OpenClawPID             *int                   `json:"openclaw_pid,omitempty"`
	OpenClawVersion         *string                `json:"openclaw_version,omitempty"`
	CurrentConfigRevisionID *int                   `json:"current_config_revision_id,omitempty"`
	DesiredConfigRevisionID *int                   `json:"desired_config_revision_id,omitempty"`
	SystemInfo              map[string]interface{} `json:"system_info,omitempty"`
	Health                  map[string]interface{} `json:"health,omitempty"`
	Summary                 map[string]interface{} `json:"summary,omitempty"`
	LastReportedAt          *time.Time             `json:"last_reported_at,omitempty"`
}

type InstanceRuntimeStatusService interface {
	Report(session *AgentSession, req AgentStateReportRequest, clientIP string) error
	GetByInstanceID(instanceID int) (*InstanceRuntimeStatusPayload, error)
	UpsertInfraStatus(instanceID int, infraStatus string) error
}

type instanceRuntimeStatusService struct {
	runtimeRepo      repository.InstanceRuntimeStatusRepository
	agentRepo        repository.InstanceAgentRepository
	desiredStateRepo repository.InstanceDesiredStateRepository
}

func NewInstanceRuntimeStatusService(runtimeRepo repository.InstanceRuntimeStatusRepository, agentRepo repository.InstanceAgentRepository, desiredStateRepo repository.InstanceDesiredStateRepository) InstanceRuntimeStatusService {
	return &instanceRuntimeStatusService{
		runtimeRepo:      runtimeRepo,
		agentRepo:        agentRepo,
		desiredStateRepo: desiredStateRepo,
	}
}

func (s *instanceRuntimeStatusService) Report(session *AgentSession, req AgentStateReportRequest, clientIP string) error {
	if session == nil || session.Agent == nil || session.Instance == nil {
		return fmt.Errorf("agent session is required")
	}
	if strings.TrimSpace(req.AgentID) != session.Agent.AgentID {
		return fmt.Errorf("agent id does not match session")
	}

	status, err := s.getOrCreate(session.Instance.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	reportedAt := req.ReportedAt
	if reportedAt == nil || reportedAt.IsZero() {
		reportedAt = &now
	}

	status.AgentStatus = agentStatusOnline
	if strings.TrimSpace(req.Runtime.OpenClawStatus) != "" {
		status.OpenClawStatus = strings.TrimSpace(req.Runtime.OpenClawStatus)
	}
	status.OpenClawPID = req.Runtime.OpenClawPID
	if strings.TrimSpace(req.Runtime.OpenClawVersion) != "" {
		version := strings.TrimSpace(req.Runtime.OpenClawVersion)
		status.OpenClawVersion = &version
	}
	status.CurrentConfigRevisionID = req.Runtime.CurrentConfigRevisionID
	status.LastReportedAt = reportedAt

	systemInfoJSON, err := marshalOptionalJSON(mergeLLMConfigIntoSystemInfo(req.SystemInfo, req.Runtime))
	if err != nil {
		return fmt.Errorf("failed to encode system info: %w", err)
	}
	healthJSON, err := marshalOptionalJSON(req.Health)
	if err != nil {
		return fmt.Errorf("failed to encode health info: %w", err)
	}
	status.SystemInfoJSON = systemInfoJSON
	status.HealthJSON = healthJSON
	if err := s.runtimeRepo.Update(status); err != nil {
		return err
	}

	session.Agent.Status = agentStatusOnline
	session.Agent.LastHeartbeatAt = reportedAt
	session.Agent.LastReportedAt = reportedAt
	session.Agent.LastSeenIP = optionalString(strings.TrimSpace(clientIP))
	if err := s.agentRepo.Update(session.Agent); err != nil {
		return err
	}

	return nil
}

func (s *instanceRuntimeStatusService) GetByInstanceID(instanceID int) (*InstanceRuntimeStatusPayload, error) {
	status, err := s.runtimeRepo.GetByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, nil
	}
	payload := &InstanceRuntimeStatusPayload{
		InstanceID:              status.InstanceID,
		InfraStatus:             status.InfraStatus,
		AgentStatus:             status.AgentStatus,
		OpenClawStatus:          status.OpenClawStatus,
		OpenClawPID:             status.OpenClawPID,
		OpenClawVersion:         status.OpenClawVersion,
		CurrentConfigRevisionID: status.CurrentConfigRevisionID,
		DesiredConfigRevisionID: status.DesiredConfigRevisionID,
		LastReportedAt:          status.LastReportedAt,
	}
	if status.SystemInfoJSON != nil && strings.TrimSpace(*status.SystemInfoJSON) != "" {
		if err := json.Unmarshal([]byte(*status.SystemInfoJSON), &payload.SystemInfo); err != nil {
			return nil, fmt.Errorf("failed to decode system info: %w", err)
		}
	}
	if status.HealthJSON != nil && strings.TrimSpace(*status.HealthJSON) != "" {
		if err := json.Unmarshal([]byte(*status.HealthJSON), &payload.Health); err != nil {
			return nil, fmt.Errorf("failed to decode health info: %w", err)
		}
	}
	if status.SummaryJSON != nil && strings.TrimSpace(*status.SummaryJSON) != "" {
		if err := json.Unmarshal([]byte(*status.SummaryJSON), &payload.Summary); err != nil {
			return nil, fmt.Errorf("failed to decode runtime summary: %w", err)
		}
	}
	return payload, nil
}

func mergeLLMConfigIntoSystemInfo(systemInfo map[string]interface{}, runtime AgentRuntimePayload) map[string]interface{} {
	merged := map[string]interface{}{}
	for key, value := range systemInfo {
		merged[key] = value
	}
	if runtime.LLMConfigFingerprint != nil && strings.TrimSpace(*runtime.LLMConfigFingerprint) != "" {
		merged["llm_config_fingerprint"] = strings.TrimSpace(*runtime.LLMConfigFingerprint)
	}
	if runtime.LLMConfigStatus != nil && strings.TrimSpace(*runtime.LLMConfigStatus) != "" {
		merged["llm_config_status"] = strings.TrimSpace(*runtime.LLMConfigStatus)
	}
	if runtime.LLMProviderBaseURL != nil && strings.TrimSpace(*runtime.LLMProviderBaseURL) != "" {
		merged["llm_provider_base_url"] = strings.TrimSpace(*runtime.LLMProviderBaseURL)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func (s *instanceRuntimeStatusService) UpsertInfraStatus(instanceID int, infraStatus string) error {
	status, err := s.getOrCreate(instanceID)
	if err != nil {
		return err
	}
	status.InfraStatus = infraStatus

	desiredState, err := s.desiredStateRepo.GetByInstanceID(instanceID)
	if err == nil && desiredState != nil {
		status.DesiredConfigRevisionID = desiredState.DesiredConfigRevisionID
	}
	return s.runtimeRepo.Update(status)
}

func (s *instanceRuntimeStatusService) getOrCreate(instanceID int) (*models.InstanceRuntimeStatus, error) {
	status, err := s.runtimeRepo.GetByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if status != nil {
		return status, nil
	}
	now := time.Now().UTC()
	status = &models.InstanceRuntimeStatus{
		InstanceID:     instanceID,
		InfraStatus:    "creating",
		AgentStatus:    agentStatusOffline,
		OpenClawStatus: openClawStatusUnknown,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.runtimeRepo.Create(status); err != nil {
		return nil, err
	}
	return status, nil
}
