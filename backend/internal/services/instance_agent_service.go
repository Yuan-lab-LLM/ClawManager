package services

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const (
	AgentProtocolVersionV1 = "v1"

	agentStatusOnline  = "online"
	agentStatusOffline = "offline"
	agentStatusStale   = "stale"

	openClawStatusUnknown = "unknown"
)

type AgentRegisterRequest struct {
	InstanceID      int                    `json:"instance_id" binding:"required,min=1"`
	AgentID         string                 `json:"agent_id" binding:"required"`
	AgentVersion    string                 `json:"agent_version" binding:"required"`
	ProtocolVersion string                 `json:"protocol_version" binding:"required"`
	Capabilities    []string               `json:"capabilities"`
	HostInfo        map[string]interface{} `json:"host_info"`
}

type AgentRegisterResponse struct {
	SessionToken               string    `json:"session_token"`
	SessionExpiresAt           time.Time `json:"session_expires_at"`
	HeartbeatIntervalSeconds   int       `json:"heartbeat_interval_seconds"`
	CommandPollIntervalSeconds int       `json:"command_poll_interval_seconds"`
	ServerTime                 time.Time `json:"server_time"`
}

type AgentHeartbeatRequest struct {
	AgentID                 string                 `json:"agent_id" binding:"required"`
	Timestamp               time.Time              `json:"timestamp"`
	OpenClawStatus          string                 `json:"openclaw_status"`
	CurrentConfigRevisionID *int                   `json:"current_config_revision_id,omitempty"`
	Summary                 map[string]interface{} `json:"summary"`
}

type AgentHeartbeatResponse struct {
	ServerTime              time.Time `json:"server_time"`
	HasPendingCommand       bool      `json:"has_pending_command"`
	DesiredPowerState       string    `json:"desired_power_state"`
	DesiredConfigRevisionID *int      `json:"desired_config_revision_id,omitempty"`
}

type InstanceAgentPayload struct {
	AgentID         string                 `json:"agent_id"`
	AgentVersion    string                 `json:"agent_version"`
	ProtocolVersion string                 `json:"protocol_version"`
	Status          string                 `json:"status"`
	Capabilities    []string               `json:"capabilities"`
	HostInfo        map[string]interface{} `json:"host_info,omitempty"`
	LastHeartbeatAt *time.Time             `json:"last_heartbeat_at,omitempty"`
	LastReportedAt  *time.Time             `json:"last_reported_at,omitempty"`
	LastSeenIP      *string                `json:"last_seen_ip,omitempty"`
	RegisteredAt    *time.Time             `json:"registered_at,omitempty"`
}

type AgentSession struct {
	Instance *models.Instance
	Agent    *models.InstanceAgent
}

type InstanceAgentService interface {
	Register(bootstrapToken string, req AgentRegisterRequest, clientIP string) (*AgentRegisterResponse, error)
	AuthenticateSession(sessionToken string) (*AgentSession, error)
	Heartbeat(session *AgentSession, req AgentHeartbeatRequest, clientIP string) (*AgentHeartbeatResponse, error)
	GetPayloadByInstanceID(instanceID int) (*InstanceAgentPayload, error)
}

type instanceAgentService struct {
	instanceRepo     repository.InstanceRepository
	agentRepo        repository.InstanceAgentRepository
	desiredStateRepo repository.InstanceDesiredStateRepository
	runtimeRepo      repository.InstanceRuntimeStatusRepository
	commandRepo      repository.InstanceCommandRepository
}

func NewInstanceAgentService(instanceRepo repository.InstanceRepository, agentRepo repository.InstanceAgentRepository, desiredStateRepo repository.InstanceDesiredStateRepository, runtimeRepo repository.InstanceRuntimeStatusRepository, commandRepo repository.InstanceCommandRepository) InstanceAgentService {
	return &instanceAgentService{
		instanceRepo:     instanceRepo,
		agentRepo:        agentRepo,
		desiredStateRepo: desiredStateRepo,
		runtimeRepo:      runtimeRepo,
		commandRepo:      commandRepo,
	}
}

