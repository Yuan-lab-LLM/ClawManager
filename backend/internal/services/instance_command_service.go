package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const (
	InstanceCommandTypeStartOpenClaw         = "start_openclaw"
	InstanceCommandTypeStopOpenClaw          = "stop_openclaw"
	InstanceCommandTypeRestartOpenClaw       = "restart_openclaw"
	InstanceCommandTypeCollectSystemInfo     = "collect_system_info"
	InstanceCommandTypeApplyConfigRevision   = "apply_config_revision"
	InstanceCommandTypeReloadConfig          = "reload_config"
	InstanceCommandTypeHealthCheck           = "health_check"
	InstanceCommandTypeInstallSkill          = "install_skill"
	InstanceCommandTypeUpdateSkill           = "update_skill"
	InstanceCommandTypeUninstallSkill        = "uninstall_skill"
	InstanceCommandTypeRemoveSkill           = "remove_skill"
	InstanceCommandTypeDisableSkill          = "disable_skill"
	InstanceCommandTypeQuarantineSkill       = "quarantine_skill"
	InstanceCommandTypeHandleSkillRisk       = "handle_skill_risk"
	InstanceCommandTypeSyncSkillInventory    = "sync_skill_inventory"
	InstanceCommandTypeRefreshSkillInventory = "refresh_skill_inventory"
	InstanceCommandTypeCollectSkillPackage   = "collect_skill_package"
	instanceCommandStatusPending             = "pending"
	instanceCommandStatusDispatched          = "dispatched"
	instanceCommandStatusRunning             = "running"
	instanceCommandStatusSucceeded           = "succeeded"
	instanceCommandStatusFailed              = "failed"
)

type CreateInstanceCommandRequest struct {
	CommandType    string                 `json:"command_type"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
}

type InstanceCommandPayload struct {
	ID             int                    `json:"id"`
	CommandType    string                 `json:"command_type"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
	Status         string                 `json:"status"`
	IdempotencyKey string                 `json:"idempotency_key"`
	IssuedBy       *int                   `json:"issued_by,omitempty"`
	IssuedAt       time.Time              `json:"issued_at"`
	DispatchedAt   *time.Time             `json:"dispatched_at,omitempty"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	FinishedAt     *time.Time             `json:"finished_at,omitempty"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
	Result         map[string]interface{} `json:"result,omitempty"`
	ErrorMessage   *string                `json:"error_message,omitempty"`
}

type AgentCommandEnvelope struct {
	ID             int                    `json:"id"`
	CommandType    string                 `json:"command_type"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
	IssuedAt       time.Time              `json:"issued_at"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
	IdempotencyKey string                 `json:"idempotency_key"`
}

type AgentCommandFinishRequest struct {
	AgentID      string                 `json:"agent_id" binding:"required"`
	Status       string                 `json:"status" binding:"required"`
	FinishedAt   *time.Time             `json:"finished_at,omitempty"`
	Result       map[string]interface{} `json:"result,omitempty"`
	ErrorMessage string                 `json:"error_message"`
}

type InstanceCommandService interface {
	Create(instanceID int, issuedBy *int, req CreateInstanceCommandRequest) (*InstanceCommandPayload, error)
	GetNextForAgent(session *AgentSession) (*AgentCommandEnvelope, error)
	MarkStarted(session *AgentSession, commandID int, startedAt *time.Time) error
	MarkFinished(session *AgentSession, commandID int, req AgentCommandFinishRequest) error
	ListByInstanceID(instanceID int, limit int) ([]InstanceCommandPayload, error)
}

type instanceCommandService struct {
	commandRepo      repository.InstanceCommandRepository
	runtimeRepo      repository.InstanceRuntimeStatusRepository
	desiredStateRepo repository.InstanceDesiredStateRepository
	skillRepo        repository.SkillRepository
}

func NewInstanceCommandService(commandRepo repository.InstanceCommandRepository, runtimeRepo repository.InstanceRuntimeStatusRepository, desiredStateRepo repository.InstanceDesiredStateRepository, skillRepos ...repository.SkillRepository) InstanceCommandService {
	var skillRepo repository.SkillRepository
	if len(skillRepos) > 0 {
		skillRepo = skillRepos[0]
	}
	return &instanceCommandService{
		commandRepo:      commandRepo,
		runtimeRepo:      runtimeRepo,
		desiredStateRepo: desiredStateRepo,
		skillRepo:        skillRepo,
	}
}

