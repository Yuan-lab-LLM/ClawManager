package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	posixpath "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	teamSharedMountPath     = "/team"
	teamConfigFileName      = "team.json"
	teamAgentsFileName      = "AGENTS.md"
	teamSoulFileName        = "SOUL.md"
	teamConfigMountDirPath  = "/etc/clawmanager/team"
	teamConfigMountPath     = teamConfigMountDirPath + "/" + teamConfigFileName
	teamHermesSoulMountPath = "/config/.hermes/SOUL.md"
	teamSharedUID           = 1000
	teamSharedGID           = 1000
	teamSharedUmask         = "0002"
	teamRedisURLSecretKey   = "CLAWMANAGER_TEAM_REDIS_URL"
	teamTokenSecretKey      = "CLAWMANAGER_TEAM_TOKEN"

	defaultTeamTaskStaleTimeout = 30 * time.Minute
	teamTaskStaleSweepInterval  = 30 * time.Second
	teamConsumerScanInterval    = 10 * time.Second
	teamAssignmentMonitorEvery  = 3 * time.Minute
	teamEventOutboxBatchSize    = 100

	initialLeaderTaskIntent = "team_bootstrap_introduction"
	teamTaskCompletionTool  = "team_complete_task"
	teamTaskReplyTarget     = "clawmanager"
)

const (
	teamCommunicationModeLeaderMediated = "leader_mediated"
	teamCommunicationModePeerAssisted   = "peer_assisted"
	teamCommunicationModeFullMesh       = "full_mesh"
)

const (
	teamWorkflowStatePlanning               = "planning"
	teamWorkflowStateExecuting              = "executing"
	teamWorkflowStateAwaitingPhaseResults   = "awaiting_phase_results"
	teamWorkflowStateAwaitingLeaderDecision = "awaiting_leader_decision"
	teamWorkflowStateSynthesizing           = "synthesizing"
	teamWorkflowStateCompletionPending      = "completion_pending"
	teamWorkflowStateCompleted              = "completed"
	teamWorkflowStateFailed                 = "failed"

	teamPhaseStatusPlanned                = "planned"
	teamPhaseStatusActive                 = "active"
	teamPhaseStatusAwaitingResults        = "awaiting_results"
	teamPhaseStatusAwaitingLeaderDecision = "awaiting_leader_decision"
	teamPhaseStatusCompleted              = "completed"
	teamPhaseStatusCancelled              = "cancelled"
	teamPhaseStatusSuperseded             = "superseded"

	teamCompletionDecisionAccepted          = "accepted"
	teamCompletionDecisionDeferred          = "deferred"
	teamCompletionDecisionNeedsConfirmation = "needs_confirmation"
	teamCompletionDecisionRejected          = "rejected"
)

var (
	teamMemberKeyPattern                = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	teamMemberInstanceNameInvalidChars  = regexp.MustCompile(`[^a-z0-9-]+`)
	teamMemberInstanceNameRepeatedDashs = regexp.MustCompile(`-+`)
)

type TeamService interface {
	StartBackground(ctx context.Context)
	StopBackground()
	CreateTeam(userID int, req CreateTeamRequest) (*TeamDetailsPayload, error)
	ListTeams(userID, offset, limit int) (*TeamListPayload, error)
	GetTeam(userID, teamID int) (*TeamDetailsPayload, error)
	ListTeamTasks(userID, teamID, beforeID, limit int) (*TeamTasksHistoryPayload, error)
	ListTeamEvents(userID, teamID, beforeID, limit int) (*TeamEventsHistoryPayload, error)
	ListWorkspaceFiles(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspaceListPayload, error)
	PreviewWorkspaceFile(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspacePreviewPayload, error)
	DownloadWorkspaceFile(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspaceDownloadPayload, error)
	CreateWorkspaceFolder(ctx context.Context, userID, teamID int, req TeamWorkspaceFolderRequest) error
	RenameWorkspaceEntry(ctx context.Context, userID, teamID int, req TeamWorkspaceRenameRequest) error
	DeleteWorkspaceEntry(ctx context.Context, userID, teamID int, relPath string) error
	UploadWorkspaceFiles(ctx context.Context, userID, teamID int, targetPath string, files []*multipart.FileHeader, relativePaths []string) error
	DispatchTask(userID, teamID int, req DispatchTeamTaskRequest) (*TeamTaskPayload, error)
	DeleteTeam(userID, teamID int) error
	DeleteMember(userID, teamID int, memberID string) error
}

type CreateTeamRequest struct {
	Name              string                    `json:"name"`
	Description       *string                   `json:"description,omitempty"`
	CommunicationMode string                    `json:"communication_mode,omitempty"`
	RedisURL          string                    `json:"redis_url,omitempty"`
	SharedStorageGB   int                       `json:"shared_storage_gb,omitempty"`
	StorageClass      string                    `json:"storage_class,omitempty"`
	Members           []CreateTeamMemberRequest `json:"members"`
}

type CreateTeamMemberRequest struct {
	MemberID             string              `json:"member_id,omitempty"`
	Name                 string              `json:"name,omitempty"`
	Role                 string              `json:"role"`
	Mode                 string              `json:"mode,omitempty"`
	InstanceMode         string              `json:"instance_mode,omitempty"`
	RuntimeType          string              `json:"runtime_type,omitempty"`
	Description          *string             `json:"description,omitempty"`
	CPUCores             float64             `json:"cpu_cores,omitempty"`
	MemoryGB             int                 `json:"memory_gb,omitempty"`
	DiskGB               int                 `json:"disk_gb,omitempty"`
	GPUEnabled           bool                `json:"gpu_enabled,omitempty"`
	GPUCount             int                 `json:"gpu_count,omitempty"`
	ImageRegistry        *string             `json:"image_registry,omitempty"`
	ImageTag             *string             `json:"image_tag,omitempty"`
	EnvironmentOverrides map[string]string   `json:"environment_overrides,omitempty"`
	OpenClawConfigPlan   *OpenClawConfigPlan `json:"openclaw_config_plan,omitempty"`
	IsLeader             bool                `json:"is_leader,omitempty"`
}

type DispatchTeamTaskRequest struct {
	TargetMemberID string                 `json:"target_member_id"`
	MessageID      string                 `json:"message_id,omitempty"`
	Payload        map[string]interface{} `json:"payload"`
}

type TeamListPayload struct {
	Teams []models.Team `json:"teams"`
	Total int           `json:"total"`
}

type TeamDetailsPayload struct {
	Team           *models.Team          `json:"team"`
	LeaderMemberID string                `json:"leader_member_id,omitempty"`
	Leader         *models.TeamMember    `json:"leader,omitempty"`
	Members        []models.TeamMember   `json:"members"`
	Tasks          []TeamTaskPayload     `json:"tasks,omitempty"`
	Events         []TeamEventPayload    `json:"events,omitempty"`
	WorkItems      []TeamWorkItemPayload `json:"work_items,omitempty"`
}

type TeamTasksHistoryPayload struct {
	Tasks        []TeamTaskPayload `json:"tasks"`
	HasMore      bool              `json:"has_more"`
	NextBeforeID *int              `json:"next_before_id,omitempty"`
}

type TeamEventsHistoryPayload struct {
	Events       []TeamEventPayload `json:"events"`
	HasMore      bool               `json:"has_more"`
	NextBeforeID *int               `json:"next_before_id,omitempty"`
}

type TeamWorkspaceFileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	ModifiedAt  string `json:"modified_at,omitempty"`
	Previewable bool   `json:"previewable"`
}

type TeamWorkspaceListPayload struct {
	Path    string                   `json:"path"`
	Root    string                   `json:"root"`
	Entries []TeamWorkspaceFileEntry `json:"entries"`
}

type TeamWorkspacePreviewPayload struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type TeamWorkspaceDownloadPayload struct {
	Path        string
	Name        string
	ContentType string
	Data        []byte
}

type TeamWorkspaceFolderRequest struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type TeamWorkspaceRenameRequest struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
}

type TeamTaskPayload struct {
	models.TeamTask
	Payload map[string]interface{} `json:"payload,omitempty"`
	Result  map[string]interface{} `json:"result,omitempty"`
}

type TeamEventPayload struct {
	models.TeamEvent
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type TeamWorkItemPayload struct {
	models.TeamWorkItem
	DependsOn    []string               `json:"depends_on,omitempty"`
	Result       map[string]interface{} `json:"result,omitempty"`
	ArtifactRefs []string               `json:"artifact_refs,omitempty"`
}

type teamService struct {
	repo                  repository.TeamRepository
	instanceService       InstanceService
	openClawConfigPlanner teamOpenClawConfigPlanner
	pvcService            *k8s.PVCService
	secretService         *k8s.SecretService
	configMapService      *k8s.ConfigMapService
	podService            *k8s.PodService

	ctx                   context.Context
	cancel                context.CancelFunc
	mu                    sync.Mutex
	running               bool
	wg                    sync.WaitGroup
	consumers             map[int]struct{}
	assignmentMonitorLast map[string]time.Time
	staleMonitorStarted   bool
	runtimeWorkspaceRoot  string
}

type teamOpenClawConfigPlanner interface {
	PlanWithoutTeamMemberLeaderOnlyChannels(userID int, plan *OpenClawConfigPlan) (*OpenClawConfigPlan, error)
}

type plannedTeamMember struct {
	Request       CreateTeamMemberRequest
	MemberKey     string
	DisplayName   string
	Role          string
	ProfileKey    string
	ProfileName   string
	EffectiveRole string
	RuntimeType   string
	InstanceMode  string
	IsLeader      bool
}

type teamRuntimeSecrets struct {
	RedisURL string
	Token    string
}

type TeamServiceOption func(*teamService)

func WithTeamRuntimeWorkspaceRoot(root string) TeamServiceOption {
	return func(s *teamService) {
		if strings.TrimSpace(root) != "" {
			s.runtimeWorkspaceRoot = strings.TrimSpace(root)
		}
	}
}

func WithTeamOpenClawConfigService(service OpenClawConfigService) TeamServiceOption {
	return func(s *teamService) {
		s.openClawConfigPlanner = service
	}
}

func NewTeamService(repo repository.TeamRepository, instanceService InstanceService, opts ...TeamServiceOption) TeamService {
	ctx, cancel := context.WithCancel(context.Background())
	service := &teamService{
		repo:                  repo,
		instanceService:       instanceService,
		pvcService:            k8s.NewPVCService(),
		secretService:         k8s.NewSecretService(),
		configMapService:      k8s.NewConfigMapService(),
		podService:            k8s.NewPodService(),
		ctx:                   ctx,
		cancel:                cancel,
		consumers:             map[int]struct{}{},
		assignmentMonitorLast: map[string]time.Time{},
		runtimeWorkspaceRoot:  "/workspaces",
	}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service
}

// StartBackground starts the leader-only background workers: a periodic scan
// that ensures a Redis event consumer is running for every active team, and
// the stale-task monitor. It is safe to call repeatedly (a second call while
// running is a no-op) and can be called again after StopBackground, which is
// required for leader-election re-acquisition. HTTP request handling does not
// depend on these workers, so followers can still serve the API and the in-pod
// nginx data plane while only the leader runs them.
func (s *teamService) StartBackground(parent context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	s.ctx = ctx
	s.cancel = cancel
	s.running = true
	s.staleMonitorStarted = false
	s.consumers = map[int]struct{}{}
	s.assignmentMonitorLast = map[string]time.Time{}
	s.wg.Add(1)
	go s.consumerScanLoop(ctx)
	s.mu.Unlock()

	fmt.Println("[TeamService] Starting leader-only background workers...")
	s.ensureStaleTaskMonitor(ctx)
}

// StopBackground stops all background workers and blocks until they have fully
// exited, so a subsequent StartBackground starts from a clean state with no
// goroutines from the previous generation still touching shared maps. It is
// idempotent.
func (s *teamService) StopBackground() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	cancel := s.cancel
	s.mu.Unlock()

	fmt.Println("[TeamService] Stopping leader-only background workers...")
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()

	s.mu.Lock()
	s.consumers = map[int]struct{}{}
	s.staleMonitorStarted = false
	s.mu.Unlock()
}

// consumerScanLoop periodically ensures a consumer goroutine exists for every
// active team. Team creation no longer starts consumers inline (that would run
// on whichever replica served the request); the leader picks up newly active
// teams here within teamConsumerScanInterval.
func (s *teamService) consumerScanLoop(ctx context.Context) {
	defer s.wg.Done()

	s.ensureConsumersForActiveTeams(ctx)

	ticker := time.NewTicker(teamConsumerScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ensureConsumersForActiveTeams(ctx)
		}
	}
}

func (s *teamService) ensureConsumersForActiveTeams(ctx context.Context) {
	teams, err := s.repo.ListActiveTeams()
	if err != nil {
		fmt.Printf("Warning: failed to list active teams for consumer scan: %v\n", err)
		return
	}
	for i := range teams {
		s.ensureConsumer(ctx, teams[i].ID)
	}
}

func (s *teamService) CreateTeam(userID int, req CreateTeamRequest) (*TeamDetailsPayload, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("team name is required")
	}
	if len(req.Members) == 0 {
		return nil, fmt.Errorf("team must include at least one member")
	}
	memberPlans, err := planTeamMembers(req.Name, req.Members)
	if err != nil {
		return nil, err
	}
	existingTeam, err := s.repo.GetTeamByUserIDAndName(userID, req.Name)
	if err != nil {
		return nil, err
	}
	if existingTeam != nil {
		if existingTeam.Status == models.TeamStatusFailed {
			if err := s.DeleteTeam(userID, existingTeam.ID); err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("team name already exists")
		}
	}

	communicationMode, err := normalizeTeamCommunicationMode(req.CommunicationMode)
	if err != nil {
		return nil, err
	}
	redisURL := strings.TrimSpace(req.RedisURL)
	if redisURL == "" {
		redisURL = defaultTeamRedisURL()
	}
	if redisURL == "" {
		return nil, fmt.Errorf("team redis url is required")
	}
	if _, err := newRedisBus(redisURL); err != nil {
		return nil, err
	}

	sharedStorageGB := req.SharedStorageGB
	if sharedStorageGB <= 0 {
		sharedStorageGB = 10
	}
	preflightTeam := &models.Team{
		ID:              0,
		Name:            req.Name,
		StorageClass:    optionalString(strings.TrimSpace(req.StorageClass)),
		SharedMountPath: teamSharedMountPath,
	}
	if err := s.instanceService.ValidateCreateRequests(userID, s.buildTeamMemberInstanceRequests(preflightTeam, memberPlans)); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	storageClass := optionalString(strings.TrimSpace(req.StorageClass))
	team := &models.Team{
		UserID:            userID,
		Name:              req.Name,
		Description:       req.Description,
		Status:            models.TeamStatusCreating,
		CommunicationMode: communicationMode,
		RedisEventsLastID: "0-0",
		SharedMountPath:   teamSharedMountPath,
		StorageClass:      storageClass,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.repo.CreateTeam(team); err != nil {
		return nil, err
	}
	if err := s.instanceService.ValidateCreateRequests(userID, s.buildTeamMemberInstanceRequests(team, memberPlans)); err != nil {
		return nil, s.rollbackTeamCreation(userID, team, err)
	}

	runtimeSecrets, err := s.provisionTeamK8s(userID, team, redisURL, sharedStorageGB, strings.TrimSpace(req.StorageClass))
	if err != nil {
		return nil, s.rollbackTeamCreation(userID, team, err)
	}
	rosterJSON, err := s.upsertTeamRosterConfig(userID, team, memberPlans)
	if err != nil {
		return nil, s.rollbackTeamCreation(userID, team, err)
	}

	for _, memberPlan := range memberPlans {
		member, err := s.createTeamMemberInstance(userID, team, memberPlan, runtimeSecrets, rosterJSON)
		if err != nil {
			return nil, s.rollbackTeamCreation(userID, team, err)
		}
		member.Status = models.TeamMemberStatusIdle
		member.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateMember(member); err != nil {
			return nil, s.rollbackTeamCreation(userID, team, err)
		}
	}

	team.Status = models.TeamStatusRunning
	team.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateTeam(team); err != nil {
		return nil, err
	}
	// Background consumers / stale-task monitor are leader-only and started by
	// the leader's periodic scan (consumerScanLoop). Starting them here would
	// run them on whichever replica served the create request, bypassing
	// leader election, so we intentionally do not call ensureConsumer here.
	if err := s.dispatchInitialLeaderTask(userID, team); err != nil {
		fmt.Printf("Warning: failed to dispatch initial Team %d leader task: %v\n", team.ID, err)
		if recordErr := s.recordInitialLeaderTaskDispatchFailure(team.ID, err); recordErr != nil {
			fmt.Printf("Warning: failed to record Team %d initial leader task dispatch failure: %v\n", team.ID, recordErr)
		}
	}
	return s.GetTeam(userID, team.ID)
}

func (s *teamService) dispatchInitialLeaderTask(userID int, team *models.Team) error {
	if team == nil {
		return fmt.Errorf("team is required")
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	leader := findTeamLeader(activeTeamMembers(members))
	if leader == nil {
		return fmt.Errorf("team leader not found")
	}
	_, err = s.DispatchTask(userID, team.ID, DispatchTeamTaskRequest{
		TargetMemberID: leader.MemberKey,
		MessageID:      initialLeaderTaskMessageID(team.ID),
		Payload:        buildInitialLeaderTaskPayload(team.Name),
	})
	return err
}

func initialLeaderTaskMessageID(teamID int) string {
	return fmt.Sprintf("team-%d-bootstrap-introduction", teamID)
}

func (s *teamService) recordInitialLeaderTaskDispatchFailure(teamID int, cause error) error {
	now := time.Now().UTC()
	payload := map[string]interface{}{
		"v":         1,
		"event":     "bootstrap_dispatch_failed",
		"teamId":    strconv.Itoa(teamID),
		"intent":    initialLeaderTaskIntent,
		"messageId": initialLeaderTaskMessageID(teamID),
		"source":    "clawmanager",
	}
	if cause != nil {
		payload["diagnostic"] = cause.Error()
	}
	payloadJSON, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	messageID := initialLeaderTaskMessageID(teamID)
	return s.repo.CreateEvent(&models.TeamEvent{
		TeamID:      teamID,
		MessageID:   &messageID,
		EventType:   "bootstrap_dispatch_failed",
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	})
}

func buildTeamTaskEnvelope(teamID int, memberKey string, task *models.TeamTask, messageID string, taskPayload map[string]interface{}, memberContext map[string]string, now time.Time) map[string]interface{} {
	if taskPayload == nil {
		taskPayload = map[string]interface{}{}
	}
	taskID := 0
	workflowState := teamWorkflowStatePlanning
	planVersion := int64(0)
	ledgerVersion := int64(0)
	if task != nil {
		taskID = task.ID
		workflowState = task.WorkflowState
		if workflowState == "" {
			workflowState = teamWorkflowStatePlanning
		}
		planVersion = task.PlanVersion
		ledgerVersion = task.LedgerVersion
	}
	taskRef := fmt.Sprintf("team-%d-task-%d", teamID, taskID)
	prompt := eventString(taskPayload, "prompt", "goal", "instruction", "instructions")
	if prompt == "" {
		rawPayload, _ := marshalJSON(taskPayload)
		prompt = rawPayload
	}
	rawPrompt := prompt
	prompt = buildTeamRuntimePrompt(prompt, memberContext)
	intent := eventString(taskPayload, "intent")
	responseLocale := eventString(taskPayload, "responseLocale", "response_locale")
	if responseLocale == "" {
		responseLocale = inferTeamResponseLocale(rawPrompt)
	}
	envelope := map[string]interface{}{
		"v":                  1,
		"protocolVersion":    3,
		"messageId":          messageID,
		"teamId":             strconv.Itoa(teamID),
		"from":               "clawmanager",
		"to":                 memberKey,
		"replyTo":            teamTaskReplyTarget,
		"requiresCompletion": true,
		"completionTool":     teamTaskCompletionTool,
		"resultSink": map[string]interface{}{
			"type":           "redis_stream",
			"eventsKey":      teamEventsKey(teamID),
			"successEvent":   "completion_proposed",
			"failureEvent":   "task_failed",
			"replyEvent":     "reply",
			"resultField":    "resultMarkdown",
			"summaryField":   "summary",
			"artifactField":  "artifactRefs",
			"completionTool": teamTaskCompletionTool,
		},
		"intent":         intent,
		"taskId":         taskRef,
		"title":          eventString(taskPayload, "title"),
		"prompt":         appendTeamResponseLocaleInstruction(appendTeamTaskCompletionInstruction(prompt, memberContext["communicationMode"], intent), responseLocale),
		"rawPrompt":      rawPrompt,
		"responseLocale": responseLocale,
		"workflowState":  workflowState,
		"planVersion":    planVersion,
		"ledgerVersion":  ledgerVersion,
		"contextRefs":    normalizeContextRefs(taskPayload["contextRefs"]),
		"memberContext":  memberContext,
		"systemPrompt":   memberContext["systemPrompt"],
		"monitorPolicy":  defaultTeamMonitorPolicy(),
		"metadata":       taskPayload,
		"createdAt":      now.Format(time.RFC3339Nano),
	}
	if workspaceContract, ok := taskPayload["workspaceContract"]; ok {
		envelope["workspaceContract"] = workspaceContract
		if contract, ok := workspaceContract.(map[string]interface{}); ok {
			physicalRoot := eventString(contract, "physicalSharedDir")
			taskRef := eventString(contract, "taskRef")
			memberArtifactPhysicalRoot := ""
			if physicalRoot != "" && taskRef != "" {
				memberArtifactPhysicalRoot = filepath.ToSlash(filepath.Join(physicalRoot, "artifacts", taskRef, "members", normalizeTeamMemberRouteKey(memberKey)))
			}
			envelope["sharedWorkspace"] = map[string]interface{}{
				"physicalPath":                physicalRoot,
				"canonicalPrefix":             "/team",
				"memberArtifactPhysicalRoot":  memberArtifactPhysicalRoot,
				"memberArtifactCanonicalRoot": "/team/artifacts/" + taskRef + "/members/" + normalizeTeamMemberRouteKey(memberKey),
			}
		}
	}
	if bootstrapSnapshot, ok := taskPayload["bootstrapSnapshot"]; ok {
		envelope["bootstrapSnapshot"] = bootstrapSnapshot
	}
	if teamConfigJSON, ok := taskPayload["teamConfigJson"]; ok {
		envelope["teamConfigJson"] = teamConfigJSON
	} else if teamConfigJSON, ok := taskPayload["teamConfigJSON"]; ok {
		envelope["teamConfigJson"] = teamConfigJSON
	}
	if envelope["intent"] == "" {
		envelope["intent"] = "run_task"
	}
	if envelope["title"] == "" {
		envelope["title"] = fmt.Sprintf("Team task %d", taskID)
	}
	return envelope
}

func inferTeamResponseLocale(text string) string {
	for _, r := range text {
		if r >= '\u3400' && r <= '\u9fff' {
			return "zh-CN"
		}
	}
	return "en-US"
}

func appendTeamResponseLocaleInstruction(prompt, locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = "zh-CN"
	}
	instruction := fmt.Sprintf("Response locale contract: use %s for every user-visible plan, assignment, progress summary, status-check summary, resultMarkdown, and final synthesis. Keep source code, API names, file names, and necessary technical terms in their original form. Do not rely on frontend translation.", locale)
	if strings.TrimSpace(prompt) == "" {
		return instruction
	}
	return strings.TrimSpace(prompt) + "\n\n" + instruction
}

func applyTeamTaskEnvelopeContext(envelope map[string]interface{}, task *models.TeamTask, targetMemberKey string) {
	if envelope == nil || task == nil {
		return
	}
	payload := map[string]interface{}{}
	if strings.TrimSpace(task.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(task.PayloadJSON), &payload)
	}
	locale := eventString(payload, "responseLocale", "response_locale")
	if locale == "" {
		locale = inferTeamResponseLocale(eventString(payload, "prompt", "goal", "instruction", "instructions"))
	}
	envelope["responseLocale"] = locale
	envelope["workflowState"] = task.WorkflowState
	envelope["planVersion"] = task.PlanVersion
	envelope["ledgerVersion"] = task.LedgerVersion
	if task.CurrentPhaseID != nil {
		envelope["currentPhaseId"] = *task.CurrentPhaseID
	}
	contract, _ := payload["workspaceContract"].(map[string]interface{})
	if contract == nil {
		return
	}
	envelope["workspaceContract"] = contract
	physicalRoot := eventString(contract, "physicalSharedDir")
	taskRef := eventString(contract, "taskRef")
	memberKey := normalizeTeamMemberRouteKey(targetMemberKey)
	memberPhysicalRoot := ""
	memberCanonicalRoot := ""
	if physicalRoot != "" && taskRef != "" && memberKey != "" {
		memberPhysicalRoot = filepath.ToSlash(filepath.Join(physicalRoot, "artifacts", taskRef, "members", memberKey))
		memberCanonicalRoot = "/team/artifacts/" + taskRef + "/members/" + memberKey
	}
	envelope["sharedWorkspace"] = map[string]interface{}{
		"physicalPath":                physicalRoot,
		"canonicalPrefix":             "/team",
		"memberArtifactPhysicalRoot":  memberPhysicalRoot,
		"memberArtifactCanonicalRoot": memberCanonicalRoot,
	}
}

func defaultTeamMonitorPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":                  true,
		"heartbeatEverySec":        30,
		"visibleHeartbeatEverySec": int(teamAssignmentMonitorEvery.Seconds()),
		"checkEverySec":            int(teamAssignmentMonitorEvery.Seconds()),
		"softTimeoutSec":           int((2 * teamAssignmentMonitorEvery).Seconds()),
		"visibleToChat":            true,
	}
}

func appendTeamTaskCompletionInstruction(prompt string, communicationMode, intent string) string {
	base := strings.TrimSpace(prompt)
	if strings.TrimSpace(intent) == initialLeaderTaskIntent {
		instruction := strings.Join([]string{
			"Bootstrap completion contract:",
			"- This is a control-plane Team snapshot assigned only to the Leader. Do not delegate it, create worker assignments, or wait for member replies.",
			"- Use the injected metadata.bootstrapSnapshot, metadata.teamConfigJson, and metadata.workspaceContract first. They are the authoritative roster, runtime, role, and workspace facts for this snapshot.",
			"- If injected metadata is insufficient, read /team/team.json from the shared workspace. $CLAWMANAGER_TEAM_CONFIG_PATH and /etc/clawmanager/team/team.json are optional system fallbacks and may be blocked by tool sandboxes. Never look for /team/members.",
			"- Do not probe member HTTP health endpoints, Redis CLI, or worker desktops for this bootstrap. If a runtime status source is unavailable in the injected snapshot, report that field as unavailable instead of blocking.",
			"- Summarize every member's identity, role, runtime, responsibilities, capability boundaries, and the configured collaboration mode.",
			"- Explain task routing, Team Redis event synchronization, shared workspace usage, and the available Team methods without asking other members to restate their own roles.",
			"- If you write a detailed report, write it under \"$CLAWMANAGER_TEAM_SHARED_DIR/results/<taskId>/\" and report it only as /team/results/<taskId>/<file>.",
			"- Complete this bootstrap in the current turn by calling team_complete_task with status=\"succeeded\", summary, and resultMarkdown.",
			"- Successful bootstrap completion is recorded as the task_completed event.",
			"- Do not finish with tool calls only and do not wait for QA/review evidence for this bootstrap snapshot.",
		}, "\n")
		if base == "" {
			return instruction
		}
		return base + "\n\n" + instruction
	}
	if strings.Contains(base, teamTaskCompletionTool) && strings.Contains(base, "task_completed") {
		return base
	}
	mode := normalizedTeamCommunicationMode(communicationMode)
	modeInstructions := []string{
		"- Leader-mediated mode is a strict hub-and-spoke workflow: user root task -> Leader -> assigned workers -> Leader -> final user-facing result.",
		"- If you are the Leader and the user names a non-Leader member or role, you MUST delegate the work to that exact member with team_send, wait for the assigned workers' actual results, then provide a final synthesis before completing the root task.",
		"- If you are the Leader and the user gives a broad task without naming one member, first create a compact plan, decompose the work into owner/member_id assignments, send those assignments, wait for the assigned workers' actual results, verify them, then complete the root task.",
		"- If you are the Leader, answer self-contained control-plane or simple tasks directly only when the request clearly stays within the Leader/control-plane scope and does not require a named worker or multi-member evidence.",
		"- If you are a Worker, execute only the assignment addressed to you and report the result, evidence, artifact paths, or blocker back to the Leader. Do not hand off directly to another Worker.",
		"- A Leader dispatch, plan, or handoff is not a final result and must not call team_complete_task. Worker completion never closes the user root task. Only the Leader may finalize the root task after reconciling all required member outputs.",
	}
	switch mode {
	case teamCommunicationModePeerAssisted:
		modeInstructions = []string{
			"- Worker-direct mode: if the root task or collaboration plan names a downstream member, you MUST hand off to that exact member with team_send before completing your own step. This handoff is required, not optional.",
			"- In worker-direct mode, do not send a completed step only to the Leader when a downstream owner is specified. The Leader is the fallback only when no downstream owner is specified, when you are blocked, or when final synthesis is explicitly requested.",
			"- A worker-to-worker handoff must include rootTaskId/rootMessageId when available, artifact paths, the requested next action, acceptance criteria, and whether a reply is required.",
		}
	case teamCommunicationModeFullMesh:
		modeInstructions = []string{
			"- Full-mesh mode: coordinate directly with the named downstream owners. If a member is specified as the next owner, hand off to that exact member before completing your own step.",
			"- Preserve rootTaskId/rootMessageId, artifact paths, requested next action, acceptance criteria, and reply requirements in every peer handoff.",
		}
	}
	instruction := strings.Join([]string{
		"Completion contract:",
		"- For multi-member Teams, first write a compact collaboration plan: subtasks, owner member_id, dependency, expected artifact, and verification rule.",
		"- Publish that plan to ClawManager with team_update_progress status=\"running\" and eventKind=\"leader_plan\" before dispatching worker assignments. Plans are process visibility, not completion.",
	}, "\n")
	instruction += "\n" + strings.Join(modeInstructions, "\n")
	instruction += "\n" + strings.Join([]string{
		"- Publish meaningful process updates with team_update_progress. Use eventKind=\"worker_plan\" for worker execution plans, \"worker_progress\" for milestones, and \"leader_synthesis\" while reconciling member outputs. Use \"assignment_check_result\" only when replying to a ClawManager Monitor envelope carrying a monitor checkId; ordinary progress must remain worker_progress.",
		"- Prefer team_artifact_write, team_artifact_read, team_artifact_list, and team_artifact_mkdir for shared artifacts. These tools enforce current-Team path isolation and cooperative permissions.",
		"- If a worker is still executing a long step, report concise progress and continue. If context was lost or an artifact path is wrong, report a recoverable blocker to the Leader instead of treating the root task as failed.",
		"- Every Team message must preserve rootTaskId/messageId context when available and must clearly state whether it is an assignment, peer request, progress update, result, review, blocker, or final synthesis.",
		"- For multi-stage work, publish a structured leader_plan with planVersion and phases. Every team_send must carry a stable phaseId, assignmentId, workId, revision, required flag, and dependencies. Completing one phase never completes the user root task.",
		"- The Leader may call team_complete_task for the root only after the workflow is sealed, remainingActions is empty, every required latest assignment and review is complete, and finalAnswerReady is true. Worker completion closes only that assignment.",
		"- A failed or stale required assignment blocks root success unless the Leader supplies a structured waiver containing assignmentId, reason, and accepted risk. Never waive running/pending work or omit the risk record.",
		"- Optional work does not need to succeed, but every omitted optional assignment must be listed in skippedAssignments with assignmentId and a concrete reason.",
		"- Report verification truthfully. If browser/DOM verification did not run or failed, label it as unverified; a hand-written simulator or static inspection is not a browser pass. Any artifact change after review invalidates that review and requires fresh validation.",
		"- The Leader must not mark the root task succeeded after merely dispatching work. Final success requires returned member evidence plus Leader synthesis, or a truly direct self-contained answer.",
		"- Write shared artifacts under the exact directory in CLAWMANAGER_TEAM_SHARED_DIR. When using shell commands, always create files under \"$CLAWMANAGER_TEAM_SHARED_DIR/<relative-path>\".",
		"- Never create or report a relative team/... folder. The path team/... is invalid because ClawManager file browsing only resolves shared artifacts through the Team shared directory.",
		"- Report shared artifact links using the canonical UI path /team/<relative-path>, even when a Lite runtime uses a different physical shared directory.",
		"- Members must report produced artifact paths and concrete outcomes through the Team channel before completing their assigned task.",
		"- When the final result is ready, call team_complete_task with status=\"succeeded\", summary, and resultMarkdown.",
		"- If the task fails, call team_complete_task with status=\"failed\" and an error message.",
		"- Do not send the final answer as a normal message to clawmanager; ClawManager consumes task_completed/task_failed events from the Team Redis event stream.",
	}, "\n")
	if base == "" {
		return instruction
	}
	return base + "\n\n" + instruction
}

func (s *teamService) provisionTeamK8s(userID int, team *models.Team, redisURL string, sharedStorageGB int, storageClass string) (*teamRuntimeSecrets, error) {
	ctx := context.Background()
	pvc, err := s.pvcService.CreateTeamSharedPVC(ctx, userID, team.ID, sharedStorageGB, storageClass)
	if err != nil {
		return nil, err
	}
	secretName := s.pvcService.GetClient().GetTeamSecretName(team.ID)
	teamToken, err := generatePrefixedToken("team")
	if err != nil {
		return nil, fmt.Errorf("failed to generate Team token: %w", err)
	}
	if err := s.secretService.UpsertSecret(ctx, userID, secretName, map[string]string{
		teamRedisURLSecretKey: redisURL,
		teamTokenSecretKey:    teamToken,
	}, map[string]string{
		"app":        "clawreef",
		"managed-by": "clawreef",
		"team-id":    strconv.Itoa(team.ID),
	}); err != nil {
		return nil, err
	}

	team.RedisURLSecretName = &secretName
	team.RedisURLSecretKey = optionalString(teamRedisURLSecretKey)
	team.TeamTokenSecretName = &secretName
	team.TeamTokenSecretKey = optionalString(teamTokenSecretKey)
	team.SharedPVCName = &pvc.Name
	team.SharedPVCNamespace = &pvc.Namespace
	team.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateTeam(team); err != nil {
		return nil, err
	}
	return &teamRuntimeSecrets{RedisURL: redisURL, Token: teamToken}, nil
}

func (s *teamService) upsertTeamRosterConfig(userID int, team *models.Team, members []plannedTeamMember) (string, error) {
	rosterJSON, err := buildTeamRosterConfig(team, members)
	if err != nil {
		return "", err
	}
	data := buildTeamRosterConfigData(rosterJSON, team, members)
	if err := s.configMapService.UpsertConfigMap(context.Background(), userID, s.teamConfigMapName(team.ID), data, map[string]string{
		"app":        "clawreef",
		"managed-by": "clawreef",
		"team-id":    strconv.Itoa(team.ID),
	}); err != nil {
		return "", err
	}
	if err := s.writeSharedTeamRosterConfig(userID, team, rosterJSON); err != nil {
		return "", err
	}
	return rosterJSON, nil
}

func (s *teamService) writeSharedTeamRosterConfig(userID int, team *models.Team, rosterJSON string) error {
	if s == nil || team == nil || strings.TrimSpace(rosterJSON) == "" {
		return nil
	}
	root := filepath.Clean(s.teamRuntimeSharedPathFor(userID, team.ID))
	if root == "." || root == string(filepath.Separator) {
		return fmt.Errorf("invalid Team shared workspace root for Team %d: %q", team.ID, root)
	}
	for _, rel := range []string{"", "results", "tasks", "inbox", "status", "artifacts", "tmp"} {
		target := root
		if rel != "" {
			target = filepath.Join(root, rel)
		}
		if err := os.MkdirAll(target, 0o2775); err != nil {
			return fmt.Errorf("failed to prepare Team shared workspace %s: %w", target, err)
		}
		_ = os.Chmod(target, 0o2775)
	}
	path := filepath.Join(root, teamConfigFileName)
	if err := os.WriteFile(path, []byte(rosterJSON), 0o664); err != nil {
		return fmt.Errorf("failed to write shared Team roster %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o664)
	return nil
}

func buildTeamRosterConfigData(rosterJSON string, team *models.Team, members []plannedTeamMember) map[string]string {
	data := map[string]string{
		teamConfigFileName: rosterJSON,
	}
	for _, member := range members {
		if member.RuntimeType != "hermes" {
			continue
		}
		data[teamMemberSoulConfigKey(member.MemberKey)] = buildTeamMemberSoulMarkdown(member, normalizedTeamCommunicationMode(team.CommunicationMode))
	}
	return data
}

func (s *teamService) teamConfigMapName(teamID int) string {
	client := k8s.GetClient()
	if client == nil {
		return fmt.Sprintf("clawreef-team-%d-config", teamID)
	}
	return client.GetTeamConfigMapName(teamID)
}

func (s *teamService) createTeamMemberInstance(userID int, team *models.Team, memberPlan plannedTeamMember, runtimeSecrets *teamRuntimeSecrets, rosterJSON string) (*models.TeamMember, error) {
	now := time.Now().UTC()
	member := &models.TeamMember{
		TeamID:       team.ID,
		UserID:       userID,
		MemberKey:    memberPlan.MemberKey,
		DisplayName:  memberPlan.DisplayName,
		Role:         memberPlan.Role,
		RuntimeType:  memberPlan.RuntimeType,
		InstanceMode: memberPlan.InstanceMode,
		Description:  optionalString(plannedTeamMemberDescription(memberPlan)),
		Status:       models.TeamMemberStatusCreating,
		Availability: models.TeamMemberAvailabilityUnknown,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateMember(member); err != nil {
		return nil, err
	}

	createReq := s.buildTeamMemberInstanceRequestWithSecrets(team, memberPlan, runtimeSecrets, rosterJSON)
	memberOpenClawPlan, err := s.openClawConfigPlanForTeamMember(userID, memberPlan)
	if err != nil {
		member.Status = models.TeamMemberStatusFailed
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(member)
		return nil, err
	}
	createReq.OpenClawConfigPlan = memberOpenClawPlan
	instance, err := s.instanceService.Create(userID, createReq)
	if err != nil {
		member.Status = models.TeamMemberStatusFailed
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(member)
		return nil, err
	}
	if err := s.writeLiteTeamMemberIdentityFiles(instance, team, memberPlan, rosterJSON); err != nil {
		member.Status = models.TeamMemberStatusFailed
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(member)
		return nil, err
	}
	member.InstanceID = &instance.ID
	member.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateMember(member); err != nil {
		return nil, err
	}
	return member, nil
}

func (s *teamService) openClawConfigPlanForTeamMember(userID int, memberPlan plannedTeamMember) (*OpenClawConfigPlan, error) {
	plan := memberPlan.Request.OpenClawConfigPlan
	if plan == nil || memberPlan.IsLeader || s.openClawConfigPlanner == nil {
		return plan, nil
	}
	return s.openClawConfigPlanner.PlanWithoutTeamMemberLeaderOnlyChannels(userID, plan)
}

func (s *teamService) buildTeamMemberInstanceRequests(team *models.Team, memberPlans []plannedTeamMember) []CreateInstanceRequest {
	requests := make([]CreateInstanceRequest, 0, len(memberPlans))
	for _, memberPlan := range memberPlans {
		requests = append(requests, s.buildTeamMemberInstanceRequest(team, memberPlan))
	}
	return requests
}

func (s *teamService) buildTeamMemberInstanceRequest(team *models.Team, memberPlan plannedTeamMember) CreateInstanceRequest {
	return s.buildTeamMemberInstanceRequestWithSecrets(team, memberPlan, nil, "")
}

func (s *teamService) buildTeamMemberInstanceRequestWithSecrets(team *models.Team, memberPlan plannedTeamMember, runtimeSecrets *teamRuntimeSecrets, rosterJSON string) CreateInstanceRequest {
	req := memberPlan.Request
	instanceMode := memberPlan.InstanceMode
	if instanceMode == "" {
		instanceMode = InstanceModeLite
	}
	runtimeBackendType, _ := RuntimeTypeForInstanceMode(instanceMode)
	memberEnv := s.teamMemberEnv(team, memberPlan)
	if instanceMode == InstanceModeLite {
		memberEnv["CLAWMANAGER_TEAM_SHARED_DIR"] = s.teamRuntimeSharedPath(team)
	}
	environmentOverrides := mergeEnvMaps(req.EnvironmentOverrides, memberEnv)
	if instanceMode == InstanceModeLite && runtimeSecrets != nil {
		environmentOverrides = mergeEnvMaps(environmentOverrides, map[string]string{
			teamRedisURLSecretKey: runtimeSecrets.RedisURL,
			teamTokenSecretKey:    runtimeSecrets.Token,
		})
		if strings.TrimSpace(rosterJSON) != "" {
			environmentOverrides["CLAWMANAGER_TEAM_CONFIG_JSON"] = rosterJSON
		}
	}
	return CreateInstanceRequest{
		Name:                 teamMemberInstanceName(team.Name, team.ID, memberPlan.MemberKey),
		Type:                 memberPlan.RuntimeType,
		Mode:                 instanceMode,
		InstanceMode:         instanceMode,
		RuntimeType:          runtimeBackendType,
		CPUCores:             defaultFloat(req.CPUCores, 2),
		MemoryGB:             defaultInt(req.MemoryGB, 4),
		DiskGB:               defaultInt(req.DiskGB, 20),
		GPUEnabled:           req.GPUEnabled,
		GPUCount:             req.GPUCount,
		OSType:               memberPlan.RuntimeType,
		OSVersion:            "latest",
		ImageRegistry:        req.ImageRegistry,
		ImageTag:             req.ImageTag,
		EnvironmentOverrides: environmentOverrides,
		StorageClass:         derefTeamString(team.StorageClass),
		OpenClawConfigPlan:   req.OpenClawConfigPlan,
		Team: &TeamInstanceConfig{
			Environment:      memberEnv,
			SecretName:       derefTeamString(team.TeamTokenSecretName),
			SharedPVCName:    derefTeamString(team.SharedPVCName),
			SharedMountPath:  team.SharedMountPath,
			ConfigMapName:    s.teamConfigMapName(team.ID),
			ConfigMountPath:  teamConfigMountDirPath,
			PersonaConfigKey: teamMemberPersonaConfigKey(memberPlan),
			SharedUID:        teamSharedUID,
			SharedGID:        teamSharedGID,
			SharedUmask:      teamSharedUmask,
		},
	}
}

func (s *teamService) teamRuntimeSharedPath(team *models.Team) string {
	if team == nil {
		return k8s.TeamSharedWorkspacePath(s.runtimeWorkspaceRoot, 0, 0)
	}
	return k8s.TeamSharedWorkspacePath(s.runtimeWorkspaceRoot, team.UserID, team.ID)
}

func (s *teamService) teamRuntimeSharedPathFor(userID, teamID int) string {
	return k8s.TeamSharedWorkspacePath(s.runtimeWorkspaceRoot, userID, teamID)
}

func (s *teamService) teamMemberEnv(team *models.Team, member plannedTeamMember) map[string]string {
	managerBaseURL, _ := defaultTeamManagerBaseURL()
	memberContext := buildPlannedTeamMemberTaskContext(member)
	communicationMode := normalizedTeamCommunicationMode(team.CommunicationMode)
	collaborationPolicy := buildTeamCollaborationPolicy(communicationMode)
	collaborationPolicyJSON, _ := json.Marshal(collaborationPolicy)
	env := map[string]string{
		"CLAWMANAGER_TEAM_ENABLED":            "true",
		"CLAWMANAGER_TEAM_ID":                 strconv.Itoa(team.ID),
		"CLAWMANAGER_TEAM_MEMBER_ID":          member.MemberKey,
		"CLAWMANAGER_TEAM_ROLE":               effectiveTeamMemberRole(member),
		"CLAWMANAGER_TEAM_EFFECTIVE_ROLE":     effectiveTeamMemberRole(member),
		"CLAWMANAGER_TEAM_COMMUNICATION_MODE": communicationMode,
		"CLAWMANAGER_TEAM_SHARED_DIR":         team.SharedMountPath,
		"CLAWMANAGER_TEAM_SHARED_UID":         strconv.Itoa(teamSharedUID),
		"CLAWMANAGER_TEAM_SHARED_GID":         strconv.Itoa(teamSharedGID),
		"CLAWMANAGER_TEAM_UMASK":              teamSharedUmask,
		"PUID":                                strconv.Itoa(teamSharedUID),
		"PGID":                                strconv.Itoa(teamSharedGID),
		"UMASK":                               teamSharedUmask,
		"CLAWMANAGER_TEAM_CONFIG_PATH":        teamConfigMountPath,
		"CLAWMANAGER_TEAM_AUTORUN":            "true",
		"CLAWMANAGER_TEAM_CONSUMER_GROUP":     "team-members",
		"CLAWMANAGER_TEAM_INBOX_KEY":          teamInboxKey(team.ID, member.MemberKey),
		"CLAWMANAGER_TEAM_EVENTS_KEY":         teamEventsKey(team.ID),
		"CLAWMANAGER_TEAM_PRESENCE_KEY":       teamPresenceKey(team.ID),
		"CLAWMANAGER_TEAM_DLQ_KEY":            teamDLQKey(team.ID),
		"CLAWMANAGER_TEAM_MANAGER_URL":        managerBaseURL,
		"GATEWAY_ALLOW_ALL_USERS":             "true",
	}
	if len(collaborationPolicyJSON) > 0 {
		env["CLAWMANAGER_TEAM_COLLABORATION_POLICY_JSON"] = string(collaborationPolicyJSON)
	}
	if profileKey := strings.TrimSpace(member.ProfileKey); profileKey != "" {
		env["CLAWMANAGER_TEAM_PROFILE_KEY"] = profileKey
	}
	if profileName := strings.TrimSpace(member.ProfileName); profileName != "" {
		env["CLAWMANAGER_TEAM_PROFILE_NAME"] = profileName
	}
	if description := strings.TrimSpace(memberContext["description"]); description != "" {
		env["CLAWMANAGER_TEAM_MEMBER_DESCRIPTION"] = description
	}
	if systemPrompt := strings.TrimSpace(memberContext["systemPrompt"]); systemPrompt != "" {
		systemPrompt = appendTeamCollaborationGuidance(systemPrompt, communicationMode)
		systemPrompt = appendTeamWorkspaceGuidance(systemPrompt)
		env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"] = systemPrompt
		env["HERMES_AGENT_HELP_GUIDANCE"] = systemPrompt
	}
	return env
}

func teamMemberPersonaConfigKey(member plannedTeamMember) string {
	if member.RuntimeType != "hermes" {
		return ""
	}
	return teamMemberSoulConfigKey(member.MemberKey)
}

func teamMemberSoulConfigKey(memberKey string) string {
	key := normalizeTeamMemberKeyForInstanceName(memberKey)
	if key == "" {
		key = "member"
	}
	return fmt.Sprintf("hermes-soul-%s.md", key)
}

func (s *teamService) ListTeams(userID, offset, limit int) (*TeamListPayload, error) {
	teams, err := s.repo.ListTeamsByUserID(userID, offset, limit)
	if err != nil {
		return nil, err
	}
	teams = activeTeams(teams)
	total, err := s.repo.CountTeamsByUserID(userID)
	if err != nil {
		return nil, err
	}
	return &TeamListPayload{Teams: teams, Total: total}, nil
}

func (s *teamService) GetTeam(userID, teamID int) (*TeamDetailsPayload, error) {
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembersByTeamID(teamID)
	if err != nil {
		return nil, err
	}
	members = activeTeamMembers(members)
	tasks, err := s.repo.ListTasksByTeamID(teamID, 20)
	if err != nil {
		return nil, err
	}
	events, err := s.repo.ListEventsByTeamID(teamID, 200)
	if err != nil {
		return nil, err
	}
	workItems, err := s.repo.ListWorkItemsByTeamID(teamID, 200)
	if err != nil {
		return nil, err
	}
	leader := findTeamLeader(members)
	return &TeamDetailsPayload{
		Team:           team,
		LeaderMemberID: leaderMemberKey(leader),
		Leader:         leader,
		Members:        members,
		Tasks:          teamTaskPayloads(tasks),
		Events:         teamEventPayloads(events),
		WorkItems:      teamWorkItemPayloads(workItems),
	}, nil
}

func (s *teamService) ListTeamTasks(userID, teamID, beforeID, limit int) (*TeamTasksHistoryPayload, error) {
	if _, err := s.requireOwnedTeam(userID, teamID); err != nil {
		return nil, err
	}
	limit = normalizeTeamHistoryLimit(limit, 20, 100)
	tasks, err := s.repo.ListTasksBeforeID(teamID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	payload := teamTaskPayloads(tasks)
	return &TeamTasksHistoryPayload{
		Tasks:        payload,
		HasMore:      hasMore,
		NextBeforeID: nextTeamTaskBeforeID(payload),
	}, nil
}

func (s *teamService) ListTeamEvents(userID, teamID, beforeID, limit int) (*TeamEventsHistoryPayload, error) {
	if _, err := s.requireOwnedTeam(userID, teamID); err != nil {
		return nil, err
	}
	limit = normalizeTeamHistoryLimit(limit, 50, 200)
	events, err := s.repo.ListEventsBeforeID(teamID, beforeID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	var nextBeforeID *int
	if len(events) > 0 {
		value := events[len(events)-1].ID
		nextBeforeID = &value
	}
	payload := teamEventPayloads(events)
	return &TeamEventsHistoryPayload{
		Events:       payload,
		HasMore:      hasMore,
		NextBeforeID: nextBeforeID,
	}, nil
}

func (s *teamService) ListWorkspaceFiles(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspaceListPayload, error) {
	cleanPath, err := cleanTeamWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	team, root, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, cleanPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Team workspace path not found")
		}
		return nil, fmt.Errorf("failed to inspect Team workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Team workspace path is not a folder")
	}
	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("failed to list Team workspace: %w", err)
	}
	entries := make([]TeamWorkspaceFileEntry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := dirEntry.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to inspect Team workspace entry %q: %w", dirEntry.Name(), err)
		}
		entries = append(entries, teamWorkspaceFileEntryFromInfo(cleanPath, info))
	}
	sortTeamWorkspaceEntries(entries)
	return &TeamWorkspaceListPayload{
		Path:    cleanPath,
		Root:    teamWorkspaceDisplayRoot(team, root),
		Entries: entries,
	}, nil
}

func (s *teamService) PreviewWorkspaceFile(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspacePreviewPayload, error) {
	cleanPath, err := cleanTeamWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	if cleanPath == "" || !isPreviewableWorkspaceFile(cleanPath) {
		return nil, fmt.Errorf("only md, txt, and json files can be previewed")
	}
	_, _, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, cleanPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Team workspace file not found")
		}
		return nil, fmt.Errorf("failed to inspect Team workspace file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("Team workspace entry is a folder")
	}
	if info.Size() > 1048576 {
		return nil, fmt.Errorf("Team workspace file is too large to preview")
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("failed to preview Team workspace file: %w", err)
	}
	return &TeamWorkspacePreviewPayload{
		Path:    cleanPath,
		Name:    posixpath.Base(cleanPath),
		Content: string(raw),
	}, nil
}

func (s *teamService) DownloadWorkspaceFile(ctx context.Context, userID, teamID int, relPath string) (*TeamWorkspaceDownloadPayload, error) {
	cleanPath, err := cleanTeamWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	if cleanPath == "" {
		return nil, fmt.Errorf("file path is required")
	}
	_, _, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, cleanPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Team workspace entry not found")
		}
		return nil, fmt.Errorf("failed to inspect Team workspace entry: %w", err)
	}
	if info.IsDir() {
		data, err := zipTeamWorkspaceDirectory(ctx, target)
		if err != nil {
			return nil, err
		}
		return &TeamWorkspaceDownloadPayload{
			Path:        cleanPath,
			Name:        posixpath.Base(cleanPath) + ".zip",
			ContentType: "application/zip",
			Data:        data,
		}, nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("failed to download Team workspace file: %w", err)
	}
	return &TeamWorkspaceDownloadPayload{
		Path:        cleanPath,
		Name:        posixpath.Base(cleanPath),
		ContentType: "application/octet-stream",
		Data:        data,
	}, nil
}

func (s *teamService) CreateWorkspaceFolder(ctx context.Context, userID, teamID int, req TeamWorkspaceFolderRequest) error {
	parent, err := cleanTeamWorkspacePath(req.Path)
	if err != nil {
		return err
	}
	name, err := cleanWorkspaceEntryName(req.Name)
	if err != nil {
		return err
	}
	_, _, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, joinTeamWorkspacePath(parent, name))
	if err != nil {
		return err
	}
	if err := ensureTeamWorkspaceDirectory(target); err != nil {
		return fmt.Errorf("failed to create Team workspace folder: %w", err)
	}
	return nil
}

func (s *teamService) RenameWorkspaceEntry(ctx context.Context, userID, teamID int, req TeamWorkspaceRenameRequest) error {
	cleanPath, err := cleanTeamWorkspacePath(req.Path)
	if err != nil {
		return err
	}
	if cleanPath == "" {
		return fmt.Errorf("path is required")
	}
	newName, err := cleanWorkspaceEntryName(req.NewName)
	if err != nil {
		return err
	}
	parent := posixpath.Dir(cleanPath)
	if parent == "." {
		parent = ""
	}
	_, _, source, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, cleanPath)
	if err != nil {
		return err
	}
	_, _, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, joinTeamWorkspacePath(parent, newName))
	if err != nil {
		return err
	}
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Team workspace entry not found")
		}
		return fmt.Errorf("failed to inspect Team workspace entry: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("target Team workspace entry already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect target Team workspace entry: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("failed to rename Team workspace entry: %w", err)
	}
	return nil
}

func (s *teamService) DeleteWorkspaceEntry(ctx context.Context, userID, teamID int, relPath string) error {
	cleanPath, err := cleanTeamWorkspacePath(relPath)
	if err != nil {
		return err
	}
	if cleanPath == "" {
		return fmt.Errorf("cannot delete workspace root")
	}
	_, _, target, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, cleanPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Team workspace entry not found")
		}
		return fmt.Errorf("failed to inspect Team workspace entry: %w", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to delete Team workspace entry: %w", err)
	}
	return nil
}

func (s *teamService) UploadWorkspaceFiles(ctx context.Context, userID, teamID int, targetPath string, files []*multipart.FileHeader, relativePaths []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no files uploaded")
	}
	basePath, err := cleanTeamWorkspacePath(targetPath)
	if err != nil {
		return err
	}
	for index, fileHeader := range files {
		if fileHeader == nil {
			continue
		}
		uploadName := fileHeader.Filename
		if index < len(relativePaths) && strings.TrimSpace(relativePaths[index]) != "" {
			uploadName = relativePaths[index]
		}
		uploadPath, err := cleanTeamWorkspacePath(uploadName)
		if err != nil {
			return err
		}
		if uploadPath == "" {
			return fmt.Errorf("uploaded file name is required")
		}
		_, _, destination, err := s.resolveTeamWorkspacePath(ctx, userID, teamID, joinTeamWorkspacePath(basePath, uploadPath))
		if err != nil {
			return err
		}
		file, err := fileHeader.Open()
		if err != nil {
			return fmt.Errorf("failed to open uploaded file: %w", err)
		}
		if err := ensureTeamWorkspaceDirectory(filepath.Dir(destination)); err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to create Team workspace upload folder: %w", err)
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0664)
		if err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to create Team workspace upload file: %w", err)
		}
		_, copyErr := io.Copy(out, file)
		closeErr := out.Close()
		_ = file.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to upload Team workspace file: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close Team workspace upload file: %w", closeErr)
		}
		chownTeamWorkspacePath(destination)
	}
	return nil
}

func (s *teamService) DispatchTask(userID, teamID int, req DispatchTeamTaskRequest) (*TeamTaskPayload, error) {
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return nil, err
	}
	memberKey := strings.TrimSpace(req.TargetMemberID)
	if memberKey == "" {
		members, err := s.repo.ListMembersByTeamID(teamID)
		if err != nil {
			return nil, err
		}
		memberKey = leaderMemberKey(findTeamLeader(activeTeamMembers(members)))
	}
	if memberKey == "" {
		return nil, fmt.Errorf("target member id is required")
	}
	if req.Payload == nil {
		return nil, fmt.Errorf("task payload is required")
	}
	if strings.TrimSpace(eventString(req.Payload, "responseLocale", "response_locale")) == "" {
		prompt := eventString(req.Payload, "prompt", "goal", "instruction", "instructions")
		req.Payload["responseLocale"] = inferTeamResponseLocale(prompt)
	}
	if _, exists := req.Payload["origin"]; !exists {
		req.Payload["origin"] = "user_query"
	}
	if _, exists := req.Payload["anchorEligible"]; !exists {
		req.Payload["anchorEligible"] = true
	}
	if strings.TrimSpace(eventString(req.Payload, "intent")) == initialLeaderTaskIntent {
		if err := s.enrichBootstrapTaskPayload(userID, team, req.Payload); err != nil {
			return nil, err
		}
	}
	member, err := s.repo.GetMemberByTeamKey(teamID, memberKey)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, fmt.Errorf("team member not found")
	}

	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		messageID = fmt.Sprintf("team-%d-task-%d", teamID, time.Now().UTC().UnixNano())
	}
	existing, err := s.repo.GetTaskByMessageID(teamID, messageID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if existing.TargetMemberID != member.ID {
			return nil, fmt.Errorf("team task message id already exists")
		}
		if existing.Status != models.TeamTaskStatusPending || existing.RedisStreamID != nil {
			return teamTaskPayload(*existing)
		}
	} else {
		payloadJSON, err := marshalJSON(req.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode task payload: %w", err)
		}
		now := time.Now().UTC()
		existing = &models.TeamTask{
			TeamID:         teamID,
			TargetMemberID: member.ID,
			CreatedBy:      &userID,
			MessageID:      messageID,
			Status:         models.TeamTaskStatusPending,
			WorkflowState:  teamWorkflowStatePlanning,
			PlanVersion:    0,
			LedgerVersion:  0,
			PayloadJSON:    payloadJSON,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.repo.CreateTask(existing); err != nil {
			return nil, err
		}
	}
	task := existing
	if err := s.prepareTeamTaskWorkspace(userID, team.ID, task.ID); err != nil {
		return nil, err
	}

	taskPayload := map[string]interface{}{}
	if strings.TrimSpace(task.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(task.PayloadJSON), &taskPayload); err != nil {
			return nil, fmt.Errorf("failed to decode task payload: %w", err)
		}
	}
	s.enrichTaskWorkspaceContract(userID, team, task, taskPayload)
	enrichedPayloadJSON, err := marshalJSON(taskPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode enriched task payload: %w", err)
	}
	if enrichedPayloadJSON != task.PayloadJSON {
		task.PayloadJSON = enrichedPayloadJSON
		task.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateTask(task); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(eventString(taskPayload, "intent")) == initialLeaderTaskIntent && backendGeneratedBootstrapEnabled() {
		return s.completeInitialLeaderTaskFromSnapshot(userID, team, task, member, taskPayload)
	}

	bus, err := s.redisBusForTeam(context.Background(), team)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	memberInstance, _ := s.teamMemberInstance(member)
	memberContext := buildTeamMemberTaskContext(member, memberInstance)
	communicationMode := normalizedTeamCommunicationMode(team.CommunicationMode)
	memberContext["communicationMode"] = communicationMode
	memberContext["systemPrompt"] = appendTeamCollaborationGuidance(memberContext["systemPrompt"], communicationMode)
	memberContext["systemPrompt"] = appendTeamWorkspaceGuidance(memberContext["systemPrompt"])
	envelope := buildTeamTaskEnvelope(teamID, member.MemberKey, task, messageID, taskPayload, memberContext, now)
	envelopeJSON, err := marshalJSON(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to encode task envelope: %w", err)
	}
	streamID, err := bus.XAdd(context.Background(), teamInboxKey(team.ID, member.MemberKey), map[string]string{
		"payload":    envelopeJSON,
		"team_id":    strconv.Itoa(team.ID),
		"task_id":    strconv.Itoa(task.ID),
		"message_id": messageID,
		"member_id":  member.MemberKey,
	})
	if err != nil {
		return nil, err
	}
	task.Status = models.TeamTaskStatusDispatched
	task.RedisStreamID = &streamID
	task.DispatchedAt = &now
	task.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateTask(task); err != nil {
		return nil, err
	}
	return teamTaskPayload(*task)
}

func (s *teamService) enrichBootstrapTaskPayload(userID int, team *models.Team, payload map[string]interface{}) error {
	if s == nil || team == nil || payload == nil {
		return nil
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	activeMembers := activeTeamMembers(members)
	rosterJSON, err := buildTeamRosterConfigFromMembersWithSharedDir(team, activeMembers, team.SharedMountPath)
	if err != nil {
		return err
	}
	roster := map[string]interface{}{}
	_ = json.Unmarshal([]byte(rosterJSON), &roster)
	physicalSharedDir := s.teamRuntimeSharedPathFor(userID, team.ID)
	workspaceContract := map[string]interface{}{
		"configPath":              teamConfigMountPath,
		"configEnv":               "CLAWMANAGER_TEAM_CONFIG_PATH",
		"sharedDir":               team.SharedMountPath,
		"sharedDirEnv":            "CLAWMANAGER_TEAM_SHARED_DIR",
		"physicalSharedDir":       physicalSharedDir,
		"sharedConfigPath":        "/team/" + teamConfigFileName,
		"fallbackConfigPaths":     []string{"/team/" + teamConfigFileName},
		"writeRoot":               "$CLAWMANAGER_TEAM_SHARED_DIR",
		"canonicalReportPrefix":   "/team",
		"validReportPathPattern":  "/team/<relative-path>",
		"invalidConfigPaths":      []string{"/team/members"},
		"invalidArtifactPrefixes": []string{"team/"},
	}
	memberSnapshots := make([]map[string]interface{}, 0, len(activeMembers))
	for _, member := range activeMembers {
		memberSnapshots = append(memberSnapshots, map[string]interface{}{
			"memberId":      member.MemberKey,
			"displayName":   member.DisplayName,
			"role":          member.Role,
			"runtimeType":   member.RuntimeType,
			"instanceMode":  member.InstanceMode,
			"description":   derefTeamString(member.Description),
			"status":        member.Status,
			"availability":  member.Availability,
			"progress":      member.Progress,
			"runtimeStatus": derefTeamString(member.RuntimeStatus),
			"runtimeTaskId": derefTeamString(member.RuntimeTaskID),
			"lastSummary":   derefTeamString(member.LastSummary),
			"isLeader":      isTeamLeaderRole(member.Role),
		})
	}
	payload["workspaceContract"] = workspaceContract
	payload["teamConfigJson"] = rosterJSON
	payload["bootstrapSnapshot"] = map[string]interface{}{
		"teamId":            team.ID,
		"teamName":          team.Name,
		"communicationMode": normalizedTeamCommunicationMode(team.CommunicationMode),
		"sharedDir":         team.SharedMountPath,
		"configPath":        teamConfigMountPath,
		"sharedConfigPath":  "/team/" + teamConfigFileName,
		"teamConfigJson":    rosterJSON,
		"roster":            roster,
		"members":           memberSnapshots,
	}
	return nil
}

func backendGeneratedBootstrapEnabled() bool {
	raw := strings.TrimSpace(os.Getenv("TEAM_BOOTSTRAP_BACKEND_GENERATED"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_BACKEND_BOOTSTRAP"))
	}
	if raw == "" {
		return true
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return true
	}
}

func (s *teamService) completeInitialLeaderTaskFromSnapshot(userID int, team *models.Team, task *models.TeamTask, leader *models.TeamMember, taskPayload map[string]interface{}) (*TeamTaskPayload, error) {
	if s == nil || team == nil || task == nil || leader == nil {
		return nil, fmt.Errorf("bootstrap completion requires team, task and leader")
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return nil, err
	}
	activeMembers := activeTeamMembers(members)
	now := time.Now().UTC()
	taskRef := fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
	reportRel := filepath.ToSlash(filepath.Join("results", taskRef, "team-introduction.md"))
	reportPath := filepath.Join(filepath.Clean(s.teamRuntimeSharedPathFor(userID, team.ID)), filepath.FromSlash(reportRel))
	if err := ensureTeamWorkspaceDirectory(filepath.Dir(reportPath)); err != nil {
		return nil, err
	}
	resultMarkdown := buildBackendBootstrapReport(team, activeMembers, taskRef, taskPayload)
	if err := os.WriteFile(reportPath, []byte(resultMarkdown), 0o664); err != nil {
		return nil, fmt.Errorf("failed to write bootstrap report: %w", err)
	}
	_ = os.Chmod(reportPath, 0o664)
	chownTeamWorkspacePath(reportPath)
	artifactRef := "/team/" + reportRel
	summary := fmt.Sprintf("Team %s 启动快照完成，已生成成员与协作机制介绍。", team.Name)
	completionID := fmt.Sprintf("clawmanager-bootstrap:%s", taskRef)
	eventID := fmt.Sprintf("%s:completed", completionID)
	eventPayload := map[string]interface{}{
		"v":                  2,
		"protocolVersion":    2,
		"event":              "task_completed",
		"type":               "task_completed",
		"eventKind":          "leader_synthesis",
		"intent":             initialLeaderTaskIntent,
		"origin":             "system_bootstrap",
		"source":             "clawmanager",
		"completionSource":   "clawmanager_backend",
		"completionId":       completionID,
		"explicitCompletion": true,
		"rootTaskTerminal":   true,
		"teamId":             strconv.Itoa(team.ID),
		"taskId":             taskRef,
		"rootTaskId":         taskRef,
		"messageId":          task.MessageID,
		"rootMessageId":      task.MessageID,
		"from":               leader.MemberKey,
		"memberId":           leader.MemberKey,
		"status":             models.TeamTaskStatusSucceeded,
		"runtimeStatus":      models.TeamTaskStatusSucceeded,
		"availability":       models.TeamMemberAvailabilityIdle,
		"summary":            summary,
		"result":             resultMarkdown,
		"resultMarkdown":     resultMarkdown,
		"artifactRefs":       []string{artifactRef},
		"visibleToChat":      true,
		"backendGenerated":   true,
	}
	payloadJSON, err := marshalOptionalJSON(eventPayload)
	if err != nil {
		return nil, err
	}
	task.Status = models.TeamTaskStatusSucceeded
	task.StartedAt = &now
	task.FinishedAt = &now
	task.ResultJSON = payloadJSON
	task.ErrorMessage = nil
	task.UpdatedAt = now
	if err := s.repo.UpdateTask(task); err != nil {
		return nil, err
	}
	leader.Status = models.TeamMemberStatusIdle
	leader.CurrentTaskID = nil
	leader.Progress = 100
	leader.Availability = models.TeamMemberAvailabilityIdle
	runtimeStatus := models.TeamTaskStatusSucceeded
	leader.RuntimeStatus = &runtimeStatus
	leader.RuntimeTaskID = &task.MessageID
	runtimeIntent := initialLeaderTaskIntent
	leader.RuntimeIntent = &runtimeIntent
	leader.BlockedReason = nil
	leader.LastSummary = &summary
	leader.LastSeenAt = &now
	leader.UpdatedAt = now
	if err := s.repo.UpdateMember(leader); err != nil {
		return nil, err
	}
	messageID := task.MessageID
	event := &models.TeamEvent{
		EventID:      &eventID,
		CompletionID: &completionID,
		TeamID:       team.ID,
		MemberID:     &leader.ID,
		TaskID:       &task.ID,
		MessageID:    &messageID,
		EventType:    "task_completed",
		PayloadJSON:  payloadJSON,
		OccurredAt:   &now,
		CreatedAt:    now,
	}
	if err := s.repo.CreateEvent(event); err != nil && !errors.Is(err, repository.ErrDuplicateTeamEvent) {
		return nil, err
	}
	return teamTaskPayload(*task)
}

func buildBackendBootstrapReport(team *models.Team, members []models.TeamMember, taskRef string, taskPayload map[string]interface{}) string {
	var b strings.Builder
	mode := normalizedTeamCommunicationMode(team.CommunicationMode)
	b.WriteString("# Team 启动介绍\n\n")
	b.WriteString(fmt.Sprintf("- Team：%s（ID %d）\n", team.Name, team.ID))
	b.WriteString(fmt.Sprintf("- 任务：%s\n", taskRef))
	b.WriteString(fmt.Sprintf("- 协作模式：%s\n", mode))
	b.WriteString(fmt.Sprintf("- 共享目录：%s\n\n", team.SharedMountPath))
	b.WriteString("## 成员\n\n")
	b.WriteString("| 成员 ID | 显示名称 | 角色 | Runtime | Mode | 状态 | 职责 |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, member := range members {
		b.WriteString(fmt.Sprintf(
			"| %s | %s | %s | %s | %s | %s/%s | %s |\n",
			markdownTableCell(member.MemberKey),
			markdownTableCell(member.DisplayName),
			markdownTableCell(member.Role),
			markdownTableCell(member.RuntimeType),
			markdownTableCell(member.InstanceMode),
			markdownTableCell(member.Status),
			markdownTableCell(member.Availability),
			markdownTableCell(derefTeamString(member.Description)),
		))
	}
	b.WriteString("\n## 协作机制\n\n")
	b.WriteString("- Leader 负责拆解任务、派发成员、整合结果，并最终关闭根任务。\n")
	b.WriteString("- 成员结果应回到 Leader，由 Leader 进行最终汇总。\n")
	b.WriteString("- Redis Streams 用于任务分发、成员回报、过程事件和完成事件同步。\n")
	b.WriteString("- NFS 共享目录用于耐久化产物、任务快照和可恢复状态，不作为实时状态唯一事实源。\n\n")
	b.WriteString("## 共享工作区\n\n")
	b.WriteString("- 规范路径前缀：`/team/`\n")
	if contract, ok := taskPayload["workspaceContract"].(map[string]interface{}); ok {
		if sharedDir := eventString(contract, "sharedDir"); sharedDir != "" {
			b.WriteString(fmt.Sprintf("- 容器共享目录：`%s`\n", sharedDir))
		}
		if physicalDir := eventString(contract, "physicalSharedDir"); physicalDir != "" {
			b.WriteString(fmt.Sprintf("- 物理共享目录：`%s`\n", physicalDir))
		}
	}
	b.WriteString("- 成员产物建议写入 `/team/artifacts/<rootTaskId>/members/<memberId>/`。\n")
	b.WriteString("- 根任务最终报告建议写入 `/team/results/<rootTaskId>/`。\n\n")
	b.WriteString("## 团队可用方法\n\n")
	b.WriteString("- `team_send`：Leader 向成员派发任务。\n")
	b.WriteString("- `team_update_progress`：记录业务计划、阶段进度、长任务状态和检查反馈。\n")
	b.WriteString("- `team_status`：查询成员和任务状态。\n")
	b.WriteString("- `team_complete_task`：仅用于成员提交分配结果或 Leader 关闭根任务。\n")
	b.WriteString("- `team_artifact_write/read/list/mkdir`：在当前 Team 共享目录内安全读写产物，自动限制路径并使用协作权限。\n")
	return b.String()
}

func markdownTableCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	value = strings.ReplaceAll(value, "\r\n", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

func (s *teamService) prepareTeamTaskWorkspace(userID, teamID, taskID int) error {
	if s == nil || taskID <= 0 {
		return nil
	}
	root := filepath.Clean(s.teamRuntimeSharedPathFor(userID, teamID))
	taskRef := fmt.Sprintf("team-%d-task-%d", teamID, taskID)
	standardDirs := []string{
		"",
		"artifacts",
		filepath.ToSlash(filepath.Join("artifacts", taskRef)),
		filepath.ToSlash(filepath.Join("artifacts", taskRef, "members")),
		"results",
		filepath.ToSlash(filepath.Join("results", taskRef)),
		filepath.ToSlash(filepath.Join("results", taskRef, "members")),
		"tasks",
		filepath.ToSlash(filepath.Join("tasks", taskRef)),
		"inbox",
		"status",
		filepath.ToSlash(filepath.Join("status", taskRef)),
		"tmp",
	}
	if s.repo != nil {
		members, err := s.repo.ListMembersByTeamID(teamID)
		if err != nil {
			return err
		}
		for _, member := range activeTeamMembers(members) {
			memberKey := normalizeTeamMemberRouteKey(member.MemberKey)
			if memberKey == "" {
				continue
			}
			standardDirs = append(standardDirs,
				filepath.ToSlash(filepath.Join("artifacts", taskRef, "members", memberKey)),
				filepath.ToSlash(filepath.Join("results", taskRef, "members", memberKey)),
				filepath.ToSlash(filepath.Join("tmp", memberKey)),
			)
		}
	}
	for _, rel := range standardDirs {
		target := root
		if rel != "" {
			target = filepath.Join(root, filepath.FromSlash(rel))
		}
		if err := ensureTeamWorkspaceDirectory(target); err != nil {
			return err
		}
	}
	return nil
}

func (s *teamService) enrichTaskWorkspaceContract(userID int, team *models.Team, task *models.TeamTask, payload map[string]interface{}) {
	if s == nil || team == nil || task == nil || payload == nil {
		return
	}
	taskRef := fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
	physicalSharedDir := s.teamRuntimeSharedPathFor(userID, team.ID)
	contract := map[string]interface{}{
		"configPath":                 teamConfigMountPath,
		"configEnv":                  "CLAWMANAGER_TEAM_CONFIG_PATH",
		"sharedDir":                  team.SharedMountPath,
		"sharedDirEnv":               "CLAWMANAGER_TEAM_SHARED_DIR",
		"physicalSharedDir":          physicalSharedDir,
		"sharedConfigPath":           "/team/" + teamConfigFileName,
		"fallbackConfigPaths":        []string{"/team/" + teamConfigFileName},
		"writeRoot":                  "$CLAWMANAGER_TEAM_SHARED_DIR",
		"canonicalReportPrefix":      "/team",
		"validReportPathPattern":     "/team/<relative-path>",
		"invalidConfigPaths":         []string{"/team/members"},
		"invalidArtifactPrefixes":    []string{"team/"},
		"taskRef":                    taskRef,
		"artifactRoot":               "/team/artifacts/" + taskRef,
		"memberArtifactRoot":         "/team/artifacts/" + taskRef + "/members/${memberId}",
		"memberArtifactPhysicalRoot": filepath.ToSlash(filepath.Join(physicalSharedDir, "artifacts", taskRef, "members", "${memberId}")),
		"memberResultRoot":           "/team/results/" + taskRef + "/members/${memberId}",
		"leaderResultRoot":           "/team/results/" + taskRef,
		"statusRoot":                 "/team/status/" + taskRef,
		"tmpRoot":                    "/team/tmp/${memberId}",
		"statusFilesAreAdvisory":     true,
		"stateAuthority":             "clawmanager_event_ledger",
		"rules": []string{
			"Use /team/artifacts/<rootTaskId>/members/<memberId>/<workId>/ for member deliverables.",
			"Use /team/results/<rootTaskId>/members/<memberId>/ for member result summaries.",
			"Only the Leader writes the root final synthesis under /team/results/<rootTaskId>/.",
			"Shared status JSON files are compatibility snapshots; do not treat them as the task truth source.",
		},
	}
	if existing, ok := payload["workspaceContract"].(map[string]interface{}); ok {
		for key, value := range contract {
			if _, exists := existing[key]; !exists {
				existing[key] = value
			}
		}
		return
	}
	payload["workspaceContract"] = contract
}

func (s *teamService) teamMemberInstance(member *models.TeamMember) (*models.Instance, error) {
	if s == nil || s.instanceService == nil || member == nil || member.InstanceID == nil || *member.InstanceID <= 0 {
		return nil, nil
	}
	return s.instanceService.GetByID(*member.InstanceID)
}

func buildTeamMemberTaskContext(member *models.TeamMember, instance *models.Instance) map[string]string {
	if member == nil {
		return map[string]string{}
	}
	displayName := strings.TrimSpace(member.DisplayName)
	if displayName == "" {
		displayName = member.MemberKey
	}
	role := strings.TrimSpace(member.Role)
	if role == "" {
		role = "member"
	}
	description := derefTeamString(member.Description)
	personaSystemPrompt, personaDescription := teamMemberPersonaFromInstance(instance)
	if description == "" {
		description = personaDescription
	}
	systemPrompt := buildTeamMemberSystemPrompt(displayName, member.MemberKey, role, description, personaSystemPrompt)
	return map[string]string{
		"memberId":     member.MemberKey,
		"displayName":  displayName,
		"role":         role,
		"description":  description,
		"systemPrompt": systemPrompt,
	}
}

func buildPlannedTeamMemberTaskContext(member plannedTeamMember) map[string]string {
	displayName := strings.TrimSpace(member.DisplayName)
	if displayName == "" {
		displayName = member.MemberKey
	}
	role := strings.TrimSpace(member.Role)
	if role == "" {
		role = "member"
	}
	description := strings.TrimSpace(derefTeamString(member.Request.Description))
	personaSystemPrompt, personaDescription := teamMemberPersonaFromEnv(member.Request.EnvironmentOverrides)
	if description == "" {
		description = personaDescription
	}
	systemPrompt := buildTeamMemberSystemPrompt(displayName, member.MemberKey, role, description, personaSystemPrompt)
	return map[string]string{
		"memberId":     member.MemberKey,
		"displayName":  displayName,
		"role":         role,
		"description":  description,
		"systemPrompt": systemPrompt,
	}
}

func buildTeamMemberSystemPrompt(displayName, memberID, role, description, personaSystemPrompt string) string {
	systemPrompt := strings.TrimSpace(fmt.Sprintf(
		"You are Team member %q (member_id=%s, role=%s). Follow this role for this task. Role responsibilities: %s",
		displayName,
		memberID,
		role,
		description,
	))
	if strings.TrimSpace(description) == "" {
		systemPrompt = fmt.Sprintf(
			"You are Team member %q (member_id=%s, role=%s). Follow this role for this task.",
			displayName,
			memberID,
			role,
		)
	}
	if personaSystemPrompt != "" {
		systemPrompt = personaSystemPrompt + "\n\n" + systemPrompt
	}
	return systemPrompt
}

func buildTeamMemberSoulMarkdown(member plannedTeamMember, communicationMode string) string {
	context := buildPlannedTeamMemberTaskContext(member)
	lines := []string{
		fmt.Sprintf("# %s", strings.TrimSpace(context["displayName"])),
		"",
		"You are running as a ClawManager Team member. Treat this file as persistent identity and role guidance.",
		"",
		"## Team Identity",
		fmt.Sprintf("- Member ID: %s", context["memberId"]),
		fmt.Sprintf("- Display name: %s", context["displayName"]),
		fmt.Sprintf("- Role: %s", context["role"]),
		fmt.Sprintf("- Effective role: %s", effectiveTeamMemberRole(member)),
	}
	if profileKey := strings.TrimSpace(member.ProfileKey); profileKey != "" {
		lines = append(lines, fmt.Sprintf("- Profile key: %s", profileKey))
	}
	if profileName := strings.TrimSpace(member.ProfileName); profileName != "" {
		lines = append(lines, fmt.Sprintf("- Profile name: %s", profileName))
	}
	if description := strings.TrimSpace(context["description"]); description != "" {
		lines = append(lines, fmt.Sprintf("- Responsibilities: %s", description))
	}
	lines = append(lines,
		"",
		"## Role Instructions",
		strings.TrimSpace(context["systemPrompt"]),
		"",
		"## Collaboration Rules",
		teamCollaborationGuidance(communicationMode),
		"- Only handle tasks addressed to your Team member inbox.",
		"- If team.json contains effectiveRole/profileName, use those fields when describing your role instead of falling back to a generic roster role.",
		"- Use the exact CLAWMANAGER_TEAM_SHARED_DIR value for shared context, durable notes, and handoff artifacts. When using shell commands, always create files under \"$CLAWMANAGER_TEAM_SHARED_DIR/<relative-path>\".",
		"- Never create or report a relative team/... folder. The path team/... is invalid because ClawManager file browsing only resolves shared artifacts through the Team shared directory.",
		"- Report shared artifact links as /team/<relative-path>. /team is the canonical ClawManager UI path even when a Lite runtime uses a different physical directory.",
		"- Report progress, blockers, verification evidence, and final results through the Team channel.",
		"- If asked about your role, answer from this Team Identity and Role Instructions section.",
		"",
	)
	return strings.Join(lines, "\n")
}

func buildTeamMemberAgentsMarkdown(team *models.Team, member plannedTeamMember) string {
	communicationMode := teamCommunicationModeLeaderMediated
	teamID := ""
	if team != nil {
		communicationMode = normalizedTeamCommunicationMode(team.CommunicationMode)
		teamID = strconv.Itoa(team.ID)
	}
	lines := []string{
		"# ClawManager Team Runtime",
		"",
		"This file defines the stable Team runtime contract. Treat SOUL.md as the member-specific identity and role file.",
		"",
		"## Runtime Capabilities",
		"- You are running inside a ClawManager managed OpenClaw/Hermes runtime.",
		"- Use the available runtime tools normally, but coordinate Team work through the ClawManager Team channel.",
		"- Use team_send for assignments, handoffs, clarifying questions, blockers, and final delivery messages.",
		"- Use team_status / progress updates to report work state when available.",
		"- Use team_complete_task only when the assigned task is actually complete and evidence has been reported.",
		"",
		"## Workspace Contract",
		"- CLAWMANAGER_TEAM_SHARED_DIR is the only writable shared artifact directory.",
		"- Create shared artifacts under \"$CLAWMANAGER_TEAM_SHARED_DIR/<relative-path>\".",
		"- Report shared artifact links as /team/<relative-path>.",
		"- Do not report bare filenames or relative team/... paths as final artifact links.",
		"",
		"## Team Identity Source Order",
		"- Prefer SOUL.md for your member identity, role, profile, and collaboration rules.",
		"- Then use CLAWMANAGER_TEAM_CONFIG_JSON / team.json for roster and communication mode.",
		"- Environment variables are compatibility fallbacks, not a reason to ignore SOUL.md.",
		"",
		"## Collaboration Contract",
		teamCollaborationGuidance(communicationMode),
		"- Never invent another member's reply or treat your own assumptions as a peer response.",
		"- Keep role answers consistent with SOUL.md and the effectiveRole/profileName fields in team.json.",
		"",
		"## Current Member",
		fmt.Sprintf("- Team ID: %s", teamID),
		fmt.Sprintf("- Member ID: %s", member.MemberKey),
		fmt.Sprintf("- Display name: %s", member.DisplayName),
		fmt.Sprintf("- Runtime: %s", member.RuntimeType),
		fmt.Sprintf("- Effective role: %s", effectiveTeamMemberRole(member)),
	}
	if member.ProfileKey != "" {
		lines = append(lines, fmt.Sprintf("- Profile key: %s", member.ProfileKey))
	}
	if member.ProfileName != "" {
		lines = append(lines, fmt.Sprintf("- Profile name: %s", member.ProfileName))
	}
	return strings.Join(lines, "\n") + "\n"
}

func plannedTeamMemberDescription(member plannedTeamMember) string {
	if description := strings.TrimSpace(derefTeamString(member.Request.Description)); description != "" {
		return description
	}
	_, description := teamMemberPersonaFromEnv(member.Request.EnvironmentOverrides)
	return strings.TrimSpace(description)
}

func effectiveTeamMemberRole(member plannedTeamMember) string {
	if role := strings.TrimSpace(member.EffectiveRole); role != "" {
		return role
	}
	if role := strings.TrimSpace(member.Role); role != "" {
		return role
	}
	return "member"
}

func (s *teamService) writeLiteTeamMemberIdentityFiles(instance *models.Instance, team *models.Team, member plannedTeamMember, rosterJSON string) error {
	if instance == nil || modeForExistingInstance(instance) != InstanceModeLite {
		return nil
	}
	workspacePath := ""
	if instance.WorkspacePath != nil {
		workspacePath = strings.TrimSpace(*instance.WorkspacePath)
	}
	if workspacePath == "" {
		return fmt.Errorf("failed to write Lite Team identity files: workspace path is empty")
	}
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		return fmt.Errorf("failed to prepare Lite Team identity workspace: %w", err)
	}
	files := map[string]string{
		teamAgentsFileName: buildTeamMemberAgentsMarkdown(team, member),
		teamSoulFileName:   buildTeamMemberSoulMarkdown(member, normalizedTeamCommunicationMode(team.CommunicationMode)),
	}
	if strings.TrimSpace(rosterJSON) != "" {
		files[teamConfigFileName] = rosterJSON
	}
	for name, content := range files {
		target := filepath.Join(workspacePath, name)
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write Lite Team identity file %s: %w", name, err)
		}
		chownTeamWorkspacePath(target)
	}
	if strings.EqualFold(member.RuntimeType, "hermes") {
		hermesDir := filepath.Join(workspacePath, ".hermes")
		if err := os.MkdirAll(hermesDir, 0755); err != nil {
			return fmt.Errorf("failed to prepare Hermes identity directory: %w", err)
		}
		chownTeamWorkspacePath(hermesDir)
		target := filepath.Join(hermesDir, teamSoulFileName)
		if err := os.WriteFile(target, []byte(files[teamSoulFileName]), 0644); err != nil {
			return fmt.Errorf("failed to write Hermes Lite SOUL.md: %w", err)
		}
		chownTeamWorkspacePath(target)
	}
	return nil
}

func appendTeamCollaborationGuidance(systemPrompt, communicationMode string) string {
	guidance := teamCollaborationGuidance(communicationMode)
	if strings.Contains(systemPrompt, guidance) {
		return systemPrompt
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + guidance
}

func appendTeamWorkspaceGuidance(systemPrompt string) string {
	guidance := "Shared workspace contract: write every shared artifact under the exact CLAWMANAGER_TEAM_SHARED_DIR value. When using shell commands, always create files under \"$CLAWMANAGER_TEAM_SHARED_DIR/<relative-path>\". Never list, search, resolve, or scan the parent of CLAWMANAGER_TEAM_SHARED_DIR, and never inspect sibling Team directories. Never create or report a relative team/... directory; team/... is invalid and may not be visible in ClawManager. When reporting an artifact to ClawManager or another member, use the canonical link /team/<relative-path>. The /team prefix is a UI/logical alias and may map to a different physical directory in Lite runtimes."
	if strings.Contains(systemPrompt, guidance) {
		return systemPrompt
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + guidance
}

func teamCollaborationGuidance(communicationMode string) string {
	switch normalizedTeamCommunicationMode(communicationMode) {
	case teamCommunicationModePeerAssisted:
		return "Collaboration mode: peer_assisted / worker-direct. This mode is isolated from leader_mediated flow: the Leader still owns final user-facing synthesis, but members must hand off directly to the named downstream owner when the root task, collaboration plan, or current instruction specifies one. Direct handoff is mandatory, not optional; sending only to the Leader is allowed only when there is no named downstream owner, when blocked, or when final synthesis is explicitly required. Preserve rootTaskId/rootMessageId, artifact paths, requested next action, acceptance criteria, and reply-required status in every peer message. Ask peer questions through the Team channel, then wait for the addressed member's real reply before continuing dependent work; never simulate, invent, or reinterpret another member's answer as if the user said it. Write durable artifacts under CLAWMANAGER_TEAM_SHARED_DIR, report peer outcomes through the Team channel, and let the Leader close the root task only after final synthesis. When receiving a peer request, respond with explicit evidence, artifact paths, blockers, or review findings so the requester can finish its own task."
	case teamCommunicationModeFullMesh:
		return "Collaboration mode: full_mesh. Team members coordinate directly with each other while preserving rootTaskId/rootMessageId context, shared artifacts under CLAWMANAGER_TEAM_SHARED_DIR, and final user-facing synthesis. If a downstream owner is named, hand off to that exact member before completing your own step. Use direct member-to-member messages for parallel research, design, implementation, review, and verification. Wait for real addressed-member replies before continuing dependent work; do not simulate peer answers or label peer messages as user replies. Keep each peer exchange bounded, evidence based, and visible in the Team channel."
	default:
		return "Collaboration mode: leader_mediated. This is a strict hub-and-spoke workflow isolated from worker-direct flow. User root tasks enter through the Leader. If the user names a non-Leader member or role, the Leader must delegate to that exact member with team_send, wait for that member's real result, then synthesize the final answer. If the user gives a broad task without naming one member, the Leader must create a compact plan, decompose work by owner/member_id, send assignments, wait for required member results, verify them, and produce final synthesis. Every delegated assignment must carry a stable workId and assignmentId; reuse that workId for all progress, result, and review messages belonging to the assignment. The Leader may answer directly only for self-contained control-plane or simple tasks that do not require a named worker or multi-member evidence. A dispatch, plan, or handoff is not a final result and must not close the root task. Workers execute only assignments addressed to them, preserve rootTaskId/rootMessageId/workId and artifact paths, and report results or blockers back to the Leader. Workers must not hand off directly to other workers. A worker completion never closes the user root task; only the Leader may finalize it after all required outputs are reconciled."
	}
}

func teamMemberPersonaFromInstance(instance *models.Instance) (string, string) {
	if instance == nil {
		return "", ""
	}
	overrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil || len(overrides) == 0 {
		return "", ""
	}
	return teamMemberPersonaFromEnv(overrides)
}

func teamMemberPersonaFromEnv(overrides map[string]string) (string, string) {
	if len(overrides) == 0 {
		return "", ""
	}
	systemPrompt := strings.TrimSpace(firstNonEmptyEnv(overrides,
		"CLAWMANAGER_AGENT_SYSTEM_PROMPT",
		"CLAWMANAGER_HERMES_SYSTEM_PROMPT",
		"CLAWMANAGER_RUNTIME_SYSTEM_PROMPT",
		"HERMES_SYSTEM_PROMPT",
	))
	description := ""
	for _, key := range []string{
		"CLAWMANAGER_AGENT_PERSONA_JSON",
		"CLAWMANAGER_HERMES_PERSONA_JSON",
		"CLAWMANAGER_RUNTIME_PERSONA_JSON",
	} {
		persona := parseTeamPersonaEnv(overrides[key])
		if persona == nil {
			continue
		}
		if systemPrompt == "" {
			systemPrompt = strings.TrimSpace(persona.SystemPrompt)
		}
		if description == "" {
			description = strings.TrimSpace(persona.Summary)
		}
	}
	for _, key := range []string{
		"CLAWMANAGER_HERMES_AGENTS_JSON",
		"CLAWMANAGER_RUNTIME_AGENTS_JSON",
		"CLAWMANAGER_OPENCLAW_AGENTS_JSON",
	} {
		agents := parseTeamAgentsEnv(overrides[key])
		if agents == nil {
			continue
		}
		if systemPrompt == "" {
			systemPrompt = strings.TrimSpace(agents.SystemPrompt)
		}
		if description == "" {
			description = strings.TrimSpace(agents.Summary)
		}
	}
	return systemPrompt, description
}

type teamPersonaEnv struct {
	ProfileKey   string `json:"profileKey"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	RoleHint     string `json:"roleHint"`
	SystemPrompt string `json:"systemPrompt"`
	Summary      string `json:"summary"`
}

type teamProfileEnv struct {
	ProfileKey  string
	ProfileName string
	RoleHint    string
	Summary     string
}

func teamMemberProfileFromEnv(overrides map[string]string) teamProfileEnv {
	if len(overrides) == 0 {
		return teamProfileEnv{}
	}
	for _, key := range []string{
		"CLAWMANAGER_AGENT_PERSONA_JSON",
		"CLAWMANAGER_HERMES_PERSONA_JSON",
		"CLAWMANAGER_RUNTIME_PERSONA_JSON",
	} {
		persona := parseTeamPersonaEnv(overrides[key])
		if persona == nil {
			continue
		}
		profile := teamProfileEnv{
			ProfileKey:  strings.TrimSpace(persona.ProfileKey),
			ProfileName: strings.TrimSpace(firstNonEmptyString(persona.DisplayName, persona.Name)),
			RoleHint:    strings.TrimSpace(persona.RoleHint),
			Summary:     strings.TrimSpace(persona.Summary),
		}
		if profile.ProfileKey != "" || profile.ProfileName != "" || profile.RoleHint != "" || profile.Summary != "" {
			return profile
		}
	}
	for _, key := range []string{
		"CLAWMANAGER_HERMES_AGENTS_JSON",
		"CLAWMANAGER_RUNTIME_AGENTS_JSON",
		"CLAWMANAGER_OPENCLAW_AGENTS_JSON",
	} {
		agents := parseTeamAgentsEnv(overrides[key])
		if agents == nil {
			continue
		}
		profile := teamProfileEnv{
			ProfileKey:  strings.TrimSpace(agents.ProfileKey),
			ProfileName: strings.TrimSpace(agents.Name),
			RoleHint:    strings.TrimSpace(agents.RoleHint),
			Summary:     strings.TrimSpace(agents.Summary),
		}
		if profile.ProfileKey != "" || profile.ProfileName != "" || profile.RoleHint != "" || profile.Summary != "" {
			return profile
		}
	}
	return teamProfileEnv{}
}

func parseTeamPersonaEnv(raw string) *teamPersonaEnv {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var persona teamPersonaEnv
	if err := json.Unmarshal([]byte(raw), &persona); err != nil {
		return nil
	}
	return &persona
}

type teamAgentsEnv struct {
	Items []struct {
		Content struct {
			Config struct {
				ProfileKey   string `json:"profileKey"`
				Name         string `json:"name"`
				DisplayName  string `json:"displayName"`
				RoleHint     string `json:"roleHint"`
				SystemPrompt string `json:"systemPrompt"`
				Summary      string `json:"summary"`
			} `json:"config"`
		} `json:"content"`
	} `json:"items"`
}

func parseTeamAgentsEnv(raw string) *teamPersonaEnv {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var payload teamAgentsEnv
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	for _, item := range payload.Items {
		systemPrompt := strings.TrimSpace(item.Content.Config.SystemPrompt)
		summary := strings.TrimSpace(item.Content.Config.Summary)
		profileKey := strings.TrimSpace(item.Content.Config.ProfileKey)
		name := strings.TrimSpace(firstNonEmptyString(item.Content.Config.DisplayName, item.Content.Config.Name))
		roleHint := strings.TrimSpace(item.Content.Config.RoleHint)
		if systemPrompt != "" || summary != "" || profileKey != "" || name != "" || roleHint != "" {
			return &teamPersonaEnv{
				ProfileKey:   profileKey,
				Name:         name,
				RoleHint:     roleHint,
				SystemPrompt: systemPrompt,
				Summary:      summary,
			}
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmptyEnv(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func buildTeamRuntimePrompt(rawPrompt string, memberContext map[string]string) string {
	prompt := strings.TrimSpace(rawPrompt)
	if len(memberContext) == 0 {
		return prompt
	}
	systemPrompt := strings.TrimSpace(memberContext["systemPrompt"])
	if systemPrompt == "" {
		return prompt
	}
	if prompt == "" {
		return systemPrompt
	}
	return fmt.Sprintf("%s\n\nUser task:\n%s", systemPrompt, prompt)
}

func (s *teamService) DeleteTeam(userID, teamID int) error {
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return err
	}
	if team.Status == models.TeamStatusDeleted {
		return nil
	}

	now := time.Now().UTC()
	team.Status = models.TeamStatusDeleting
	team.UpdatedAt = now
	if err := s.repo.UpdateTeam(team); err != nil {
		return err
	}

	members, err := s.repo.ListMembersByTeamID(teamID)
	if err != nil {
		return err
	}
	for idx := range members {
		member := members[idx]
		if member.Status == models.TeamMemberStatusDeleted {
			continue
		}
		member.Status = models.TeamMemberStatusDeleting
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(&member)
		if member.InstanceID != nil && *member.InstanceID > 0 {
			if err := s.instanceService.Delete(*member.InstanceID); err != nil {
				fmt.Printf("Warning: failed to delete Team %d member %s instance %d: %v\n", teamID, member.MemberKey, *member.InstanceID, err)
			}
		}
		member.Status = models.TeamMemberStatusDeleted
		member.CurrentTaskID = nil
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(&member)
	}

	ctx := context.Background()
	if strings.TrimSpace(derefTeamString(team.TeamTokenSecretName)) != "" {
		if err := s.secretService.DeleteSecret(ctx, userID, derefTeamString(team.TeamTokenSecretName)); err != nil {
			fmt.Printf("Warning: failed to delete Team %d secret: %v\n", teamID, err)
		}
	}
	if err := s.configMapService.DeleteConfigMap(ctx, userID, s.teamConfigMapName(teamID)); err != nil {
		fmt.Printf("Warning: failed to delete Team %d configmap: %v\n", teamID, err)
	}
	if err := s.pvcService.DeleteTeamSharedPVC(ctx, userID, teamID); err != nil {
		fmt.Printf("Warning: failed to delete Team %d shared PVC: %v\n", teamID, err)
	}

	team.Name = deletedTeamName(team.Name, team.ID)
	team.Status = models.TeamStatusDeleted
	team.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateTeam(team)
}

func (s *teamService) DeleteMember(userID, teamID int, memberID string) error {
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return err
	}
	member, err := s.findTeamMemberForDelete(teamID, memberID)
	if err != nil {
		return err
	}
	if member == nil {
		return fmt.Errorf("team member not found")
	}
	if member.UserID != userID || member.TeamID != teamID {
		return fmt.Errorf("access denied")
	}
	if member.Status == models.TeamMemberStatusDeleted {
		return nil
	}
	if isTeamLeaderRole(member.Role) {
		return fmt.Errorf("team leader cannot be deleted before assigning a new leader")
	}

	now := time.Now().UTC()
	member.Status = models.TeamMemberStatusDeleting
	member.UpdatedAt = now
	if err := s.repo.UpdateMember(member); err != nil {
		return err
	}
	if member.InstanceID != nil && *member.InstanceID > 0 {
		if err := s.instanceService.Delete(*member.InstanceID); err != nil {
			return err
		}
	}
	member.Status = models.TeamMemberStatusDeleted
	member.CurrentTaskID = nil
	member.Progress = 0
	member.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateMember(member); err != nil {
		return err
	}
	return s.refreshTeamRosterConfig(userID, team)
}

func (s *teamService) findTeamMemberForDelete(teamID int, memberID string) (*models.TeamMember, error) {
	value := strings.TrimSpace(memberID)
	if value == "" {
		return nil, fmt.Errorf("team member id is required")
	}
	if numericID, err := strconv.Atoi(value); err == nil && numericID > 0 {
		member, err := s.repo.GetMemberByID(numericID)
		if err != nil || member == nil || member.TeamID != teamID {
			return member, err
		}
		return member, nil
	}
	return s.repo.GetMemberByTeamKey(teamID, value)
}

func (s *teamService) refreshTeamRosterConfig(userID int, team *models.Team) error {
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	rosterJSON, err := buildTeamRosterConfigFromMembers(team, activeTeamMembers(members))
	if err != nil {
		return err
	}
	configData := map[string]string{
		teamConfigFileName: rosterJSON,
	}
	for _, member := range activeTeamMembers(members) {
		if member.RuntimeType != "hermes" {
			continue
		}
		configData[teamMemberSoulConfigKey(member.MemberKey)] = buildTeamMemberSoulMarkdown(plannedTeamMember{
			Request: CreateTeamMemberRequest{
				Description: member.Description,
			},
			MemberKey:    member.MemberKey,
			DisplayName:  member.DisplayName,
			Role:         member.Role,
			RuntimeType:  member.RuntimeType,
			InstanceMode: member.InstanceMode,
			IsLeader:     isTeamLeaderRole(member.Role),
		}, normalizedTeamCommunicationMode(team.CommunicationMode))
	}
	if err := s.configMapService.UpsertConfigMap(context.Background(), userID, s.teamConfigMapName(team.ID), configData, map[string]string{
		"app":        "clawreef",
		"managed-by": "clawreef",
		"team-id":    strconv.Itoa(team.ID),
	}); err != nil {
		return err
	}
	return s.writeSharedTeamRosterConfig(userID, team, rosterJSON)
}

func (s *teamService) requireOwnedTeam(userID, teamID int) (*models.Team, error) {
	team, err := s.repo.GetTeamByID(teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, fmt.Errorf("team not found")
	}
	if team.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}
	return team, nil
}

func (s *teamService) ensureConsumer(ctx context.Context, teamID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if _, exists := s.consumers[teamID]; exists {
		return
	}
	s.consumers[teamID] = struct{}{}
	s.wg.Add(1)
	go s.consumeTeamEvents(ctx, teamID)
}

func (s *teamService) consumeTeamEvents(ctx context.Context, teamID int) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.consumers, teamID)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		team, err := s.repo.GetTeamByID(teamID)
		if err != nil || team == nil {
			time.Sleep(5 * time.Second)
			continue
		}
		bus, err := s.redisBusForTeam(ctx, team)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		lastID := strings.TrimSpace(team.RedisEventsLastID)
		if lastID == "" {
			lastID = "0-0"
		}
		messages, err := bus.XRead(ctx, teamEventsKey(teamID), lastID, 5*time.Second)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for _, message := range messages {
			if err := s.projectTeamEvent(team, bus, message); err != nil {
				fmt.Printf("Warning: failed to project Team %d event %s: %v\n", teamID, message.ID, err)
			}
			team.RedisEventsLastID = message.ID
			team.UpdatedAt = time.Now().UTC()
			_ = s.repo.UpdateTeam(team)
		}
	}
}

func (s *teamService) ensureStaleTaskMonitor(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	if s.staleMonitorStarted {
		return
	}
	s.staleMonitorStarted = true
	s.wg.Add(1)
	go s.monitorStaleTasks(ctx)
}

func (s *teamService) monitorStaleTasks(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(teamTaskStaleSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sweepTeamEventOutbox(); err != nil {
				fmt.Printf("Warning: failed to deliver Team event outbox: %v\n", err)
			}
			if err := s.sweepStaleTasks(); err != nil {
				fmt.Printf("Warning: failed to sweep stale Team tasks: %v\n", err)
			}
			if err := s.sweepAssignmentStatusChecks(); err != nil {
				fmt.Printf("Warning: failed to sweep Team assignment status checks: %v\n", err)
			}
		}
	}
}

func (s *teamService) sweepTeamEventOutbox() error {
	if s == nil || s.repo == nil {
		return nil
	}
	now := time.Now().UTC()
	rows, err := s.repo.ListPendingEventOutbox(now, teamEventOutboxBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for idx := range rows {
		row := rows[idx]
		team, getErr := s.repo.GetTeamByID(row.TeamID)
		if getErr != nil {
			errs = append(errs, getErr)
			continue
		}
		if team == nil || team.Status == models.TeamStatusDeleted || team.Status == models.TeamStatusDeleting {
			continue
		}
		bus, busErr := s.redisBusForTeam(context.Background(), team)
		if busErr != nil {
			_ = s.repo.MarkEventOutboxFailed(row.ID, now.Add(teamOutboxRetryDelay(row.Attempts)), busErr.Error())
			errs = append(errs, busErr)
			continue
		}
		if deliverErr := s.deliverTeamEventOutbox(team, bus, &row); deliverErr != nil {
			_ = s.repo.MarkEventOutboxFailed(row.ID, now.Add(teamOutboxRetryDelay(row.Attempts)), deliverErr.Error())
			errs = append(errs, deliverErr)
			continue
		}
		if markErr := s.repo.MarkEventOutboxDelivered(row.ID, time.Now().UTC()); markErr != nil {
			errs = append(errs, markErr)
		}
	}
	return errors.Join(errs...)
}

func teamOutboxRetryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(1<<attempts) * 5 * time.Second
}

func (s *teamService) deliverTeamEventOutbox(team *models.Team, bus *redisBus, outbox *models.TeamEventOutbox) error {
	if team == nil || bus == nil || outbox == nil || strings.TrimSpace(outbox.Destination) == "" {
		return fmt.Errorf("team, redis bus and outbox destination are required")
	}
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode Team outbox payload %d: %w", outbox.ID, err)
	}
	messageID := strings.TrimSpace(outbox.MessageID)
	if messageID == "" {
		messageID = eventString(payload, "messageId", "message_id")
	}
	if strings.EqualFold(eventString(payload, "event", "type"), "completion_ack") {
		completionID := eventString(payload, "completionId", "completion_id")
		attemptID := eventString(payload, "attemptId", "attempt_id")
		if completionID == "" || attemptID == "" {
			return fmt.Errorf("completion acknowledgement outbox %d is missing completionId or attemptId", outbox.ID)
		}
		if err := bus.Set(context.Background(), teamCompletionAckKey(team.ID, completionID, attemptID), outbox.PayloadJSON, 24*time.Hour); err != nil {
			return err
		}
		return bus.Set(context.Background(), teamCompletionStateKey(team.ID, completionID), outbox.PayloadJSON, 7*24*time.Hour)
	}
	fields := map[string]string{
		"payload":    outbox.PayloadJSON,
		"team_id":    strconv.Itoa(team.ID),
		"message_id": messageID,
		"member_id":  eventString(payload, "to", "target", "memberId", "member_id"),
	}
	if taskID := eventString(payload, "taskId", "task_id", "rootTaskId", "root_task_id"); taskID != "" {
		fields["task_id"] = taskID
	}
	_, err := bus.XAdd(context.Background(), outbox.Destination, fields)
	return err
}

func (s *teamService) sweepStaleTasks() error {
	timeout := teamTaskStaleTimeout()
	if timeout <= 0 {
		return nil
	}
	cutoff := time.Now().UTC().Add(-timeout)
	tasks, err := s.repo.ListStaleCandidateTasks(cutoff, 100)
	if err != nil {
		return err
	}
	for idx := range tasks {
		if err := s.markTaskStale(&tasks[idx], timeout); err != nil {
			fmt.Printf("Warning: failed to mark Team task %d stale: %v\n", tasks[idx].ID, err)
		}
	}
	return nil
}

func (s *teamService) sweepAssignmentStatusChecks() error {
	if s == nil || s.repo == nil {
		return nil
	}
	now := time.Now().UTC()
	cutoff := now.Add(-teamAssignmentMonitorEvery)
	teams, err := s.repo.ListActiveTeams()
	if err != nil {
		return err
	}
	var errs []error
	for idx := range teams {
		team := teams[idx]
		if normalizedTeamCommunicationMode(team.CommunicationMode) != teamCommunicationModeLeaderMediated {
			continue
		}
		items, err := s.repo.ListWorkItemsByTeamID(team.ID, 500)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var bus *redisBus
		for itemIdx := range items {
			item := items[itemIdx]
			if item.Status == models.TeamTaskStatusSucceeded {
				if item.OwnerMemberID == nil {
					continue
				}
				task, taskErr := s.repo.GetTaskByID(item.RootTaskID)
				if taskErr != nil {
					errs = append(errs, taskErr)
					continue
				}
				owner, ownerErr := s.repo.GetMemberByID(*item.OwnerMemberID)
				if ownerErr != nil {
					errs = append(errs, ownerErr)
					continue
				}
				if task == nil || owner == nil || owner.TeamID != team.ID || isLeaderTeamMember(owner) {
					continue
				}
				if owner.Status != models.TeamMemberStatusIdle || owner.CurrentTaskID != nil || owner.Availability == models.TeamMemberAvailabilityBusy {
					terminalStatus := models.TeamTaskStatusSucceeded
					owner.Status = models.TeamMemberStatusIdle
					owner.CurrentTaskID = nil
					owner.Availability = models.TeamMemberAvailabilityIdle
					owner.RuntimeStatus = &terminalStatus
					owner.RuntimeIntent = nil
					owner.BlockedReason = nil
					owner.Progress = 100
					owner.UpdatedAt = now
					if updateErr := s.repo.UpdateMember(owner); updateErr != nil {
						errs = append(errs, updateErr)
						continue
					}
				}
				assignmentID := derefTeamString(item.AssignmentID)
				if assignmentID == "" {
					assignmentID = item.WorkID
				}
				resultPayload := map[string]interface{}{}
				if item.ResultJSON != nil && strings.TrimSpace(*item.ResultJSON) != "" {
					_ = json.Unmarshal([]byte(*item.ResultJSON), &resultPayload)
				}
				contentHash := eventString(resultPayload, "contentHash", "content_hash")
				if contentHash == "" {
					contentHash = teamResultContentHash(resultPayload)
				}
				confirmed, confirmErr := s.hasLeaderMediatedResultConfirmation(team.ID, task.ID, owner.MemberKey, assignmentID, contentHash)
				if confirmErr != nil {
					errs = append(errs, confirmErr)
					continue
				}
				if confirmed {
					continue
				}
				if bus == nil {
					bus, err = s.redisBusForTeam(context.Background(), &team)
					if err != nil {
						errs = append(errs, err)
						continue
					}
				}
				resultPayload = workItemResultPayload(item)
				resultPayload["recoveredByMonitor"] = true
				resultPayload["sourceWorkId"] = item.WorkID
				sourceEvent := &models.TeamEvent{
					TeamID:     team.ID,
					TaskID:     &task.ID,
					MemberID:   &owner.ID,
					EventType:  "monitor_result_recovery",
					OccurredAt: item.FinishedAt,
					CreatedAt:  item.UpdatedAt,
				}
				if createErr := s.createLeaderMediatedResultNotification(&team, bus, task, owner, resultPayload, sourceEvent); createErr != nil {
					errs = append(errs, createErr)
				}
				continue
			}
			if !shouldMonitorTeamWorkItem(item, cutoff) {
				continue
			}
			task, err := s.repo.GetTaskByID(item.RootTaskID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if task == nil || task.TeamID != team.ID || isTerminalTeamTaskStatus(task.Status) {
				continue
			}
			owner, err := s.repo.GetMemberByID(*item.OwnerMemberID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if owner == nil || owner.TeamID != team.ID || isLeaderTeamMember(owner) || !isActiveTeamMember(owner) {
				continue
			}
			monitorKey := fmt.Sprintf("%d:%d:%s:%d", team.ID, item.RootTaskID, item.WorkID, *item.OwnerMemberID)
			if !s.claimAssignmentMonitorSlot(monitorKey, now) {
				continue
			}
			if bus == nil {
				bus, err = s.redisBusForTeam(context.Background(), &team)
				if err != nil {
					errs = append(errs, err)
					continue
				}
			}
			if err := s.dispatchAssignmentStatusCheck(&team, bus, task, &item, owner, now); err != nil {
				errs = append(errs, err)
			}
		}
		if err := s.sweepLeaderSynthesisReminders(&team, bus, items, cutoff, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *teamService) sweepLeaderSynthesisReminders(team *models.Team, bus *redisBus, items []models.TeamWorkItem, cutoff, now time.Time) error {
	if s == nil || s.repo == nil || team == nil || !isLeaderMediatedTeam(team) {
		return nil
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return err
	}
	membersByID := make(map[int]*models.TeamMember, len(members))
	for idx := range members {
		membersByID[members[idx].ID] = &members[idx]
	}
	taskIDs := map[int]struct{}{}
	for idx := range items {
		if items[idx].RootTaskID > 0 {
			taskIDs[items[idx].RootTaskID] = struct{}{}
		}
	}
	var errs []error
	for taskID := range taskIDs {
		task, err := s.repo.GetTaskByID(taskID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if task == nil || task.TeamID != team.ID || isTerminalTeamTaskStatus(task.Status) {
			continue
		}
		leader := membersByID[task.TargetMemberID]
		if leader == nil || !isLeaderTeamMember(leader) || !isActiveTeamMember(leader) {
			continue
		}
		// Phase state is derived data. Repair it before applying the normal
		// reminder age gate so a stuck workflow can advance without another
		// Leader model round.
		ledgerRepaired, repairErr := s.reconcileTeamWorkflowLedger(task, false, now)
		if repairErr != nil {
			errs = append(errs, repairErr)
			continue
		}
		reconciled, reconcileErr := s.reconcileDeferredTeamCompletion(team, bus, task, leader)
		if reconcileErr != nil {
			errs = append(errs, reconcileErr)
			continue
		}
		if reconciled || isTerminalTeamTaskStatus(task.Status) {
			continue
		}
		if !ledgerRepaired && !task.UpdatedAt.IsZero() && task.UpdatedAt.After(cutoff) {
			continue
		}
		ready, resultItems := leaderMediatedRootNeedsSynthesisReminder(task, items, membersByID)
		if !ready {
			continue
		}
		monitorKey := fmt.Sprintf("%d:%d:%s:%d", team.ID, task.ID, task.WorkflowState, task.LedgerVersion)
		if !s.claimAssignmentMonitorSlot(monitorKey, now) {
			continue
		}
		teamBus := bus
		if teamBus == nil {
			teamBus, err = s.redisBusForTeam(context.Background(), team)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := s.createLeaderSynthesisReminder(team, teamBus, task, leader, resultItems, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *teamService) reconcileDeferredTeamCompletion(team *models.Team, bus *redisBus, task *models.TeamTask, leader *models.TeamMember) (bool, error) {
	if s == nil || team == nil || task == nil || leader == nil || isTerminalTeamTaskStatus(task.Status) {
		return false, nil
	}
	events, err := s.repo.ListEventsByTeamID(team.ID, 500)
	if err != nil {
		return false, err
	}
	for idx := range events {
		event := events[idx]
		if event.TaskID == nil || *event.TaskID != task.ID || event.EventType != "completion_deferred" {
			continue
		}
		payload := teamEventPayloadMap(event)
		if !eventBool(payload, "explicitCompletion", "explicit_completion") || eventString(payload, "completionId", "completion_id") == "" {
			continue
		}
		proposalPlanVersion := int64(eventInt(payload, "planVersion", "plan_version"))
		if proposalPlanVersion > 0 && task.PlanVersion > 0 && proposalPlanVersion != task.PlanVersion {
			// A completion report produced for an older plan cannot summarize work
			// introduced by a later plan. Ask the Leader to synthesize again.
			continue
		}
		if teamRedisProtocolVersion(payload) >= 3 && (!eventBool(payload, "workflowFinal", "workflow_final", "sealWorkflow", "seal_workflow") ||
			!eventBool(payload, "finalAnswerReady", "final_answer_ready") ||
			len(normalizeContextRefs(firstTeamValue(payload, "remainingActions", "remaining_actions", "nextActions", "next_actions"))) > 0) {
			continue
		}
		// Only a current, explicitly sealed completion proposal may retire an
		// unused planned phase. An older report must not mutate a newer plan.
		if _, err := s.reconcileTeamWorkflowLedger(task, true, time.Now().UTC()); err != nil {
			return false, err
		}
		payload["event"] = "completion_proposed"
		payload["type"] = "completion_proposed"
		payload["rootTaskTerminal"] = true
		payload["status"] = models.TeamTaskStatusSucceeded
		payload["runtimeStatus"] = models.TeamTaskStatusSucceeded
		payload["eventId"] = fmt.Sprintf("completion-reconcile:%d:%d:%s:%d", team.ID, task.ID, normalizeTeamRedisKeyPart(eventString(payload, "completionId", "completion_id")), task.LedgerVersion)
		payload["attemptId"] = fmt.Sprintf("reconcile:%d", task.LedgerVersion)
		payload["ledgerVersion"] = task.LedgerVersion
		payload["planVersion"] = task.PlanVersion
		payload["memberId"] = leader.MemberKey
		payload["taskId"] = fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
		payload["rootTaskId"] = fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
		delete(payload, "completionDecision")
		delete(payload, "completionDecisionReason")
		delete(payload, "pendingAssignments")
		delete(payload, "pendingPhases")
		encoded, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		streamID := fmt.Sprintf("reconcile-%d-%d", task.ID, task.LedgerVersion)
		if err := s.projectTeamEvent(team, bus, redisStreamMessage{ID: streamID, Fields: map[string]string{"payload": string(encoded)}}); err != nil {
			return false, err
		}
		updated, err := s.repo.GetTaskByID(task.ID)
		if err != nil {
			return false, err
		}
		if updated != nil && updated.Status == models.TeamTaskStatusSucceeded {
			*task = *updated
			return true, nil
		}
		return false, nil
	}
	return false, nil
}

func leaderMediatedRootNeedsSynthesisReminder(task *models.TeamTask, items []models.TeamWorkItem, membersByID map[int]*models.TeamMember) (bool, []models.TeamWorkItem) {
	if task == nil || isTerminalTeamTaskStatus(task.Status) {
		return false, nil
	}
	latest := map[string]models.TeamWorkItem{}
	for idx := range items {
		item := items[idx]
		if item.RootTaskID != task.ID || item.SupersededBy != nil {
			continue
		}
		key := derefTeamString(item.AssignmentID)
		if key == "" {
			key = item.WorkID
		}
		if current, ok := latest[key]; !ok || teamMaxInt(item.Revision, 1) >= teamMaxInt(current.Revision, 1) {
			latest[key] = item
		}
	}
	var resultItems []models.TeamWorkItem
	for _, item := range latest {
		owner := memberForWorkItem(item, membersByID)
		if owner == nil {
			continue
		}
		if isLeaderTeamMember(owner) {
			if item.Status == models.TeamTaskStatusSucceeded && isLeaderFinalSynthesisWorkItem(item) {
				return false, nil
			}
			continue
		}
		if !isActiveTeamMember(owner) {
			continue
		}
		if !(item.RequiredForRoot || item.AssignmentID == nil) {
			continue
		}
		switch item.Status {
		case models.TeamTaskStatusSucceeded:
			resultItems = append(resultItems, item)
		case models.TeamTaskStatusFailed, models.TeamTaskStatusStale:
			return false, nil
		default:
			return false, nil
		}
	}
	sort.Slice(resultItems, func(i, j int) bool {
		return resultItems[i].WorkID < resultItems[j].WorkID
	})
	return len(resultItems) > 0, resultItems
}

func memberForWorkItem(item models.TeamWorkItem, membersByID map[int]*models.TeamMember) *models.TeamMember {
	if item.OwnerMemberID == nil || membersByID == nil {
		return nil
	}
	return membersByID[*item.OwnerMemberID]
}

func isLeaderFinalSynthesisWorkItem(item models.TeamWorkItem) bool {
	text := strings.ToLower(strings.TrimSpace(item.WorkID + " " + item.Title))
	return strings.Contains(text, "leader") &&
		(strings.Contains(text, "final") ||
			strings.Contains(text, "synthesis") ||
			strings.Contains(text, "result") ||
			strings.Contains(text, "complete"))
}

func (s *teamService) createLeaderSynthesisReminder(team *models.Team, bus *redisBus, task *models.TeamTask, leader *models.TeamMember, resultItems []models.TeamWorkItem, now time.Time) error {
	if s == nil || s.repo == nil || team == nil || task == nil || leader == nil || len(resultItems) == 0 {
		return nil
	}
	eventID := fmt.Sprintf("leader-workflow-reminder:%d:%d:%s:%d", team.ID, task.ID, normalizeTeamRedisKeyPart(task.WorkflowState), task.LedgerVersion)
	exists, err := s.repo.EventExistsByEventID(team.ID, eventID)
	if err != nil || exists {
		return err
	}
	rootTaskRef := fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	memberResults := make([]map[string]interface{}, 0, len(resultItems))
	for idx := range resultItems {
		item := resultItems[idx]
		memberResult := map[string]interface{}{
			"workId":  item.WorkID,
			"title":   item.Title,
			"status":  item.Status,
			"summary": workItemResultSummary(item),
		}
		if item.OwnerMemberID != nil {
			memberResult["ownerMemberId"] = *item.OwnerMemberID
		}
		if refs := workItemArtifactRefs(item); len(refs) > 0 {
			memberResult["artifactRefs"] = refs
		}
		memberResults = append(memberResults, memberResult)
	}
	decisionReminder := task.WorkflowState == teamWorkflowStateAwaitingLeaderDecision || task.WorkflowState == teamWorkflowStateExecuting
	summary := "All tracked member assignments have delivered results; Leader should synthesize the final answer and close the root task."
	eventKind := "leader_synthesis_reminder"
	intent := "leader_synthesis_reminder"
	title := "Leader final synthesis requested"
	if decisionReminder {
		summary = "The current phase has delivered its results. Leader must decide whether to create the next phase or explicitly seal the workflow before final synthesis."
		eventKind = "leader_decision_reminder"
		intent = "leader_workflow_decision"
		title = "Leader workflow decision requested"
	}
	payload := map[string]interface{}{
		"event":              eventKind,
		"type":               eventKind,
		"eventKind":          eventKind,
		"protocolVersion":    3,
		"source":             "clawmanager_monitor",
		"nonAuthoritative":   true,
		"rootTaskTerminal":   false,
		"teamId":             strconv.Itoa(team.ID),
		"taskId":             rootTaskRef,
		"rootTaskId":         rootTaskRef,
		"rootMessageId":      task.MessageID,
		"messageId":          eventID,
		"memberId":           leader.MemberKey,
		"from":               "clawmanager-monitor",
		"to":                 leader.MemberKey,
		"target":             leader.MemberKey,
		"workId":             "leader-final-synthesis",
		"assignmentId":       "leader-final-synthesis",
		"status":             models.TeamTaskStatusRunning,
		"runtimeStatus":      models.TeamTaskStatusRunning,
		"availability":       models.TeamMemberAvailabilityBusy,
		"summary":            summary,
		"workflowState":      task.WorkflowState,
		"planVersion":        task.PlanVersion,
		"ledgerVersion":      task.LedgerVersion,
		"visibleToChat":      true,
		"chatDigestEligible": true,
		"dedupeKey":          fmt.Sprintf("leader-synthesis:%d:%d", team.ID, task.ID),
		"monitor":            true,
		"monitorType":        eventKind,
		"memberResults":      memberResults,
		"collaborationStep": map[string]interface{}{
			"type":          "progress",
			"status":        models.TeamTaskStatusRunning,
			"actor":         "clawmanager-monitor",
			"target":        leader.MemberKey,
			"rootTaskId":    rootTaskRef,
			"rootMessageId": task.MessageID,
			"workId":        "leader-final-synthesis",
			"title":         title,
			"summary":       summary,
			"content":       summary,
			"source":        "clawmanager_monitor",
		},
	}
	payloadJSON, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	event := &models.TeamEvent{
		TeamID:      team.ID,
		TaskID:      &task.ID,
		MemberID:    &leader.ID,
		MessageID:   &eventID,
		EventID:     &eventID,
		EventType:   eventKind,
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	}
	if err := s.repo.CreateEvent(event); err != nil && !errors.Is(err, repository.ErrDuplicateTeamEvent) {
		return err
	}
	if bus == nil {
		return nil
	}
	prompt := buildLeaderSynthesisReminderPrompt(task, resultItems, rootTaskRef)
	if decisionReminder {
		prompt = "Review the confirmed results for the current phase. If the root task requires another implementation, verification, review, or refinement phase, publish a new planVersion and dispatch those assignments now. If no required action remains, explicitly seal the workflow and then submit the final user-facing result. Do not call team_complete_task merely because the current workers have finished.\n\n" + prompt
	}
	envelope := map[string]interface{}{
		"v":                  1,
		"protocolVersion":    3,
		"messageId":          eventID,
		"teamId":             strconv.Itoa(team.ID),
		"from":               "clawmanager-monitor",
		"to":                 leader.MemberKey,
		"replyTo":            teamTaskReplyTarget,
		"requiresCompletion": false,
		"completionTool":     teamTaskCompletionTool,
		"intent":             intent,
		"taskId":             rootTaskRef,
		"rootTaskId":         rootTaskRef,
		"rootMessageId":      task.MessageID,
		"workId":             "leader-final-synthesis",
		"assignmentId":       "leader-final-synthesis",
		"title":              title,
		"prompt":             prompt,
		"rawPrompt":          prompt,
		"monitorPolicy":      defaultTeamMonitorPolicy(),
		"metadata":           payload,
		"createdAt":          now.Format(time.RFC3339Nano),
	}
	applyTeamTaskEnvelopeContext(envelope, task, leader.MemberKey)
	envelopeJSON, err := marshalJSON(envelope)
	if err != nil {
		return err
	}
	_, err = bus.XAdd(context.Background(), teamInboxKey(team.ID, leader.MemberKey), map[string]string{
		"payload":    envelopeJSON,
		"team_id":    strconv.Itoa(team.ID),
		"task_id":    strconv.Itoa(task.ID),
		"message_id": eventID,
		"member_id":  leader.MemberKey,
	})
	return err
}

func buildLeaderSynthesisReminderPrompt(task *models.TeamTask, resultItems []models.TeamWorkItem, rootTaskRef string) string {
	lines := []string{
		"[LEADER_SYNTHESIS_REMINDER] All tracked member assignments for this root task have delivered terminal results in the ClawManager ledger.",
		fmt.Sprintf("rootTaskId=%s rootMessageId=%s workId=leader-final-synthesis assignmentId=leader-final-synthesis", rootTaskRef, task.MessageID),
		"Synthesize the final user-facing answer from the confirmed member results below, then call team_complete_task for the root task. Do not re-dispatch finished assignments unless the evidence below is actually insufficient.",
		"",
		"Confirmed member results:",
	}
	for idx := range resultItems {
		item := resultItems[idx]
		summary := workItemResultSummary(item)
		if summary == "" {
			summary = item.Title
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", item.WorkID, summary))
	}
	return strings.Join(lines, "\n")
}

func workItemResultPayload(item models.TeamWorkItem) map[string]interface{} {
	payload := map[string]interface{}{}
	if item.ResultJSON != nil && strings.TrimSpace(*item.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(*item.ResultJSON), &payload)
	}
	return payload
}

func workItemResultSummary(item models.TeamWorkItem) string {
	payload := workItemResultPayload(item)
	summary := eventString(payload, "summary", "title", "resultMarkdown", "result_markdown", "result", "answer", "text", "message")
	if summary == "" {
		summary = item.Title
	}
	return truncateForSummary(summary, 240)
}

func workItemArtifactRefs(item models.TeamWorkItem) []string {
	payload := workItemResultPayload(item)
	refs := explicitTeamArtifactReferences(payload)
	if len(refs) > 0 {
		return refs
	}
	if item.ArtifactRefsJSON == nil || strings.TrimSpace(*item.ArtifactRefsJSON) == "" {
		return nil
	}
	var parsed []string
	if err := json.Unmarshal([]byte(*item.ArtifactRefsJSON), &parsed); err == nil {
		return parsed
	}
	return nil
}

func shouldMonitorTeamWorkItem(item models.TeamWorkItem, cutoff time.Time) bool {
	if item.OwnerMemberID == nil || strings.TrimSpace(item.WorkID) == "" {
		return false
	}
	switch item.Status {
	case models.TeamTaskStatusDispatched, models.TeamTaskStatusRunning:
	default:
		return false
	}
	return item.UpdatedAt.IsZero() || item.UpdatedAt.Before(cutoff)
}

func (s *teamService) claimAssignmentMonitorSlot(key string, now time.Time) bool {
	if s == nil || strings.TrimSpace(key) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assignmentMonitorLast == nil {
		s.assignmentMonitorLast = map[string]time.Time{}
	}
	if last, ok := s.assignmentMonitorLast[key]; ok && now.Sub(last) < teamAssignmentMonitorEvery {
		return false
	}
	s.assignmentMonitorLast[key] = now
	return true
}

func (s *teamService) dispatchAssignmentStatusCheck(team *models.Team, bus *redisBus, task *models.TeamTask, item *models.TeamWorkItem, owner *models.TeamMember, now time.Time) error {
	if team == nil || bus == nil || task == nil || item == nil || owner == nil {
		return nil
	}
	envelope, messageID := buildAssignmentStatusCheckEnvelope(team, task, item, owner, now)
	if envelope == nil || strings.TrimSpace(messageID) == "" {
		return nil
	}
	envelopeJSON, err := marshalJSON(envelope)
	if err != nil {
		return err
	}
	_, err = bus.XAdd(context.Background(), teamInboxKey(team.ID, owner.MemberKey), map[string]string{
		"payload":    envelopeJSON,
		"team_id":    strconv.Itoa(team.ID),
		"task_id":    strconv.Itoa(task.ID),
		"message_id": messageID,
		"member_id":  owner.MemberKey,
	})
	if err != nil {
		return err
	}
	payload := map[string]interface{}{
		"v":                 1,
		"protocolVersion":   2,
		"event":             "assignment_check_requested",
		"type":              "assignment_check_requested",
		"eventKind":         "assignment_check_requested",
		"teamId":            strconv.Itoa(team.ID),
		"taskId":            fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootTaskId":        fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootMessageId":     task.MessageID,
		"messageId":         messageID,
		"memberId":          owner.MemberKey,
		"from":              "clawmanager-monitor",
		"to":                owner.MemberKey,
		"workId":            item.WorkID,
		"assignmentId":      item.WorkID,
		"status":            models.TeamTaskStatusRunning,
		"runtimeStatus":     models.TeamTaskStatusRunning,
		"availability":      models.TeamMemberAvailabilityBusy,
		"summary":           "ClawManager requested an automatic assignment status check.",
		"visibleToChat":     false,
		"nonAuthoritative":  true,
		"rootTaskTerminal":  false,
		"monitor":           true,
		"monitorType":       "assignment_status_check",
		"workItemId":        item.ID,
		"workItemTitle":     item.Title,
		"lastWorkUpdatedAt": item.UpdatedAt.Format(time.RFC3339Nano),
	}
	payloadJSON, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	event := &models.TeamEvent{
		TeamID:      team.ID,
		TaskID:      &task.ID,
		MemberID:    &owner.ID,
		EventType:   "assignment_check_requested",
		MessageID:   &messageID,
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	}
	if err := s.repo.CreateEvent(event); err != nil && !errors.Is(err, repository.ErrDuplicateTeamEvent) {
		return err
	}
	return nil
}

func buildAssignmentStatusCheckEnvelope(team *models.Team, task *models.TeamTask, item *models.TeamWorkItem, owner *models.TeamMember, now time.Time) (map[string]interface{}, string) {
	if team == nil || task == nil || item == nil || owner == nil {
		return nil, ""
	}
	taskRef := fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	checkSequence := now.Unix()
	if seconds := int64(teamAssignmentMonitorEvery.Seconds()); seconds > 0 {
		checkSequence = now.Unix() / seconds
	}
	messageID := fmt.Sprintf("monitor:%s:%s:%d", taskRef, item.WorkID, checkSequence)
	prompt := strings.Join([]string{
		"[STATUS_CHECK] This is an automatic ClawManager assignment monitor.",
		fmt.Sprintf("rootTaskId=%s rootMessageId=%s workId=%s assignmentId=%s", taskRef, task.MessageID, item.WorkID, item.WorkID),
		"Check the current assignment state. If you are still working, call team_update_progress with status=\"running\", eventKind=\"assignment_check_result\", a concise progress summary, and continue the same assignment. If you stopped or lost context, resume from the assignment evidence and report the recoverable blocker or needed retry to the Leader. Do not mark the assignment complete unless the deliverable is actually ready.",
	}, "\n")
	envelope := map[string]interface{}{
		"v":                  1,
		"protocolVersion":    2,
		"messageId":          messageID,
		"teamId":             strconv.Itoa(team.ID),
		"from":               "clawmanager-monitor",
		"to":                 owner.MemberKey,
		"replyTo":            teamTaskReplyTarget,
		"requiresCompletion": false,
		"completionTool":     teamTaskCompletionTool,
		"intent":             "assignment_status_check",
		"taskId":             taskRef,
		"rootTaskId":         taskRef,
		"rootMessageId":      task.MessageID,
		"workId":             item.WorkID,
		"assignmentId":       item.WorkID,
		"checkId":            messageID,
		"checkSequence":      checkSequence,
		"requestedAt":        now.Format(time.RFC3339Nano),
		"title":              "Assignment status check",
		"prompt":             prompt,
		"rawPrompt":          prompt,
		"monitorPolicy":      defaultTeamMonitorPolicy(),
		"metadata": map[string]interface{}{
			"monitor":       true,
			"monitorType":   "assignment_status_check",
			"eventKind":     "assignment_check_requested",
			"visibleToChat": false,
			"workItemId":    item.ID,
			"workItemTitle": item.Title,
			"lastUpdatedAt": item.UpdatedAt.Format(time.RFC3339Nano),
			"checkId":       messageID,
			"checkSequence": checkSequence,
			"requestedAt":   now.Format(time.RFC3339Nano),
		},
		"createdAt": now.Format(time.RFC3339Nano),
	}
	applyTeamTaskEnvelopeContext(envelope, task, owner.MemberKey)
	return envelope, messageID
}

func (s *teamService) markTaskStale(task *models.TeamTask, timeout time.Duration) error {
	if task == nil {
		return nil
	}
	if task.Status != models.TeamTaskStatusDispatched && task.Status != models.TeamTaskStatusRunning {
		return nil
	}
	lastUpdatedAt := task.UpdatedAt
	team, err := s.repo.GetTeamByID(task.TeamID)
	if err != nil {
		return err
	}
	if team == nil || team.Status == models.TeamStatusDeleted || team.Status == models.TeamStatusDeleting {
		return nil
	}
	if payloadJSON, terminal, err := s.taskHasTerminalCompletionEvidence(team, task); err != nil {
		return err
	} else if terminal {
		now := time.Now().UTC()
		task.Status = models.TeamTaskStatusSucceeded
		task.FinishedAt = &now
		task.UpdatedAt = now
		task.ErrorMessage = nil
		if payloadJSON != nil {
			task.ResultJSON = payloadJSON
		}
		if err := s.repo.UpdateTask(task); err != nil {
			return err
		}
		if member, err := s.repo.GetMemberByID(task.TargetMemberID); err != nil {
			return err
		} else if member != nil && member.TeamID == task.TeamID && member.CurrentTaskID != nil && *member.CurrentTaskID == task.ID {
			member.Status = models.TeamMemberStatusIdle
			member.CurrentTaskID = nil
			member.Availability = models.TeamMemberAvailabilityIdle
			member.BlockedReason = nil
			member.Progress = 100
			member.UpdatedAt = now
			if err := s.repo.UpdateMember(member); err != nil {
				return err
			}
		}
		return nil
	}
	cutoff := time.Now().UTC().Add(-timeout)
	active, err := s.taskHasRecentActivity(team, task, cutoff)
	if err != nil {
		return err
	}
	if active {
		return nil
	}

	now := time.Now().UTC()
	previousStatus := task.Status
	task.Status = models.TeamTaskStatusStale
	task.FinishedAt = &now
	message := fmt.Sprintf("Team task stale: no runtime event for %s since %s", timeout.String(), task.UpdatedAt.Format(time.RFC3339))
	task.ErrorMessage = &message
	task.UpdatedAt = now
	if err := s.repo.UpdateTask(task); err != nil {
		return err
	}

	member, err := s.repo.GetMemberByID(task.TargetMemberID)
	if err != nil {
		return err
	}
	if member != nil && member.TeamID == task.TeamID && member.CurrentTaskID != nil && *member.CurrentTaskID == task.ID {
		member.Status = models.TeamMemberStatusIdle
		member.CurrentTaskID = nil
		member.Availability = models.TeamMemberAvailabilityBlocked
		member.RuntimeTaskID = &task.MessageID
		member.RuntimeIntent = nil
		member.BlockedReason = &message
		member.LastSummary = &message
		member.Progress = 0
		member.UpdatedAt = now
		if err := s.repo.UpdateMember(member); err != nil {
			return err
		}
	}

	payload := map[string]interface{}{
		"v":                 1,
		"event":             "task_stale",
		"teamId":            strconv.Itoa(task.TeamID),
		"taskId":            fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"messageId":         task.MessageID,
		"previousStatus":    previousStatus,
		"staleAfterSeconds": int(timeout.Seconds()),
		"lastTaskUpdatedAt": lastUpdatedAt.Format(time.RFC3339Nano),
		"diagnostic":        message,
		"source":            "clawmanager",
	}
	payloadJSON, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	event := &models.TeamEvent{
		TeamID:      task.TeamID,
		TaskID:      &task.ID,
		EventType:   "task_stale",
		MessageID:   &task.MessageID,
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	}
	if member != nil && member.TeamID == task.TeamID {
		event.MemberID = &member.ID
	}
	return s.repo.CreateEvent(event)
}

func (s *teamService) taskHasRecentActivity(team *models.Team, task *models.TeamTask, cutoff time.Time) (bool, error) {
	if s == nil || team == nil || task == nil {
		return false, nil
	}
	events, err := s.repo.ListEventsByTeamID(team.ID, 500)
	if err != nil {
		return false, err
	}
	for idx := range events {
		event := events[idx]
		// CreatedAt is assigned by ClawManager when the event is accepted. Runtime
		// clocks can drift and must not keep a dead task alive or mark an active
		// task stale merely because occurredAt arrived out of order.
		eventTime := event.CreatedAt
		if !eventTime.After(cutoff) {
			continue
		}
		payload := teamEventPayloadMap(event)
		if teamEventMatchesRootTask(event, payload, task) {
			return true, nil
		}
	}
	workItems, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	for idx := range workItems {
		item := workItems[idx]
		if item.Status != models.TeamTaskStatusDispatched && item.Status != models.TeamTaskStatusRunning {
			continue
		}
		if item.UpdatedAt.After(cutoff) {
			return true, nil
		}
	}
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		return false, err
	}
	for idx := range members {
		member := members[idx]
		ownsRootTask := member.CurrentTaskID != nil && *member.CurrentTaskID == task.ID
		runtimeTracksRoot := member.RuntimeTaskID != nil && strings.TrimSpace(*member.RuntimeTaskID) == task.MessageID
		if !ownsRootTask && !runtimeTracksRoot {
			continue
		}
		if member.UpdatedAt.After(cutoff) {
			return true, nil
		}
		if member.LastSeenAt != nil && member.LastSeenAt.After(cutoff) {
			return true, nil
		}
	}
	return false, nil
}

func (s *teamService) taskHasTerminalCompletionEvidence(team *models.Team, task *models.TeamTask) (*string, bool, error) {
	if s == nil || team == nil || task == nil {
		return nil, false, nil
	}
	events, err := s.repo.ListEventsByTeamID(team.ID, 1000)
	if err != nil {
		return nil, false, err
	}
	for idx := range events {
		event := events[idx]
		payload := teamEventPayloadMap(event)
		if !teamEventMatchesRootTask(event, payload, task) {
			continue
		}
		if eventBool(payload, "artifactValidationFailed", "artifact_validation_failed") {
			continue
		}
		member := (*models.TeamMember)(nil)
		if event.MemberID != nil {
			found, err := s.repo.GetMemberByID(*event.MemberID)
			if err != nil {
				return nil, false, err
			}
			if found != nil && found.TeamID == team.ID {
				member = found
			}
		}
		eventType := event.EventType
		if markedType := markLegacyRuntimeCompletionCandidate(eventType, payload, task, member); markedType != eventType {
			eventType = markedType
		}
		if isTeamTaskCompletionSignal(eventType, normalizedTeamTaskEventStatus(payload), payload) {
			payloadJSON, err := marshalOptionalJSON(payload)
			if err != nil {
				return nil, false, err
			}
			return payloadJSON, true, nil
		}
	}
	return nil, false, nil
}

func (s *teamService) redisBusForTeam(ctx context.Context, team *models.Team) (*redisBus, error) {
	redisURL := ""
	if team.RedisURLSecretName != nil && team.RedisURLSecretKey != nil {
		client := k8s.GetClient()
		if client == nil {
			return nil, fmt.Errorf("k8s client not initialized")
		}
		value, err := s.secretService.GetSecretValue(ctx, client.GetNamespace(team.UserID), *team.RedisURLSecretName, *team.RedisURLSecretKey)
		if err != nil {
			return nil, err
		}
		redisURL = strings.TrimSpace(value)
	}
	if redisURL == "" {
		redisURL = defaultTeamRedisURL()
	}
	if redisURL == "" {
		return nil, fmt.Errorf("team redis url is required")
	}
	return newRedisBus(redisURL)
}

type teamTaskProjectionResult struct {
	status  string
	changed bool
}

func projectTeamTaskRuntimeState(task *models.TeamTask, payload map[string]interface{}, eventType string, payloadJSON *string, now time.Time) teamTaskProjectionResult {
	if task == nil {
		return teamTaskProjectionResult{}
	}
	status := normalizedTeamTaskEventStatus(payload)
	completed := isTeamTaskCompletionSignal(eventType, status, payload)
	failed := isTeamTaskFailureSignal(eventType, status, payload)
	running := isTeamTaskRunningSignal(eventType, status, payload)

	result := teamTaskProjectionResult{}
	setStatus := func(next string) {
		result.status = next
		if task.Status != next {
			task.Status = next
			result.changed = true
		}
	}
	setStarted := func() {
		if task.StartedAt == nil {
			task.StartedAt = &now
			result.changed = true
		}
	}
	setFinished := func() {
		if task.FinishedAt == nil || !task.FinishedAt.Equal(now) {
			task.FinishedAt = &now
			result.changed = true
		}
	}

	if eventBool(payload, "artifactValidationFailed", "artifact_validation_failed") {
		setStatus(models.TeamTaskStatusRunning)
		setStarted()
		if task.FinishedAt != nil {
			task.FinishedAt = nil
			result.changed = true
		}
		// Keep the member's final body available while the artifact contract is
		// unresolved. A later valid completion replaces this payload and closes
		// the task normally.
		if payloadJSON != nil && (task.ResultJSON == nil || *task.ResultJSON != *payloadJSON) {
			task.ResultJSON = payloadJSON
			result.changed = true
		}
		return result
	}

	if completed {
		setStatus(models.TeamTaskStatusSucceeded)
		setFinished()
		if payloadJSON != nil && (task.ResultJSON == nil || *task.ResultJSON != *payloadJSON) {
			task.ResultJSON = payloadJSON
			result.changed = true
		}
		if task.ErrorMessage != nil {
			task.ErrorMessage = nil
			result.changed = true
		}
		return result
	}

	if failed && task.Status != models.TeamTaskStatusSucceeded {
		setStatus(models.TeamTaskStatusFailed)
		setFinished()
		if errText := eventString(payload, "error_message", "error", "reason", "diagnostic", "lastSummary", "last_summary", "summary"); errText != "" {
			if task.ErrorMessage == nil || *task.ErrorMessage != errText {
				task.ErrorMessage = &errText
				result.changed = true
			}
		}
		return result
	}

	if isTerminalTeamTaskStatus(task.Status) {
		return result
	}

	switch eventType {
	case "task_received":
		if task.Status == models.TeamTaskStatusPending {
			setStatus(models.TeamTaskStatusDispatched)
		}
	case "task_started":
		setStatus(models.TeamTaskStatusRunning)
		setStarted()
	case "outbound", "task_assigned", "team_send", "peer_request", "peer_handoff", "peer_review_request":
		if task.Status == models.TeamTaskStatusPending {
			setStatus(models.TeamTaskStatusDispatched)
		}
		setStarted()
	default:
		if running {
			setStatus(models.TeamTaskStatusRunning)
			setStarted()
		}
	}
	return result
}

func normalizedTeamTaskEventStatus(payload map[string]interface{}) string {
	raw := eventString(payload, "task_status", "taskStatus", "result_status", "resultStatus", "status", "state")
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = strings.ReplaceAll(raw, " ", "_")
	return raw
}

func isTeamTaskCompletionSignal(eventType, status string, payload map[string]interface{}) bool {
	if isFailedTeamTaskEventStatus(status) || isDispatchOnlyCompletionPayload(payload) {
		return false
	}
	if teamRedisProtocolVersion(payload) >= 2 {
		return (eventType == "task_completed" || eventType == "completion_proposed") &&
			isSuccessfulTeamTaskEventStatus(status) &&
			isExplicitTeamTaskCompletion(payload) &&
			hasStrictTeamCompletionEnvelope(payload)
	}
	if eventBool(payload, "legacyCompletionCandidate", "legacy_completion_candidate") {
		return hasTeamCompletionResultBody(payload) && (status == "" || isSuccessfulTeamTaskEventStatus(status))
	}
	if !hasAuthoritativeTeamCompletionPayload(eventType, status, payload) {
		return false
	}
	switch eventType {
	case "task_completed", "completion", "task_failed", "message_failed":
		return true
	}
	return hasTeamTaskCompletionToolCall(payload) && isSuccessfulTeamTaskEventStatus(status)
}

func hasAuthoritativeTeamCompletionPayload(eventType, status string, payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	if isDispatchOnlyCompletionPayload(payload) {
		return false
	}
	source := strings.ToLower(strings.TrimSpace(eventString(payload, "completionSource", "completion_source")))
	explicitCompletion := hasTeamTaskCompletionToolCall(payload) ||
		source == teamTaskCompletionTool ||
		eventBool(payload, "explicitCompletion", "explicit_completion", "rootTaskTerminal")
	if !explicitCompletion {
		return false
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		switch strings.ToLower(strings.TrimSpace(eventString(step, "type"))) {
		case "assignment", "progress", "ack", "peer_request":
			return false
		}
	}
	if eventType != "task_completed" && eventType != "completion" && eventType != "task_failed" && eventType != "message_failed" {
		return false
	}
	if !hasTeamCompletionResultBody(payload) {
		return false
	}
	return status == "" || isSuccessfulTeamTaskEventStatus(status)
}

func hasTeamCompletionResultBody(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	if body := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer"); body != "" {
		return true
	}
	if summary := eventString(payload, "summary"); summary != "" {
		return true
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		if body := eventString(step, "resultMarkdown", "result_markdown", "result", "answer"); body != "" {
			return true
		}
		if summary := eventString(step, "summary"); summary != "" {
			return true
		}
	}
	for _, record := range eventRecordCandidates(payload) {
		if body := eventString(record, "resultMarkdown", "result_markdown", "result", "answer"); body != "" {
			return true
		}
		if summary := eventString(record, "summary"); summary != "" {
			return true
		}
	}
	return false
}

func isTeamTaskFailureSignal(eventType, status string, payload map[string]interface{}) bool {
	if isSuccessfulTeamTaskEventStatus(status) || isNonAuthoritativeDispatchFailure(eventType, payload) {
		return false
	}
	if teamRedisProtocolVersion(payload) >= 2 {
		if eventType != "task_failed" && eventType != "completion_proposed" {
			return false
		}
		source := strings.ToLower(strings.TrimSpace(eventString(payload, "completionSource", "completion_source")))
		if source != teamTaskCompletionTool && source != "runtime_error" && source != "runtime_processing" {
			return false
		}
		return hasStrictTeamFailureEnvelope(payload)
	}
	switch eventType {
	case "task_failed", "message_failed":
		return true
	}
	if !hasTeamTaskCompletionToolCall(payload) {
		return false
	}
	switch status {
	case "failed", "failure", "error", "errored", "blocked":
		return true
	default:
		return false
	}
}

func hasStrictTeamFailureEnvelope(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	for _, keys := range [][]string{
		{"eventId", "event_id"},
		{"completionId", "completion_id"},
		{"taskId", "task_id"},
		{"rootTaskId", "root_task_id"},
		{"memberId", "member_id"},
		{"summary"},
	} {
		if eventString(payload, keys...) == "" {
			return false
		}
	}
	return len(explicitTeamArtifactReferences(payload)) > 0
}

func teamRedisProtocolVersion(payload map[string]interface{}) int {
	if payload == nil {
		return 1
	}
	if version := eventInt(payload, "protocolVersion"); version > 0 {
		return version
	}
	if version := eventInt(payload, "protocol_version"); version > 0 {
		return version
	}
	return 1
}

func isExplicitTeamTaskCompletion(payload map[string]interface{}) bool {
	if payload == nil || eventString(payload, "completionId", "completion_id") == "" {
		return false
	}
	if !eventBool(payload, "explicitCompletion", "explicit_completion") {
		return false
	}
	return strings.EqualFold(
		strings.TrimSpace(eventString(payload, "completionSource", "completion_source")),
		teamTaskCompletionTool,
	)
}

func hasStrictTeamCompletionEnvelope(payload map[string]interface{}) bool {
	if payload == nil || !hasTeamCompletionResultBody(payload) {
		return false
	}
	for _, keys := range [][]string{
		{"eventId", "event_id"},
		{"completionId", "completion_id"},
		{"taskId", "task_id"},
		{"rootTaskId", "root_task_id"},
		{"memberId", "member_id"},
		{"summary"},
		{"resultMarkdown", "result_markdown"},
	} {
		if eventString(payload, keys...) == "" {
			return false
		}
	}
	// Artifact references are validated separately when they are present.
	// Some valid control-plane/bootstrap tasks only return resultMarkdown and
	// rely on the completion tool/runtime to persist the standard result files.
	// Requiring artifactRefs here leaves those completed root tasks stuck in a
	// dispatched/running Kanban state even though the explicit completion
	// envelope is otherwise authoritative.
	return true
}

func (s *teamService) hasAcceptedTeamCompletionID(teamID int, completionID string) (bool, error) {
	completionID = strings.TrimSpace(completionID)
	if s == nil || completionID == "" {
		return false, nil
	}
	return s.repo.EventExistsByCompletionID(teamID, completionID)
}

func isUnauthoritativeCompletionEvent(eventType string, completion, failure bool) bool {
	if completion || failure {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "task_completed", "completion":
		return true
	default:
		return false
	}
}

func isSuccessfulTeamTaskEventStatus(status string) bool {
	switch status {
	case "succeeded", "success", "completed", "complete", "done", "finished", "ok":
		return true
	default:
		return false
	}
}

func isFailedTeamTaskEventStatus(status string) bool {
	switch status {
	case "failed", "failure", "error", "errored", "blocked":
		return true
	default:
		return false
	}
}

func isDispatchOnlyCompletionPayload(payload map[string]interface{}) bool {
	for _, key := range []string{"resultMarkdown", "result_markdown", "result", "answer"} {
		if body := eventString(payload, key); looksLikeSubstantiveResultDocument(body) {
			return false
		}
	}
	text := strings.ToLower(strings.Join(strings.Fields(strings.Join([]string{
		eventString(payload, "summary", "lastSummary", "last_summary", "message", "text", "diagnostic"),
		eventString(payload, "title", "intent"),
	}, " ")), " "))
	if text == "" {
		return false
	}
	if text == "redis team task completed" || text == "redis team task processing completed" {
		return true
	}
	if strings.Contains(text, "result already delivered") || strings.Contains(text, "\u7ed3\u679c\u5df2\u53cd\u9988") {
		return true
	}
	compact := strings.ReplaceAll(text, " ", "")
	if len([]rune(compact)) > 120 {
		return false
	}
	if strings.Contains(text, "dispatch") && (strings.Contains(text, "worker") || strings.Contains(text, "member")) {
		return true
	}
	return strings.Contains(compact, "\u5728\u7ebf\u7a7a\u95f2") ||
		strings.Contains(compact, "\u6d3e\u5355") ||
		strings.Contains(compact, "\u5df2\u6d3e\u53d1") ||
		strings.Contains(compact, "\u7b49\u5f85\u5176") ||
		strings.Contains(compact, "\u4efb\u52a1\u5206\u6d3e")
}

func looksLikeSubstantiveResultDocument(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	compact := strings.ReplaceAll(strings.ToLower(strings.Join(strings.Fields(trimmed), "")), " ", "")
	if compact == "redisteamtaskcompleted" || compact == "redisteamtaskprocessingcompleted" {
		return false
	}
	if len([]rune(compact)) >= 180 {
		return true
	}
	return strings.Contains(trimmed, "\n## ") ||
		strings.Contains(trimmed, "\n|") ||
		strings.Contains(trimmed, "\n- ") ||
		strings.Contains(trimmed, "\n1.") ||
		strings.Contains(trimmed, "\n### ")
}
func isTeamTaskRunningSignal(eventType, status string, payload map[string]interface{}) bool {
	switch eventType {
	case "task_started", "task_progress", "progress":
		return true
	}
	switch status {
	case "running", "in_progress", "processing", "busy", "working":
		return true
	}
	progress := eventInt(payload, "progress")
	return progress > 0 && progress < 100
}

func isTerminalTeamTaskStatus(status string) bool {
	return status == models.TeamTaskStatusSucceeded ||
		status == models.TeamTaskStatusFailed ||
		status == models.TeamTaskStatusStale
}

func isAssignmentHeartbeatEvent(eventType string, payload map[string]interface{}) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType == "assignment_heartbeat" {
		return true
	}
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	return eventKind == "assignment_heartbeat"
}

func isPassiveAssignmentMonitorEvent(eventType string, payload map[string]interface{}) bool {
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "assignment_heartbeat", "assignment_check_requested":
		return true
	}
	switch eventKind {
	case "assignment_heartbeat", "assignment_check_requested", "assignment_check_result":
		return true
	}
	monitorType := strings.ToLower(strings.TrimSpace(eventString(payload, "monitorType", "monitor_type")))
	return monitorType == "assignment_status_check" && eventBool(payload, "nonAuthoritative", "non_authoritative")
}

func isTeamPresenceEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "presence", "member_presence", "status", "member_status":
		return true
	default:
		return false
	}
}

func normalizePassiveAssignmentMonitorPayload(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	payload["visibleToChat"] = false
	payload["visible_to_chat"] = false
	payload["chatDigestEligible"] = true
	payload["chat_digest_eligible"] = true
	payload["nonAuthoritative"] = true
	payload["rootTaskTerminal"] = false
}

func normalizeAssignmentHeartbeatPayload(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	normalizePassiveAssignmentMonitorPayload(payload)
	summary := strings.TrimSpace(eventString(payload, "summary", "message", "text", "diagnostic"))
	if summary == "" || strings.EqualFold(summary, "Agent turn is still running") {
		payload["summary"] = "\u4efb\u52a1\u4ecd\u5728\u6267\u884c\uff0cAgent \u6b63\u5728\u7ee7\u7eed\u5904\u7406\u5f53\u524d\u56de\u5408\u3002"
	}
}

func normalizeUnauthorizedAssignmentCheckResult(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	if eventKind != "assignment_check_result" && eventKind != "assignment_check_requested" {
		return
	}
	checkID := strings.TrimSpace(eventString(payload, "checkId", "check_id"))
	if strings.HasPrefix(checkID, "monitor:") {
		return
	}
	payload["originalEventKind"] = eventKind
	payload["eventKind"] = "worker_progress"
	payload["checkSemanticCorrected"] = true
	delete(payload, "checkId")
	delete(payload, "check_id")
}

func (s *teamService) leaderMediatedMonitorTargetsTerminalWorkItem(team *models.Team, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}) (bool, error) {
	if s == nil || s.repo == nil || team == nil || task == nil || member == nil || payload == nil {
		return false, nil
	}
	if isLeaderTeamMember(member) {
		return false, nil
	}
	items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	workID := eventString(payload, "workId", "work_id", "assignmentId", "assignment_id")
	canonicalWorkID := "member-" + normalizeTeamMemberRouteKey(member.MemberKey)
	found := false
	for idx := range items {
		item := items[idx]
		if item.OwnerMemberID == nil || *item.OwnerMemberID != member.ID {
			continue
		}
		if workID != "" && item.WorkID != workID && item.WorkID != canonicalWorkID {
			continue
		}
		found = true
		if !isTerminalTeamTaskStatus(item.Status) {
			return false, nil
		}
	}
	return found, nil
}

func isLeaderControlPlaneSnapshotTask(task *models.TeamTask, payload map[string]interface{}) bool {
	if payload != nil {
		if strings.TrimSpace(eventString(payload, "intent")) == initialLeaderTaskIntent {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(eventString(payload, "origin")), "system_bootstrap") {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(eventString(payload, "executionMode", "execution_mode")), "leader_control_plane_snapshot") {
			return true
		}
	}
	if task == nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(task.MessageID)), "bootstrap-introduction")
}

func shouldAssociateEventWithCurrentMemberTask(eventType string, payload map[string]interface{}) bool {
	switch eventType {
	case "reply", "completion", "task_completed", "task_failed", "message_failed", "message_warning", "task_started", "task_progress", "progress":
		return true
	case "outbound", "task_assigned", "team_send", "peer_request", "peer_handoff", "peer_review_request", "peer_reply":
		return true
	}
	if eventString(payload, "taskId", "task_id", "runtimeTaskId", "runtime_task_id", "messageId", "message_id") != "" {
		return true
	}
	return teamEventHasBody(payload)
}

func isNonAuthoritativeDispatchFailure(eventType string, payload map[string]interface{}) bool {
	if eventType != "task_failed" && eventType != "message_failed" {
		return false
	}
	text := strings.ToLower(strings.Join(strings.Fields(strings.Join([]string{
		eventString(payload, "error_message", "error", "reason", "diagnostic", "lastSummary", "last_summary", "summary", "text", "message"),
	}, " ")), " "))
	if text == "redis team task failed" {
		return true
	}
	return strings.Contains(text, "dispatch finished without reply/completion") ||
		strings.Contains(text, "without reply/completion")
}

func isNonAuthoritativeDispatchWarning(eventType string, payload map[string]interface{}) bool {
	return eventType == "message_warning" && isNonAuthoritativeDispatchFailure(eventString(payload, "originalEvent"), payload)
}

func isLeaderMediatedLeaderDispatchOnlyCompletion(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask, completion bool) bool {
	return completion && isLeaderMediatedLeaderDispatchOnlyMessage(team, eventType, payload, member, task)
}

func isLeaderMediatedLeaderDispatchOnlyMessage(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) bool {
	if team == nil || payload == nil || member == nil || task == nil {
		return false
	}
	if normalizedTeamCommunicationMode(team.CommunicationMode) != teamCommunicationModeLeaderMediated {
		return false
	}
	if member.ID != task.TargetMemberID || !isLeaderTeamMember(member) {
		return false
	}
	if strings.TrimSpace(eventString(payload, "intent")) == initialLeaderTaskIntent {
		return false
	}
	if eventBool(payload, "rootTaskTerminal", "root_task_terminal", "finalSynthesis", "final_synthesis") {
		return false
	}
	return looksLikeLeaderDispatchOnlyText(eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary"))
}

func (s *teamService) leaderMediatedRootCompletionReady(team *models.Team, task *models.TeamTask, member *models.TeamMember) (bool, error) {
	if !isLeaderMediatedTeam(team) || task == nil || member == nil || member.ID != task.TargetMemberID || !isLeaderTeamMember(member) {
		return true, nil
	}
	if isLeaderControlPlaneSnapshotTask(task, nil) {
		return true, nil
	}
	workItems, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	requiredOwners := map[int]struct{}{}
	deliveredOwners := map[int]struct{}{}
	for idx := range workItems {
		item := workItems[idx]
		if item.OwnerMemberID == nil || *item.OwnerMemberID == member.ID || item.WorkID == "leader-final-synthesis" {
			continue
		}
		ownerID := *item.OwnerMemberID
		requiredOwners[ownerID] = struct{}{}
		if item.Status == models.TeamTaskStatusSucceeded {
			deliveredOwners[ownerID] = struct{}{}
		}
	}
	if len(requiredOwners) > 0 {
		for ownerID := range requiredOwners {
			if _, ok := deliveredOwners[ownerID]; !ok {
				return false, nil
			}
		}
		return true, nil
	}
	events, err := s.repo.ListEventsByTeamID(team.ID, 500)
	if err != nil {
		return false, err
	}
	required := map[string]struct{}{}
	delivered := map[string]struct{}{}
	for idx := range events {
		event := events[idx]
		payload := teamEventPayloadMap(event)
		if !teamEventMatchesRootTask(event, payload, task) {
			continue
		}
		step, _ := payload["collaborationStep"].(map[string]interface{})
		stepType := eventString(step, "type")
		actor := normalizeTeamMemberRouteKey(eventString(step, "actor"))
		target := normalizeTeamMemberRouteKey(eventString(step, "target"))
		if actor == "" && event.MemberID != nil {
			// The event table keeps member_id numeric; fall back to payload fields only here.
			actor = normalizeTeamMemberRouteKey(eventString(payload, "memberId", "member_id", "from", "sourceMemberId", "senderMemberId"))
		}
		if target == "" {
			target = normalizeTeamMemberRouteKey(leaderMediatedRouteTarget(payload))
		}
		if stepType == "assignment" && isLeaderRouteTarget(actor) && target != "" && !isLeaderRouteTarget(target) {
			required[target] = struct{}{}
			continue
		}
		if eventBool(payload, "assignmentResultOnly", "assignment_result_only") && actor != "" && !isLeaderRouteTarget(actor) {
			delivered[actor] = struct{}{}
			continue
		}
		if stepType == "result" && actor != "" && !isLeaderRouteTarget(actor) && (target == "" || isLeaderRouteTarget(target)) {
			delivered[actor] = struct{}{}
		}
	}
	if len(required) == 0 {
		return true, nil
	}
	for memberKey := range required {
		if !leaderMediatedDeliveredForRequiredMember(memberKey, delivered) {
			return false, nil
		}
	}
	return true, nil
}

type teamCompletionEvaluation struct {
	Decision             string
	Reason               string
	PendingAssignments   []string
	PendingPhases        []string
	WaivedAssignments    []string
	SkippedAssignments   []string
	StrongContradictions []string
	LedgerVersion        int64
	PlanVersion          int64
}

type teamCompletionWaiver struct {
	AssignmentID string
	Reason       string
	Risk         string
}

func structuredTeamCompletionWaivers(payload map[string]interface{}) map[string]teamCompletionWaiver {
	result := map[string]teamCompletionWaiver{}
	raw := firstTeamValue(payload, "waivers", "assignmentWaivers", "assignment_waivers")
	values, ok := raw.([]interface{})
	if !ok {
		return result
	}
	for _, value := range values {
		entry, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		waiver := teamCompletionWaiver{
			AssignmentID: eventString(entry, "assignmentId", "assignment_id", "workId", "work_id"),
			Reason:       eventString(entry, "reason", "waiverReason", "waiver_reason"),
			Risk:         eventString(entry, "risk", "riskAccepted", "risk_accepted"),
		}
		if waiver.AssignmentID != "" && waiver.Reason != "" && waiver.Risk != "" {
			result[waiver.AssignmentID] = waiver
		}
	}
	return result
}

func structuredTeamSkippedAssignments(payload map[string]interface{}) map[string]string {
	result := map[string]string{}
	raw := firstTeamValue(payload, "skippedAssignments", "skipped_assignments")
	values, ok := raw.([]interface{})
	if !ok {
		return result
	}
	for _, value := range values {
		entry, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		assignmentID := eventString(entry, "assignmentId", "assignment_id", "workId", "work_id")
		reason := eventString(entry, "reason", "skipReason", "skip_reason")
		if assignmentID != "" && reason != "" {
			result[assignmentID] = reason
		}
	}
	return result
}

func acceptedTeamCompletionEvaluation(task *models.TeamTask) teamCompletionEvaluation {
	result := teamCompletionEvaluation{Decision: teamCompletionDecisionAccepted}
	if task != nil {
		result.LedgerVersion = task.LedgerVersion
		result.PlanVersion = task.PlanVersion
	}
	return result
}

func (s *teamService) evaluateLeaderRootCompletion(team *models.Team, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}) (teamCompletionEvaluation, error) {
	if !isLeaderMediatedTeam(team) || task == nil || member == nil || payload == nil || isLeaderControlPlaneSnapshotTask(task, payload) {
		return acceptedTeamCompletionEvaluation(task), nil
	}
	result := acceptedTeamCompletionEvaluation(task)
	if member.ID != task.TargetMemberID || !isLeaderTeamMember(member) {
		result.Decision = teamCompletionDecisionRejected
		result.Reason = "invalid_root_completion_owner"
		return result, nil
	}

	protocolVersion := teamRedisProtocolVersion(payload)
	if protocolVersion >= 2 && (!isExplicitTeamTaskCompletion(payload) || !eventBool(payload, "rootTaskTerminal", "root_task_terminal")) {
		result.Decision = teamCompletionDecisionRejected
		result.Reason = "invalid_completion_envelope"
		return result, nil
	}
	proposalPlanVersion := int64(eventInt(payload, "planVersion", "plan_version"))
	proposalLedgerVersion := int64(eventInt(payload, "ledgerVersion", "ledger_version"))
	if proposalPlanVersion > 0 && task.PlanVersion > 0 && proposalPlanVersion != task.PlanVersion {
		result.Decision = teamCompletionDecisionDeferred
		result.Reason = "stale_plan"
		return result, nil
	}
	if proposalLedgerVersion > 0 && task.LedgerVersion > 0 && proposalLedgerVersion != task.LedgerVersion {
		result.Decision = teamCompletionDecisionDeferred
		result.Reason = "stale_ledger"
		return result, nil
	}

	items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return result, err
	}
	if len(items) == 0 && protocolVersion < 3 {
		legacyReady, legacyErr := s.leaderMediatedRootCompletionReady(team, task, member)
		if legacyErr != nil {
			return result, legacyErr
		}
		if !legacyReady {
			result.Decision = teamCompletionDecisionDeferred
			result.Reason = "pending_legacy_assignments"
			return result, nil
		}
	}
	byBusinessID := make(map[string]models.TeamWorkItem, len(items))
	waivers := structuredTeamCompletionWaivers(payload)
	skippedAssignments := structuredTeamSkippedAssignments(payload)
	for idx := range items {
		item := items[idx]
		key := strings.TrimSpace(derefTeamString(item.AssignmentID))
		if key == "" {
			key = strings.TrimSpace(item.WorkID)
		}
		if key != "" {
			if current, ok := byBusinessID[key]; !ok || item.Revision >= current.Revision {
				byBusinessID[key] = item
			}
		}
	}
	for key, item := range byBusinessID {
		if item.OwnerMemberID == nil || *item.OwnerMemberID == member.ID || item.WorkID == "leader-final-synthesis" || item.SupersededBy != nil {
			continue
		}
		required := item.RequiredForRoot || item.AssignmentID == nil
		if !required {
			if item.Status != models.TeamTaskStatusSucceeded {
				if skippedAssignments[key] == "" {
					result.PendingAssignments = append(result.PendingAssignments, key+":skip_reason")
				} else {
					result.SkippedAssignments = append(result.SkippedAssignments, key)
				}
			}
			continue
		}
		if item.Status != models.TeamTaskStatusSucceeded {
			if (item.Status == models.TeamTaskStatusFailed || item.Status == models.TeamTaskStatusStale) && waivers[key].AssignmentID != "" {
				result.WaivedAssignments = append(result.WaivedAssignments, key)
				continue
			}
			result.PendingAssignments = append(result.PendingAssignments, key)
			continue
		}
		if item.ReviewRequired && (item.ValidatedRevision == nil || *item.ValidatedRevision < teamMaxInt(item.Revision, 1)) {
			result.PendingAssignments = append(result.PendingAssignments, key+":review")
			continue
		}
		for _, dependency := range teamWorkItemDependencies(item) {
			dependencyItem, ok := byBusinessID[dependency]
			if !ok || dependencyItem.Status != models.TeamTaskStatusSucceeded {
				result.PendingAssignments = append(result.PendingAssignments, key+":depends_on:"+dependency)
			}
		}
	}
	sort.Strings(result.PendingAssignments)
	result.PendingAssignments = uniqueTeamStrings(result.PendingAssignments)
	sort.Strings(result.WaivedAssignments)
	result.WaivedAssignments = uniqueTeamStrings(result.WaivedAssignments)
	sort.Strings(result.SkippedAssignments)
	result.SkippedAssignments = uniqueTeamStrings(result.SkippedAssignments)
	if len(result.PendingAssignments) > 0 {
		result.Decision = teamCompletionDecisionDeferred
		result.Reason = "pending_assignments"
		return result, nil
	}

	phases, err := s.repo.ListWorkflowPhasesByRootTaskID(task.ID)
	if err != nil {
		return result, err
	}
	latestPlanVersion := task.PlanVersion
	if latestPlanVersion <= 0 {
		for idx := range phases {
			if phases[idx].PlanVersion > latestPlanVersion {
				latestPlanVersion = phases[idx].PlanVersion
			}
		}
	}
	workflowFinal := eventBool(payload, "workflowFinal", "workflow_final", "sealWorkflow", "seal_workflow")
	finalAnswerReady := eventBool(payload, "finalAnswerReady", "final_answer_ready")
	remainingActions := normalizeContextRefs(firstTeamValue(payload, "remainingActions", "remaining_actions", "nextActions", "next_actions"))
	if protocolVersion >= 3 && (!workflowFinal || !finalAnswerReady || len(remainingActions) > 0) {
		result.Decision = teamCompletionDecisionDeferred
		result.Reason = "workflow_not_sealed"
		result.PendingAssignments = append(result.PendingAssignments, remainingActions...)
		return result, nil
	}
	for idx := range phases {
		phase := phases[idx]
		if latestPlanVersion > 0 && phase.PlanVersion != latestPlanVersion {
			continue
		}
		if !phase.RequiredForRoot || phase.Status == teamPhaseStatusCompleted || phase.Status == teamPhaseStatusCancelled || phase.Status == teamPhaseStatusSuperseded {
			continue
		}
		// The phase ledger is a projection, not an additional source of work.  A
		// planned phase with no dispatched required work must never keep an
		// explicitly sealed workflow open (Team 58 was stuck exactly this way).
		// Actual current work items and their dependencies were checked above.
		if phaseHasIncompleteRequiredWork(phase.PhaseID, byBusinessID, member.ID, waivers) {
			result.PendingPhases = append(result.PendingPhases, phase.PhaseID)
			continue
		}
		if phase.DecisionRequired && !workflowFinal {
			result.PendingPhases = append(result.PendingPhases, phase.PhaseID+":leader_decision")
		}
	}
	sort.Strings(result.PendingPhases)
	result.PendingPhases = uniqueTeamStrings(result.PendingPhases)
	if len(result.PendingPhases) > 0 {
		result.Decision = teamCompletionDecisionDeferred
		result.Reason = "open_workflow_phases"
		return result, nil
	}

	result.StrongContradictions = analyzeCompletionNarrativeContradictions(payload)
	if len(result.StrongContradictions) > 0 && !eventBool(payload, "confirmFinal", "confirm_final") {
		result.Decision = teamCompletionDecisionNeedsConfirmation
		result.Reason = "narrative_indicates_remaining_work"
		return result, nil
	}
	return result, nil
}

func firstTeamValue(payload map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := payload[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func teamWorkItemDependencies(item models.TeamWorkItem) []string {
	if item.DependsOnJSON == nil || strings.TrimSpace(*item.DependsOnJSON) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(*item.DependsOnJSON), &values); err != nil {
		return nil
	}
	return uniqueTeamStrings(values)
}

func uniqueTeamStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func teamMaxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (s *teamService) invalidateModifiedTeamArtifactReviews(task *models.TeamTask, payload map[string]interface{}, now time.Time) (bool, error) {
	if s == nil || s.repo == nil || task == nil || payload == nil ||
		!(eventBool(payload, "artifactChanged", "artifact_changed") || strings.EqualFold(eventString(payload, "eventKind", "event_kind"), "artifact_changed")) {
		return false, nil
	}
	changedRefs := explicitTeamArtifactReferences(payload)
	if len(changedRefs) == 0 {
		return false, nil
	}
	changedSet := make(map[string]struct{}, len(changedRefs))
	for _, ref := range changedRefs {
		changedSet[strings.TrimSpace(ref)] = struct{}{}
	}
	items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	invalidated := false
	for idx := range items {
		item := items[idx]
		if item.ID <= 0 || item.SupersededBy != nil || !item.ReviewRequired || item.ValidatedRevision == nil {
			continue
		}
		matches := false
		for _, ref := range workItemArtifactRefs(item) {
			if _, ok := changedSet[strings.TrimSpace(ref)]; ok {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		if err := s.repo.InvalidateWorkItemReview(item.ID, now); err != nil {
			return invalidated, err
		}
		invalidated = true
	}
	if invalidated {
		task.LedgerVersion++
		task.UpdatedAt = now
	}
	return invalidated, nil
}

func phaseHasIncompleteRequiredWork(phaseID string, items map[string]models.TeamWorkItem, leaderID int, waivers map[string]teamCompletionWaiver) bool {
	for key, item := range items {
		if strings.TrimSpace(derefTeamString(item.PhaseID)) != strings.TrimSpace(phaseID) || item.SupersededBy != nil {
			continue
		}
		if item.OwnerMemberID == nil || *item.OwnerMemberID == leaderID {
			continue
		}
		if !(item.RequiredForRoot || item.AssignmentID == nil) {
			continue
		}
		if (item.Status == models.TeamTaskStatusFailed || item.Status == models.TeamTaskStatusStale) && waivers[key].AssignmentID != "" {
			continue
		}
		if item.Status != models.TeamTaskStatusSucceeded {
			return true
		}
	}
	return false
}

// reconcileTeamWorkflowLedger repairs the derived phase view from the current
// work-item ledger. It deliberately does not invent or complete work: a phase
// moves to completed only when all of its actual required assignments succeeded.
// A future planned phase without assignments remains planned until the Leader
// explicitly seals the workflow, at which point it is cancelled as unused.
func (s *teamService) reconcileTeamWorkflowLedger(task *models.TeamTask, workflowFinal bool, now time.Time) (bool, error) {
	if s == nil || s.repo == nil || task == nil || task.ID <= 0 || isTerminalTeamTaskStatus(task.Status) {
		return false, nil
	}
	phases, err := s.repo.ListWorkflowPhasesByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	latestPlanVersion := task.PlanVersion
	if latestPlanVersion <= 0 {
		for _, phase := range phases {
			if phase.PlanVersion > latestPlanVersion {
				latestPlanVersion = phase.PlanVersion
			}
		}
	}
	if latestPlanVersion <= 0 {
		return false, nil
	}

	latestItems := map[string]models.TeamWorkItem{}
	for _, item := range items {
		if item.SupersededBy != nil {
			continue
		}
		key := strings.TrimSpace(derefTeamString(item.AssignmentID))
		if key == "" {
			key = strings.TrimSpace(item.WorkID)
		}
		if key == "" {
			continue
		}
		if current, ok := latestItems[key]; !ok || teamMaxInt(item.Revision, 1) >= teamMaxInt(current.Revision, 1) {
			latestItems[key] = item
		}
	}

	changed := false
	anyIncomplete := false
	anyDecision := false
	var currentPhase string
	for idx := range phases {
		phase := phases[idx]
		if phase.PlanVersion != latestPlanVersion || !phase.RequiredForRoot ||
			phase.Status == teamPhaseStatusCancelled || phase.Status == teamPhaseStatusSuperseded || phase.Status == teamPhaseStatusCompleted {
			continue
		}
		phaseHasWork := false
		phaseComplete := true
		for _, item := range latestItems {
			if strings.TrimSpace(derefTeamString(item.PhaseID)) != strings.TrimSpace(phase.PhaseID) ||
				item.OwnerMemberID == nil || *item.OwnerMemberID == task.TargetMemberID ||
				!(item.RequiredForRoot || item.AssignmentID == nil) {
				continue
			}
			phaseHasWork = true
			if item.Status != models.TeamTaskStatusSucceeded {
				phaseComplete = false
			}
		}
		previousStatus := phase.Status
		switch {
		case !phaseHasWork && workflowFinal && phase.Status == teamPhaseStatusPlanned:
			phase.Status = teamPhaseStatusCancelled
		case phaseHasWork && phaseComplete && phase.DecisionRequired && !workflowFinal:
			phase.Status = teamPhaseStatusAwaitingLeaderDecision
			anyDecision = true
		case phaseHasWork && phaseComplete:
			phase.Status = teamPhaseStatusCompleted
			phase.CompletedAt = &now
		case phaseHasWork:
			phase.Status = teamPhaseStatusAwaitingResults
			anyIncomplete = true
			currentPhase = phase.PhaseID
		case phase.Status == teamPhaseStatusAwaitingLeaderDecision:
			anyDecision = true
		}
		if phase.Status != previousStatus {
			phase.UpdatedAt = now
			if err := s.repo.UpsertWorkflowPhase(&phase); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	newState := task.WorkflowState
	switch {
	case anyIncomplete:
		newState = teamWorkflowStateAwaitingPhaseResults
	case anyDecision:
		newState = teamWorkflowStateAwaitingLeaderDecision
	case workflowFinal:
		newState = teamWorkflowStateSynthesizing
	default:
		newState = teamWorkflowStateAwaitingLeaderDecision
	}
	if task.WorkflowState != newState {
		task.WorkflowState = newState
		changed = true
	}
	if currentPhase == "" && !anyIncomplete {
		if task.CurrentPhaseID != nil {
			task.CurrentPhaseID = nil
			changed = true
		}
	} else if currentPhase != "" && (task.CurrentPhaseID == nil || *task.CurrentPhaseID != currentPhase) {
		task.CurrentPhaseID = &currentPhase
		changed = true
	}
	if changed {
		task.LedgerVersion++
		task.UpdatedAt = now
		if err := s.repo.UpdateTask(task); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func analyzeCompletionNarrativeContradictions(payload map[string]interface{}) []string {
	if payload == nil {
		return nil
	}
	texts := []string{
		eventString(payload, "summary"),
		eventString(payload, "resultMarkdown", "result_markdown", "result", "answer"),
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		texts = append(texts, eventString(step, "summary"), eventString(step, "content", "detail"))
	}
	patterns := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"waiting_for_member", regexp.MustCompile(`(?i)(仍在等待|继续等待|等待\s*(pm|architect|designer|developer|reviewer|worker)|still waiting|waiting for\s+(pm|architect|designer|developer|reviewer|worker))`)},
		{"future_dispatch", regexp.MustCompile(`(?i)((下一步|接下来|随后|然后).{0,10}(将|需要|必须|准备|继续).{0,24}(派发|下发|交给|发送给|实现|评审|验证)|(next|then).{0,12}(will|must|need to|going to|continue to).{0,24}(dispatch|assign|send to|implement|review|verify))`)},
		{"phase_not_final", regexp.MustCompile(`(?i)(完成|结束|汇总).{0,12}(第一阶段|阶段一|phase\s*1).{0,30}(继续|下一阶段|phase\s*2)`)},
	}
	result := []string{}
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		for _, candidate := range patterns {
			if candidate.pattern.MatchString(text) {
				result = append(result, candidate.name)
			}
		}
	}
	return uniqueTeamStrings(result)
}

func leaderMediatedDeliveredForRequiredMember(required string, delivered map[string]struct{}) bool {
	if _, ok := delivered[required]; ok {
		return true
	}
	for candidate := range delivered {
		if teamMemberRouteEquivalent(required, candidate) {
			return true
		}
	}
	return false
}

func teamMemberRouteEquivalent(a, b string) bool {
	left := normalizeTeamMemberRouteKey(a)
	right := normalizeTeamMemberRouteKey(b)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	aliasGroups := [][]string{
		{"pm", "product-manager", "product"},
		{"designer", "ui-designer", "ui-ux-designer", "ux-designer"},
		{"architect", "solution-architect", "software-architect"},
		{"developer", "worker", "senior-developer", "frontend-developer", "backend-developer"},
	}
	for _, group := range aliasGroups {
		leftMatch := false
		rightMatch := false
		for _, alias := range group {
			if left == alias || strings.Contains(left, "-"+alias) || strings.Contains(left, alias+"-") {
				leftMatch = true
			}
			if right == alias || strings.Contains(right, "-"+alias) || strings.Contains(right, alias+"-") {
				rightMatch = true
			}
		}
		if leftMatch && rightMatch {
			return true
		}
	}
	return false
}

func markLeaderMediatedPrematureCompletion(eventType string, payload map[string]interface{}) string {
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	payload["event"] = "reply"
	payload["type"] = "reply"
	payload["status"] = models.TeamTaskStatusRunning
	payload["runtimeStatus"] = models.TeamTaskStatusRunning
	payload["availability"] = models.TeamMemberAvailabilityBusy
	payload["rootTaskTerminal"] = false
	payload["leaderPrematureCompletion"] = true
	if eventString(payload, "summary") == "" {
		payload["summary"] = "Leader is waiting for assigned member results before final synthesis."
	}
	return "reply"
}

func markStructuredCompletionDecision(eventType string, payload map[string]interface{}, evaluation teamCompletionEvaluation) string {
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	decisionType := "completion_" + evaluation.Decision
	payload["event"] = decisionType
	payload["type"] = decisionType
	payload["completionDecision"] = evaluation.Decision
	payload["completionDecisionReason"] = evaluation.Reason
	payload["pendingAssignments"] = evaluation.PendingAssignments
	payload["pendingPhases"] = evaluation.PendingPhases
	payload["waivedAssignments"] = evaluation.WaivedAssignments
	payload["skippedAssignmentsAccepted"] = evaluation.SkippedAssignments
	payload["strongNarrativeContradictions"] = evaluation.StrongContradictions
	payload["ledgerVersion"] = evaluation.LedgerVersion
	payload["planVersion"] = evaluation.PlanVersion
	payload["status"] = models.TeamTaskStatusRunning
	payload["runtimeStatus"] = models.TeamTaskStatusRunning
	payload["availability"] = models.TeamMemberAvailabilityBusy
	payload["rootTaskTerminal"] = false
	payload["completionProposalPreserved"] = true
	// A deferred completion is business information: it contains the Leader's
	// delivery proposal plus the exact reason the system did not accept it.
	// Keep it in the group chat; only the transport ACK is hidden.
	payload["visibleToChat"] = true
	payload["visible_to_chat"] = true
	payload["chatPolicy"] = "warning"
	payload["chatKind"] = "completion_" + evaluation.Decision
	if completionID := eventString(payload, "completionId", "completion_id"); completionID != "" {
		payload["displayKey"] = fmt.Sprintf("completion:%s:%d", completionID, evaluation.LedgerVersion)
	}
	return decisionType
}

func applyTeamChatPolicy(eventType string, payload map[string]interface{}, task *models.TeamTask, member *models.TeamMember) {
	if payload == nil {
		return
	}
	normalizedEvent := strings.ToLower(strings.TrimSpace(eventType))
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	// Runtime-provided visibility is advisory. A real plan, assignment,
	// narrative, delivery, review, or synthesis must not be hidden simply
	// because an older adapter labelled it as transport traffic.
	if teamChatEventIsBusinessContent(normalizedEvent, eventKind, payload) {
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		payload["visible_to_chat"] = true
		if eventKind == "assignment_check_result" {
			payload["chatBusinessKind"] = "worker_progress"
		}
		if eventString(payload, "chatKind", "chat_kind") != "final_delivery" &&
			eventString(payload, "completionDecision", "completion_decision") != teamCompletionDecisionAccepted {
			// Legacy display keys can be shared by multiple workers when the
			// assignment id was absent. Let content-based dedupe handle business
			// messages instead of suppressing a different worker's narrative.
			delete(payload, "displayKey")
			delete(payload, "display_key")
		}
		return
	}
	if eventString(payload, "chatPolicy", "chat_policy") != "" {
		return
	}
	rootKey := eventString(payload, "rootTaskId", "root_task_id")
	if rootKey == "" && task != nil {
		rootKey = fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	}
	assignmentID := teamChatAssignmentIdentity(payload)
	actor := eventString(payload, "from", "memberId", "member_id")
	if actor == "" && member != nil {
		actor = member.MemberKey
	}
	displayKey := ""
	switch {
	case normalizedEvent == "member_result_confirmed":
		payload["chatPolicy"] = "hidden"
		payload["visibleToChat"] = false
		return
	case eventKind == "assignment_heartbeat" || normalizedEvent == "assignment_heartbeat" || eventKind == "assignment_check_requested" || normalizedEvent == "assignment_check_requested":
		payload["chatPolicy"] = "digest"
		payload["visibleToChat"] = false
		return
	case eventKind == "assignment_check_result" || normalizedEvent == "assignment_check_result":
		if teamMonitorEventHasBusinessChange(payload) {
			payload["chatPolicy"] = "visible"
			payload["chatBusinessKind"] = "worker_progress"
			payload["visibleToChat"] = true
		} else {
			payload["chatPolicy"] = "digest"
			payload["visibleToChat"] = false
			return
		}
	case eventKind == "leader_plan" || normalizedEvent == "leader_plan":
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		displayKey = "leader-plan:" + rootKey + ":" + strconv.FormatInt(int64(eventInt(payload, "planVersion", "plan_version")), 10)
	case eventKind == "worker_plan" || normalizedEvent == "worker_plan":
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
	case eventKind == "worker_progress" || normalizedEvent == "worker_progress":
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
	case eventKind == "artifact_changed" || normalizedEvent == "artifact_changed":
		payload["chatPolicy"] = "visible"
		payload["chatBusinessKind"] = "worker_progress"
		payload["visibleToChat"] = true
	case eventBool(payload, "leaderDispatchOnly", "leader_dispatch_only") || normalizedEvent == "team_send" || normalizedEvent == "task_assigned" || normalizedEvent == "outbound":
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		displayKey = "assignment:" + rootKey + ":" + assignmentID
	case eventBool(payload, "assignmentResultOnly", "assignment_result_only"):
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		displayKey = "worker-result:" + rootKey + ":" + assignmentID + ":" + strconv.Itoa(teamMaxInt(eventInt(payload, "revision"), 1))
	case normalizedEvent == "task_completed" && eventString(payload, "completionDecision", "completion_decision") == teamCompletionDecisionAccepted:
		payload["chatPolicy"] = "visible"
		payload["visibleToChat"] = true
		displayKey = "root-final:" + rootKey
	case normalizedEvent == "message_warning" || strings.Contains(normalizedEvent, "rejected") || strings.Contains(normalizedEvent, "needs_confirmation"):
		payload["chatPolicy"] = "warning"
		payload["visibleToChat"] = true
	case normalizedEvent == "task_received" || normalizedEvent == "task_started" || normalizedEvent == "presence":
		payload["chatPolicy"] = "hidden"
		payload["visibleToChat"] = false
	}
	if displayKey != "" {
		payload["displayKey"] = displayKey
	}
}

func teamChatAssignmentIdentity(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	if identity := eventString(payload, "assignmentId", "assignment_id", "canonicalWorkId", "canonical_work_id", "workId", "work_id"); identity != "" {
		return identity
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		if identity := eventString(step, "assignmentId", "assignment_id", "workId", "work_id", "id"); identity != "" {
			return identity
		}
	}
	actor := eventString(payload, "from", "memberId", "member_id")
	phase := eventString(payload, "phaseId", "phase_id", "phase", "stage")
	if actor != "" || phase != "" {
		return "member:" + normalizeTeamMemberRouteKey(actor) + ":phase:" + normalizeTeamRedisKeyPart(phase)
	}
	return ""
}

func teamChatEventIsBusinessContent(eventType, eventKind string, payload map[string]interface{}) bool {
	if payload == nil || teamChatEventIsTransportNoise(eventType, eventKind, payload) {
		return false
	}
	switch eventKind {
	case "leader_plan", "worker_plan", "worker_progress", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder",
		"agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis",
		"completion_deferred", "completion_candidate", "completion_validation_warning", "assignment_recovery_started", "assignment_reissued", "assignment_recovery_exhausted":
		return teamChatHasMeaningfulBody(payload)
	}
	if eventBool(payload, "leaderDispatchOnly", "leader_dispatch_only", "assignmentResultOnly", "assignment_result_only") {
		return teamChatHasMeaningfulBody(payload)
	}
	switch eventType {
	case "outbound", "team_send", "task_assigned", "peer_request", "peer_handoff", "peer_review_request", "peer_reply", "reply", "completion_proposed", "task_completed", "completion", "message_warning":
		return teamChatHasMeaningfulBody(payload)
	case "task_progress", "progress":
		return teamChatHasMeaningfulBody(payload)
	default:
		return false
	}
}

func teamChatEventIsTransportNoise(eventType, eventKind string, payload map[string]interface{}) bool {
	if eventType == "member_result_confirmed" || eventType == "inbound" {
		return true
	}
	switch eventKind {
	case "assignment_heartbeat", "assignment_check_requested":
		return true
	case "assignment_check_result":
		return !teamMonitorEventHasBusinessChange(payload)
	}
	switch eventType {
	case "assignment_heartbeat", "assignment_check_requested", "task_received", "task_started", "presence":
		return true
	}
	return false
}

func teamChatHasMeaningfulBody(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	values := []string{
		eventString(payload, "content", "detail", "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary"),
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		values = append(values, eventString(step, "content", "detail", "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary"))
	}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(strings.Join(strings.Fields(value), " ")))
		if normalized == "" {
			continue
		}
		switch normalized {
		case "task_received", "task_started", "redis team task received", "redis team task started", "redis team task processing completed", "agent turn is still running", "still running", "status unchanged", "no change":
			continue
		}
		return true
	}
	return false
}

func teamMonitorEventHasBusinessChange(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	if eventBool(payload, "blocked", "recovered", "artifactChanged", "artifact_changed", "phaseChanged", "phase_changed") ||
		eventInt(payload, "progress") > 0 || len(explicitTeamArtifactReferences(payload)) > 0 {
		return true
	}
	summary := strings.ToLower(strings.TrimSpace(eventString(payload, "summary", "detail", "message", "text")))
	if summary == "" {
		return false
	}
	for _, generic := range []string{"agent turn is still running", "still running", "status unchanged", "no change", "仍在执行", "状态无变化"} {
		if summary == generic {
			return false
		}
	}
	return len([]rune(strings.Join(strings.Fields(summary), ""))) >= 12
}

func buildCompletionAcknowledgement(team *models.Team, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}, decision, reason string) (map[string]interface{}, string, string) {
	completionID := eventString(payload, "completionId", "completion_id")
	attemptID := eventString(payload, "attemptId", "attempt_id")
	if attemptID == "" {
		attemptID = eventString(payload, "eventId", "event_id")
	}
	if attemptID == "" {
		attemptID = completionID
	}
	memberKey := "leader"
	if member != nil && strings.TrimSpace(member.MemberKey) != "" {
		memberKey = member.MemberKey
	}
	rootTaskID := ""
	ledgerVersion := int64(0)
	planVersion := int64(0)
	if task != nil {
		rootTaskID = fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
		ledgerVersion = task.LedgerVersion
		planVersion = task.PlanVersion
	}
	ack := map[string]interface{}{
		"v":                  1,
		"protocolVersion":    3,
		"event":              "completion_ack",
		"type":               "completion_ack",
		"intent":             "completion_ack",
		"teamId":             strconv.Itoa(team.ID),
		"memberId":           memberKey,
		"rootTaskId":         rootTaskID,
		"rootMessageId":      eventString(payload, "rootMessageId", "root_message_id"),
		"completionId":       completionID,
		"attemptId":          attemptID,
		"decision":           decision,
		"reason":             reason,
		"pendingAssignments": firstTeamValue(payload, "pendingAssignments", "pending_assignments"),
		"pendingPhases":      firstTeamValue(payload, "pendingPhases", "pending_phases"),
		"ledgerVersion":      ledgerVersion,
		"planVersion":        planVersion,
		"requiresCompletion": false,
		"visibleToChat":      false,
		"chatPolicy":         "hidden",
		"acknowledgedAt":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return ack, completionID, attemptID
}

func (s *teamService) emitCompletionAcknowledgement(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}, decision, reason string) error {
	if team == nil || payload == nil {
		return nil
	}
	ack, completionID, attemptID := buildCompletionAcknowledgement(team, task, member, payload, decision, reason)
	if completionID == "" || attemptID == "" || bus == nil {
		return nil
	}
	encoded, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	if err := bus.Set(context.Background(), teamCompletionAckKey(team.ID, completionID, attemptID), string(encoded), 24*time.Hour); err != nil {
		return err
	}
	return bus.Set(context.Background(), teamCompletionStateKey(team.ID, completionID), string(encoded), 7*24*time.Hour)
}

func (s *teamService) completeWorkflowPhases(task *models.TeamTask, now time.Time) error {
	if s == nil || task == nil || task.ID <= 0 {
		return nil
	}
	phases, err := s.repo.ListWorkflowPhasesByRootTaskID(task.ID)
	if err != nil {
		return err
	}
	for idx := range phases {
		phase := phases[idx]
		if task.PlanVersion > 0 && phase.PlanVersion != task.PlanVersion {
			continue
		}
		if phase.Status == teamPhaseStatusCancelled || phase.Status == teamPhaseStatusSuperseded {
			continue
		}
		phase.Status = teamPhaseStatusCompleted
		phase.CompletedAt = &now
		phase.UpdatedAt = now
		if err := s.repo.UpsertWorkflowPhase(&phase); err != nil {
			return err
		}
	}
	return nil
}

func completionAcknowledgementOutbox(team *models.Team, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}, decision, reason, sourceEventID string, now time.Time) (*models.TeamEventOutbox, error) {
	ack, completionID, attemptID := buildCompletionAcknowledgement(team, task, member, payload, decision, reason)
	if completionID == "" || attemptID == "" {
		return nil, fmt.Errorf("completion acknowledgement requires completionId and attemptId")
	}
	encoded, err := json.Marshal(ack)
	if err != nil {
		return nil, err
	}
	memberKey := "leader"
	if member != nil && strings.TrimSpace(member.MemberKey) != "" {
		memberKey = member.MemberKey
	}
	return &models.TeamEventOutbox{
		TeamID:        team.ID,
		SourceEventID: sourceEventID,
		Destination:   teamCompletionAckStreamKey(team.ID, memberKey),
		MessageID:     fmt.Sprintf("completion-ack:%s:%s", completionID, attemptID),
		PayloadJSON:   string(encoded),
		Status:        "pending",
		AvailableAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func isLeaderMediatedInterimRootCompletion(team *models.Team, task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}) bool {
	if !isLeaderMediatedTeam(team) || task == nil || member == nil || payload == nil {
		return false
	}
	if member.ID != task.TargetMemberID || !isLeaderTeamMember(member) {
		return false
	}
	if isLeaderControlPlaneSnapshotTask(task, payload) {
		return false
	}
	text := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary")
	if text == "" {
		return false
	}
	return isInterimOrDelegationReplyText(text)
}

func teamEventPayloadMap(event models.TeamEvent) map[string]interface{} {
	payload := map[string]interface{}{}
	if event.PayloadJSON != nil && strings.TrimSpace(*event.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(*event.PayloadJSON), &payload)
	}
	return payload
}

func teamEventMatchesRootTask(event models.TeamEvent, payload map[string]interface{}, task *models.TeamTask) bool {
	if task == nil {
		return false
	}
	if event.TaskID != nil && *event.TaskID == task.ID {
		return true
	}
	for _, key := range []string{"taskId", "task_id", "rootTaskId", "root_task_id", "parentTaskId", "parent_task_id", "currentTaskId", "current_task_id", "runtimeTaskId", "runtime_task_id"} {
		if parseClawManagerTeamTaskRef(task.TeamID, eventString(payload, key)) == task.ID {
			return true
		}
	}
	for _, key := range []string{"messageId", "message_id", "rootMessageId", "root_message_id", "parentMessageId", "parent_message_id", "inReplyTo", "in_reply_to", "replyTo", "reply_to"} {
		if task.MessageID != "" && eventString(payload, key) == task.MessageID {
			return true
		}
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		return teamEventMatchesRootTask(event, step, task)
	}
	return false
}

func normalizeTeamMemberRouteKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isLeaderTeamMember(member *models.TeamMember) bool {
	if member == nil {
		return false
	}
	normalizedKey := strings.ToLower(strings.TrimSpace(member.MemberKey))
	normalizedRole := strings.ToLower(strings.TrimSpace(member.Role))
	return normalizedKey == "leader" || strings.Contains(normalizedKey, "leader") ||
		normalizedRole == "leader" || strings.Contains(normalizedRole, "leader")
}

func isLeaderMediatedTeam(team *models.Team) bool {
	return team != nil && normalizedTeamCommunicationMode(team.CommunicationMode) == teamCommunicationModeLeaderMediated
}

func leaderMediatedRouteTarget(payload map[string]interface{}) string {
	target := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id")
	if target != "" {
		return target
	}
	return eventString(payload, "assignee", "owner", "targetMember")
}

func isLeaderRouteTarget(target string) bool {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if normalized == "" {
		return false
	}
	return normalized == "leader" ||
		normalized == teamTaskReplyTarget ||
		strings.Contains(normalized, "leader") ||
		strings.Contains(normalized, "clawmanager")
}

func isLeaderMediatedOutboundLikeEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "outbound", "task_assigned", "team_send", "reply", "task_completed", "completion", "completion_proposed":
		return true
	default:
		return false
	}
}

func isLeaderMediatedInvalidWorkerRoute(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember) bool {
	if !isLeaderMediatedTeam(team) || payload == nil || member == nil || isLeaderTeamMember(member) {
		return false
	}
	if !isLeaderMediatedOutboundLikeEvent(eventType) {
		return false
	}
	target := leaderMediatedRouteTarget(payload)
	if target == "" {
		return false
	}
	return !isLeaderRouteTarget(target)
}

func markLeaderMediatedRouteViolation(eventType string, payload map[string]interface{}, member *models.TeamMember) string {
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	target := leaderMediatedRouteTarget(payload)
	payload["event"] = "message_warning"
	payload["type"] = "message_warning"
	payload["status"] = "warning"
	payload["runtimeStatus"] = "warning"
	payload["availability"] = models.TeamMemberAvailabilityIdle
	payload["leaderMediatedRouteViolation"] = true
	payload["nonAuthoritative"] = true
	payload["rootTaskTerminal"] = false
	payload["summary"] = "Leader-mediated mode ignores worker-to-worker/self routing; workers must return assignment results to the Leader."
	if target != "" {
		payload["target"] = target
		payload["to"] = target
	}
	if member != nil {
		payload["from"] = member.MemberKey
	}
	return "message_warning"
}

func isLeaderMediatedMonitorBlockerCandidate(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) bool {
	if !isLeaderMediatedTeam(team) || payload == nil || member == nil || task == nil || member.ID != task.TargetMemberID || !isLeaderTeamMember(member) {
		return false
	}
	if hasTeamTaskCompletionToolCall(payload) ||
		strings.EqualFold(strings.TrimSpace(eventString(payload, "completionSource", "completion_source")), teamTaskCompletionTool) ||
		eventBool(payload, "explicitCompletion", "explicit_completion", "rootTaskTerminal") {
		return false
	}
	if eventType != "task_failed" && eventType != "message_failed" {
		status := normalizedTeamTaskEventStatus(payload)
		if !isFailedTeamTaskEventStatus(status) {
			return false
		}
	}
	return looksLikeNonAuthoritativeMonitorBlockerText(eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary", "error_message", "error", "reason", "diagnostic", "lastSummary", "last_summary"))
}

func markLeaderMediatedMonitorBlockerCandidate(eventType string, payload map[string]interface{}) string {
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	payload["event"] = "message_warning"
	payload["type"] = "message_warning"
	payload["status"] = "attention_required"
	payload["runtimeStatus"] = models.TeamTaskStatusRunning
	payload["availability"] = models.TeamMemberAvailabilityBusy
	payload["rootTaskTerminal"] = false
	payload["nonAuthoritative"] = true
	payload["blockerCandidate"] = true
	payload["monitorBlockerCandidate"] = true
	if eventString(payload, "summary") == "" {
		payload["summary"] = "Leader monitor reported a possible blocker; waiting for member result or explicit Leader finalization."
	}
	return "message_warning"
}

func looksLikeNonAuthoritativeMonitorBlockerText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	lower := strings.ToLower(normalized)
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	if strings.Contains(lower, "monitoring agent") ||
		strings.Contains(lower, "status check") ||
		strings.Contains(lower, "status checks") ||
		strings.Contains(lower, "unresponsive") ||
		strings.Contains(lower, "no summary created") ||
		strings.Contains(lower, "zero paper files") ||
		strings.Contains(lower, "artifacts not found") ||
		strings.Contains(lower, "artifact not found") ||
		strings.Contains(lower, "blocker report sent") {
		return true
	}
	return strings.Contains(compact, "监控") ||
		strings.Contains(compact, "状态检查") ||
		strings.Contains(compact, "无响应") ||
		strings.Contains(compact, "未找到产物") ||
		strings.Contains(compact, "找不到产物")
}

func isLeaderMediatedWorkerToLeaderResult(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) bool {
	if !isLeaderMediatedTeam(team) || payload == nil || member == nil || task == nil || isLeaderTeamMember(member) {
		return false
	}
	if member.ID == task.TargetMemberID {
		return false
	}
	if isFailedTeamTaskEventStatus(normalizedTeamTaskEventStatus(payload)) {
		return false
	}
	if !isLeaderMediatedOutboundLikeEvent(eventType) || !teamEventHasBody(payload) {
		return false
	}
	if isNonAuthoritativeDispatchFailure(eventType, payload) ||
		isDispatchOnlyCompletionPayload(payload) ||
		eventBool(payload, "leaderMediatedRouteViolation") {
		return false
	}
	target := leaderMediatedRouteTarget(payload)
	if target == "" {
		target = eventString(payload, "replyTarget", "reply_target")
	}
	if target != "" && !isLeaderRouteTarget(target) {
		return false
	}
	if teamRedisProtocolVersion(payload) >= 2 && isExplicitTeamTaskCompletion(payload) {
		return true
	}
	body := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary")
	if looksLikeLeaderDispatchOnlyText(body) {
		return false
	}
	if looksLikeNonTerminalProgressText(body) {
		return false
	}
	return true
}

func markLeaderMediatedAssignmentResult(eventType string, payload map[string]interface{}, member *models.TeamMember) {
	payload["assignmentResultOnly"] = true
	payload["memberResultConfirmed"] = true
	payload["rootTaskTerminal"] = false
	payload["status"] = models.TeamTaskStatusSucceeded
	payload["runtimeStatus"] = models.TeamTaskStatusSucceeded
	payload["availability"] = models.TeamMemberAvailabilityIdle
	if eventString(payload, "normalizedResultSource") == "" {
		if teamRedisProtocolVersion(payload) >= 2 {
			payload["normalizedResultSource"] = "explicit_completion"
		} else {
			payload["normalizedResultSource"] = "legacy_normalized_reply"
		}
	}
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	if eventString(payload, "contentHash", "content_hash") == "" {
		payload["contentHash"] = teamResultContentHash(payload)
	}
	if member != nil {
		payload["from"] = member.MemberKey
		payload["memberId"] = member.MemberKey
	}
	if leaderMediatedRouteTarget(payload) == "" {
		payload["to"] = "leader"
		payload["target"] = "leader"
	}
}

func teamResultContentHash(payload map[string]interface{}) string {
	content := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary")
	normalized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	refs := explicitTeamArtifactReferences(payload)
	sort.Strings(refs)
	if normalized == "" && len(refs) == 0 {
		return ""
	}
	fingerprint := normalized + "\nrefs=" + strings.Join(refs, "|")
	digest := sha256.Sum256([]byte(fingerprint))
	return fmt.Sprintf("sha256:%x", digest[:])
}

func (s *teamService) reopenLeaderMediatedRootAfterMemberResult(team *models.Team, task *models.TeamTask, payload map[string]interface{}, now time.Time) error {
	if s == nil || task == nil || !isLeaderMediatedTeam(team) || !isTerminalTeamTaskStatus(task.Status) {
		return nil
	}
	if task.Status == models.TeamTaskStatusSucceeded {
		return nil
	}
	errText := derefTeamString(task.ErrorMessage)
	if errText == "" {
		errText = eventString(payload, "previousError", "previous_error")
	}
	if !looksLikeNonAuthoritativeMonitorBlockerText(errText) {
		return nil
	}
	task.Status = models.TeamTaskStatusRunning
	task.FinishedAt = nil
	task.ErrorMessage = nil
	task.UpdatedAt = now
	return s.repo.UpdateTask(task)
}

func looksLikeNonTerminalProgressText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	if looksLikeFinalResultText(normalized) ||
		containsAnyTeamTextMarker(normalized, []string{
			"final delivery",
			"final confirmation",
			"files are at",
			"qa verdict",
			"pass with",
			"pass:",
			"needs revision",
			"ready for review",
			"delivered",
			"交付",
			"最终",
			"已归档",
		}) {
		return false
	}
	lower := strings.ToLower(normalized)
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	if strings.Contains(lower, "progress update") ||
		strings.Contains(lower, "status update") ||
		strings.Contains(lower, "phase 1") ||
		strings.Contains(lower, "phase 2") ||
		strings.Contains(lower, "starting ") ||
		strings.Contains(lower, "now extracting") ||
		strings.Contains(lower, "now processing") ||
		strings.Contains(lower, "still working") ||
		strings.Contains(lower, "in progress") ||
		strings.Contains(lower, "currently ") ||
		strings.Contains(lower, "processing all") ||
		strings.Contains(lower, "downloaded successfully") ||
		strings.Contains(lower, "curating to") {
		return true
	}
	return strings.Contains(compact, "进度更新") ||
		strings.Contains(compact, "状态更新") ||
		strings.Contains(compact, "正在") ||
		strings.Contains(compact, "处理中") ||
		strings.Contains(compact, "继续执行") ||
		strings.Contains(compact, "阶段一") ||
		strings.Contains(compact, "阶段二")
}

func looksLikeLeaderDispatchOnlyText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	if containsAnyTeamTextMarker(normalized, finalResultTextMarkers()) {
		return false
	}
	if containsAnyTeamTextMarker(normalized, leaderDispatchTextMarkers()) {
		return true
	}
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	lower := strings.ToLower(normalized)
	if strings.Contains(normalized, "$CLAWMANAGER_TEAM_SHARED_DIR") ||
		(containsTeamTextMarker(normalized, "\u5171\u4eab\u76ee\u5f55") && containsTeamTextMarker(normalized, "\u89c4\u8303\u8def\u5f84")) ||
		(strings.Contains(lower, "shared directory") && strings.Contains(lower, "canonical path")) {
		return true
	}
	if containsTeamTextMarker(normalized, "\u5b8c\u6210\u540e") &&
		(containsTeamTextMarker(normalized, "\u56de\u4f20") || containsTeamTextMarker(normalized, "\u8fd4\u56de\u7ed9\u6211") || containsTeamTextMarker(normalized, "\u901a\u77e5\u6211") || containsTeamTextMarker(normalized, "\u4ea4\u4ed8\u7ed9\u6211")) {
		return true
	}
	if containsTeamTextMarker(normalized, "\u7528\u6237\u60f3\u8ba9\u4f60") && containsTeamTextMarker(normalized, "\u8bf7") {
		return true
	}
	for _, target := range []string{"pm", "designer", "ui-designer", "architect", "worker", "product-manager", "solution-architect"} {
		if strings.Contains(compact, target+"\u4f60\u597d") || strings.Contains(compact, "@"+target) {
			return true
		}
	}
	if strings.Contains(lower, "please ") &&
		(strings.Contains(lower, "write") || strings.Contains(lower, "complete") || strings.Contains(lower, "return") || strings.Contains(lower, "report back")) &&
		(strings.Contains(lower, "designer") || strings.Contains(lower, "pm") || strings.Contains(lower, "architect") || strings.Contains(lower, "worker")) {
		return true
	}
	return false
}

func looksLikeFinalResultText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	return containsAnyTeamTextMarker(normalized, finalResultTextMarkers())
}

func finalResultTextMarkers() []string {
	return []string{
		"\u4efb\u52a1\u7ed3\u679c\u53cd\u9988",
		"\u4efb\u52a1\u8f93\u51fa",
		"\u6700\u7ec8\u56de\u7b54",
		"\u6700\u7ec8\u65b9\u6848",
		"\u6700\u7ec8\u603b\u7ed3",
		"\u6700\u7ec8\u4ea4\u4ed8",
		"\u6700\u7ec8\u4ea4\u4ed8\u62a5\u544a",
		"\u6c47\u603b\u5982\u4e0b",
		"\u7ed3\u679c\u5982\u4e0b",
		"\u5df2\u5b8c\u6210\u5e76",
		"\u5df2\u5b8c\u6210\uff0c",
		"\u5df2\u5b8c\u6210",
		"\u5df2\u5b8c\u6210\uff1a",
		"\u4ea7\u51fa\u6458\u8981",
		"final answer",
		"final synthesis",
		"final delivery",
		"result summary",
		"task result",
		"completed with evidence",
	}
}

func leaderDispatchTextMarkers() []string {
	return []string{
		"\u4efb\u52a1\u5206\u6d3e",
		"\u4efb\u52a1\u4e0b\u53d1",
		"\u5206\u6d3e\u7ed9",
		"\u6d3e\u53d1\u7ed9",
		"\u4ea4\u7ed9",
		"\u8bf7\u4f60",
		"\u8bf7\u5199",
		"\u8bf7\u5b8c\u6210",
		"assignment",
		"assigned to",
		"handoff",
	}
}

func containsAnyTeamTextMarker(text string, markers []string) bool {
	for _, marker := range markers {
		if containsTeamTextMarker(text, marker) {
			return true
		}
	}
	return false
}

func containsTeamTextMarker(text, marker string) bool {
	lowerText := strings.ToLower(text)
	lowerMarker := strings.ToLower(marker)
	compactText := strings.ToLower(strings.Join(strings.Fields(text), ""))
	compactMarker := strings.ToLower(strings.Join(strings.Fields(marker), ""))
	return strings.Contains(lowerText, lowerMarker) || strings.Contains(compactText, compactMarker)
}
func inferLeaderDispatchTarget(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}
	if target := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id", "assignee", "owner"); target != "" {
		return target
	}
	text := strings.ToLower(eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary"))
	for _, target := range []string{"ui-designer", "designer", "product-manager", "pm", "solution-architect", "architect", "worker"} {
		if strings.Contains(text, target) {
			return target
		}
	}
	return ""
}

func (s *teamService) projectTeamEvent(team *models.Team, bus *redisBus, message redisStreamMessage) error {
	if exists, err := s.repo.EventExistsByStreamID(team.ID, message.ID); err != nil || exists {
		return err
	}
	payload := mergeRedisEventPayload(message.Fields)
	eventID := eventString(payload, "eventId", "event_id")
	if eventID != "" {
		if exists, err := s.repo.EventExistsByEventID(team.ID, eventID); err != nil || exists {
			return err
		}
	}
	eventType := eventString(payload, "event_type", "event", "type")
	if eventType == "" {
		eventType = "message"
	}
	incomingEventType := strings.ToLower(strings.TrimSpace(eventType))
	isCompletionProposal := incomingEventType == "completion_proposed"
	if strings.EqualFold(eventType, "member_result_confirmed") {
		payload["visibleToChat"] = false
		payload["visible_to_chat"] = false
		payload["assignmentResultOnly"] = true
		payload["rootTaskTerminal"] = false
	}
	messageID := eventString(payload, "message_id", "messageId")
	memberKey := eventString(payload, "member_id", "memberId", "member_key")
	if isOutboundTeamEvent(eventType) && messageID != "" && !teamEventHasBody(payload) && bus != nil {
		enriched, err := s.enrichOutboundEventFromInbox(team.ID, bus, payload, messageID)
		if err != nil {
			fmt.Printf("Warning: failed to enrich Team %d outbound event %s from inbox: %v\n", team.ID, messageID, err)
		} else {
			payload = enriched
		}
	}

	var member *models.TeamMember
	if memberKey != "" {
		found, err := s.repo.GetMemberByTeamKey(team.ID, memberKey)
		if err != nil {
			return err
		}
		member = found
	}
	var err error
	eventType, member, err = s.normalizePeerAssistedTeamEvent(team, eventType, payload, member)
	if err != nil {
		return err
	}

	var task *models.TeamTask
	if taskID := eventInt(payload, "task_id", "taskId"); taskID > 0 {
		found, err := s.repo.GetTaskByID(taskID)
		if err != nil {
			return err
		}
		if found != nil && found.TeamID == team.ID {
			task = found
		}
	}
	if task == nil && messageID != "" {
		found, err := s.repo.GetTaskByMessageID(team.ID, messageID)
		if err != nil {
			return err
		}
		task = found
	}
	if task == nil {
		found, err := s.resolveTeamTaskFromEventReferences(team.ID, payload)
		if err != nil {
			return err
		}
		task = found
	}
	if task == nil && isLeaderMediatedTeam(team) && member != nil {
		found, err := s.resolveLeaderMediatedTaskFromWorkItem(team.ID, payload, member)
		if err != nil {
			return err
		}
		task = found
	}
	if task == nil && member != nil && member.CurrentTaskID != nil && shouldAssociateEventWithCurrentMemberTask(eventType, payload) {
		found, err := s.activeTaskForMember(team.ID, member, true)
		if err != nil {
			return err
		}
		task = found
	}
	if task == nil && shouldAssociateEventWithCurrentMemberTask(eventType, payload) {
		found, err := s.activeTaskFromPeerContext(team.ID, payload, member)
		if err != nil {
			return err
		}
		task = found
	}
	normalizeUnauthorizedAssignmentCheckResult(payload)
	passiveMonitorEvent := isPassiveAssignmentMonitorEvent(eventType, payload)
	if isAssignmentHeartbeatEvent(eventType, payload) {
		normalizeAssignmentHeartbeatPayload(payload)
		if task != nil && isTerminalTeamTaskStatus(task.Status) {
			return nil
		}
		passiveMonitorEvent = true
	} else if passiveMonitorEvent {
		normalizePassiveAssignmentMonitorPayload(payload)
	}
	if passiveMonitorEvent && task != nil && member != nil {
		targetsTerminalWorkItem, err := s.leaderMediatedMonitorTargetsTerminalWorkItem(team, task, member, payload)
		if err != nil {
			return err
		}
		if targetsTerminalWorkItem {
			return nil
		}
	}
	if isTeamPresenceEvent(eventType) && task != nil && member != nil {
		terminalWorkItem, err := s.leaderMediatedMonitorTargetsTerminalWorkItem(team, task, member, payload)
		if err != nil {
			return err
		}
		if terminalWorkItem {
			payload["availability"] = models.TeamMemberAvailabilityIdle
			payload["runtimeStatus"] = models.TeamTaskStatusSucceeded
			payload["status"] = models.TeamTaskStatusSucceeded
			payload["staleRunningSuppressed"] = true
		}
	}
	if isNonAuthoritativeDispatchFailure(eventType, payload) {
		if eventString(payload, "originalEvent") == "" {
			payload["originalEvent"] = eventType
		}
		payload["event"] = "message_warning"
		payload["type"] = "message_warning"
		payload["status"] = "warning"
		payload["availability"] = "idle"
		payload["nonAuthoritative"] = true
		eventType = "message_warning"
	}
	leaderMediatedRouteViolation := isLeaderMediatedInvalidWorkerRoute(team, eventType, payload, member)
	if leaderMediatedRouteViolation {
		eventType = markLeaderMediatedRouteViolation(eventType, payload, member)
	}
	assignmentResultOnly := isLeaderMediatedWorkerToLeaderResult(team, eventType, payload, member, task)
	if assignmentResultOnly {
		markLeaderMediatedAssignmentResult(eventType, payload, member)
		if task != nil && member != nil {
			assignmentID := eventString(payload, "assignmentId", "assignment_id", "canonicalWorkId", "canonical_work_id", "workId", "work_id")
			contentHash := eventString(payload, "contentHash", "content_hash")
			alreadyProjected, confirmErr := s.hasLeaderMediatedResultConfirmation(team.ID, task.ID, member.MemberKey, assignmentID, contentHash)
			if confirmErr != nil {
				return confirmErr
			}
			if alreadyProjected {
				payload["visibleToChat"] = false
				payload["visible_to_chat"] = false
				payload["duplicateResultProjection"] = true
			}
		}
	}
	if !assignmentResultOnly && isLeaderMediatedMonitorBlockerCandidate(team, eventType, payload, member, task) {
		eventType = markLeaderMediatedMonitorBlockerCandidate(eventType, payload)
	}
	eventType = normalizeFinalReplyTaskEvent(eventType, payload, task, member)
	eventType = markLegacyRuntimeCompletionCandidate(eventType, payload, task, member)
	eventStatus := normalizedTeamTaskEventStatus(payload)
	eventSignalsCompletion := isTeamTaskCompletionSignal(eventType, eventStatus, payload)
	eventSignalsFailure := isTeamTaskFailureSignal(eventType, eventStatus, payload)
	if isCompletionProposal && !eventSignalsCompletion && !eventSignalsFailure {
		evaluation := teamCompletionEvaluation{Decision: teamCompletionDecisionRejected, Reason: "invalid_completion_envelope"}
		if task != nil {
			evaluation.LedgerVersion = task.LedgerVersion
			evaluation.PlanVersion = task.PlanVersion
		}
		eventType = markStructuredCompletionDecision(eventType, payload, evaluation)
		eventStatus = normalizedTeamTaskEventStatus(payload)
	}
	if isUnauthoritativeCompletionEvent(eventType, eventSignalsCompletion, eventSignalsFailure) {
		if eventString(payload, "originalEvent") == "" {
			payload["originalEvent"] = eventType
		}
		payload["event"] = "reply"
		payload["type"] = "reply"
		payload["status"] = models.TeamTaskStatusRunning
		payload["runtimeStatus"] = models.TeamTaskStatusRunning
		payload["availability"] = models.TeamMemberAvailabilityBusy
		payload["rootTaskTerminal"] = false
		payload["nonAuthoritativeCompletion"] = true
		eventType = "reply"
		eventStatus = normalizedTeamTaskEventStatus(payload)
		eventSignalsCompletion = false
		eventSignalsFailure = false
	}
	leaderDispatchOnly := false
	if !eventSignalsCompletion && !eventSignalsFailure {
		leaderDispatchOnly = isLeaderMediatedLeaderDispatchOnlyMessage(team, eventType, payload, member, task)
	}
	if leaderDispatchOnly {
		if eventString(payload, "originalEvent") == "" {
			payload["originalEvent"] = eventType
		}
		payload["event"] = "reply"
		payload["type"] = "reply"
		payload["status"] = models.TeamTaskStatusDispatched
		payload["runtimeStatus"] = models.TeamTaskStatusRunning
		payload["availability"] = models.TeamMemberAvailabilityBusy
		payload["rootTaskTerminal"] = false
		payload["leaderDispatchOnly"] = true
		if target := inferLeaderDispatchTarget(payload); target != "" {
			payload["target"] = target
			payload["to"] = target
		}
		eventType = "reply"
		eventStatus = normalizedTeamTaskEventStatus(payload)
		eventSignalsCompletion = false
		eventSignalsFailure = false
	}
	if eventSignalsCompletion && !leaderDispatchOnly && !assignmentResultOnly && !leaderMediatedRouteViolation {
		// Repair a stale derived phase state before judging an explicit final
		// delivery. This is local projection repair, not a concurrent-plan change,
		// so carry the repaired ledger version into this evaluation.
		if task != nil {
			workflowFinal := eventBool(payload, "workflowFinal", "workflow_final", "sealWorkflow", "seal_workflow")
			reconciled, reconcileErr := s.reconcileTeamWorkflowLedger(task, workflowFinal, time.Now().UTC())
			if reconcileErr != nil {
				return reconcileErr
			}
			if reconciled {
				payload["ledgerVersion"] = task.LedgerVersion
				payload["planVersion"] = task.PlanVersion
				payload["workflowLedgerReconciled"] = true
			}
		}
		narrative := eventString(payload, "summary") + "\n" + eventString(payload, "resultMarkdown", "result_markdown", "result", "answer")
		payload["interimNarrativeSignal"] = isInterimOrDelegationReplyText(narrative)
		evaluation, err := s.evaluateLeaderRootCompletion(team, task, member, payload)
		if err != nil {
			return err
		}
		payload["completionEvaluation"] = evaluation.Decision
		if len(evaluation.WaivedAssignments) > 0 {
			payload["waivedAssignments"] = evaluation.WaivedAssignments
		}
		if len(evaluation.SkippedAssignments) > 0 {
			payload["skippedAssignmentsAccepted"] = evaluation.SkippedAssignments
		}
		if evaluation.Decision != teamCompletionDecisionAccepted {
			eventType = markStructuredCompletionDecision(eventType, payload, evaluation)
			eventStatus = normalizedTeamTaskEventStatus(payload)
			eventSignalsCompletion = false
			eventSignalsFailure = false
		} else {
			payload["event"] = "task_completed"
			payload["type"] = "task_completed"
			payload["completionDecision"] = teamCompletionDecisionAccepted
			payload["chatKind"] = "final_delivery"
			payload["chatPolicy"] = "visible"
			payload["visibleToChat"] = true
			payload["visible_to_chat"] = true
			payload["displayKey"] = fmt.Sprintf("root-final:%d", task.ID)
		}
	}
	s.normalizeTeamArtifactReferences(team, payload)
	if eventSignalsCompletion {
		if missing := s.missingTeamArtifactReferences(team, payload); len(missing) > 0 {
			if eventString(payload, "originalEvent") == "" {
				payload["originalEvent"] = eventType
			}
			payload["event"] = "message_warning"
			payload["type"] = "message_warning"
			payload["status"] = "blocked"
			payload["runtimeStatus"] = models.TeamTaskStatusRunning
			payload["availability"] = models.TeamMemberAvailabilityBusy
			payload["rootTaskTerminal"] = false
			payload["artifactValidationFailed"] = true
			payload["missingArtifactRefs"] = missing
			if isCompletionProposal || isExplicitTeamTaskCompletion(payload) {
				payload["completionDecision"] = teamCompletionDecisionRejected
				payload["completionDecisionReason"] = "missing_artifacts"
				payload["completionEvaluation"] = teamCompletionDecisionRejected
			}
			// Preserve the final answer. Artifact validation is workflow state, not a
			// replacement for the result the member already returned.
			payload["artifactValidationMessage"] = "Team artifact references are not readable from the shared workspace; task remains open."
			eventType = "message_warning"
			eventStatus = normalizedTeamTaskEventStatus(payload)
			eventSignalsCompletion = false
			eventSignalsFailure = false
		}
	}
	if (eventSignalsCompletion || eventSignalsFailure) && teamRedisProtocolVersion(payload) >= 2 {
		duplicate, err := s.hasAcceptedTeamCompletionID(
			team.ID,
			eventString(payload, "completionId", "completion_id"),
		)
		if err != nil {
			return err
		}
		if duplicate {
			if isCompletionProposal {
				if task != nil && task.Status == models.TeamTaskStatusSucceeded {
					if err := s.completeWorkflowPhases(task, time.Now().UTC()); err != nil {
						return err
					}
				}
				if err := s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionAccepted, "already_accepted"); err != nil {
					return err
				}
			}
			return nil
		}
	}
	memberTerminalOnly := task != nil &&
		member != nil &&
		member.ID != task.TargetMemberID &&
		(eventSignalsCompletion || eventSignalsFailure)
	if memberTerminalOnly {
		payload["memberTerminalOnly"] = true
		payload["rootTaskTerminal"] = false
	}
	applyTeamChatPolicy(eventType, payload, task, member)
	enrichTeamCollaborationStep(team, eventType, payload, member, task)

	payloadJSON, err := marshalOptionalJSON(payload)
	if err != nil {
		return err
	}
	streamID := message.ID
	event := &models.TeamEvent{
		TeamID:        team.ID,
		EventType:     eventType,
		PayloadJSON:   payloadJSON,
		RedisStreamID: &streamID,
		OccurredAt:    eventTime(payload),
	}
	if eventID != "" {
		event.EventID = &eventID
	}
	if eventSignalsCompletion || eventSignalsFailure {
		completionID := eventString(payload, "completionId", "completion_id")
		if completionID != "" {
			event.CompletionID = &completionID
		}
	}
	if member != nil {
		event.MemberID = &member.ID
	}
	if task != nil {
		event.TaskID = &task.ID
	}
	if messageID != "" {
		event.MessageID = &messageID
	}
	atomicRootCompletionAccepted := false
	if eventSignalsCompletion && task != nil && member != nil && member.ID == task.TargetMemberID && isLeaderTeamMember(member) {
		now := time.Now().UTC()
		expectedLedgerVersion := task.LedgerVersion
		completionID := eventString(payload, "completionId", "completion_id")
		if completionID == "" {
			seed := eventID
			if seed == "" {
				seed = message.ID
			}
			completionID = fmt.Sprintf("legacy:%d:%s", task.ID, normalizeTeamRedisKeyPart(seed))
			payload["completionId"] = completionID
			payloadJSON, err = marshalOptionalJSON(payload)
			if err != nil {
				return err
			}
			event.PayloadJSON = payloadJSON
			event.CompletionID = &completionID
		}
		task.Status = models.TeamTaskStatusSucceeded
		task.WorkflowState = teamWorkflowStateCompleted
		task.LedgerVersion = expectedLedgerVersion + 1
		task.CurrentPhaseID = nil
		task.AcceptedCompletionID = &completionID
		task.ResultJSON = payloadJSON
		task.ErrorMessage = nil
		task.FinishedAt = &now
		task.UpdatedAt = now
		sourceEventID := eventID
		if sourceEventID == "" {
			sourceEventID = message.ID
		}
		ackOutbox, ackErr := completionAcknowledgementOutbox(team, task, member, payload, teamCompletionDecisionAccepted, "workflow_closed", sourceEventID, now)
		if ackErr != nil {
			return ackErr
		}
		accepted, acceptErr := s.repo.AcceptRootCompletion(task, expectedLedgerVersion, event, ackOutbox)
		if acceptErr != nil {
			return acceptErr
		}
		if accepted {
			atomicRootCompletionAccepted = true
			if err := s.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionAccepted, "workflow_closed"); err != nil {
				return err
			}
			if ackOutbox.ID > 0 && bus != nil {
				if err := s.repo.MarkEventOutboxDelivered(ackOutbox.ID, time.Now().UTC()); err != nil {
					return err
				}
			}
		} else {
			task.LedgerVersion = expectedLedgerVersion
			task.Status = models.TeamTaskStatusRunning
			task.WorkflowState = teamWorkflowStateCompletionPending
			task.AcceptedCompletionID = nil
			task.FinishedAt = nil
			evaluation := teamCompletionEvaluation{
				Decision:      teamCompletionDecisionDeferred,
				Reason:        "stale_ledger",
				LedgerVersion: expectedLedgerVersion,
				PlanVersion:   task.PlanVersion,
			}
			eventType = markStructuredCompletionDecision(eventType, payload, evaluation)
			eventSignalsCompletion = false
			event.CompletionID = nil
			event.EventType = eventType
			payloadJSON, err = marshalOptionalJSON(payload)
			if err != nil {
				return err
			}
			event.PayloadJSON = payloadJSON
		}
	}
	if !atomicRootCompletionAccepted {
		if err := s.repo.CreateEvent(event); err != nil {
			if errors.Is(err, repository.ErrDuplicateTeamEvent) {
				return nil
			}
			return err
		}
	}
	reviewInvalidated, err := s.invalidateModifiedTeamArtifactReviews(task, payload, time.Now().UTC())
	if err != nil {
		return err
	}
	workflowChanged := reviewInvalidated
	if err := s.projectTeamWorkItem(team, task, member, eventType, payload, event); err != nil {
		return err
	}
	if !atomicRootCompletionAccepted {
		ledgerChanged, ledgerErr := s.projectTeamWorkflowLedger(team, task, member, eventType, payload, time.Now().UTC())
		err = ledgerErr
		if err != nil {
			return err
		}
		workflowChanged = workflowChanged || ledgerChanged
	}
	now := time.Now().UTC()
	if assignmentResultOnly {
		if err := s.createLeaderMediatedResultNotification(team, bus, task, member, payload, event); err != nil {
			return err
		}
		if err := s.reopenLeaderMediatedRootAfterMemberResult(team, task, payload, now); err != nil {
			return err
		}
	}
	if !assignmentResultOnly && !passiveMonitorEvent && isLeaderMediatedRecoverableWarning(team, eventType, payload, member, task) {
		if err := s.createLeaderMediatedRecoveryRequest(team, bus, task, member, payload, event); err != nil {
			return err
		}
	}

	taskProjection := teamTaskProjectionResult{}
	if atomicRootCompletionAccepted {
		taskProjection = teamTaskProjectionResult{changed: true, status: models.TeamTaskStatusSucceeded}
	} else if task != nil && !passiveMonitorEvent && !memberTerminalOnly && !assignmentResultOnly && !leaderMediatedRouteViolation {
		taskProjection = projectTeamTaskRuntimeState(task, payload, eventType, payloadJSON, now)
		if taskProjection.changed || workflowChanged {
			task.UpdatedAt = now
			if err := s.repo.UpdateTask(task); err != nil {
				return err
			}
		}
	}
	if task != nil && (memberTerminalOnly || leaderDispatchOnly || assignmentResultOnly || leaderMediatedRouteViolation || workflowChanged) && !taskProjection.changed && !isTerminalTeamTaskStatus(task.Status) {
		task.UpdatedAt = now
		if err := s.repo.UpdateTask(task); err != nil {
			return err
		}
	}
	if member != nil {
		member.LastSeenAt = &now
		if !passiveMonitorEvent {
			applyTeamMemberRuntimeProjection(member, payload, eventType)
		}
		taskIsActive := task != nil && !isTerminalTeamTaskStatus(task.Status)
		if !passiveMonitorEvent && taskIsActive && (leaderDispatchOnly || eventType == "task_received" || eventType == "task_started" || taskProjection.status == models.TeamTaskStatusRunning || taskProjection.status == models.TeamTaskStatusDispatched) {
			member.Status = models.TeamMemberStatusBusy
			if member.Availability == "" || member.Availability == models.TeamMemberAvailabilityUnknown {
				member.Availability = models.TeamMemberAvailabilityBusy
			}
			member.CurrentTaskID = &task.ID
			member.Progress = eventInt(payload, "progress")
		}
		taskProjectedTerminal := taskProjection.status == models.TeamTaskStatusSucceeded || taskProjection.status == models.TeamTaskStatusFailed
		terminalEventWithoutTask := (task == nil || memberTerminalOnly || assignmentResultOnly) && (eventSignalsCompletion || eventSignalsFailure || assignmentResultOnly)
		if taskProjectedTerminal || terminalEventWithoutTask {
			member.Status = models.TeamMemberStatusIdle
			member.CurrentTaskID = nil
			if taskProjection.status == models.TeamTaskStatusSucceeded || assignmentResultOnly || (terminalEventWithoutTask && eventSignalsCompletion) {
				member.Progress = 100
				if member.Availability != models.TeamMemberAvailabilityBlocked {
					member.Availability = models.TeamMemberAvailabilityIdle
					member.BlockedReason = nil
				}
			} else if taskProjection.status == models.TeamTaskStatusFailed || (terminalEventWithoutTask && eventSignalsFailure) {
				member.Progress = 0
				if member.Availability == "" || member.Availability == models.TeamMemberAvailabilityUnknown {
					member.Availability = models.TeamMemberAvailabilityBlocked
				}
				if member.BlockedReason == nil {
					if errText := eventString(payload, "error_message", "error", "reason", "diagnostic", "lastSummary", "last_summary"); errText != "" {
						member.BlockedReason = &errText
					}
				}
			}
		}
		if task != nil && task.Status == models.TeamTaskStatusSucceeded {
			member.Status = models.TeamMemberStatusIdle
			member.CurrentTaskID = nil
			member.Progress = 100
			member.Availability = models.TeamMemberAvailabilityIdle
			member.BlockedReason = nil
		}
		member.UpdatedAt = now
		if err := s.repo.UpdateMember(member); err != nil {
			return err
		}
	}
	if isCompletionProposal && !atomicRootCompletionAccepted {
		decision := eventString(payload, "completionDecision", "completion_decision")
		reason := eventString(payload, "completionDecisionReason", "completion_decision_reason")
		if assignmentResultOnly || memberTerminalOnly || eventSignalsFailure {
			decision = teamCompletionDecisionAccepted
			if eventSignalsFailure {
				reason = "failure_recorded"
			} else {
				reason = "assignment_result_recorded"
			}
		}
		if decision == "" {
			decision = teamCompletionDecisionDeferred
		}
		sourceEventID := eventID
		if sourceEventID == "" {
			sourceEventID = message.ID
		}
		var ackOutbox *models.TeamEventOutbox
		if eventString(payload, "completionId", "completion_id") != "" {
			var err error
			ackOutbox, err = completionAcknowledgementOutbox(team, task, member, payload, decision, reason, sourceEventID, now)
			if err != nil {
				return err
			}
			if err := s.repo.CreateEventOutbox(ackOutbox); err != nil {
				return err
			}
		}
		if err := s.emitCompletionAcknowledgement(team, bus, task, member, payload, decision, reason); err != nil {
			return err
		}
		if ackOutbox != nil && ackOutbox.ID > 0 && bus != nil {
			if err := s.repo.MarkEventOutboxDelivered(ackOutbox.ID, time.Now().UTC()); err != nil {
				return err
			}
		}
	}
	return nil
}

func isLeaderMediatedRecoverableWarning(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) bool {
	if team == nil || task == nil || member == nil || payload == nil {
		return false
	}
	if !isLeaderMediatedTeam(team) || isLeaderTeamMember(member) || isTerminalTeamTaskStatus(task.Status) {
		return false
	}
	if isPassiveAssignmentMonitorEvent(eventType, payload) {
		return false
	}
	if eventBool(payload, "rootTaskTerminal", "root_task_terminal") {
		return false
	}
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	if eventKind == "assignment_recovery_exhausted" {
		return false
	}
	if eventBool(payload, "artifactValidationFailed", "artifact_validation_failed") {
		return true
	}
	if eventBool(payload, "nonAuthoritativeCompletion", "non_authoritative_completion") {
		return true
	}
	if strings.EqualFold(eventType, "message_warning") {
		return true
	}
	return eventKind == "completion_validation_warning"
}

func (s *teamService) createLeaderMediatedRecoveryRequest(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, sourcePayload map[string]interface{}, sourceEvent *models.TeamEvent) error {
	if s == nil || team == nil || bus == nil || task == nil || member == nil || sourcePayload == nil || sourceEvent == nil {
		return nil
	}
	leaderKey := "leader"
	if leader, err := s.repo.GetMemberByID(task.TargetMemberID); err == nil && leader != nil && strings.TrimSpace(leader.MemberKey) != "" {
		leaderKey = leader.MemberKey
	}
	streamRef := ""
	if sourceEvent.RedisStreamID != nil {
		streamRef = *sourceEvent.RedisStreamID
	}
	if streamRef == "" {
		streamRef = strconv.FormatInt(sourceEvent.CreatedAt.UnixNano(), 10)
	}
	eventID := fmt.Sprintf("leader-recovery:%d:%d:%s:%s", team.ID, task.ID, member.MemberKey, streamRef)
	exists, err := s.repo.EventExistsByEventID(team.ID, eventID)
	if err != nil || exists {
		return err
	}
	rootTaskRef := fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	summary := eventString(sourcePayload, "summary", "diagnostic", "error_message", "error", "message", "text")
	if summary == "" {
		summary = "Recoverable assignment issue detected; Leader should replan, retry, or reassign before surfacing an error to the user."
	}
	workID := eventString(sourcePayload, "workId", "work_id", "assignmentId", "assignment_id")
	if workID == "" {
		workID = "member-" + normalizeTeamMemberRouteKey(member.MemberKey)
	}
	notificationPayload := map[string]interface{}{
		"event":            "assignment_recovery_started",
		"type":             "assignment_recovery_started",
		"eventKind":        "assignment_recovery_started",
		"protocolVersion":  2,
		"source":           "clawmanager_recovery_controller",
		"recoverable":      true,
		"rootTaskTerminal": false,
		"nonAuthoritative": true,
		"rootTaskId":       rootTaskRef,
		"rootMessageId":    task.MessageID,
		"messageId":        task.MessageID,
		"assignmentId":     workID,
		"workId":           workID,
		"from":             "clawmanager",
		"memberId":         member.MemberKey,
		"to":               leaderKey,
		"target":           leaderKey,
		"status":           models.TeamTaskStatusRunning,
		"runtimeStatus":    models.TeamTaskStatusRunning,
		"availability":     models.TeamMemberAvailabilityBusy,
		"summary":          summary,
		"sourceEventId":    sourceEvent.ID,
		"sourceEventType":  sourceEvent.EventType,
		"sourcePayload":    sourcePayload,
		"visibleToChat":    true,
		"recoveryAction":   "leader_replan_or_reissue",
		"collaborationStep": map[string]interface{}{
			"type":          "progress",
			"status":        models.TeamTaskStatusRunning,
			"actor":         "clawmanager",
			"target":        member.MemberKey,
			"rootTaskId":    rootTaskRef,
			"rootMessageId": task.MessageID,
			"workId":        workID,
			"title":         "Recovery started for " + member.MemberKey,
			"summary":       summary,
			"content":       summary,
			"source":        "clawmanager_recovery_controller",
		},
	}
	payloadJSON, err := marshalOptionalJSON(notificationPayload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	event := &models.TeamEvent{
		TeamID:      team.ID,
		TaskID:      &task.ID,
		MemberID:    &member.ID,
		MessageID:   &task.MessageID,
		EventID:     &eventID,
		EventType:   "assignment_recovery_started",
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	}
	if err := s.repo.CreateEvent(event); err != nil {
		return err
	}
	s.dispatchLeaderMediatedRecoveryRequestToInbox(team, bus, task, member, leaderKey, notificationPayload, eventID)
	return nil
}

func (s *teamService) dispatchLeaderMediatedRecoveryRequestToInbox(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, leaderKey string, notificationPayload map[string]interface{}, eventID string) {
	if team == nil || bus == nil || task == nil || member == nil || notificationPayload == nil {
		return
	}
	if strings.TrimSpace(leaderKey) == "" {
		leaderKey = "leader"
	}
	summary := eventString(notificationPayload, "summary")
	prompt := fmt.Sprintf(
		"ClawManager detected a recoverable issue for member %s on root task %s.\n\nIssue:\n%s\n\nFirst try to recover without exposing this as a user-facing failure: inspect existing progress/artifacts, ask the same member to continue if appropriate, reissue the assignment with corrected instructions, or reassign to another member. Only report failure to the user after recovery is exhausted.",
		member.MemberKey,
		task.MessageID,
		summary,
	)
	envelope := map[string]interface{}{
		"v":                  1,
		"messageId":          eventID,
		"teamId":             strconv.Itoa(team.ID),
		"from":               "clawmanager",
		"to":                 leaderKey,
		"replyTo":            teamTaskReplyTarget,
		"requiresCompletion": false,
		"completionTool":     teamTaskCompletionTool,
		"intent":             "assignment_recovery_request",
		"taskId":             fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootTaskId":         fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootMessageId":      task.MessageID,
		"workId":             eventString(notificationPayload, "workId", "assignmentId"),
		"assignmentId":       eventString(notificationPayload, "assignmentId", "workId"),
		"title":              member.MemberKey + " assignment recovery requested",
		"prompt":             prompt,
		"rawPrompt":          prompt,
		"monitorPolicy":      defaultTeamMonitorPolicy(),
		"metadata":           notificationPayload,
		"createdAt":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	applyTeamTaskEnvelopeContext(envelope, task, leaderKey)
	envelopeJSON, err := marshalJSON(envelope)
	if err != nil {
		fmt.Printf("Warning: failed to encode Leader recovery notification for Team %d task %d: %v\n", team.ID, task.ID, err)
		return
	}
	if _, err := bus.XAdd(context.Background(), teamInboxKey(team.ID, leaderKey), map[string]string{
		"payload":    envelopeJSON,
		"team_id":    strconv.Itoa(team.ID),
		"task_id":    strconv.Itoa(task.ID),
		"message_id": eventID,
		"member_id":  leaderKey,
	}); err != nil {
		fmt.Printf("Warning: failed to dispatch Leader recovery notification for Team %d task %d: %v\n", team.ID, task.ID, err)
	}
}

func (s *teamService) createLeaderMediatedResultNotification(team *models.Team, bus *redisBus, task *models.TeamTask, member *models.TeamMember, sourcePayload map[string]interface{}, sourceEvent *models.TeamEvent) error {
	if s == nil || team == nil || task == nil || member == nil || sourcePayload == nil || sourceEvent == nil {
		return nil
	}
	if !isLeaderMediatedTeam(team) || isLeaderTeamMember(member) {
		return nil
	}
	workID := eventString(sourcePayload, "assignmentId", "assignment_id", "canonicalWorkId", "canonical_work_id", "workId", "work_id")
	if workID == "" {
		workID = "member-" + normalizeTeamMemberRouteKey(member.MemberKey)
	}
	revision := eventInt(sourcePayload, "revision", "workRevision", "work_revision")
	if revision <= 0 {
		revision = 1
	}
	source := eventString(sourcePayload, "normalizedResultSource")
	if source == "" {
		source = "legacy_normalized_reply"
	}
	summary := eventString(sourcePayload, "summary", "title")
	resultMarkdown := eventString(sourcePayload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary")
	if summary == "" {
		summary = truncateForSummary(resultMarkdown, 180)
	}
	if strings.TrimSpace(summary) == "" && strings.TrimSpace(resultMarkdown) == "" {
		return nil
	}
	contentHash := eventString(sourcePayload, "contentHash", "content_hash")
	if contentHash == "" {
		contentHash = teamResultContentHash(sourcePayload)
	}
	alreadyConfirmed, err := s.hasLeaderMediatedResultConfirmation(team.ID, task.ID, member.MemberKey, workID, contentHash)
	if err != nil || alreadyConfirmed {
		return err
	}
	confirmationSeed := fmt.Sprintf("%d:%d:%s:%s:%d:%s", team.ID, task.ID, normalizeTeamMemberRouteKey(member.MemberKey), workID, revision, contentHash)
	confirmationDigest := sha256.Sum256([]byte(confirmationSeed))
	eventID := fmt.Sprintf("member-result-confirmed:%d:%d:%x", team.ID, task.ID, confirmationDigest[:12])
	exists, err := s.repo.EventExistsByEventID(team.ID, eventID)
	if err != nil || exists {
		return err
	}
	rootTaskRef := fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
	sourceEventID := ""
	if sourceEvent.EventID != nil {
		sourceEventID = strings.TrimSpace(*sourceEvent.EventID)
	}
	if sourceEventID == "" && sourceEvent.RedisStreamID != nil {
		sourceEventID = strings.TrimSpace(*sourceEvent.RedisStreamID)
	}
	if sourceEventID == "" && sourceEvent.ID > 0 {
		sourceEventID = strconv.Itoa(sourceEvent.ID)
	}
	sourceCompletionID := eventString(sourcePayload, "completionId", "completion_id")
	sourceWorkID := eventString(sourcePayload, "workId", "work_id", "assignmentId", "assignment_id")
	if sourceWorkID == "" {
		sourceWorkID = workID
	}
	notificationPayload := map[string]interface{}{
		"event":                  "member_result_confirmed",
		"type":                   "member_result_confirmed",
		"protocolVersion":        2,
		"source":                 "clawmanager_assignment_ledger",
		"normalizedResultSource": source,
		"memberResultConfirmed":  true,
		"assignmentResultOnly":   true,
		"rootTaskTerminal":       false,
		"visibleToChat":          false,
		"visible_to_chat":        false,
		"chatPolicy":             "hidden",
		"sourceEventId":          sourceEventID,
		"sourceCompletionId":     sourceCompletionID,
		"sourceWorkId":           sourceWorkID,
		"contentHash":            contentHash,
		"rootTaskId":             rootTaskRef,
		"rootMessageId":          task.MessageID,
		"messageId":              task.MessageID,
		"assignmentId":           workID,
		"workId":                 workID,
		"revision":               revision,
		"from":                   member.MemberKey,
		"memberId":               member.MemberKey,
		"to":                     "leader",
		"target":                 "leader",
		"status":                 models.TeamTaskStatusSucceeded,
		"summary":                member.MemberKey + " assignment result confirmed",
		"collaborationStep": map[string]interface{}{
			"type":          "ack",
			"status":        models.TeamTaskStatusSucceeded,
			"actor":         member.MemberKey,
			"target":        "leader",
			"rootTaskId":    rootTaskRef,
			"rootMessageId": task.MessageID,
			"workId":        workID,
			"title":         member.MemberKey + " result confirmed",
			"summary":       member.MemberKey + " assignment result confirmed",
			"source":        "clawmanager_assignment_ledger",
		},
	}
	payloadJSON, err := marshalOptionalJSON(notificationPayload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	event := &models.TeamEvent{
		TeamID:      team.ID,
		TaskID:      &task.ID,
		MemberID:    &member.ID,
		MessageID:   &task.MessageID,
		EventID:     &eventID,
		EventType:   "member_result_confirmed",
		PayloadJSON: payloadJSON,
		OccurredAt:  &now,
		CreatedAt:   now,
	}
	resultJSON, err := marshalOptionalJSON(sourcePayload)
	if err != nil {
		return err
	}
	workItem := &models.TeamWorkItem{
		TeamID:          team.ID,
		RootTaskID:      task.ID,
		WorkID:          workID,
		AssignmentID:    &workID,
		CanonicalWorkID: &workID,
		Revision:        revision,
		RequiredForRoot: true,
		OwnerMemberID:   &member.ID,
		Title:           member.MemberKey + " delivers result",
		Status:          models.TeamTaskStatusSucceeded,
		ResultJSON:      resultJSON,
		FinishedAt:      &now,
		UpdatedAt:       now,
	}
	if items, listErr := s.repo.ListWorkItemsByRootTaskID(task.ID); listErr == nil {
		for idx := range items {
			candidateID := derefTeamString(items[idx].AssignmentID)
			if candidateID == "" {
				candidateID = items[idx].WorkID
			}
			if items[idx].TeamID == team.ID && candidateID == workID && teamMaxInt(items[idx].Revision, 1) == revision {
				clone := items[idx]
				clone.Status = models.TeamTaskStatusSucceeded
				clone.ResultJSON = resultJSON
				clone.FinishedAt = &now
				clone.UpdatedAt = now
				workItem = &clone
				break
			}
		}
	}
	deliveryPayload := make(map[string]interface{}, len(notificationPayload)+3)
	for key, value := range notificationPayload {
		deliveryPayload[key] = value
	}
	deliveryPayload["summary"] = summary
	deliveryPayload["resultMarkdown"] = resultMarkdown
	deliveryPayload["artifactRefs"] = explicitTeamArtifactReferences(sourcePayload)
	deliveryEnvelope, leaderKey := s.buildLeaderMediatedResultNotificationEnvelope(team, task, member, deliveryPayload, eventID)
	deliveryJSON, err := marshalJSON(deliveryEnvelope)
	if err != nil {
		return err
	}
	outbox := &models.TeamEventOutbox{
		TeamID:        team.ID,
		SourceEventID: eventID,
		Destination:   teamInboxKey(team.ID, leaderKey),
		MessageID:     eventID,
		PayloadJSON:   deliveryJSON,
		Status:        "pending",
		AvailableAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.ConfirmWorkItemResult(workItem, event, outbox); err != nil {
		return err
	}
	if outbox.ID > 0 {
		if err := s.deliverTeamEventOutbox(team, bus, outbox); err != nil {
			_ = s.repo.MarkEventOutboxFailed(outbox.ID, now.Add(teamOutboxRetryDelay(outbox.Attempts)), err.Error())
			return nil
		}
		if err := s.repo.MarkEventOutboxDelivered(outbox.ID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s *teamService) hasLeaderMediatedResultConfirmation(teamID, taskID int, memberKey string, assignmentID string, contentHashes ...string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, nil
	}
	events, err := s.repo.ListEventsByTeamID(teamID, 500)
	if err != nil {
		return false, err
	}
	normalizedMember := normalizeTeamMemberRouteKey(memberKey)
	expectedAssignment := strings.TrimSpace(assignmentID)
	expectedHash := ""
	if len(contentHashes) > 0 {
		expectedHash = strings.TrimSpace(contentHashes[0])
	}
	for idx := range events {
		event := events[idx]
		if event.EventType != "member_result_confirmed" {
			continue
		}
		if event.TaskID == nil || *event.TaskID != taskID {
			continue
		}
		payload := teamEventPayloadMap(event)
		from := normalizeTeamMemberRouteKey(eventString(payload, "from", "memberId", "member_id"))
		if from == "" || !teamMemberRouteEquivalent(from, normalizedMember) {
			continue
		}
		confirmedAssignmentID := eventString(payload, "assignmentId", "assignment_id", "workId", "work_id", "sourceWorkId", "source_work_id")
		if expectedAssignment != "" && confirmedAssignmentID != "" && confirmedAssignmentID != expectedAssignment {
			continue
		}
		confirmedHash := eventString(payload, "contentHash", "content_hash")
		if confirmedHash == "" {
			confirmedHash = teamResultContentHash(payload)
		}
		if expectedHash == "" || confirmedHash == "" {
			continue
		}
		if confirmedHash == expectedHash {
			return true, nil
		}
	}
	return false, nil
}

func (s *teamService) buildLeaderMediatedResultNotificationEnvelope(team *models.Team, task *models.TeamTask, member *models.TeamMember, notificationPayload map[string]interface{}, eventID string) (map[string]interface{}, string) {
	leaderKey := "leader"
	if s != nil && s.repo != nil {
		if leader, err := s.repo.GetMemberByID(task.TargetMemberID); err == nil && leader != nil && strings.TrimSpace(leader.MemberKey) != "" {
			leaderKey = leader.MemberKey
		}
	}
	resultMarkdown := eventString(notificationPayload, "resultMarkdown", "summary")
	prompt := fmt.Sprintf(
		"Member %s delivered a confirmed assignment result for root task %s.\n\nResult:\n%s\n\nRecord this result in your synthesis context. If every required assignment has delivered, provide the final user-facing synthesis and call %s for the root task. Do not close the root task if any required assignment is still missing or blocked.",
		member.MemberKey,
		task.MessageID,
		resultMarkdown,
		teamTaskCompletionTool,
	)
	envelope := map[string]interface{}{
		"v":                  1,
		"protocolVersion":    2,
		"messageId":          eventID,
		"teamId":             strconv.Itoa(team.ID),
		"from":               "clawmanager",
		"to":                 leaderKey,
		"replyTo":            teamTaskReplyTarget,
		"requiresCompletion": false,
		"completionTool":     teamTaskCompletionTool,
		"intent":             "member_result_confirmed",
		"taskId":             fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootTaskId":         fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID),
		"rootMessageId":      task.MessageID,
		"workId":             eventString(notificationPayload, "workId", "assignmentId"),
		"assignmentId":       eventString(notificationPayload, "assignmentId", "workId"),
		"title":              member.MemberKey + " assignment result confirmed",
		"prompt":             prompt,
		"rawPrompt":          prompt,
		"monitorPolicy":      defaultTeamMonitorPolicy(),
		"metadata":           notificationPayload,
		"createdAt":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	applyTeamTaskEnvelopeContext(envelope, task, leaderKey)
	return envelope, leaderKey
}

func truncateForSummary(text string, max int) string {
	value := strings.TrimSpace(text)
	if value == "" || max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "..."
}

func (s *teamService) resolveTeamTaskFromEventReferences(teamID int, payload map[string]interface{}) (*models.TeamTask, error) {
	if payload == nil {
		return nil, nil
	}
	for _, key := range []string{
		"rootMessageId", "root_message_id",
		"completionMessageId", "completion_message_id",
		"messageId", "message_id",
		"parentMessageId", "parent_message_id",
		"inReplyTo", "in_reply_to",
		"replyTo", "reply_to",
	} {
		messageID := eventString(payload, key)
		if messageID == "" {
			continue
		}
		if found, err := s.repo.GetTaskByMessageID(teamID, messageID); err != nil {
			return nil, err
		} else if found != nil && found.TeamID == teamID {
			return found, nil
		}
	}
	for _, key := range []string{
		"rootTaskId", "root_task_id",
		"completionTaskId", "completion_task_id",
		"taskId", "task_id",
		"parentTaskId", "parent_task_id",
		"currentTaskId", "current_task_id",
		"runtimeTaskId", "runtime_task_id",
	} {
		if taskID := parseClawManagerTeamTaskRef(teamID, eventString(payload, key)); taskID > 0 {
			found, err := s.repo.GetTaskByID(taskID)
			if err != nil {
				return nil, err
			}
			if found != nil && found.TeamID == teamID {
				return found, nil
			}
		}
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		return s.resolveTeamTaskFromEventReferences(teamID, step)
	}
	return nil, nil
}

func (s *teamService) resolveLeaderMediatedTaskFromWorkItem(teamID int, payload map[string]interface{}, member *models.TeamMember) (*models.TeamTask, error) {
	if s == nil || payload == nil || member == nil || isLeaderTeamMember(member) {
		return nil, nil
	}
	status := normalizedTeamTaskEventStatus(payload)
	eventType := strings.ToLower(strings.TrimSpace(eventString(payload, "event", "type", "event_type")))
	if !isLeaderMediatedWorkerResultLikeEvent(eventType, status, payload) && !isPassiveAssignmentMonitorEvent(eventType, payload) {
		return nil, nil
	}
	ownerKey := normalizeTeamMemberRouteKey(member.MemberKey)
	if ownerKey == "" {
		return nil, nil
	}
	workIDs := map[string]struct{}{
		"member-" + ownerKey: {},
	}
	for _, key := range []string{"workId", "work_id", "assignmentId", "assignment_id", "subtaskId", "subtask_id"} {
		if value := normalizeTeamMemberRouteKey(eventString(payload, key)); value != "" {
			workIDs[value] = struct{}{}
		}
	}
	if step, ok := payload["collaborationStep"].(map[string]interface{}); ok {
		for _, key := range []string{"workId", "work_id", "id", "assignmentId", "assignment_id"} {
			if value := normalizeTeamMemberRouteKey(eventString(step, key)); value != "" {
				workIDs[value] = struct{}{}
			}
		}
	}
	items, err := s.repo.ListWorkItemsByTeamID(teamID, 500)
	if err != nil {
		return nil, err
	}
	for idx := range items {
		item := items[idx]
		if item.TeamID != teamID || item.OwnerMemberID == nil || *item.OwnerMemberID != member.ID {
			continue
		}
		normalizedWorkID := normalizeTeamMemberRouteKey(item.WorkID)
		_, directWorkMatch := workIDs[normalizedWorkID]
		canonicalOwnerMatch := normalizedWorkID == "member-"+ownerKey
		if !directWorkMatch && !canonicalOwnerMatch {
			continue
		}
		task, err := s.repo.GetTaskByID(item.RootTaskID)
		if err != nil {
			return nil, err
		}
		if task == nil || task.TeamID != teamID {
			continue
		}
		if isPassiveAssignmentMonitorEvent(eventType, payload) && isTerminalTeamTaskStatus(task.Status) {
			return nil, nil
		}
		payload["rootTaskId"] = fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
		payload["rootMessageId"] = task.MessageID
		if eventString(payload, "workId", "work_id") == "" {
			payload["workId"] = item.WorkID
		}
		if eventString(payload, "assignmentId", "assignment_id") == "" {
			payload["assignmentId"] = item.WorkID
		}
		return task, nil
	}
	return nil, nil
}

func isLeaderMediatedWorkerResultLikeEvent(eventType, status string, payload map[string]interface{}) bool {
	if isTeamTaskCompletionSignal(eventType, status, payload) || eventBool(payload, "assignmentResultOnly", "assignment_result_only") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "reply", "outbound", "completion", "task_completed":
		return teamEventHasBody(payload)
	default:
		return false
	}
}

func parseClawManagerTeamTaskRef(teamID int, raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if matches := regexp.MustCompile(`^team-(\d+)-task-(\d+)$`).FindStringSubmatch(strings.ToLower(value)); len(matches) == 3 {
		refTeamID, _ := strconv.Atoi(matches[1])
		taskID, _ := strconv.Atoi(matches[2])
		if refTeamID == teamID {
			return taskID
		}
		return 0
	}
	if matches := regexp.MustCompile(`^clawmanager-task-(\d+)$`).FindStringSubmatch(strings.ToLower(value)); len(matches) == 2 {
		taskID, _ := strconv.Atoi(matches[1])
		return taskID
	}
	return 0
}

func (s *teamService) activeTaskForMember(teamID int, member *models.TeamMember, requireTarget bool) (*models.TeamTask, error) {
	if member == nil || member.CurrentTaskID == nil {
		return nil, nil
	}
	found, err := s.repo.GetTaskByID(*member.CurrentTaskID)
	if err != nil {
		return nil, err
	}
	if found == nil || found.TeamID != teamID || isTerminalTeamTaskStatus(found.Status) {
		return nil, nil
	}
	if requireTarget && found.TargetMemberID != member.ID {
		return nil, nil
	}
	return found, nil
}

func (s *teamService) activeTaskFromPeerContext(teamID int, payload map[string]interface{}, member *models.TeamMember) (*models.TeamTask, error) {
	candidates := []*models.TeamMember{member}
	for _, key := range []string{"from", "source", "sourceMemberId", "source_member_id", "sender", "senderMemberId", "sender_member_id", "to", "recipient", "target", "targetMemberId", "target_member_id", "memberId", "member_id"} {
		memberKey := eventString(payload, key)
		if memberKey == "" || memberKey == teamTaskReplyTarget {
			continue
		}
		found, err := s.repo.GetMemberByTeamKey(teamID, memberKey)
		if err != nil {
			return nil, err
		}
		if found != nil {
			candidates = append(candidates, found)
		}
	}
	seen := map[int]struct{}{}
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if _, ok := seen[candidate.ID]; ok {
			continue
		}
		seen[candidate.ID] = struct{}{}
		if task, err := s.activeTaskForMember(teamID, candidate, false); err != nil {
			return nil, err
		} else if task != nil {
			return task, nil
		}
	}
	return nil, nil
}

func (s *teamService) normalizePeerAssistedTeamEvent(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember) (string, *models.TeamMember, error) {
	if team == nil || payload == nil || !isPeerCapableTeamEvent(eventType) {
		return eventType, member, nil
	}
	communicationMode := normalizedTeamCommunicationMode(team.CommunicationMode)
	if communicationMode != teamCommunicationModePeerAssisted && communicationMode != teamCommunicationModeFullMesh {
		return eventType, member, nil
	}
	sourceKey := eventString(payload, "from", "source", "sourceMemberId", "source_member_id", "sender", "senderMemberId", "sender_member_id")
	if sourceKey == "" && member != nil {
		sourceKey = member.MemberKey
	}
	targetKey := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id")
	if sourceKey == "" || targetKey == "" || sourceKey == targetKey || sourceKey == teamTaskReplyTarget || targetKey == teamTaskReplyTarget {
		return eventType, member, nil
	}

	sourceMember, err := s.repo.GetMemberByTeamKey(team.ID, sourceKey)
	if err != nil {
		return eventType, member, err
	}
	targetMember, err := s.repo.GetMemberByTeamKey(team.ID, targetKey)
	if err != nil {
		return eventType, member, err
	}
	if !isActiveTeamMember(sourceMember) || !isActiveTeamMember(targetMember) {
		return eventType, member, nil
	}

	if member == nil || member.MemberKey != sourceMember.MemberKey {
		member = sourceMember
	}
	payload["peer"] = true
	payload["communicationMode"] = communicationMode
	payload["sourceMemberId"] = sourceMember.MemberKey
	payload["targetMemberId"] = targetMember.MemberKey
	payload["from"] = sourceMember.MemberKey
	payload["to"] = targetMember.MemberKey

	rawAction := eventString(payload, "peerAction", "peer_action", "action", "intent", "kind")
	action := normalizeTeamPeerAction(rawAction)
	if strings.TrimSpace(rawAction) == "" && (eventType == "outbound" || eventType == "task_assigned" || eventType == "team_send") && isTeamLeaderRole(sourceMember.Role) && !isTeamLeaderRole(targetMember.Role) {
		action = "handoff"
	}
	payload["peerAction"] = action
	if strings.HasPrefix(eventType, "peer_") {
		return eventType, member, nil
	}
	switch action {
	case "review_request", "peer_review":
		return "peer_review_request", member, nil
	case "handoff":
		return "peer_handoff", member, nil
	default:
		return "peer_request", member, nil
	}
}

func isPeerCapableTeamEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "outbound", "task_assigned", "team_send", "peer_request", "peer_handoff", "peer_review_request", "peer_reply":
		return true
	default:
		return false
	}
}

func normalizeTeamPeerAction(raw string) string {
	action := strings.ToLower(strings.TrimSpace(raw))
	action = strings.ReplaceAll(action, "-", "_")
	action = strings.ReplaceAll(action, " ", "_")
	switch action {
	case "handoff", "assign", "assignment":
		return "handoff"
	case "review", "review_request", "peer_review", "code_review":
		return "review_request"
	case "artifact", "artifact_request", "file_request":
		return "artifact_request"
	case "blocker", "blocker_help", "help":
		return "blocker_help"
	default:
		return "ask"
	}
}

func isActiveTeamMember(member *models.TeamMember) bool {
	return member != nil &&
		member.Status != models.TeamMemberStatusDeleted &&
		member.Status != models.TeamMemberStatusDeleting
}

func enrichTeamCollaborationStep(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) {
	if payload == nil {
		return
	}
	if existing, ok := payload["collaborationStep"].(map[string]interface{}); ok && len(existing) > 0 {
		normalizeExistingCollaborationStep(existing, team, eventType, payload, member, task)
		return
	}

	stepType := collaborationStepTypeForEvent(eventType, payload)
	if stepType == "" {
		return
	}
	actor := collaborationActorKey(payload, member)
	target := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id", "memberId")
	if stepType == "assignment" && target == "" {
		target = eventString(payload, "assignee", "owner", "targetMember")
	}
	status := collaborationStepStatusForEvent(eventType, payload)
	title := collaborationStepTitle(stepType, actor, target, payload)
	summary := eventString(payload, "summary", "resultMarkdown", "result_markdown", "result", "text", "message", "prompt", "instruction", "instructions", "diagnostic", "error", "reason")
	fullContent := collaborationStepContentForEvent(stepType, payload, summary)
	messageID := eventString(payload, "messageId", "message_id")
	rootTaskID := ""
	rootMessageID := ""
	if task != nil {
		rootTaskID = fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
		rootMessageID = task.MessageID
		if messageID == "" {
			messageID = task.MessageID
		}
	}
	if rootTaskID == "" {
		rootTaskID = eventString(payload, "rootTaskId", "root_task_id", "parentTaskId", "parent_task_id")
	}
	if rootMessageID == "" {
		rootMessageID = eventString(payload, "rootMessageId", "root_message_id", "parentMessageId", "parent_message_id", "inReplyTo", "in_reply_to")
	}
	workID := collaborationWorkID(stepType, actor, target, messageID, payload)
	step := map[string]interface{}{
		"id":            workID,
		"workId":        workID,
		"type":          stepType,
		"status":        status,
		"title":         title,
		"summary":       summary,
		"content":       fullContent,
		"actor":         actor,
		"target":        target,
		"messageId":     messageID,
		"rootTaskId":    rootTaskID,
		"rootMessageId": rootMessageID,
		"eventType":     eventType,
		"source":        "clawmanager",
	}
	if progress := eventInt(payload, "progress"); progress > 0 {
		step["progress"] = progress
	}
	if action := eventString(payload, "peerAction", "peer_action", "action", "intent", "kind"); action != "" {
		step["action"] = normalizeTeamPeerAction(action)
	}
	if phase := inferCollaborationPhase(stepType, title, summary, payload); phase != "" {
		step["phase"] = phase
	}
	payload["collaborationStep"] = step
	if rootTaskID != "" && eventString(payload, "rootTaskId", "root_task_id") == "" {
		payload["rootTaskId"] = rootTaskID
	}
	if rootMessageID != "" && eventString(payload, "rootMessageId", "root_message_id") == "" {
		payload["rootMessageId"] = rootMessageID
	}
}

func (s *teamService) projectTeamWorkItem(
	team *models.Team,
	task *models.TeamTask,
	member *models.TeamMember,
	eventType string,
	payload map[string]interface{},
	event *models.TeamEvent,
) error {
	if s == nil || team == nil || task == nil || payload == nil || event == nil {
		return nil
	}
	if isLeaderControlPlaneSnapshotTask(task, payload) {
		return nil
	}
	step, _ := payload["collaborationStep"].(map[string]interface{})
	stepType := eventString(step, "type")
	if stepType == "" {
		stepType = collaborationStepTypeForEvent(eventType, payload)
	}
	if stepType == "" || stepType == "warning" {
		return nil
	}
	if isPassiveAssignmentMonitorEvent(eventType, payload) {
		return nil
	}
	explicitAssignmentID := eventString(payload, "assignmentId", "assignment_id")
	explicitSourceWorkID := eventString(payload, "workId", "work_id", "subtaskId", "subtask_id")
	explicitWorkID := explicitAssignmentID
	if explicitWorkID == "" {
		explicitWorkID = explicitSourceWorkID
	}
	// Transport acknowledgements and unscoped heartbeats are useful in the
	// event log, but they are not business work and must not become Kanban
	// cards. A progress event is materialized only when the orchestrator gave
	// it a stable work identifier.
	if stepType == "ack" || (stepType == "progress" && explicitWorkID == "") {
		return nil
	}
	actor := eventString(step, "actor")
	target := eventString(step, "target")
	if actor == "" {
		actor = collaborationActorKey(payload, member)
	}
	if target == "" {
		target = leaderMediatedRouteTarget(payload)
	}
	if isLeaderMediatedTeam(team) && stepType == "assignment" {
		normalizedTarget := normalizeTeamMemberRouteKey(target)
		if normalizedTarget == "" || isLeaderRouteTarget(normalizedTarget) {
			return nil
		}
	}
	ownerKey := actor
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	if eventKind == "assignment_heartbeat" || strings.EqualFold(eventType, "assignment_heartbeat") {
		return nil
	}
	if stepType == "progress" && eventKind == "assignment_check_requested" && target != "" {
		ownerKey = target
	}
	if stepType == "assignment" && target != "" {
		ownerKey = target
	}
	rootCompletion := isLeaderTeamMember(member) && isTeamTaskCompletionSignal(eventType, normalizedTeamTaskEventStatus(payload), payload)
	if rootCompletion {
		ownerKey = member.MemberKey
		stepType = "final_synthesis"
	}
	ownerKey = normalizeTeamMemberRouteKey(ownerKey)
	if ownerKey == "" || ownerKey == "system" || ownerKey == "clawmanager" {
		return nil
	}
	if isLeaderMediatedTeam(team) && stepType == "progress" && isLeaderRouteTarget(ownerKey) {
		return nil
	}
	owner := member
	if owner == nil || !teamMemberRouteEquivalent(owner.MemberKey, ownerKey) {
		found, err := s.repo.GetMemberByTeamKey(team.ID, ownerKey)
		if err != nil {
			return err
		}
		owner = found
	}
	if eventBool(payload, "assignmentResultOnly", "assignment_result_only") && owner != nil {
		items, listErr := s.repo.ListWorkItemsByRootTaskID(task.ID)
		if listErr != nil {
			return listErr
		}
		matching := make([]models.TeamWorkItem, 0)
		for idx := range items {
			candidate := items[idx]
			if candidate.OwnerMemberID == nil || *candidate.OwnerMemberID != owner.ID || candidate.SupersededBy != nil || candidate.WorkID == "leader-final-synthesis" {
				continue
			}
			candidateAssignmentID := derefTeamString(candidate.AssignmentID)
			if candidateAssignmentID == "" {
				candidateAssignmentID = candidate.WorkID
			}
			if explicitWorkID != "" && (candidateAssignmentID == explicitWorkID || candidate.WorkID == explicitWorkID) {
				matching = []models.TeamWorkItem{candidate}
				break
			}
			matching = append(matching, candidate)
		}
		if len(matching) == 1 {
			canonical := derefTeamString(matching[0].AssignmentID)
			if canonical == "" {
				canonical = matching[0].WorkID
			}
			if explicitSourceWorkID != "" && explicitSourceWorkID != canonical {
				payload["sourceWorkId"] = explicitSourceWorkID
			}
			explicitAssignmentID = canonical
			explicitWorkID = canonical
			payload["assignmentId"] = canonical
			payload["canonicalWorkId"] = canonical
			if matching[0].PhaseID != nil && eventString(payload, "phaseId", "phase_id") == "" {
				payload["phaseId"] = *matching[0].PhaseID
			}
			if matching[0].Revision > 0 && eventInt(payload, "revision") <= 0 {
				payload["revision"] = matching[0].Revision
			}
		}
	}
	assignmentID := explicitAssignmentID
	if assignmentID == "" {
		assignmentID = explicitWorkID
	}
	workID := assignmentID
	// The runtime may echo an assignmentId from a prompt or old plugin payload,
	// but the backend ledger must be owned by the member that actually produced
	// the result. Otherwise one worker can accidentally overwrite another
	// worker's Kanban lane and the Leader will wait on the wrong member.
	if rootCompletion {
		workID = "leader-final-synthesis"
		assignmentID = workID
	} else if workID == "" {
		workID = "member-" + normalizeTeamMemberRouteKey(ownerKey)
		assignmentID = workID
	}
	revision := eventInt(payload, "revision", "workRevision", "work_revision", "artifactRevision", "artifact_revision")
	if revision <= 0 {
		revision = 1
	}
	canonicalWorkID := eventString(payload, "canonicalWorkId", "canonical_work_id")
	if canonicalWorkID == "" {
		canonicalWorkID = assignmentID
	}
	if revision > 1 && !strings.HasSuffix(workID, fmt.Sprintf(":r%d", revision)) {
		workID = fmt.Sprintf("%s:r%d", workID, revision)
	}
	status := models.TeamTaskStatusRunning
	switch stepType {
	case "assignment":
		status = models.TeamTaskStatusDispatched
	case "ack", "progress", "peer_request", "peer_reply":
		status = models.TeamTaskStatusRunning
	case "result", "final_synthesis":
		status = models.TeamTaskStatusSucceeded
	case "blocker":
		status = models.TeamTaskStatusFailed
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	title := eventString(step, "title")
	if title == "" {
		title = collaborationStepTitle(stepType, actor, target, payload)
	}
	requiredForRoot := !eventBool(payload, "optional", "isOptional", "is_optional")
	if required, ok := teamEventBoolValue(payload, "required", "requiredForRoot", "required_for_root"); ok {
		requiredForRoot = required
	}
	item := &models.TeamWorkItem{
		TeamID:          team.ID,
		RootTaskID:      task.ID,
		WorkID:          workID,
		Title:           title,
		Status:          status,
		Revision:        revision,
		RequiredForRoot: requiredForRoot,
		ReviewRequired:  eventBool(payload, "reviewRequired", "review_required"),
		UpdatedAt:       now,
	}
	if assignmentID != "" {
		item.AssignmentID = &assignmentID
	}
	if canonicalWorkID != "" {
		item.CanonicalWorkID = &canonicalWorkID
	}
	phaseID := eventString(payload, "phaseId", "phase_id", "phase", "stage")
	if phaseID == "" {
		phaseID = eventString(step, "phase")
	}
	if phaseID != "" {
		item.PhaseID = &phaseID
	}
	if validatedRevision := eventInt(payload, "validatedRevision", "validated_revision", "reviewedRevision", "reviewed_revision"); validatedRevision > 0 {
		item.ValidatedRevision = &validatedRevision
	}
	if supersededBy := eventString(payload, "supersededBy", "superseded_by"); supersededBy != "" {
		item.SupersededBy = &supersededBy
	}
	if owner != nil {
		item.OwnerMemberID = &owner.ID
	}
	if status == models.TeamTaskStatusRunning {
		item.StartedAt = &now
	}
	if status == models.TeamTaskStatusSucceeded || status == models.TeamTaskStatusFailed {
		item.FinishedAt = &now
		if encoded, err := marshalOptionalJSON(payload); err == nil {
			item.ResultJSON = encoded
		}
		if refs := explicitTeamArtifactReferences(payload); len(refs) > 0 {
			if encoded, err := json.Marshal(refs); err == nil {
				value := string(encoded)
				item.ArtifactRefsJSON = &value
			}
		}
	}
	dependencies := normalizeContextRefs(step["dependsOn"])
	if len(dependencies) == 0 {
		dependencies = normalizeContextRefs(firstTeamValue(payload, "dependsOn", "depends_on"))
	}
	if len(dependencies) > 0 {
		if encoded, err := json.Marshal(dependencies); err == nil {
			value := string(encoded)
			item.DependsOnJSON = &value
		}
	}
	if assignmentID != "" && revision > 1 {
		existingItems, listErr := s.repo.ListWorkItemsByRootTaskID(task.ID)
		if listErr != nil {
			return listErr
		}
		for idx := range existingItems {
			existing := existingItems[idx]
			existingAssignmentID := derefTeamString(existing.AssignmentID)
			if existingAssignmentID == "" {
				existingAssignmentID = existing.WorkID
			}
			if existingAssignmentID != assignmentID || teamMaxInt(existing.Revision, 1) >= revision || existing.SupersededBy != nil {
				continue
			}
			supersededBy := workID
			existing.SupersededBy = &supersededBy
			existing.UpdatedAt = now
			if err := s.repo.UpsertWorkItem(&existing); err != nil {
				return err
			}
		}
	}
	return s.repo.UpsertWorkItem(item)
}

func normalizeExistingCollaborationStep(step map[string]interface{}, team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) {
	if eventBool(payload, "leaderMediatedRouteViolation", "leader_mediated_route_violation") {
		step["type"] = "warning"
		step["status"] = "warning"
	}
	if eventBool(payload, "assignmentResultOnly", "assignment_result_only") {
		step["type"] = "result"
		step["status"] = models.TeamTaskStatusSucceeded
	}
	if eventString(step, "type") == "" {
		step["type"] = collaborationStepTypeForEvent(eventType, payload)
	}
	if eventString(step, "status") == "" {
		step["status"] = collaborationStepStatusForEvent(eventType, payload)
	}
	if eventString(step, "actor") == "" {
		step["actor"] = collaborationActorKey(payload, member)
	}
	if eventString(step, "target") == "" {
		if target := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id", "memberId"); target != "" {
			step["target"] = target
		}
	}
	if task != nil {
		if eventString(step, "rootTaskId") == "" {
			step["rootTaskId"] = fmt.Sprintf("team-%d-task-%d", task.TeamID, task.ID)
		}
		if eventString(step, "rootMessageId") == "" {
			step["rootMessageId"] = task.MessageID
		}
	}
	if eventString(step, "eventType") == "" {
		step["eventType"] = eventType
	}
	if eventString(step, "content", "detail") == "" {
		stepType := eventString(step, "type")
		summary := eventString(step, "summary")
		if content := collaborationStepContentForEvent(stepType, payload, summary); content != "" {
			step["content"] = content
		}
	}
	if eventString(step, "source") == "" {
		step["source"] = "clawmanager"
	}
	if eventString(step, "id", "workId") == "" {
		stepType := eventString(step, "type")
		actor := eventString(step, "actor")
		target := eventString(step, "target")
		messageID := eventString(step, "messageId", "message_id")
		if messageID == "" {
			messageID = eventString(payload, "messageId", "message_id")
		}
		workID := collaborationWorkID(stepType, actor, target, messageID, payload)
		step["id"] = workID
		step["workId"] = workID
	}
	if team != nil && eventString(step, "teamId") == "" {
		step["teamId"] = strconv.Itoa(team.ID)
	}
}

func collaborationStepContentForEvent(stepType string, payload map[string]interface{}, fallback string) string {
	if stepType == "result" || stepType == "blocker" {
		if full := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "diagnostic", "error_message", "error", "message", "text"); full != "" {
			return full
		}
	}
	if content := eventString(payload, "content", "detail", "text", "message", "resultMarkdown", "result_markdown", "result", "answer"); content != "" {
		return content
	}
	return fallback
}

func (s *teamService) projectTeamWorkflowLedger(team *models.Team, task *models.TeamTask, member *models.TeamMember, eventType string, payload map[string]interface{}, now time.Time) (bool, error) {
	if s == nil || team == nil || task == nil || payload == nil || isTerminalTeamTaskStatus(task.Status) {
		return false, nil
	}
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	step, _ := payload["collaborationStep"].(map[string]interface{})
	stepType := strings.ToLower(strings.TrimSpace(eventString(step, "type")))
	isPlan := eventKind == "leader_plan" && member != nil && isLeaderTeamMember(member)
	isAssignment := stepType == "assignment" || eventBool(payload, "leaderDispatchOnly", "leader_dispatch_only") || eventType == "team_send" || eventType == "task_assigned" || eventType == "outbound"
	isAssignmentResult := eventBool(payload, "assignmentResultOnly", "assignment_result_only")
	if !isPlan && !isAssignment && !isAssignmentResult {
		return false, nil
	}

	changed := false
	planVersion := int64(eventInt(payload, "planVersion", "plan_version"))
	if planVersion <= 0 {
		planVersion = task.PlanVersion
		if planVersion <= 0 {
			planVersion = 1
		}
	}
	if planVersion > task.PlanVersion {
		task.PlanVersion = planVersion
		changed = true
	}

	if isPlan {
		phaseValues := firstTeamValue(payload, "phases", "workflowPhases", "workflow_phases")
		if plan, ok := payload["workflowPlan"].(map[string]interface{}); ok {
			if nested := firstTeamValue(plan, "phases", "workflowPhases", "workflow_phases"); nested != nil {
				phaseValues = nested
			}
		}
		if rawPhases, ok := phaseValues.([]interface{}); ok {
			for index, rawPhase := range rawPhases {
				phaseMap, ok := rawPhase.(map[string]interface{})
				if !ok {
					continue
				}
				phaseID := eventString(phaseMap, "phaseId", "phase_id", "id", "name")
				if phaseID == "" {
					continue
				}
				phase := &models.TeamWorkflowPhase{
					TeamID:           team.ID,
					RootTaskID:       task.ID,
					PhaseID:          phaseID,
					PlanVersion:      planVersion,
					SequenceNo:       index,
					Status:           eventString(phaseMap, "status"),
					RequiredForRoot:  true,
					DecisionRequired: eventBool(phaseMap, "decisionRequiredAfterCompletion", "decisionRequired", "decision_required"),
					UpdatedAt:        now,
				}
				if phase.Status == "" {
					phase.Status = teamPhaseStatusPlanned
				}
				if required, exists := teamEventBoolValue(phaseMap, "required", "requiredForRoot", "required_for_root"); exists {
					phase.RequiredForRoot = required
				}
				if dependencies := normalizeContextRefs(firstTeamValue(phaseMap, "dependsOn", "depends_on")); len(dependencies) > 0 {
					if encoded, err := json.Marshal(dependencies); err == nil {
						value := string(encoded)
						phase.DependsOnJSON = &value
					}
				}
				if nextPhaseID := eventString(phaseMap, "nextPhaseId", "next_phase_id"); nextPhaseID != "" {
					phase.NextPhaseID = &nextPhaseID
				}
				if policy := eventString(phaseMap, "completionPolicy", "completion_policy"); policy != "" {
					phase.CompletionPolicy = &policy
				}
				if err := s.repo.UpsertWorkflowPhase(phase); err != nil {
					return changed, err
				}
				if task.CurrentPhaseID == nil && phase.Status != teamPhaseStatusPlanned {
					task.CurrentPhaseID = &phaseID
				}
				changed = true
			}
		}
		workflowState := eventString(payload, "workflowState", "workflow_state")
		if workflowState == "" || workflowState == "open" {
			workflowState = teamWorkflowStateExecuting
		}
		if task.WorkflowState != workflowState {
			task.WorkflowState = workflowState
			changed = true
		}
	}

	phaseID := eventString(payload, "phaseId", "phase_id", "phase", "stage")
	if phaseID == "" {
		phaseID = eventString(step, "phase")
	}
	if isAssignment {
		if task.WorkflowState != teamWorkflowStateExecuting && task.WorkflowState != teamWorkflowStateAwaitingPhaseResults {
			task.WorkflowState = teamWorkflowStateExecuting
			changed = true
		}
		if phaseID != "" {
			task.CurrentPhaseID = &phaseID
			phase := &models.TeamWorkflowPhase{
				TeamID:          team.ID,
				RootTaskID:      task.ID,
				PhaseID:         phaseID,
				PlanVersion:     planVersion,
				Status:          teamPhaseStatusAwaitingResults,
				RequiredForRoot: true,
				UpdatedAt:       now,
			}
			if err := s.repo.UpsertWorkflowPhase(phase); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	if isAssignmentResult && phaseID != "" {
		phases, err := s.repo.ListWorkflowPhasesByRootTaskID(task.ID)
		if err != nil {
			return changed, err
		}
		items, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
		if err != nil {
			return changed, err
		}
		phaseIncomplete := false
		for idx := range items {
			item := items[idx]
			if derefTeamString(item.PhaseID) != phaseID || item.SupersededBy != nil || !(item.RequiredForRoot || item.AssignmentID == nil) {
				continue
			}
			if item.Status != models.TeamTaskStatusSucceeded {
				phaseIncomplete = true
				break
			}
		}
		for idx := range phases {
			phase := phases[idx]
			if phase.PhaseID != phaseID || phase.PlanVersion != planVersion {
				continue
			}
			if phaseIncomplete {
				phase.Status = teamPhaseStatusAwaitingResults
				task.WorkflowState = teamWorkflowStateAwaitingPhaseResults
			} else if phase.DecisionRequired {
				phase.Status = teamPhaseStatusAwaitingLeaderDecision
				task.WorkflowState = teamWorkflowStateAwaitingLeaderDecision
			} else {
				phase.Status = teamPhaseStatusCompleted
				phase.CompletedAt = &now
				task.WorkflowState = teamWorkflowStateAwaitingLeaderDecision
			}
			phase.UpdatedAt = now
			if err := s.repo.UpsertWorkflowPhase(&phase); err != nil {
				return changed, err
			}
			changed = true
			break
		}
	}
	if changed {
		task.LedgerVersion++
		task.UpdatedAt = now
	}
	return changed, nil
}

func collaborationStepTypeForEvent(eventType string, payload map[string]interface{}) string {
	status := normalizedTeamTaskEventStatus(payload)
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	switch eventKind {
	case "leader_plan", "worker_plan", "worker_progress", "artifact_changed", "assignment_check_requested", "assignment_check_result", "assignment_heartbeat", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder", "completion_deferred", "assignment_recovery_started", "assignment_reissued", "agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis":
		return "progress"
	case "completion_candidate":
		return "progress"
	case "completion_validation_warning", "completion_needs_confirmation", "completion_rejected", "assignment_recovery_exhausted":
		return "warning"
	}
	if eventBool(payload, "leaderMediatedRouteViolation", "leader_mediated_route_violation") {
		return "warning"
	}
	if eventBool(payload, "assignmentResultOnly", "assignment_result_only") {
		return "result"
	}
	if eventBool(payload, "leaderDispatchOnly", "leader_dispatch_only") {
		return "assignment"
	}
	if isTeamTaskCompletionSignal(eventType, status, payload) {
		return "result"
	}
	if isNonAuthoritativeDispatchFailure(eventType, payload) {
		return "warning"
	}
	switch eventType {
	case "leader_plan", "worker_plan", "worker_progress", "artifact_changed", "assignment_check_requested", "assignment_check_result", "assignment_heartbeat", "leader_synthesis", "leader_synthesis_reminder", "leader_decision_reminder", "completion_candidate", "completion_deferred", "assignment_recovery_started", "assignment_reissued", "agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis":
		return "progress"
	case "completion_validation_warning", "completion_needs_confirmation", "completion_rejected", "assignment_recovery_exhausted":
		return "warning"
	case "outbound", "task_assigned", "team_send":
		return "assignment"
	case "peer_handoff":
		return "assignment"
	case "peer_request", "peer_review_request":
		return "peer_request"
	case "peer_reply":
		return "peer_reply"
	case "task_received":
		return "ack"
	case "task_started", "task_progress", "progress":
		return "progress"
	case "task_completed", "completion":
		return "progress"
	case "task_failed", "message_failed", "task_stale":
		if !isTeamTaskFailureSignal(eventType, status, payload) && eventType != "task_stale" {
			return "warning"
		}
		return "blocker"
	case "message_warning":
		return "warning"
	case "reply":
		if eventBool(payload, "final", "isFinal", "complete", "completed", "taskCompleted") || isFinalCompletionPayload(payload) {
			return "result"
		}
		return "progress"
	default:
		if eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id") != "" {
			return "progress"
		}
		return ""
	}
}

func collaborationStepStatusForEvent(eventType string, payload map[string]interface{}) string {
	status := normalizedTeamTaskEventStatus(payload)
	eventKind := strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind")))
	switch eventKind {
	case "assignment_check_requested", "assignment_check_result", "assignment_heartbeat", "leader_synthesis_reminder", "assignment_recovery_started", "assignment_reissued":
		return models.TeamTaskStatusRunning
	case "completion_candidate":
		return "result_pending_confirmation"
	case "completion_validation_warning", "assignment_recovery_exhausted":
		return "warning"
	}
	if eventBool(payload, "leaderMediatedRouteViolation", "leader_mediated_route_violation") {
		return "warning"
	}
	if eventBool(payload, "assignmentResultOnly", "assignment_result_only") {
		return models.TeamTaskStatusSucceeded
	}
	if eventBool(payload, "leaderDispatchOnly", "leader_dispatch_only") {
		return models.TeamTaskStatusDispatched
	}
	if isTeamTaskCompletionSignal(eventType, status, payload) {
		return models.TeamTaskStatusSucceeded
	}
	if isSuccessfulTeamTaskEventStatus(status) {
		switch eventType {
		case "task_completed", "completion", "task_failed", "message_failed":
			return models.TeamTaskStatusRunning
		default:
			return models.TeamTaskStatusSucceeded
		}
	}
	if isFailedTeamTaskEventStatus(status) || eventType == "task_failed" || eventType == "message_failed" {
		if isNonAuthoritativeDispatchFailure(eventType, payload) {
			return "warning"
		}
		return models.TeamTaskStatusFailed
	}
	switch eventType {
	case "assignment_check_requested", "assignment_check_result", "assignment_heartbeat", "leader_synthesis_reminder", "assignment_recovery_started", "assignment_reissued":
		return models.TeamTaskStatusRunning
	case "completion_candidate":
		return "result_pending_confirmation"
	case "completion_validation_warning", "assignment_recovery_exhausted":
		return "warning"
	case "task_stale":
		return models.TeamTaskStatusStale
	case "outbound", "task_assigned", "team_send", "peer_request", "peer_handoff", "peer_review_request":
		return models.TeamTaskStatusDispatched
	case "task_received":
		return "acknowledged"
	case "task_started", "task_progress", "progress", "reply", "peer_reply":
		return models.TeamTaskStatusRunning
	case "message_warning":
		return "warning"
	default:
		if status != "" {
			return status
		}
		return "observed"
	}
}

func collaborationActorKey(payload map[string]interface{}, member *models.TeamMember) string {
	if actor := eventString(payload, "from", "sourceMemberId", "source_member_id", "sender", "senderMemberId", "sender_member_id", "memberId", "member_id"); actor != "" {
		return actor
	}
	if member != nil {
		return member.MemberKey
	}
	return "system"
}

func collaborationStepTitle(stepType, actor, target string, payload map[string]interface{}) string {
	if title := eventString(payload, "stepTitle", "step_title", "title", "intent"); title != "" && !looksLikeOpaqueRuntimeTaskID(title) {
		return title
	}
	switch strings.ToLower(strings.TrimSpace(eventString(payload, "eventKind", "event_kind", "kind"))) {
	case "leader_plan":
		return "Leader execution plan"
	case "worker_plan":
		return actor + " execution plan"
	case "worker_progress":
		return actor + " updates progress"
	case "assignment_check_requested":
		if target != "" {
			return "ClawManager checks " + target
		}
		return "ClawManager checks assignment status"
	case "assignment_check_result":
		return actor + " reports status"
	case "assignment_heartbeat":
		return actor + " heartbeat"
	case "leader_synthesis":
		return "Leader synthesizes results"
	case "agent_narrative", "agent_plan", "agent_assignment", "agent_handoff", "agent_progress", "agent_delivery", "agent_review", "agent_synthesis":
		return actor + " collaboration update"
	case "leader_synthesis_reminder":
		return "Leader final synthesis requested"
	case "completion_candidate":
		return actor + " produced candidate result"
	case "completion_validation_warning":
		return "Completion needs artifact recovery"
	case "assignment_recovery_started":
		return "Recovery started for " + actor
	case "assignment_reissued":
		return "Assignment reissued to " + actor
	case "assignment_recovery_exhausted":
		return "Recovery needs attention"
	}
	switch stepType {
	case "assignment":
		if target != "" {
			return "Assign to " + target
		}
		return "Assign subtask"
	case "peer_request":
		if target != "" {
			return actor + " asks " + target
		}
		return "Peer request"
	case "peer_reply":
		return actor + " replies"
	case "ack":
		return actor + " accepted task"
	case "progress":
		return actor + " updates progress"
	case "result":
		return actor + " delivers result"
	case "blocker":
		return actor + " reports blocker"
	case "warning":
		return actor + " reports warning"
	default:
		return stepType
	}
}

func collaborationWorkID(stepType, actor, target, messageID string, payload map[string]interface{}) string {
	if id := eventString(payload, "workId", "work_id", "stepId", "step_id", "subtaskId", "subtask_id"); id != "" {
		return id
	}
	parts := []string{stepType, actor, target}
	if messageID != "" {
		parts = append(parts, messageID)
	}
	if len(parts) == 0 {
		return "team-step"
	}
	return strings.ToLower(strings.Trim(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		if r == '-' || r == '_' {
			return '-'
		}
		return '-'
	}, strings.Join(parts, "-")), "-"))
}

func inferCollaborationPhase(stepType, title, summary string, payload map[string]interface{}) string {
	if phase := eventString(payload, "phase", "stage"); phase != "" {
		return phase
	}
	text := strings.ToLower(title + " " + summary)
	switch {
	case strings.Contains(text, "review") || strings.Contains(text, "verify"):
		return "verification"
	case strings.Contains(text, "design") || strings.Contains(text, "ui") || strings.Contains(text, "prototype"):
		return "design"
	case strings.Contains(text, "research") || strings.Contains(text, "璋冪爺"):
		return "research"
	case stepType == "assignment":
		return "decomposition"
	case stepType == "result":
		return "delivery"
	default:
		return "execution"
	}
}

func isFinalCompletionPayload(payload map[string]interface{}) bool {
	status := normalizedTeamTaskEventStatus(payload)
	return hasAuthoritativeTeamCompletionPayload("reply", status, payload)
}

func looksLikeOpaqueRuntimeTaskID(value string) bool {
	normalized := strings.TrimSpace(value)
	return regexp.MustCompile(`^(task[-_][a-z0-9-]+|team-\d+-task-\d+)$`).MatchString(strings.ToLower(normalized))
}

func normalizeFinalReplyTaskEvent(eventType string, payload map[string]interface{}, task *models.TeamTask, _ *models.TeamMember) string {
	if task == nil || !strings.EqualFold(strings.TrimSpace(eventType), "reply") {
		return eventType
	}
	hasCompletionTool := hasTeamTaskCompletionToolCall(payload)
	if !hasCompletionTool {
		return eventType
	}
	if !eventBool(payload, "final", "isFinal", "complete", "completed", "taskCompleted") {
		return eventType
	}
	if !teamEventHasBody(payload) {
		return eventType
	}
	payload["originalEvent"] = eventType
	payload["event"] = "task_completed"
	payload["type"] = "task_completed"
	payload["status"] = "succeeded"
	payload["availability"] = models.TeamMemberAvailabilityIdle
	payload["runtimeStatus"] = "succeeded"
	if eventString(payload, "resultMarkdown") == "" {
		if text := eventString(payload, "text", "result", "summary"); text != "" {
			payload["resultMarkdown"] = text
		}
	}
	if eventString(payload, "summary") == "" {
		if text := eventString(payload, "text", "resultMarkdown", "result"); text != "" {
			payload["summary"] = text
		}
	}
	return "task_completed"
}

func markLegacyRuntimeCompletionCandidate(eventType string, payload map[string]interface{}, task *models.TeamTask, member *models.TeamMember) string {
	if task == nil || member == nil || payload == nil || teamRedisProtocolVersion(payload) >= 2 {
		return eventType
	}
	normalizedEvent := strings.ToLower(strings.TrimSpace(eventType))
	if normalizedEvent != "reply" && normalizedEvent != "message" {
		return eventType
	}
	explicitControlPlaneCompletion := isLeaderControlPlaneSnapshotTask(task, payload) &&
		hasTeamCompletionResultBody(payload) &&
		(strings.EqualFold(strings.TrimSpace(eventString(payload, "completionSource", "completion_source")), teamTaskCompletionTool) ||
			hasTeamTaskCompletionToolCall(payload) ||
			eventBool(payload, "explicitCompletion", "explicit_completion"))
	if member.ID != task.TargetMemberID || (isDispatchOnlyCompletionPayload(payload) && !explicitControlPlaneCompletion) {
		return eventType
	}
	resultText := directTaskCompletionReplyText(payload)
	if resultText == "" {
		resultText = eventString(payload, "summary")
	}
	if resultText == "" {
		return eventType
	}
	if !explicitControlPlaneCompletion {
		if isInterimOrDelegationReplyText(resultText) || !looksLikeLegacyRuntimeCompletionReport(task, payload, resultText) {
			return eventType
		}
	}
	payload["legacyCompletionCandidate"] = true
	payload["completionSource"] = "legacy_runtime_reply"
	payload["rootTaskTerminal"] = true
	payload["final"] = true
	payload["status"] = models.TeamTaskStatusSucceeded
	payload["availability"] = models.TeamMemberAvailabilityIdle
	payload["runtimeStatus"] = models.TeamTaskStatusSucceeded
	if eventString(payload, "resultMarkdown", "result_markdown") == "" {
		payload["resultMarkdown"] = resultText
	}
	if eventString(payload, "summary") == "" {
		payload["summary"] = compactTeamEventSummary(resultText, 240)
	}
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
	}
	payload["event"] = "task_completed"
	payload["type"] = "task_completed"
	return "task_completed"
}

func looksLikeLegacyRuntimeCompletionReport(task *models.TeamTask, payload map[string]interface{}, resultText string) bool {
	text := strings.TrimSpace(resultText)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	hasCanonicalArtifact := regexp.MustCompile(`/team/(artifacts|results|tasks)/[^\s)\]<>{},\\\x60"']+\.[A-Za-z0-9]+`).FindString(text) != ""
	hasCompletionWord := strings.Contains(lower, "completed") ||
		strings.Contains(lower, "complete") ||
		strings.Contains(lower, "succeeded") ||
		strings.Contains(lower, "delivered") ||
		strings.Contains(text, "\u5b8c\u6210") ||
		strings.Contains(text, "\u5df2\u5b8c\u6210") ||
		strings.Contains(text, "\u4ea4\u4ed8")
	hasBootstrapWord := strings.Contains(lower, "bootstrap") ||
		strings.Contains(lower, "introduction") ||
		strings.Contains(text, "\u5f15\u5bfc") ||
		strings.Contains(text, "\u4ecb\u7ecd") ||
		strings.Contains(text, "\u56e2\u961f")
	if hasCanonicalArtifact && (hasCompletionWord || looksLikeFinalResultText(text) || len([]rune(compact)) >= 80) {
		return true
	}
	if strings.Contains(strings.ToLower(task.MessageID), "bootstrap-introduction") && hasBootstrapWord && hasCompletionWord && len([]rune(compact)) >= 80 {
		return true
	}
	if eventBool(payload, "final", "isFinal", "complete", "completed", "taskCompleted") && looksLikeFinalResultText(text) && len([]rune(compact)) >= 120 {
		return true
	}
	return false
}

func isImplicitDirectTaskCompletionReply(task *models.TeamTask, member *models.TeamMember, payload map[string]interface{}) bool {
	if task == nil || member == nil || payload == nil {
		return false
	}
	if member.ID != task.TargetMemberID {
		return false
	}
	status := normalizedTeamTaskEventStatus(payload)
	if isTeamTaskFailureSignal("reply", status, payload) || isTeamTaskRunningSignal("reply", status, payload) {
		return false
	}
	resultText := directTaskCompletionReplyText(payload)
	if resultText == "" || isInterimOrDelegationReplyText(resultText) {
		return false
	}
	if eventBool(payload, "final", "isFinal", "complete", "completed", "taskCompleted") {
		return true
	}
	if eventString(payload, "resultMarkdown", "result_markdown", "result", "answer") != "" {
		return true
	}
	switch status {
	case "succeeded", "success", "completed", "complete", "done", "finished", "ok":
		return true
	}
	compact := strings.TrimSpace(resultText)
	compact = strings.Join(strings.Fields(compact), "")
	return len([]rune(compact)) >= 36 && looksLikeSubstantialFinalReply(resultText)
}

func directTaskCompletionReplyText(payload map[string]interface{}) string {
	for _, record := range eventRecordCandidates(payload) {
		if text := eventString(record, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary"); text != "" {
			return text
		}
	}
	return ""
}

func compactTeamEventSummary(value string, limit int) string {
	text := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if text == "" || limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func isInterimOrDelegationReplyText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return true
	}
	if looksLikeLeaderDispatchOnlyText(normalized) {
		return true
	}
	if looksLikeFinalResultText(normalized) {
		return false
	}
	lower := strings.ToLower(normalized)
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	if len([]rune(compact)) <= 12 {
		return true
	}
	for _, prefix := range interimReplyPrefixes() {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) || strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	if containsAnyTeamTextMarker(normalized, interimReplyMarkers()) {
		return true
	}
	return false
}

func interimReplyPrefixes() []string {
	return []string{
		"\u6536\u5230",
		"\u597d\u7684",
		"\u597d\uff0c",
		"\u597d",
		"ok",
		"okay",
		"\u5904\u7406\u4e2d",
		"\u6b63\u5728",
		"\u51c6\u5907",
		"\u6211\u5c06",
		"\u8ba9\u6211",
		"\u5148\u770b",
		"\u7a0d\u7b49",
		"let me",
		"now let me",
		"i will",
		"i'll",
		"checking",
		"working on",
		"good, i have",
		"i have the",
		"i have all",
		"i can see",
	}
}

func interimReplyMarkers() []string {
	return []string{
		"\u6b63\u5728\u6574\u7406",
		"\u73b0\u5728\u6574\u7406",
		"\u7a0d\u540e",
		"\u7ee7\u7eed\u7b49\u5f85",
		"\u4ecd\u5728\u7b49\u5f85",
		"\u5df2\u6536\u5230\u5e76\u5f52\u6863",
		"\u5ef6\u8fdf\u9001\u8fbe",
		"\u91cd\u590d\u901a\u77e5",
		"\u91cd\u590d\u7ed3\u679c",
		"\u91cd\u590d\u6295\u9012",
		"\u7b49\u5f85\u5b8c\u6210",
		"\u7b49\u5f85\u4ed6",
		"\u7b49\u5f85\u5979",
		"\u7b49\u5f85 designer",
		"\u7b49\u5f85 architect",
		"\u7b49\u5f85 pm",
		"\u7b49\u5f85worker",
		"\u7b49\u5f85 worker",
		"\u6d3e\u5355",
		"\u5df2\u6d3e\u53d1",
		"\u6d3e\u53d1\u7ed9",
		"\u4e0b\u53d1\u7ed9",
		"\u8f6c\u6d3e\u7ed9",
		"\u4ea4\u7ed9worker",
		"\u4ea4\u7ed9 worker",
		"\u8ba9worker",
		"\u8ba9 worker",
		"\u8bf7worker",
		"\u8bf7 worker",
		"worker\u5728\u7ebf\u7a7a\u95f2",
		"sent to worker",
		"assigned to worker",
		"waiting for worker",
		"still waiting",
		"continuing to wait",
		"waiting on ",
		"duplicate notification",
		"duplicate result",
		"duplicate delivery",
		"delayed notification",
		"handoff to worker",
		"now let me write",
		"then finalize",
		"and then finalize",
		"write the comprehensive report",
	}
}

func looksLikeSubstantialFinalReply(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	if strings.ContainsAny(normalized, "#*>|`") {
		return true
	}
	if strings.ContainsAny(normalized, ";\n") || strings.Contains(normalized, "\u3002") || strings.Contains(normalized, "\uff1b") || strings.Contains(normalized, "\uff0c") {
		return true
	}
	return containsAnyTeamTextMarker(normalized, []string{
		"\u5b8c\u6210",
		"\u603b\u7ed3",
		"\u62a5\u544a",
		"\u7ed3\u679c",
		"completed",
		"summary",
	})
}
func hasTeamTaskCompletionToolCall(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	if eventString(payload, "toolCallName", "tool_call_name", "calledTool", "called_tool") == teamTaskCompletionTool {
		return true
	}
	for _, key := range []string{"toolCall", "tool_call", "function_call"} {
		record, ok := payload[key].(map[string]interface{})
		if !ok || record == nil {
			continue
		}
		if eventString(record, "name", "function", "functionName", "function_name", "tool", "toolName", "tool_name") == teamTaskCompletionTool {
			return true
		}
	}
	return false
}

func eventRecordCandidates(payload map[string]interface{}) []map[string]interface{} {
	if payload == nil {
		return nil
	}
	records := []map[string]interface{}{payload}
	for _, key := range []string{"sent", "metadata", "data", "envelope", "task", "toolCall", "tool_call", "function_call"} {
		if record, ok := payload[key].(map[string]interface{}); ok && record != nil {
			records = append(records, record)
		}
	}
	return records
}

func (s *teamService) enrichOutboundEventFromInbox(teamID int, bus *redisBus, payload map[string]interface{}, messageID string) (map[string]interface{}, error) {
	targetMember := eventString(payload, "to", "recipient", "target", "targetMemberId", "target_member_id")
	if targetMember == "" {
		return payload, nil
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		messages, err := bus.XRevRange(context.Background(), teamInboxKey(teamID, targetMember), 100)
		if err != nil {
			lastErr = err
			continue
		}
		for _, inboxMessage := range messages {
			if !redisStreamMessageMatches(inboxMessage, messageID) {
				continue
			}
			envelope := mergeRedisEventPayload(inboxMessage.Fields)
			return mergeMissingEventFields(payload, envelope), nil
		}
	}
	return payload, lastErr
}

func redisStreamMessageMatches(message redisStreamMessage, messageID string) bool {
	if strings.TrimSpace(message.Fields["message_id"]) == messageID {
		return true
	}
	payload := mergeRedisEventPayload(message.Fields)
	return eventString(payload, "message_id", "messageId") == messageID
}

func mergeMissingEventFields(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		if existing, ok := merged[key]; !ok || isEmptyEventValue(existing) {
			merged[key] = value
		}
	}
	if metadata, ok := extra["metadata"].(map[string]interface{}); ok {
		for key, value := range metadata {
			if existing, ok := merged[key]; !ok || isEmptyEventValue(existing) {
				merged[key] = value
			}
		}
	}
	return merged
}

func isEmptyEventValue(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func isOutboundTeamEvent(eventType string) bool {
	switch eventType {
	case "outbound", "task_assigned":
		return true
	default:
		return false
	}
}

func teamEventHasBody(payload map[string]interface{}) bool {
	if eventString(payload, "text", "title", "prompt", "instruction", "instructions", "summary", "resultMarkdown") != "" {
		return true
	}
	for idx, record := range eventRecordCandidates(payload) {
		if idx == 0 {
			continue
		}
		if eventString(record, "text", "title", "prompt", "instruction", "instructions", "summary", "resultMarkdown") != "" {
			return true
		}
	}
	return false
}

func (s *teamService) normalizeTeamArtifactReferences(team *models.Team, payload map[string]interface{}) {
	if s == nil || team == nil || payload == nil {
		return
	}
	physicalRoot := filepath.ToSlash(filepath.Clean(s.teamRuntimeSharedPathFor(team.UserID, team.ID)))
	logicalRoot := strings.TrimRight(strings.TrimSpace(team.SharedMountPath), "/")
	if logicalRoot == "" {
		logicalRoot = teamSharedMountPath
	}
	normalizePath := func(raw string) string {
		value := strings.TrimSpace(raw)
		if value == "" {
			return raw
		}
		value = strings.TrimPrefix(value, "file://")
		value = strings.ReplaceAll(value, "\\", "/")
		switch {
		case strings.HasPrefix(value, logicalRoot+"/"):
			return teamSharedMountPath + strings.TrimPrefix(value, logicalRoot)
		case strings.HasPrefix(value, teamSharedMountPath+"/"):
			return value
		case strings.HasPrefix(value, physicalRoot+"/"):
			return teamSharedMountPath + strings.TrimPrefix(value, physicalRoot)
		case strings.HasPrefix(value, "$CLAWMANAGER_TEAM_SHARED_DIR/"):
			return teamSharedMountPath + strings.TrimPrefix(value, "$CLAWMANAGER_TEAM_SHARED_DIR")
		case strings.HasPrefix(value, "CLAWMANAGER_TEAM_SHARED_DIR/"):
			return teamSharedMountPath + strings.TrimPrefix(value, "CLAWMANAGER_TEAM_SHARED_DIR")
		case strings.HasPrefix(value, "team/artifacts/") ||
			strings.HasPrefix(value, "team/results/") ||
			strings.HasPrefix(value, "team/tasks/") ||
			strings.HasPrefix(value, "team/inbox/") ||
			strings.HasPrefix(value, "team/tmp/") ||
			strings.HasPrefix(value, "team/status/"):
			return "/" + value
		default:
			return raw
		}
	}
	normalizeText := func(raw string) string {
		if strings.TrimSpace(raw) == "" {
			return raw
		}
		// Free-form fields may contain serialized JSON and Markdown. Replacing
		// every backslash corrupts escapes such as `\n` into `/n`, after which
		// artifact extraction sees JSON syntax as part of the filename.
		text := raw
		if physicalRoot != "" {
			text = strings.ReplaceAll(text, "file://"+physicalRoot+"/", teamSharedMountPath+"/")
			text = strings.ReplaceAll(text, physicalRoot+"/", teamSharedMountPath+"/")
		}
		text = strings.ReplaceAll(text, "$CLAWMANAGER_TEAM_SHARED_DIR/", teamSharedMountPath+"/")
		text = regexp.MustCompile(`(^|[\s(\[>])team/(artifacts|results|tasks|inbox|status|tmp)/([^\s)\]<>{},\\\x60"']+)`).ReplaceAllString(text, `${1}/team/${2}/${3}`)
		return text
	}
	var normalizeValue func(value interface{}) interface{}
	normalizeValue = func(value interface{}) interface{} {
		switch typed := value.(type) {
		case string:
			normalized := normalizePath(typed)
			if normalized != typed {
				return normalized
			}
			return normalizeText(typed)
		case []interface{}:
			for idx := range typed {
				typed[idx] = normalizeValue(typed[idx])
			}
			return typed
		case map[string]interface{}:
			for key, nested := range typed {
				typed[key] = normalizeValue(nested)
			}
			return typed
		default:
			return value
		}
	}
	for key, value := range payload {
		payload[key] = normalizeValue(value)
	}
}

func (s *teamService) missingTeamArtifactReferences(team *models.Team, payload map[string]interface{}) []string {
	if s == nil || team == nil || payload == nil {
		return nil
	}
	explicitRefsOnly := teamRedisProtocolVersion(payload) >= 2
	refs := collectTeamArtifactReferences(payload)
	invalidRelativeRefs := collectInvalidRelativeTeamArtifactReferences(payload)
	if explicitRefsOnly {
		refs = explicitTeamArtifactReferences(payload)
		invalidRelativeRefs = nil
	}
	if len(refs) == 0 && len(invalidRelativeRefs) == 0 {
		return nil
	}
	root := filepath.Clean(s.teamRuntimeSharedPathFor(team.UserID, team.ID))
	missing := make([]string, 0)
	for _, ref := range invalidRelativeRefs {
		missing = append(missing, "invalid relative team artifact path: "+ref)
	}
	seen := map[string]struct{}{}
	for _, ref := range refs {
		rel := strings.TrimPrefix(strings.TrimSpace(ref), teamSharedMountPath+"/")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		target := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		if info, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, ref)
			}
		} else if !info.Mode().IsRegular() && explicitRefsOnly {
			missing = append(missing, ref)
		}
	}
	return missing
}

func explicitTeamArtifactReferences(payload map[string]interface{}) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload["artifactRefs"]
	if !ok {
		value = payload["artifact_refs"]
	}
	refs := make([]string, 0)
	seen := map[string]struct{}{}
	appendRef := func(raw interface{}) {
		ref := trimTeamArtifactReferenceToken(fmt.Sprintf("%v", raw))
		if !strings.HasPrefix(ref, teamSharedMountPath+"/") || strings.ContainsAny(ref, "`\"'{}[]") {
			return
		}
		if strings.HasSuffix(ref, "/") {
			return
		}
		if _, exists := seen[ref]; exists {
			return
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			appendRef(item)
		}
	case []string:
		for _, item := range typed {
			appendRef(item)
		}
	}
	return refs
}

func collectTeamArtifactReferences(value interface{}) []string {
	refs := make([]string, 0)
	// Stop at Markdown delimiters and JSON syntax. Runtime adapters can embed
	// the same completion payload as text, Markdown, or serialized JSON; a
	// greedy token here turns `/team/file.md` into a nonexistent filename such
	// as `/team/file.md`/n\",\"artifactRefs\"...`.
	pattern := regexp.MustCompile(`/team/(artifacts|results|tasks|inbox|status|tmp)/[^\s)\]<>{},\\\x60"']+`)
	seen := map[string]struct{}{}
	var walk func(interface{})
	walk = func(current interface{}) {
		switch typed := current.(type) {
		case string:
			for _, match := range pattern.FindAllString(typed, -1) {
				ref := trimTeamArtifactReferenceToken(match)
				if ref == "" || !looksLikeTeamArtifactFileReference(ref) {
					continue
				}
				if _, exists := seen[ref]; exists {
					continue
				}
				seen[ref] = struct{}{}
				refs = append(refs, ref)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item)
			}
		case map[string]interface{}:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return refs
}

func trimTeamArtifactReferenceToken(value string) string {
	ref := strings.TrimSpace(value)
	if ref == "" {
		return ""
	}
	ref = strings.TrimRight(ref, " \t\r\n.,;:!?。；：，、！？)")
	ref = strings.TrimRight(ref, "]}>）】》”’")
	return ref
}

func looksLikeTeamArtifactFileReference(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasSuffix(ref, "/") {
		return false
	}
	lastSlash := strings.LastIndex(ref, "/")
	name := ref
	if lastSlash >= 0 {
		name = ref[lastSlash+1:]
	}
	return strings.Contains(name, ".")
}
func collectInvalidRelativeTeamArtifactReferences(value interface{}) []string {
	refs := make([]string, 0)
	pattern := regexp.MustCompile(`(^|[\s(\[>])(team/[^\s)\]<>{},\\\x60"']+)`)
	seen := map[string]struct{}{}
	var walk func(interface{})
	walk = func(current interface{}) {
		switch typed := current.(type) {
		case string:
			for _, match := range pattern.FindAllStringSubmatch(typed, -1) {
				if len(match) < 3 {
					continue
				}
				ref := strings.TrimRight(match[2], ".,;:")
				if strings.HasPrefix(ref, "team/artifacts/") ||
					strings.HasPrefix(ref, "team/results/") ||
					strings.HasPrefix(ref, "team/tasks/") ||
					strings.HasPrefix(ref, "team/inbox/") ||
					strings.HasPrefix(ref, "team/tmp/") ||
					strings.HasPrefix(ref, "team/status/") {
					continue
				}
				if _, exists := seen[ref]; exists {
					continue
				}
				seen[ref] = struct{}{}
				refs = append(refs, ref)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item)
			}
		case map[string]interface{}:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return refs
}
func buildInitialLeaderTaskPayload(teamName string) map[string]interface{} {
	normalizedTeamName := strings.TrimSpace(teamName)
	if normalizedTeamName == "" {
		normalizedTeamName = "current"
	}
	prompt := fmt.Sprintf("\u8bf7\u4ecb\u7ecdteam %s\u5f53\u524d Redis Team\u6210\u5458\u6784\u6210\uff0c\u5305\u62ec\u5404\u89d2\u8272\u7684\u804c\u8d23\u5206\u5de5\u3001\u8fd0\u884c\u72b6\u6001\u4e0e\u6280\u672f\u80fd\u529b\u8fb9\u754c\u3002\u540c\u65f6\u8bf4\u660e\u56e2\u961f\u5185\u90e8\u7684\u534f\u4f5c\u4e0e\u901a\u4fe1\u673a\u5236(team_send)\uff0c\u4f8b\u5982\u4efb\u52a1\u6d41\u8f6c\u65b9\u5f0f\u3001\u6d88\u606f\u540c\u6b65\u65b9\u5f0f\u3001\u4e0a\u4e0b\u6587\u5171\u4eab\u65b9\u5f0f\u4ee5\u53ca\u53ef\u8c03\u7528\u7684\u65b9\u6cd5\u3001\u5de5\u5177\u4e0e\u64cd\u4f5c\u80fd\u529b\uff0c\u4ee5\u4fbf\u540e\u7eed\u80fd\u591f\u66f4\u9ad8\u6548\u5730\u5f00\u5c55\u56e2\u961f\u5de5\u4f5c", normalizedTeamName)
	return map[string]interface{}{
		"intent":             initialLeaderTaskIntent,
		"title":              "\u4ecb\u7ecd\u5f53\u524d Redis Team \u6210\u5458\u4e0e\u534f\u4f5c\u673a\u5236",
		"prompt":             prompt,
		"origin":             "system_bootstrap",
		"executionMode":      "leader_control_plane_snapshot",
		"requiresDelegation": false,
		"anchorEligible":     false,
	}
}

func (s *teamService) markTeamFailed(team *models.Team, cause error) error {
	team.Status = models.TeamStatusFailed
	team.UpdatedAt = time.Now().UTC()
	_ = s.repo.UpdateTeam(team)
	return cause
}

func (s *teamService) rollbackTeamCreation(userID int, team *models.Team, cause error) error {
	members, err := s.repo.ListMembersByTeamID(team.ID)
	if err != nil {
		fmt.Printf("Warning: failed to list Team %d members during create rollback: %v\n", team.ID, err)
	}
	for idx := range members {
		member := members[idx]
		if member.InstanceID != nil && *member.InstanceID > 0 {
			if err := s.instanceService.Delete(*member.InstanceID); err != nil {
				fmt.Printf("Warning: failed to delete Team %d member %s instance %d during create rollback: %v\n", team.ID, member.MemberKey, *member.InstanceID, err)
			}
		}
		member.Status = models.TeamMemberStatusDeleted
		member.CurrentTaskID = nil
		member.UpdatedAt = time.Now().UTC()
		_ = s.repo.UpdateMember(&member)
	}
	ctx := context.Background()
	if strings.TrimSpace(derefTeamString(team.TeamTokenSecretName)) != "" {
		if err := s.secretService.DeleteSecret(ctx, userID, derefTeamString(team.TeamTokenSecretName)); err != nil {
			fmt.Printf("Warning: failed to delete Team %d secret during create rollback: %v\n", team.ID, err)
		}
	}
	if err := s.configMapService.DeleteConfigMap(ctx, userID, s.teamConfigMapName(team.ID)); err != nil {
		fmt.Printf("Warning: failed to delete Team %d configmap during create rollback: %v\n", team.ID, err)
	}
	if err := s.pvcService.DeleteTeamSharedPVC(ctx, userID, team.ID); err != nil {
		fmt.Printf("Warning: failed to delete Team %d shared PVC during create rollback: %v\n", team.ID, err)
	}
	team.Name = deletedTeamName(team.Name, team.ID)
	team.Status = models.TeamStatusDeleted
	team.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateTeam(team); err != nil {
		fmt.Printf("Warning: failed to mark Team %d deleted during create rollback: %v\n", team.ID, err)
	}
	return cause
}

func teamTaskPayloads(tasks []models.TeamTask) []TeamTaskPayload {
	result := make([]TeamTaskPayload, 0, len(tasks))
	for _, task := range tasks {
		payload, err := teamTaskPayload(task)
		if err != nil {
			result = append(result, TeamTaskPayload{TeamTask: task})
			continue
		}
		result = append(result, *payload)
	}
	return result
}

func normalizeTeamHistoryLimit(limit, defaultLimit, maxLimit int) int {
	if defaultLimit <= 0 {
		defaultLimit = 50
	}
	if maxLimit <= 0 {
		maxLimit = defaultLimit
	}
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func nextTeamTaskBeforeID(tasks []TeamTaskPayload) *int {
	if len(tasks) == 0 {
		return nil
	}
	minID := tasks[0].ID
	for _, task := range tasks[1:] {
		if task.ID < minID {
			minID = task.ID
		}
	}
	if minID <= 0 {
		return nil
	}
	next := minID
	return &next
}

func nextTeamEventBeforeID(events []TeamEventPayload) *int {
	if len(events) == 0 {
		return nil
	}
	minID := events[0].ID
	for _, event := range events[1:] {
		if event.ID < minID {
			minID = event.ID
		}
	}
	if minID <= 0 {
		return nil
	}
	next := minID
	return &next
}

func teamTaskPayload(task models.TeamTask) (*TeamTaskPayload, error) {
	payload := TeamTaskPayload{TeamTask: task}
	if strings.TrimSpace(task.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(task.PayloadJSON), &payload.Payload); err != nil {
			return nil, err
		}
	}
	if task.ResultJSON != nil && strings.TrimSpace(*task.ResultJSON) != "" {
		if err := json.Unmarshal([]byte(*task.ResultJSON), &payload.Result); err != nil {
			return nil, err
		}
	}
	return &payload, nil
}

func teamEventPayloads(events []models.TeamEvent) []TeamEventPayload {
	result := make([]TeamEventPayload, 0, len(events))
	for _, event := range events {
		payload := TeamEventPayload{TeamEvent: event}
		if event.PayloadJSON != nil && strings.TrimSpace(*event.PayloadJSON) != "" {
			_ = json.Unmarshal([]byte(*event.PayloadJSON), &payload.Payload)
		}
		chatPolicy := strings.ToLower(strings.TrimSpace(eventString(payload.Payload, "chatPolicy", "chat_policy")))
		hidden, hiddenDefined := teamEventBoolValue(payload.Payload, "visibleToChat", "visible_to_chat")
		eventKind := strings.ToLower(strings.TrimSpace(eventString(payload.Payload, "eventKind", "event_kind", "kind")))
		business := teamChatEventIsBusinessContent(strings.ToLower(strings.TrimSpace(event.EventType)), eventKind, payload.Payload)
		if event.EventType == "member_result_confirmed" || (!business && (chatPolicy == "hidden" || (hiddenDefined && !hidden && chatPolicy == ""))) {
			continue
		}
		result = append(result, payload)
	}
	return result
}

func teamWorkItemPayloads(items []models.TeamWorkItem) []TeamWorkItemPayload {
	result := make([]TeamWorkItemPayload, 0, len(items))
	for _, item := range items {
		payload := TeamWorkItemPayload{TeamWorkItem: item}
		if item.DependsOnJSON != nil && strings.TrimSpace(*item.DependsOnJSON) != "" {
			_ = json.Unmarshal([]byte(*item.DependsOnJSON), &payload.DependsOn)
		}
		if item.ResultJSON != nil && strings.TrimSpace(*item.ResultJSON) != "" {
			_ = json.Unmarshal([]byte(*item.ResultJSON), &payload.Result)
		}
		if item.ArtifactRefsJSON != nil && strings.TrimSpace(*item.ArtifactRefsJSON) != "" {
			_ = json.Unmarshal([]byte(*item.ArtifactRefsJSON), &payload.ArtifactRefs)
		}
		result = append(result, payload)
	}
	return result
}

func mergeRedisEventPayload(fields map[string]string) map[string]interface{} {
	payload := map[string]interface{}{}
	if raw := strings.TrimSpace(fields["payload"]); raw != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	for key, value := range fields {
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return payload
}

func eventString(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case float64:
			return strconv.Itoa(int(typed))
		case int:
			return strconv.Itoa(typed)
		default:
			return strings.TrimSpace(fmt.Sprintf("%v", typed))
		}
	}
	return ""
}

func eventBool(payload map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "yes", "y", "on":
				return true
			case "0", "false", "no", "n", "off":
				return false
			}
		case float64:
			return typed != 0
		case int:
			return typed != 0
		default:
			text := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", typed)))
			if text == "true" || text == "yes" || text == "1" {
				return true
			}
		}
	}
	return false
}

func applyTeamMemberRuntimeProjection(member *models.TeamMember, payload map[string]interface{}, eventType string) {
	if member == nil {
		return
	}
	nonAuthoritativeDispatchWarning := isNonAuthoritativeDispatchWarning(eventType, payload)
	status := normalizedTeamTaskEventStatus(payload)
	availability := normalizeTeamAvailability(eventString(payload, "availability", "memberAvailability"))
	if nonAuthoritativeDispatchWarning && availability == models.TeamMemberAvailabilityBlocked {
		availability = ""
	}
	explicitlyBlocked := availability == models.TeamMemberAvailabilityBlocked
	if availability != "" {
		member.Availability = availability
	}
	if member.Availability == "" {
		member.Availability = models.TeamMemberAvailabilityUnknown
	}
	if nonAuthoritativeDispatchWarning {
		if member.Availability == models.TeamMemberAvailabilityBlocked || member.Availability == models.TeamMemberAvailabilityUnknown {
			member.Availability = models.TeamMemberAvailabilityIdle
		}
		member.BlockedReason = nil
		return
	}
	if runtimeStatus := eventString(payload, "runtime_status", "runtimeStatus", "runtime", "liveness"); runtimeStatus != "" {
		member.RuntimeStatus = &runtimeStatus
	}
	if runtimeTaskID := eventString(payload, "runtime_task_id", "runtimeTaskId", "current_task_id", "currentTaskId", "taskId"); runtimeTaskID != "" {
		member.RuntimeTaskID = &runtimeTaskID
	}
	if runtimeIntent := eventString(payload, "runtime_intent", "runtimeIntent", "current_intent", "currentIntent", "intent"); runtimeIntent != "" {
		member.RuntimeIntent = &runtimeIntent
	}
	if summary := eventString(payload, "last_summary", "lastSummary", "summary", "diagnostic"); summary != "" && eventType != "assignment_heartbeat" {
		member.LastSummary = &summary
	}
	if reason := eventString(payload, "blocked_reason", "blockedReason", "error_message", "error", "reason"); reason != "" {
		if !nonAuthoritativeDispatchWarning {
			member.BlockedReason = &reason
		}
	}
	switch eventType {
	case "presence", "member_presence", "status", "member_status":
		return
	case "task_completed":
		if !explicitlyBlocked {
			member.Availability = models.TeamMemberAvailabilityIdle
			member.BlockedReason = nil
		}
	case "task_failed", "message_failed":
		if !isTeamTaskFailureSignal(eventType, status, payload) {
			return
		}
		if member.Availability == "" || member.Availability == models.TeamMemberAvailabilityUnknown || member.Availability == models.TeamMemberAvailabilityBusy {
			member.Availability = models.TeamMemberAvailabilityBlocked
		}
	}
}

func normalizeTeamAvailability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "idle", "available", "ready":
		return models.TeamMemberAvailabilityIdle
	case "busy", "running", "working":
		return models.TeamMemberAvailabilityBusy
	case "blocked", "error", "failed":
		return models.TeamMemberAvailabilityBlocked
	case "offline", "unavailable":
		return models.TeamMemberAvailabilityOffline
	case "unknown":
		return models.TeamMemberAvailabilityUnknown
	default:
		return ""
	}
}

func eventInt(payload map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case string:
			parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
			return parsed
		}
	}
	return 0
}

func teamEventBoolValue(payload map[string]interface{}, keys ...string) (bool, bool) {
	if payload == nil {
		return false, false
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case float64:
			return typed != 0, true
		case int:
			return typed != 0, true
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			if normalized == "" {
				continue
			}
			return normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "on", true
		}
	}
	return false, false
}

func eventTime(payload map[string]interface{}) *time.Time {
	for _, key := range []string{"occurred_at", "occurredAt", "timestamp"} {
		raw := eventString(payload, key)
		if raw == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return &parsed
		}
	}
	now := time.Now().UTC()
	return &now
}

func normalizeContextRefs(value interface{}) []string {
	rawItems, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	refs := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		ref := strings.TrimSpace(fmt.Sprintf("%v", item))
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (s *teamService) workspaceProxyInstance(userID, teamID int) (*models.Team, int, error) {
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return nil, 0, err
	}
	members, err := s.repo.ListMembersByTeamID(teamID)
	if err != nil {
		return nil, 0, err
	}
	for _, member := range activeTeamMembers(members) {
		if member.InstanceID == nil {
			continue
		}
		instance, err := s.instanceService.GetByID(*member.InstanceID)
		if err != nil || instance == nil || instance.UserID != userID {
			continue
		}
		if instance.Status == "running" || member.Status == models.TeamMemberStatusIdle || member.Status == models.TeamMemberStatusBusy {
			return team, instance.ID, nil
		}
	}
	for _, member := range activeTeamMembers(members) {
		if member.InstanceID != nil {
			instance, err := s.instanceService.GetByID(*member.InstanceID)
			if err == nil && instance != nil && instance.UserID == userID {
				return team, instance.ID, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("no available Team member instance for workspace access")
}

func (s *teamService) execTeamWorkspace(ctx context.Context, userID, instanceID int, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if s.podService == nil || s.podService.GetClient() == nil || s.podService.GetClient().Clientset == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	pod, err := s.podService.GetPod(ctx, userID, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get pod: %w", err)
	}
	req := s.podService.GetClient().Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "desktop",
		Command:   command,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
		TTY:       false,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(s.podService.GetClient().Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to initialize exec stream: %w", err)
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}

func cleanTeamWorkspacePath(raw string) (string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	if value == "team" {
		value = ""
	} else if strings.HasPrefix(value, "team/") {
		value = strings.TrimPrefix(value, "team/")
	}
	if value == "" || value == "." {
		return "", nil
	}
	cleaned := posixpath.Clean(value)
	if cleaned == "." {
		return "", nil
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid workspace path")
		}
	}
	return cleaned, nil
}

func cleanWorkspaceEntryName(raw string) (string, error) {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if name == "" || strings.Contains(name, "/") || name == "." || name == ".." {
		return "", fmt.Errorf("invalid file or folder name")
	}
	return name, nil
}

func joinTeamWorkspacePath(base, child string) string {
	if strings.TrimSpace(base) == "" {
		return child
	}
	if strings.TrimSpace(child) == "" {
		return base
	}
	return posixpath.Clean(base + "/" + child)
}

func teamWorkspaceFullPath(team *models.Team, relPath string) string {
	root := strings.TrimRight(strings.TrimSpace(team.SharedMountPath), "/")
	if root == "" {
		root = teamSharedMountPath
	}
	if relPath == "" {
		return root
	}
	return root + "/" + relPath
}

func (s *teamService) resolveTeamWorkspacePath(ctx context.Context, userID, teamID int, cleanPath string) (*models.Team, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	team, err := s.requireOwnedTeam(userID, teamID)
	if err != nil {
		return nil, "", "", err
	}
	root := filepath.Clean(s.teamRuntimeSharedPathFor(userID, team.ID))
	if root == "." || root == string(filepath.Separator) || strings.TrimSpace(root) == "" {
		return nil, "", "", fmt.Errorf("invalid Team workspace root")
	}
	if err := ensureTeamWorkspaceDirectory(root); err != nil {
		return nil, "", "", fmt.Errorf("failed to prepare Team workspace: %w", err)
	}
	target := root
	if cleanPath != "" {
		target = filepath.Join(root, filepath.FromSlash(cleanPath))
	}
	target = filepath.Clean(target)
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return nil, "", "", fmt.Errorf("invalid Team workspace path")
	}
	return team, root, target, nil
}

func teamWorkspaceDisplayRoot(team *models.Team, runtimeRoot string) string {
	if team != nil {
		if value := strings.TrimSpace(team.SharedMountPath); value != "" {
			return value
		}
	}
	return filepath.ToSlash(runtimeRoot)
}

func teamWorkspaceFileEntryFromInfo(parentPath string, info os.FileInfo) TeamWorkspaceFileEntry {
	entryType := "file"
	size := info.Size()
	if info.IsDir() {
		entryType = "directory"
		size = 0
	}
	name := info.Name()
	entryPath := joinTeamWorkspacePath(parentPath, name)
	modifiedAt := ""
	if !info.ModTime().IsZero() {
		modifiedAt = info.ModTime().UTC().Format(time.RFC3339)
	}
	return TeamWorkspaceFileEntry{
		Name:        name,
		Path:        entryPath,
		Type:        entryType,
		Size:        size,
		ModifiedAt:  modifiedAt,
		Previewable: entryType == "file" && isPreviewableWorkspaceFile(name),
	}
}

func sortTeamWorkspaceEntries(entries []TeamWorkspaceFileEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func chownTeamWorkspacePath(path string) {
	_ = os.Chown(path, teamSharedUID, teamSharedGID)
}

func ensureTeamWorkspaceDirectory(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("invalid Team workspace directory")
	}
	if err := os.MkdirAll(clean, 0o2775); err != nil {
		return err
	}
	_ = os.Chmod(clean, 0o2775)
	chownTeamWorkspacePath(clean)
	return nil
}

func zipTeamWorkspaceDirectory(ctx context.Context, root string) ([]byte, error) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	base := filepath.Base(root)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		zipName := filepath.ToSlash(rel)
		if entry.IsDir() {
			if path == root {
				return nil
			}
			_, err := archive.CreateHeader(&zip.FileHeader{
				Name:     strings.TrimSuffix(zipName, "/") + "/",
				Method:   zip.Store,
				Modified: info.ModTime(),
			})
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		if zipName == "." {
			zipName = base
		}
		header.Name = zipName
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}); err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("failed to download Team workspace folder: %w", err)
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize Team workspace folder download: %w", err)
	}
	return buf.Bytes(), nil
}

func parseTeamWorkspaceList(parentPath, raw string) []TeamWorkspaceFileEntry {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	entries := make([]TeamWorkspaceFileEntry, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		mtimeUnix, _ := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		name := parts[1]
		entryPath := joinTeamWorkspacePath(parentPath, name)
		modifiedAt := ""
		if mtimeUnix > 0 {
			modifiedAt = time.Unix(mtimeUnix, 0).UTC().Format(time.RFC3339)
		}
		entries = append(entries, TeamWorkspaceFileEntry{
			Name:        name,
			Path:        entryPath,
			Type:        parts[0],
			Size:        size,
			ModifiedAt:  modifiedAt,
			Previewable: parts[0] == "file" && isPreviewableWorkspaceFile(name),
		})
	}
	sortTeamWorkspaceEntries(entries)
	return entries
}

func isPreviewableWorkspaceFile(path string) bool {
	name := strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json")
}

func planTeamMembers(teamName string, members []CreateTeamMemberRequest) ([]plannedTeamMember, error) {
	plans := make([]plannedTeamMember, 0, len(members))
	memberKeys := map[string]struct{}{}
	leaderCount := 0
	for idx, memberReq := range members {
		role := normalizeTeamMemberRole(memberReq.Role, memberReq.IsLeader)
		memberKey, err := normalizeTeamMemberKey(memberReq.MemberID, role, idx)
		if err != nil {
			return nil, err
		}
		if _, exists := memberKeys[memberKey]; exists {
			return nil, fmt.Errorf("duplicate team member id: %s", memberKey)
		}
		memberKeys[memberKey] = struct{}{}
		runtimeType, err := normalizeTeamMemberRuntimeType(memberReq.RuntimeType)
		if err != nil {
			return nil, err
		}
		instanceMode, err := normalizeTeamMemberInstanceMode(memberReq.Mode, memberReq.InstanceMode)
		if err != nil {
			return nil, err
		}

		isLeader := memberReq.IsLeader || isTeamLeaderRole(role)
		if isLeader {
			leaderCount++
			role = "leader"
		}
		profile := teamMemberProfileFromEnv(memberReq.EnvironmentOverrides)
		effectiveRole := role
		if !isLeader && strings.TrimSpace(profile.RoleHint) != "" && !isTeamLeaderRole(profile.RoleHint) {
			effectiveRole = strings.TrimSpace(profile.RoleHint)
			role = effectiveRole
		}
		displayName := strings.TrimSpace(memberReq.Name)
		if displayName == "" {
			displayName = fmt.Sprintf("%s-%s", teamName, memberKey)
		}
		plans = append(plans, plannedTeamMember{
			Request:       memberReq,
			MemberKey:     memberKey,
			DisplayName:   displayName,
			Role:          role,
			ProfileKey:    profile.ProfileKey,
			ProfileName:   profile.ProfileName,
			EffectiveRole: effectiveRole,
			RuntimeType:   runtimeType,
			InstanceMode:  instanceMode,
			IsLeader:      isLeader,
		})
	}
	if leaderCount != 1 {
		return nil, fmt.Errorf("team must include exactly one leader")
	}
	return plans, nil
}

func teamMemberInstanceName(teamName string, teamID int, memberKey string) string {
	teamPart := normalizeTeamMemberKeyForInstanceName(teamName)
	if teamPart == "" {
		teamPart = "team"
	}
	memberPart := normalizeTeamMemberKeyForInstanceName(memberKey)
	if memberPart == "" {
		memberPart = "member"
	}
	const maxInstanceNameLength = 50
	idPart := fmt.Sprintf("%d", teamID)
	maxMemberLength := maxInstanceNameLength - len(idPart) - len("--t")
	if maxMemberLength < 1 {
		maxMemberLength = 1
	}
	if len(memberPart) > maxMemberLength {
		memberPart = strings.Trim(memberPart[:maxMemberLength], "-")
		if memberPart == "" {
			memberPart = "member"
		}
	}
	suffix := fmt.Sprintf("-%s-%s", idPart, memberPart)
	if len(teamPart)+len(suffix) <= maxInstanceNameLength {
		return teamPart + suffix
	}
	maxTeamLength := maxInstanceNameLength - len(suffix)
	if maxTeamLength < 1 {
		maxTeamLength = 1
	}
	return strings.Trim(teamPart[:maxTeamLength], "-") + suffix
}

func normalizeTeamMemberKeyForInstanceName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = teamMemberInstanceNameInvalidChars.ReplaceAllString(normalized, "")
	normalized = teamMemberInstanceNameRepeatedDashs.ReplaceAllString(normalized, "-")
	return strings.Trim(normalized, "-")
}

func normalizeTeamMemberRuntimeType(raw string) (string, error) {
	runtimeType := strings.ToLower(strings.TrimSpace(raw))
	if runtimeType == "" {
		return "openclaw", nil
	}
	switch runtimeType {
	case "openclaw", "hermes":
		return runtimeType, nil
	default:
		return "", fmt.Errorf("unsupported team member runtime type: %s", raw)
	}
}

func normalizeTeamMemberInstanceMode(rawMode, rawInstanceMode string) (string, error) {
	if mode, ok := NormalizeInstanceMode(rawMode); ok {
		return mode, nil
	}
	if strings.TrimSpace(rawMode) != "" {
		return "", fmt.Errorf("unsupported team member instance mode: %s", rawMode)
	}
	if mode, ok := NormalizeInstanceMode(rawInstanceMode); ok {
		return mode, nil
	}
	if strings.TrimSpace(rawInstanceMode) != "" {
		return "", fmt.Errorf("unsupported team member instance mode: %s", rawInstanceMode)
	}
	return InstanceModeLite, nil
}

func normalizeTeamCommunicationMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	mode = strings.ReplaceAll(mode, "-", "_")
	mode = strings.ReplaceAll(mode, " ", "_")
	switch mode {
	case "", teamCommunicationModeLeaderMediated, "leader", "leader_only":
		return teamCommunicationModeLeaderMediated, nil
	case teamCommunicationModePeerAssisted, "peer", "peer_to_peer", "peer_assist":
		return teamCommunicationModePeerAssisted, nil
	case teamCommunicationModeFullMesh, "mesh":
		return teamCommunicationModeFullMesh, nil
	default:
		return "", fmt.Errorf("unsupported team communication mode: %s", raw)
	}
}

func normalizedTeamCommunicationMode(raw string) string {
	mode, err := normalizeTeamCommunicationMode(raw)
	if err != nil {
		return teamCommunicationModeLeaderMediated
	}
	return mode
}

func normalizeTeamMemberRole(raw string, isLeader bool) string {
	role := strings.TrimSpace(raw)
	if isLeader || isTeamLeaderRole(role) {
		return "leader"
	}
	if role == "" {
		return "member"
	}
	return role
}

func isTeamLeaderRole(role string) bool {
	normalized := strings.ToLower(strings.TrimSpace(role))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return normalized == "leader" || normalized == "team-leader"
}

func findTeamLeader(members []models.TeamMember) *models.TeamMember {
	for idx := range members {
		if isTeamLeaderRole(members[idx].Role) {
			member := members[idx]
			return &member
		}
	}
	return nil
}

func leaderMemberKey(member *models.TeamMember) string {
	if member == nil {
		return ""
	}
	return member.MemberKey
}

type teamRosterConfig struct {
	Version             int                           `json:"version"`
	TeamID              string                        `json:"teamId"`
	LeaderMemberID      string                        `json:"leaderMemberId"`
	CommunicationMode   string                        `json:"communicationMode"`
	CollaborationPolicy teamRosterCollaborationPolicy `json:"collaborationPolicy"`
	SharedDir           string                        `json:"sharedDir"`
	Members             []teamRosterMember            `json:"members"`
	Redis               teamRosterRedis               `json:"redis"`
}

type teamRosterMember struct {
	MemberID      string `json:"memberId"`
	Role          string `json:"role"`
	EffectiveRole string `json:"effectiveRole,omitempty"`
	ProfileKey    string `json:"profileKey,omitempty"`
	ProfileName   string `json:"profileName,omitempty"`
	RuntimeType   string `json:"runtimeType"`
	InstanceMode  string `json:"instanceMode"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description,omitempty"`
	IsLeader      bool   `json:"isLeader"`
}

type teamRosterRedis struct {
	EventsKey   string `json:"eventsKey"`
	PresenceKey string `json:"presenceKey"`
	DLQKey      string `json:"dlqKey"`
}

type teamRosterCollaborationPolicy struct {
	Mode               string   `json:"mode"`
	AllowPeerToPeer    bool     `json:"allowPeerToPeer"`
	LeaderFinalizes    bool     `json:"leaderFinalizes"`
	PeerReplyRequired  bool     `json:"peerReplyRequired"`
	AllowedPeerActions []string `json:"allowedPeerActions,omitempty"`
}

func buildTeamCollaborationPolicy(communicationMode string) teamRosterCollaborationPolicy {
	mode := normalizedTeamCommunicationMode(communicationMode)
	policy := teamRosterCollaborationPolicy{
		Mode:            mode,
		LeaderFinalizes: true,
	}
	switch mode {
	case teamCommunicationModePeerAssisted:
		policy.AllowPeerToPeer = true
		policy.PeerReplyRequired = true
		policy.AllowedPeerActions = []string{"ask", "handoff", "review_request", "artifact_request", "blocker_help", "peer_review"}
	case teamCommunicationModeFullMesh:
		policy.AllowPeerToPeer = true
		policy.PeerReplyRequired = true
		policy.AllowedPeerActions = []string{"ask", "handoff", "review_request", "artifact_request", "blocker_help", "peer_review", "delegate"}
	}
	return policy
}

func buildTeamRosterConfig(team *models.Team, members []plannedTeamMember) (string, error) {
	return buildTeamRosterConfigWithSharedDir(team, members, team.SharedMountPath)
}

func buildTeamRosterConfigWithSharedDir(team *models.Team, members []plannedTeamMember, sharedDir string) (string, error) {
	communicationMode := normalizedTeamCommunicationMode(team.CommunicationMode)
	sharedDir = strings.TrimSpace(sharedDir)
	if sharedDir == "" {
		sharedDir = team.SharedMountPath
	}
	config := teamRosterConfig{
		Version:             1,
		TeamID:              strconv.Itoa(team.ID),
		CommunicationMode:   communicationMode,
		CollaborationPolicy: buildTeamCollaborationPolicy(communicationMode),
		SharedDir:           sharedDir,
		Members:             make([]teamRosterMember, 0, len(members)),
		Redis: teamRosterRedis{
			EventsKey:   teamEventsKey(team.ID),
			PresenceKey: teamPresenceKey(team.ID),
			DLQKey:      teamDLQKey(team.ID),
		},
	}
	for _, member := range members {
		if member.IsLeader {
			config.LeaderMemberID = member.MemberKey
		}
		config.Members = append(config.Members, teamRosterMember{
			MemberID:      member.MemberKey,
			Role:          member.Role,
			EffectiveRole: effectiveTeamMemberRole(member),
			ProfileKey:    member.ProfileKey,
			ProfileName:   member.ProfileName,
			RuntimeType:   member.RuntimeType,
			InstanceMode:  member.InstanceMode,
			DisplayName:   member.DisplayName,
			Description:   plannedTeamMemberDescription(member),
			IsLeader:      member.IsLeader,
		})
	}
	if config.LeaderMemberID == "" {
		return "", fmt.Errorf("team must include exactly one leader")
	}
	return marshalJSON(config)
}

func buildTeamRosterConfigFromMembers(team *models.Team, members []models.TeamMember) (string, error) {
	return buildTeamRosterConfigFromMembersWithSharedDir(team, members, team.SharedMountPath)
}

func buildTeamRosterConfigFromMembersWithSharedDir(team *models.Team, members []models.TeamMember, sharedDir string) (string, error) {
	communicationMode := normalizedTeamCommunicationMode(team.CommunicationMode)
	sharedDir = strings.TrimSpace(sharedDir)
	if sharedDir == "" {
		sharedDir = team.SharedMountPath
	}
	config := teamRosterConfig{
		Version:             1,
		TeamID:              strconv.Itoa(team.ID),
		CommunicationMode:   communicationMode,
		CollaborationPolicy: buildTeamCollaborationPolicy(communicationMode),
		SharedDir:           sharedDir,
		Members:             make([]teamRosterMember, 0, len(members)),
		Redis: teamRosterRedis{
			EventsKey:   teamEventsKey(team.ID),
			PresenceKey: teamPresenceKey(team.ID),
			DLQKey:      teamDLQKey(team.ID),
		},
	}
	for _, member := range members {
		isLeader := isTeamLeaderRole(member.Role)
		runtimeType := strings.TrimSpace(member.RuntimeType)
		if runtimeType == "" {
			runtimeType = "openclaw"
		}
		instanceMode := strings.TrimSpace(member.InstanceMode)
		if instanceMode == "" {
			instanceMode = InstanceModeLite
		}
		if isLeader {
			config.LeaderMemberID = member.MemberKey
		}
		config.Members = append(config.Members, teamRosterMember{
			MemberID:      member.MemberKey,
			Role:          member.Role,
			EffectiveRole: member.Role,
			RuntimeType:   runtimeType,
			InstanceMode:  instanceMode,
			DisplayName:   member.DisplayName,
			Description:   derefTeamString(member.Description),
			IsLeader:      isLeader,
		})
	}
	if config.LeaderMemberID == "" {
		return "", fmt.Errorf("team must include exactly one leader")
	}
	return marshalJSON(config)
}

func activeTeamMembers(members []models.TeamMember) []models.TeamMember {
	active := make([]models.TeamMember, 0, len(members))
	for _, member := range members {
		if member.Status == models.TeamMemberStatusDeleted || member.Status == models.TeamMemberStatusDeleting {
			continue
		}
		active = append(active, member)
	}
	return active
}

func activeTeams(teams []models.Team) []models.Team {
	active := make([]models.Team, 0, len(teams))
	for _, team := range teams {
		if team.Status == models.TeamStatusDeleted {
			continue
		}
		active = append(active, team)
	}
	return active
}

func deletedTeamName(name string, teamID int) string {
	const maxTeamNameLength = 255
	suffix := fmt.Sprintf("__deleted_%d", teamID)
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		trimmed = "team"
	}
	if strings.HasSuffix(trimmed, suffix) {
		return trimmed
	}
	if len(trimmed)+len(suffix) <= maxTeamNameLength {
		return trimmed + suffix
	}
	runes := []rune(trimmed)
	maxPrefixLength := maxTeamNameLength - len(suffix)
	if len(runes) > maxPrefixLength {
		runes = runes[:maxPrefixLength]
	}
	return string(runes) + suffix
}

func normalizeTeamMemberKey(raw, role string, index int) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(role))
	}
	if value == "" {
		value = fmt.Sprintf("member-%d", index+1)
	}
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	if !teamMemberKeyPattern.MatchString(value) {
		return "", fmt.Errorf("team member id is invalid")
	}
	return value, nil
}

func defaultTeamRedisURL() string {
	for _, key := range []string{"CLAWMANAGER_TEAM_REDIS_URL", "TEAM_REDIS_URL", "REDIS_URL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if value, ok := defaultTeamRedisServiceURL(); ok {
		return value
	}
	return ""
}

func defaultTeamRedisServiceURL() (string, bool) {
	systemNamespace := strings.TrimSpace(os.Getenv("CLAWMANAGER_SYSTEM_NAMESPACE"))
	if systemNamespace == "" {
		if client := k8s.GetClient(); client != nil {
			systemNamespace = client.GetSystemNamespace()
		} else if baseNamespace := strings.TrimSpace(os.Getenv("K8S_NAMESPACE")); baseNamespace != "" {
			systemNamespace = fmt.Sprintf("%s-system", baseNamespace)
		}
	}
	if systemNamespace == "" {
		return "", false
	}

	serviceName := strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_REDIS_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_REDIS_SERVICE"))
	}
	if serviceName == "" {
		serviceName = "clawmanager-team-redis"
	}

	port := normalizePortValue(
		strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_REDIS_SERVICE_PORT")),
		strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_REDIS_PORT")),
	)
	if port == "" {
		port = "6379"
	}

	db := strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_REDIS_DB"))
	if db == "" {
		db = strings.TrimSpace(os.Getenv("TEAM_REDIS_DB"))
	}
	if db == "" {
		db = "0"
	}

	return fmt.Sprintf("redis://%s.%s.svc.cluster.local:%s/%s", serviceName, systemNamespace, port, db), true
}

func teamTaskStaleTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_TASK_STALE_SECONDS"))
	if raw == "" {
		return defaultTeamTaskStaleTimeout
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return defaultTeamTaskStaleTimeout
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func defaultTeamManagerBaseURL() (string, bool) {
	if override := strings.TrimSpace(os.Getenv("CLAWMANAGER_TEAM_MANAGER_BASE_URL")); override != "" {
		return override, true
	}
	return defaultAgentControlBaseURL()
}

func teamInboxKey(teamID int, memberID string) string {
	return fmt.Sprintf("claw:team:%d:inbox:%s", teamID, memberID)
}

func teamEventsKey(teamID int) string {
	return fmt.Sprintf("claw:team:%d:events", teamID)
}

func teamCompletionAckKey(teamID int, completionID, attemptID string) string {
	return fmt.Sprintf("claw:team:%d:completion-ack:%s:%s", teamID, normalizeTeamRedisKeyPart(completionID), normalizeTeamRedisKeyPart(attemptID))
}

func teamCompletionStateKey(teamID int, completionID string) string {
	return fmt.Sprintf("claw:team:%d:completion-state:%s", teamID, normalizeTeamRedisKeyPart(completionID))
}

func teamCompletionAckStreamKey(teamID int, memberKey string) string {
	return fmt.Sprintf("claw:team:%d:completion-acks:%s", teamID, normalizeTeamRedisKeyPart(memberKey))
}

func normalizeTeamRedisKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ':' || r == '.' {
			return r
		}
		return '-'
	}, value)
}

func teamPresenceKey(teamID int) string {
	return fmt.Sprintf("claw:team:%d:presence", teamID)
}

func teamDLQKey(teamID int) string {
	return fmt.Sprintf("claw:team:%d:dlq", teamID)
}

func defaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func defaultFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func derefTeamString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