func (s *instanceAgentService) Register(bootstrapToken string, req AgentRegisterRequest, clientIP string) (*AgentRegisterResponse, error) {
	bootstrapToken = strings.TrimSpace(bootstrapToken)
	if bootstrapToken == "" {
		return nil, fmt.Errorf("agent bootstrap token is required")
	}
	if strings.TrimSpace(req.AgentID) == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	if strings.TrimSpace(req.ProtocolVersion) != AgentProtocolVersionV1 {
		return nil, fmt.Errorf("unsupported agent protocol version")
	}

	instance, err := s.instanceRepo.GetByAgentBootstrapToken(bootstrapToken)
	if err != nil {
		return nil, err
	}
	if instance == nil || instance.ID != req.InstanceID {
		return nil, fmt.Errorf("invalid agent bootstrap token")
	}
	if !supportsManagedRuntimeIntegration(instance.Type) {
		return nil, fmt.Errorf("agent registration is only supported for openclaw or hermes instances")
	}

	now := time.Now().UTC()
	sessionToken, err := generatePrefixedToken("agt_sess")
	if err != nil {
		return nil, fmt.Errorf("failed to generate agent session token: %w", err)
	}
	sessionExpiresAt := now.Add(24 * time.Hour)

	capabilitiesJSON, err := marshalJSON(req.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to encode agent capabilities: %w", err)
	}
	hostInfoJSON, err := marshalOptionalJSON(req.HostInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to encode agent host info: %w", err)
	}

	agent, err := s.agentRepo.GetByInstanceID(instance.ID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		agent = &models.InstanceAgent{
			InstanceID:       instance.ID,
			AgentID:          strings.TrimSpace(req.AgentID),
			AgentVersion:     strings.TrimSpace(req.AgentVersion),
			ProtocolVersion:  strings.TrimSpace(req.ProtocolVersion),
			Status:           agentStatusOnline,
			CapabilitiesJSON: capabilitiesJSON,
			HostInfoJSON:     hostInfoJSON,
			SessionToken:     &sessionToken,
			SessionExpiresAt: &sessionExpiresAt,
			LastHeartbeatAt:  &now,
			LastReportedAt:   nil,
			LastSeenIP:       optionalString(strings.TrimSpace(clientIP)),
			RegisteredAt:     &now,
		}
		if err := s.agentRepo.Create(agent); err != nil {
			return nil, err
		}
	} else {
		agent.AgentID = strings.TrimSpace(req.AgentID)
		agent.AgentVersion = strings.TrimSpace(req.AgentVersion)
		agent.ProtocolVersion = strings.TrimSpace(req.ProtocolVersion)
		agent.Status = agentStatusOnline
		agent.CapabilitiesJSON = capabilitiesJSON
		agent.HostInfoJSON = hostInfoJSON
		agent.SessionToken = &sessionToken
		agent.SessionExpiresAt = &sessionExpiresAt
		agent.LastHeartbeatAt = &now
		agent.LastSeenIP = optionalString(strings.TrimSpace(clientIP))
		agent.RegisteredAt = &now
		if err := s.agentRepo.Update(agent); err != nil {
			return nil, err
		}
	}

	if _, err := s.ensureDesiredState(instance); err != nil {
		return nil, err
	}
	if _, err := s.ensureRuntimeStatus(instance.ID); err != nil {
		return nil, err
	}

	return &AgentRegisterResponse{
		SessionToken:               sessionToken,
		SessionExpiresAt:           sessionExpiresAt,
		HeartbeatIntervalSeconds:   15,
		CommandPollIntervalSeconds: 5,
		ServerTime:                 now,
	}, nil
}

func (s *instanceAgentService) AuthenticateSession(sessionToken string) (*AgentSession, error) {
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return nil, fmt.Errorf("agent session token is required")
	}

	agent, err := s.agentRepo.GetBySessionToken(sessionToken)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.SessionExpiresAt == nil || agent.SessionExpiresAt.Before(time.Now().UTC()) {
		return nil, fmt.Errorf("invalid or expired agent session token")
	}

	instance, err := s.instanceRepo.GetByID(agent.InstanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}

	return &AgentSession{Instance: instance, Agent: agent}, nil
}