func (s *instanceCommandService) Create(instanceID int, issuedBy *int, req CreateInstanceCommandRequest) (*InstanceCommandPayload, error) {
	commandType := strings.TrimSpace(req.CommandType)
	if !isSupportedCommandType(commandType) {
		return nil, fmt.Errorf("invalid instance command type")
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("%s-%d", commandType, time.Now().UTC().UnixNano())
	}

	existing, err := s.commandRepo.GetByInstanceIdempotencyKey(instanceID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return commandPayloadFromModel(*existing)
	}

	payloadJSON, err := marshalOptionalJSON(req.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode command payload: %w", err)
	}
	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}

	now := time.Now().UTC()
	command := &models.InstanceCommand{
		InstanceID:     instanceID,
		CommandType:    commandType,
		PayloadJSON:    payloadJSON,
		Status:         instanceCommandStatusPending,
		IdempotencyKey: idempotencyKey,
		IssuedBy:       issuedBy,
		IssuedAt:       now,
		TimeoutSeconds: timeoutSeconds,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.commandRepo.Create(command); err != nil {
		return nil, err
	}

	if err := s.applyDesiredStateSideEffects(instanceID, commandType, req.Payload); err != nil {
		return nil, err
	}
	return commandPayloadFromModel(*command)
}

func (s *instanceCommandService) GetNextForAgent(session *AgentSession) (*AgentCommandEnvelope, error) {
	command, err := s.commandRepo.GetNextPendingByInstance(session.Instance.ID)
	if err != nil {
		return nil, err
	}
	if command == nil {
		return nil, nil
	}

	now := time.Now().UTC()
	command.Status = instanceCommandStatusDispatched
	command.AgentID = &session.Agent.AgentID
	command.DispatchedAt = &now
	if err := s.commandRepo.Update(command); err != nil {
		return nil, err
	}

	payload := map[string]interface{}{}
	if command.PayloadJSON != nil && strings.TrimSpace(*command.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(*command.PayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("failed to decode command payload: %w", err)
		}
	}

	return &AgentCommandEnvelope{
		ID:             command.ID,
		CommandType:    command.CommandType,
		Payload:        payload,
		IssuedAt:       command.IssuedAt,
		TimeoutSeconds: command.TimeoutSeconds,
		IdempotencyKey: command.IdempotencyKey,
	}, nil
}

func (s *instanceCommandService) MarkStarted(session *AgentSession, commandID int, startedAt *time.Time) error {
	command, err := s.commandRepo.GetByID(commandID)
	if err != nil {
		return err
	}
	if command == nil || command.InstanceID != session.Instance.ID {
		return fmt.Errorf("instance command not found")
	}

	if command.Status == instanceCommandStatusRunning || command.Status == instanceCommandStatusSucceeded || command.Status == instanceCommandStatusFailed {
		return nil
	}
	now := time.Now().UTC()
	if startedAt == nil || startedAt.IsZero() {
		startedAt = &now
	}
	command.Status = instanceCommandStatusRunning
	command.AgentID = &session.Agent.AgentID
	command.StartedAt = startedAt
	return s.commandRepo.Update(command)
}

func (s *instanceCommandService) MarkFinished(session *AgentSession, commandID int, req AgentCommandFinishRequest) error {
	command, err := s.commandRepo.GetByID(commandID)
	if err != nil {
		return err
	}
	if command == nil || command.InstanceID != session.Instance.ID {
		return fmt.Errorf("instance command not found")
	}
	if strings.TrimSpace(req.AgentID) != session.Agent.AgentID {
		return fmt.Errorf("agent id does not match session")
	}
	if req.Status != instanceCommandStatusSucceeded && req.Status != instanceCommandStatusFailed {
		return fmt.Errorf("invalid instance command finish status")
	}

	if command.Status == instanceCommandStatusSucceeded || command.Status == instanceCommandStatusFailed {
		return nil
	}

	finishedAt := req.FinishedAt
	if finishedAt == nil || finishedAt.IsZero() {
		now := time.Now().UTC()
		finishedAt = &now
	}
	command.Status = req.Status
	command.AgentID = &session.Agent.AgentID
	command.FinishedAt = finishedAt
	if req.Result != nil {
		resultJSON, err := marshalOptionalJSON(req.Result)
		if err != nil {
			return fmt.Errorf("failed to encode command result: %w", err)
		}
		command.ResultJSON = resultJSON
	}
	if strings.TrimSpace(req.ErrorMessage) != "" {
		command.ErrorMessage = optionalString(strings.TrimSpace(req.ErrorMessage))
	}
	if command.CommandType == InstanceCommandTypeUninstallSkill && req.Status == instanceCommandStatusSucceeded {
		if err := s.markUninstalledSkillRemoved(command, *finishedAt); err != nil {
			return err
		}
	}
	return s.commandRepo.Update(command)
}