func (s *instanceAgentService) Heartbeat(session *AgentSession, req AgentHeartbeatRequest, clientIP string) (*AgentHeartbeatResponse, error) {
	if session == nil || session.Agent == nil || session.Instance == nil {
		return nil, fmt.Errorf("agent session is required")
	}
	if strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.AgentID) != session.Agent.AgentID {
		return nil, fmt.Errorf("agent id does not match session")
	}

	now := time.Now().UTC()
	session.Agent.Status = agentStatusOnline
	session.Agent.LastHeartbeatAt = &now
	session.Agent.LastSeenIP = optionalString(strings.TrimSpace(clientIP))

	// Renew session expiry on each successful heartbeat (rolling window).
	// Without this, the 24-hour session token issued at Register() silently
	// expires and the agent is permanently locked out until pod restart.
	newExpiry := now.Add(24 * time.Hour)
	session.Agent.SessionExpiresAt = &newExpiry

	if err := s.agentRepo.Update(session.Agent); err != nil {
		return nil, err
	}

	runtimeStatus, err := s.ensureRuntimeStatus(session.Instance.ID)
	if err != nil {
		return nil, err
	}
	runtimeStatus.AgentStatus = agentStatusOnline
	if strings.TrimSpace(req.OpenClawStatus) != "" {
		runtimeStatus.OpenClawStatus = strings.TrimSpace(req.OpenClawStatus)
	}
	runtimeStatus.CurrentConfigRevisionID = req.CurrentConfigRevisionID
	if req.Summary != nil {
		summaryJSON, err := marshalOptionalJSON(req.Summary)
		if err != nil {
			return nil, fmt.Errorf("failed to encode heartbeat summary: %w", err)
		}
		runtimeStatus.SummaryJSON = summaryJSON
		if openClawPID, ok := req.Summary["openclaw_pid"].(float64); ok {
			pid := int(openClawPID)
			runtimeStatus.OpenClawPID = &pid
		}
	}
	runtimeStatus.LastReportedAt = &now
	if err := s.runtimeRepo.Update(runtimeStatus); err != nil {
		return nil, err
	}

	desiredState, err := s.ensureDesiredState(session.Instance)
	if err != nil {
		return nil, err
	}
	nextCommand, err := s.commandRepo.GetNextPendingByInstance(session.Instance.ID)
	if err != nil {
		return nil, err
	}

	return &AgentHeartbeatResponse{
		ServerTime:              now,
		HasPendingCommand:       nextCommand != nil,
		DesiredPowerState:       desiredState.DesiredPowerState,
		DesiredConfigRevisionID: desiredState.DesiredConfigRevisionID,
	}, nil
}

func (s *instanceAgentService) GetPayloadByInstanceID(instanceID int) (*InstanceAgentPayload, error) {
	agent, err := s.agentRepo.GetByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, nil
	}

	payload := &InstanceAgentPayload{
		AgentID:         agent.AgentID,
		AgentVersion:    agent.AgentVersion,
		ProtocolVersion: agent.ProtocolVersion,
		Status:          deriveAgentStatus(agent),
		LastHeartbeatAt: agent.LastHeartbeatAt,
		LastReportedAt:  agent.LastReportedAt,
		LastSeenIP:      agent.LastSeenIP,
		RegisteredAt:    agent.RegisteredAt,
	}
	if err := unmarshalJSON(agent.CapabilitiesJSON, &payload.Capabilities); err != nil {
		return nil, fmt.Errorf("failed to decode agent capabilities: %w", err)
	}
	if agent.HostInfoJSON != nil && strings.TrimSpace(*agent.HostInfoJSON) != "" {
		if err := unmarshalJSON(*agent.HostInfoJSON, &payload.HostInfo); err != nil {
			return nil, fmt.Errorf("failed to decode agent host info: %w", err)
		}
	}
	return payload, nil
}

func (s *instanceAgentService) ensureDesiredState(instance *models.Instance) (*models.InstanceDesiredState, error) {
	state, err := s.desiredStateRepo.GetByInstanceID(instance.ID)
	if err != nil {
		return nil, err
	}
	if state != nil {
		return state, nil
	}

	desiredPowerState := "stopped"
	if instance.Status == "running" || instance.Status == "creating" {
		desiredPowerState = "running"
	}
	now := time.Now().UTC()
	state = &models.InstanceDesiredState{
		InstanceID:        instance.ID,
		DesiredPowerState: desiredPowerState,
		UpdatedAt:         now,
		CreatedAt:         now,
	}
	if err := s.desiredStateRepo.Create(state); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *instanceAgentService) ensureRuntimeStatus(instanceID int) (*models.InstanceRuntimeStatus, error) {
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

func deriveAgentStatus(agent *models.InstanceAgent) string {
	if agent == nil || agent.LastHeartbeatAt == nil {
		return agentStatusOffline
	}
	age := time.Since(*agent.LastHeartbeatAt)
	switch {
	case age <= 45*time.Second:
		return agentStatusOnline
	case age <= 120*time.Second:
		return agentStatusStale
	default:
		return agentStatusOffline
	}
}

func generatePrefixedToken(prefix string) (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(bytes), nil
}

func marshalJSON(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func marshalOptionalJSON(value interface{}) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded := string(raw)
	return &encoded, nil
}

func unmarshalJSON(raw string, target interface{}) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}