func (s *instanceCommandService) markUninstalledSkillRemoved(command *models.InstanceCommand, observedAt time.Time) error {
	if s.skillRepo == nil || command == nil || command.PayloadJSON == nil || strings.TrimSpace(*command.PayloadJSON) == "" {
		return nil
	}
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(*command.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("failed to decode uninstall skill payload: %w", err)
	}
	if skillID, ok := intPayloadValue(payload["skill_id"]); ok {
		if err := s.skillRepo.MarkInstanceSkillRemoved(command.InstanceID, skillID, observedAt); err != nil {
			return err
		}
	}
	if skillKey, ok := stringPayloadValue(payload["target_name"]); ok {
		return s.skillRepo.MarkInstanceSkillRemovedBySkillKey(command.InstanceID, skillKey, observedAt)
	}
	return nil
}

func intPayloadValue(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int(typed), true
		}
	case int:
		if typed > 0 {
			return typed, true
		}
	}
	return 0, false
}

func stringPayloadValue(value interface{}) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func (s *instanceCommandService) ListByInstanceID(instanceID int, limit int) ([]InstanceCommandPayload, error) {
	items, err := s.commandRepo.ListByInstanceID(instanceID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]InstanceCommandPayload, 0, len(items))
	for _, item := range items {
		payload, err := commandPayloadFromModel(item)
		if err != nil {
			return nil, err
		}
		result = append(result, *payload)
	}
	return result, nil
}

func commandPayloadFromModel(item models.InstanceCommand) (*InstanceCommandPayload, error) {
	payload := &InstanceCommandPayload{
		ID:             item.ID,
		CommandType:    item.CommandType,
		Status:         item.Status,
		IdempotencyKey: item.IdempotencyKey,
		IssuedBy:       item.IssuedBy,
		IssuedAt:       item.IssuedAt,
		DispatchedAt:   item.DispatchedAt,
		StartedAt:      item.StartedAt,
		FinishedAt:     item.FinishedAt,
		TimeoutSeconds: item.TimeoutSeconds,
		ErrorMessage:   item.ErrorMessage,
	}
	if item.PayloadJSON != nil && strings.TrimSpace(*item.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(*item.PayloadJSON), &payload.Payload); err != nil {
			return nil, fmt.Errorf("failed to decode command payload: %w", err)
		}
	}
	if item.ResultJSON != nil && strings.TrimSpace(*item.ResultJSON) != "" {
		if err := json.Unmarshal([]byte(*item.ResultJSON), &payload.Result); err != nil {
			return nil, fmt.Errorf("failed to decode command result: %w", err)
		}
	}
	return payload, nil
}

func isSupportedCommandType(commandType string) bool {
	switch strings.TrimSpace(commandType) {
	case InstanceCommandTypeStartOpenClaw,
		InstanceCommandTypeStopOpenClaw,
		InstanceCommandTypeRestartOpenClaw,
		InstanceCommandTypeCollectSystemInfo,
		InstanceCommandTypeApplyConfigRevision,
		InstanceCommandTypeReloadConfig,
		InstanceCommandTypeHealthCheck,
		InstanceCommandTypeInstallSkill,
		InstanceCommandTypeUpdateSkill,
		InstanceCommandTypeUninstallSkill,
		InstanceCommandTypeRemoveSkill,
		InstanceCommandTypeDisableSkill,
		InstanceCommandTypeQuarantineSkill,
		InstanceCommandTypeHandleSkillRisk,
		InstanceCommandTypeSyncSkillInventory,
		InstanceCommandTypeRefreshSkillInventory,
		InstanceCommandTypeCollectSkillPackage:
		return true
	default:
		return false
	}
}

func (s *instanceCommandService) applyDesiredStateSideEffects(instanceID int, commandType string, payload map[string]interface{}) error {
	state, err := s.desiredStateRepo.GetByInstanceID(instanceID)
	if err != nil {
		return err
	}
	if state == nil {
		return nil
	}

	updated := false
	switch commandType {
	case InstanceCommandTypeStartOpenClaw, InstanceCommandTypeRestartOpenClaw:
		if state.DesiredPowerState != "running" {
			state.DesiredPowerState = "running"
			updated = true
		}
	case InstanceCommandTypeStopOpenClaw:
		if state.DesiredPowerState != "stopped" {
			state.DesiredPowerState = "stopped"
			updated = true
		}
	case InstanceCommandTypeApplyConfigRevision:
		if revisionID, ok := payload["revision_id"].(float64); ok {
			rev := int(revisionID)
			state.DesiredConfigRevisionID = &rev
			updated = true
		}
	}
	if !updated {
		return nil
	}

	state.UpdatedAt = time.Now().UTC()
	if err := s.desiredStateRepo.Update(state); err != nil {
		return err
	}

	runtime, err := s.runtimeRepo.GetByInstanceID(instanceID)
	if err == nil && runtime != nil {
		runtime.DesiredConfigRevisionID = state.DesiredConfigRevisionID
		_ = s.runtimeRepo.Update(runtime)
	}
	return nil
}
