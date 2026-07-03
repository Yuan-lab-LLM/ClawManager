package services

import (
	"archive/zip"
	"bytes"
	"context"
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

	initialLeaderTaskIntent = "team_bootstrap_introduction"
	teamTaskCompletionTool  = "team_complete_task"
	teamTaskReplyTarget     = "clawmanager"
)

const (
	teamCommunicationModeLeaderMediated = "leader_mediated"
	teamCommunicationModePeerAssisted   = "peer_assisted"
	teamCommunicationModeFullMesh       = "full_mesh"
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

	ctx                  context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	running              bool
	wg                   sync.WaitGroup
	consumers            map[int]struct{}
	staleMonitorStarted  bool
	runtimeWorkspaceRoot string
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
		repo:                 repo,
		instanceService:      instanceService,
		pvcService:           k8s.NewPVCService(),
		secretService:        k8s.NewSecretService(),
		configMapService:     k8s.NewConfigMapService(),
		podService:           k8s.NewPodService(),
		ctx:                  ctx,
		cancel:               cancel,
		consumers:            map[int]struct{}{},
		runtimeWorkspaceRoot: "/workspaces",
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
	if task != nil {
		taskID = task.ID
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
	envelope := map[string]interface{}{
		"v":                  1,
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
			"successEvent":   "task_completed",
			"failureEvent":   "task_failed",
			"replyEvent":     "reply",
			"resultField":    "resultMarkdown",
			"summaryField":   "summary",
			"artifactField":  "artifactRefs",
			"completionTool": teamTaskCompletionTool,
		},
		"intent":        intent,
		"taskId":        taskRef,
		"title":         eventString(taskPayload, "title"),
		"prompt":        appendTeamTaskCompletionInstruction(prompt, memberContext["communicationMode"], intent),
		"rawPrompt":     rawPrompt,
		"contextRefs":   normalizeContextRefs(taskPayload["contextRefs"]),
		"memberContext": memberContext,
		"systemPrompt":  memberContext["systemPrompt"],
		"metadata":      taskPayload,
		"createdAt":     now.Format(time.RFC3339Nano),
	}
	if workspaceContract, ok := taskPayload["workspaceContract"]; ok {
		envelope["workspaceContract"] = workspaceContract
	}
	if bootstrapSnapshot, ok := taskPayload["bootstrapSnapshot"]; ok {
		envelope["bootstrapSnapshot"] = bootstrapSnapshot
	}
	if envelope["intent"] == "" {
		envelope["intent"] = "run_task"
	}
	if envelope["title"] == "" {
		envelope["title"] = fmt.Sprintf("Team task %d", taskID)
	}
	return envelope
}

func appendTeamTaskCompletionInstruction(prompt string, communicationMode, intent string) string {
	base := strings.TrimSpace(prompt)
	if strings.TrimSpace(intent) == initialLeaderTaskIntent {
		instruction := strings.Join([]string{
			"Bootstrap completion contract:",
			"- This is a control-plane Team snapshot assigned only to the Leader. Do not delegate it, create worker assignments, or wait for member replies.",
			"- Use the injected metadata.bootstrapSnapshot and metadata.workspaceContract first. They are the authoritative roster, runtime, role, and workspace facts for this snapshot.",
			"- If you need to read the roster file, read $CLAWMANAGER_TEAM_CONFIG_PATH or /etc/clawmanager/team/team.json. Never look for /team/team.json or /team/members; /team is the shared artifact workspace, not the config mount.",
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
	}, "\n")
	instruction += "\n" + strings.Join(modeInstructions, "\n")
	instruction += "\n" + strings.Join([]string{
		"- Every Team message must preserve rootTaskId/messageId context when available and must clearly state whether it is an assignment, peer request, progress update, result, review, blocker, or final synthesis.",
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
	return rosterJSON, nil
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
	events, err := s.repo.ListEventsByTeamID(teamID, 50)
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
	payload := teamEventPayloads(events)
	return &TeamEventsHistoryPayload{
		Events:       payload,
		HasMore:      hasMore,
		NextBeforeID: nextTeamEventBeforeID(payload),
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

	bus, err := s.redisBusForTeam(context.Background(), team)
	if err != nil {
		return nil, err
	}
	taskPayload := map[string]interface{}{}
	if strings.TrimSpace(task.PayloadJSON) != "" {
		if err := json.Unmarshal([]byte(task.PayloadJSON), &taskPayload); err != nil {
			return nil, fmt.Errorf("failed to decode task payload: %w", err)
		}
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
		"writeRoot":               "$CLAWMANAGER_TEAM_SHARED_DIR",
		"canonicalReportPrefix":   "/team",
		"validReportPathPattern":  "/team/<relative-path>",
		"invalidConfigPaths":      []string{"/team/team.json", "/team/members"},
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
	payload["bootstrapSnapshot"] = map[string]interface{}{
		"teamId":            team.ID,
		"teamName":          team.Name,
		"communicationMode": normalizedTeamCommunicationMode(team.CommunicationMode),
		"sharedDir":         team.SharedMountPath,
		"configPath":        teamConfigMountPath,
		"roster":            roster,
		"members":           memberSnapshots,
	}
	return nil
}

func (s *teamService) prepareTeamTaskWorkspace(userID, teamID, taskID int) error {
	if s == nil || taskID <= 0 {
		return nil
	}
	root := filepath.Clean(s.teamRuntimeSharedPathFor(userID, teamID))
	taskRef := fmt.Sprintf("team-%d-task-%d", teamID, taskID)
	for _, rel := range []string{
		"",
		"results",
		"tasks",
		"inbox",
		"status",
		filepath.ToSlash(filepath.Join("results", taskRef)),
		filepath.ToSlash(filepath.Join("tasks", taskRef)),
		filepath.ToSlash(filepath.Join("status", taskRef)),
	} {
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
	guidance := "Shared workspace contract: write every shared artifact under the exact CLAWMANAGER_TEAM_SHARED_DIR value. When using shell commands, always create files under \"$CLAWMANAGER_TEAM_SHARED_DIR/<relative-path>\". Never create or report a relative team/... directory; team/... is invalid and may not be visible in ClawManager. When reporting an artifact to ClawManager or another member, use the canonical link /team/<relative-path>. The /team prefix is a UI/logical alias and may map to a different physical directory in Lite runtimes."
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
	return s.configMapService.UpsertConfigMap(context.Background(), userID, s.teamConfigMapName(team.ID), configData, map[string]string{
		"app":        "clawreef",
		"managed-by": "clawreef",
		"team-id":    strconv.Itoa(team.ID),
	})
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
			if err := s.sweepStaleTasks(); err != nil {
				fmt.Printf("Warning: failed to sweep stale Team tasks: %v\n", err)
			}
		}
	}
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
		return eventType == "task_completed" &&
			isSuccessfulTeamTaskEventStatus(status) &&
			isExplicitTeamTaskCompletion(payload) &&
			hasStrictTeamCompletionEnvelope(payload)
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
		if eventType != "task_failed" {
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
	refs := explicitTeamArtifactReferences(payload)
	return len(refs) > 0
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
	workItems, err := s.repo.ListWorkItemsByRootTaskID(task.ID)
	if err != nil {
		return false, err
	}
	hasAssignments := false
	for idx := range workItems {
		item := workItems[idx]
		if item.OwnerMemberID == nil || *item.OwnerMemberID == member.ID || item.WorkID == "leader-final-synthesis" {
			continue
		}
		hasAssignments = true
		if item.Status != models.TeamTaskStatusSucceeded {
			return false, nil
		}
	}
	if hasAssignments {
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
	case "outbound", "task_assigned", "team_send", "reply", "task_completed", "completion":
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

func isLeaderMediatedWorkerToLeaderResult(team *models.Team, eventType string, payload map[string]interface{}, member *models.TeamMember, task *models.TeamTask) bool {
	if !isLeaderMediatedTeam(team) || payload == nil || member == nil || task == nil || isLeaderTeamMember(member) {
		return false
	}
	if member.ID == task.TargetMemberID {
		return false
	}
	if !isLeaderMediatedOutboundLikeEvent(eventType) || !teamEventHasBody(payload) {
		return false
	}
	if isNonAuthoritativeDispatchFailure(eventType, payload) || eventBool(payload, "leaderMediatedRouteViolation") {
		return false
	}
	target := leaderMediatedRouteTarget(payload)
	if target == "" {
		target = eventString(payload, "replyTarget", "reply_target")
	}
	if target != "" && !isLeaderRouteTarget(target) {
		return false
	}
	body := eventString(payload, "resultMarkdown", "result_markdown", "result", "answer", "text", "message", "summary")
	if looksLikeLeaderDispatchOnlyText(body) {
		return false
	}
	return true
}

func markLeaderMediatedAssignmentResult(eventType string, payload map[string]interface{}, member *models.TeamMember) {
	payload["assignmentResultOnly"] = true
	payload["rootTaskTerminal"] = false
	payload["status"] = models.TeamTaskStatusSucceeded
	payload["runtimeStatus"] = models.TeamTaskStatusSucceeded
	payload["availability"] = models.TeamMemberAvailabilityIdle
	if eventString(payload, "originalEvent") == "" {
		payload["originalEvent"] = eventType
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

func looksLikeLeaderDispatchOnlyText(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	lower := strings.ToLower(normalized)
	for _, marker := range []string{
		"任务结果反馈", "任务输出", "最终回答", "最终方案", "最终总结", "汇总如下", "结果如下",
		"已完成并", "已完成，", "已完成:", "已完成：", "产出摘要",
		"final answer", "final synthesis", "result summary", "task result", "completed with evidence",
	} {
		if strings.Contains(compact, strings.ToLower(strings.Join(strings.Fields(marker), ""))) || strings.Contains(lower, strings.ToLower(marker)) {
			return false
		}
	}
	for _, marker := range []string{
		"任务分派", "任务下发", "分派给", "派发给", "交给", "请你", "请写", "请完成",
		"assignment", "assigned to", "handoff",
	} {
		if strings.Contains(compact, strings.ToLower(strings.Join(strings.Fields(marker), ""))) || strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	if strings.Contains(normalized, "$CLAWMANAGER_TEAM_SHARED_DIR") ||
		(strings.Contains(compact, "共享目录") && strings.Contains(compact, "规范路径")) ||
		(strings.Contains(lower, "shared directory") && strings.Contains(lower, "canonical path")) {
		return true
	}
	if strings.Contains(compact, "完成后") &&
		(strings.Contains(compact, "回传") || strings.Contains(compact, "返回给我") || strings.Contains(compact, "通知我") || strings.Contains(compact, "交付给我")) {
		return true
	}
	if strings.Contains(compact, "用户想让你") && strings.Contains(compact, "请") {
		return true
	}
	for _, target := range []string{"pm", "designer", "ui-designer", "architect", "worker", "product-manager", "solution-architect"} {
		if strings.Contains(compact, target+"你好") || strings.Contains(compact, "@"+target) {
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
	compact := strings.ToLower(strings.Join(strings.Fields(normalized), ""))
	lower := strings.ToLower(normalized)
	for _, marker := range []string{
		"任务结果反馈", "任务输出", "最终回答", "最终方案", "最终总结", "汇总如下", "结果如下",
		"已完成并", "已完成，", "已完成:", "已完成：", "产出摘要",
		"final answer", "final synthesis", "result summary", "task result", "completed with evidence",
	} {
		if strings.Contains(compact, strings.ToLower(strings.Join(strings.Fields(marker), ""))) || strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
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
	}
	eventType = normalizeFinalReplyTaskEvent(eventType, payload, task, member)
	eventStatus := normalizedTeamTaskEventStatus(payload)
	eventSignalsCompletion := isTeamTaskCompletionSignal(eventType, eventStatus, payload)
	eventSignalsFailure := isTeamTaskFailureSignal(eventType, eventStatus, payload)
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
	leaderDispatchOnly := isLeaderMediatedLeaderDispatchOnlyMessage(team, eventType, payload, member, task)
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
		ready, err := s.leaderMediatedRootCompletionReady(team, task, member)
		if err != nil {
			return err
		}
		if !ready {
			eventType = markLeaderMediatedPrematureCompletion(eventType, payload)
			eventStatus = normalizedTeamTaskEventStatus(payload)
			eventSignalsCompletion = false
			eventSignalsFailure = false
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
	if err := s.repo.CreateEvent(event); err != nil {
		if errors.Is(err, repository.ErrDuplicateTeamEvent) {
			return nil
		}
		return err
	}
	if err := s.projectTeamWorkItem(team, task, member, eventType, payload, event); err != nil {
		return err
	}

	now := time.Now().UTC()
	taskProjection := teamTaskProjectionResult{}
	if task != nil && !memberTerminalOnly && !assignmentResultOnly && !leaderMediatedRouteViolation {
		taskProjection = projectTeamTaskRuntimeState(task, payload, eventType, payloadJSON, now)
		if taskProjection.changed {
			task.UpdatedAt = now
			if err := s.repo.UpdateTask(task); err != nil {
				return err
			}
		}
	}
	if task != nil && (memberTerminalOnly || leaderDispatchOnly || assignmentResultOnly || leaderMediatedRouteViolation) && !taskProjection.changed && !isTerminalTeamTaskStatus(task.Status) {
		task.UpdatedAt = now
		if err := s.repo.UpdateTask(task); err != nil {
			return err
		}
	}
	if member != nil {
		member.LastSeenAt = &now
		applyTeamMemberRuntimeProjection(member, payload, eventType)
		taskIsActive := task != nil && !isTerminalTeamTaskStatus(task.Status)
		if taskIsActive && (leaderDispatchOnly || eventType == "task_received" || eventType == "task_started" || taskProjection.status == models.TeamTaskStatusRunning || taskProjection.status == models.TeamTaskStatusDispatched) {
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
	return nil
}

func (s *teamService) resolveTeamTaskFromEventReferences(teamID int, payload map[string]interface{}) (*models.TeamTask, error) {
	if payload == nil {
		return nil, nil
	}
	for _, key := range []string{"rootMessageId", "root_message_id", "parentMessageId", "parent_message_id", "inReplyTo", "in_reply_to", "replyTo", "reply_to"} {
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
	for _, key := range []string{"rootTaskId", "root_task_id", "parentTaskId", "parent_task_id", "currentTaskId", "current_task_id", "runtimeTaskId", "runtime_task_id"} {
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
	step, _ := payload["collaborationStep"].(map[string]interface{})
	stepType := eventString(step, "type")
	if stepType == "" {
		stepType = collaborationStepTypeForEvent(eventType, payload)
	}
	if stepType == "" || stepType == "warning" {
		return nil
	}
	explicitWorkID := eventString(payload, "workId", "work_id", "assignmentId", "assignment_id", "subtaskId", "subtask_id")
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
	ownerKey := actor
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
	owner := member
	if owner == nil || !teamMemberRouteEquivalent(owner.MemberKey, ownerKey) {
		found, err := s.repo.GetMemberByTeamKey(team.ID, ownerKey)
		if err != nil {
			return err
		}
		owner = found
	}
	workID := explicitWorkID
	if workID == "" {
		if rootCompletion {
			workID = "leader-final-synthesis"
		} else {
			workID = "member-" + normalizeTeamMemberRouteKey(ownerKey)
		}
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
	item := &models.TeamWorkItem{
		TeamID:     team.ID,
		RootTaskID: task.ID,
		WorkID:     workID,
		Title:      title,
		Status:     status,
		UpdatedAt:  now,
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
	if dependencies := normalizeContextRefs(step["dependsOn"]); len(dependencies) > 0 {
		if encoded, err := json.Marshal(dependencies); err == nil {
			value := string(encoded)
			item.DependsOnJSON = &value
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

func collaborationStepTypeForEvent(eventType string, payload map[string]interface{}) string {
	status := normalizedTeamTaskEventStatus(payload)
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
	case strings.Contains(text, "research") || strings.Contains(text, "调研"):
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
	for _, prefix := range []string{
		"收到", "好的", "好，", "好,", "ok", "okay", "处理中", "正在", "准备", "我将", "让我", "先看", "稍等",
		"let me", "now let me", "i will", "i'll", "checking", "working on", "good, i have", "i have the", "i have all", "i can see",
	} {
		if strings.HasPrefix(lower, prefix) || strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	for _, marker := range []string{
		"正在整理", "现在整理", "稍后", "等待其", "等待他", "等待她", "等待worker", "等待 worker",
		"派单", "已派发", "派发给", "下发给", "转派给", "交给worker", "交给 worker",
		"让worker", "让 worker", "请worker", "请 worker", "worker在线空闲",
		"sent to worker", "assigned to worker", "waiting for worker", "handoff to worker",
		"now let me write", "then finalize", "and then finalize", "write the comprehensive report",
	} {
		if strings.Contains(compact, strings.ReplaceAll(strings.ToLower(marker), " ", "")) || strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func looksLikeSubstantialFinalReply(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	if strings.ContainsAny(normalized, "#*>|`") {
		return true
	}
	if strings.ContainsAny(normalized, "。；;：:\n") {
		return true
	}
	return strings.Contains(normalized, "完成") ||
		strings.Contains(normalized, "总结") ||
		strings.Contains(normalized, "报告") ||
		strings.Contains(normalized, "结果") ||
		strings.Contains(strings.ToLower(normalized), "completed") ||
		strings.Contains(strings.ToLower(normalized), "summary")
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
		case strings.HasPrefix(value, "team/results/") ||
			strings.HasPrefix(value, "team/tasks/") ||
			strings.HasPrefix(value, "team/inbox/") ||
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
		text = regexp.MustCompile(`(^|[\s(\[>])team/(results|tasks|inbox|status)/([^\s)\]<>{},\\\x60"']+)`).ReplaceAllString(text, `${1}/team/${2}/${3}`)
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
	refs := collectTeamArtifactReferences(payload)
	invalidRelativeRefs := collectInvalidRelativeTeamArtifactReferences(payload)
	if teamRedisProtocolVersion(payload) >= 2 {
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
		} else if !info.Mode().IsRegular() {
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
		ref := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if !strings.HasPrefix(ref, teamSharedMountPath+"/") || strings.ContainsAny(ref, "`\"'{}[]") {
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
	pattern := regexp.MustCompile(`/team/(results|tasks|inbox|status)/[^\s)\]<>{},\\\x60"']+`)
	seen := map[string]struct{}{}
	var walk func(interface{})
	walk = func(current interface{}) {
		switch typed := current.(type) {
		case string:
			for _, match := range pattern.FindAllString(typed, -1) {
				ref := strings.TrimRight(match, ".,;:，。；、")
				if ref == "" {
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
				ref := strings.TrimRight(match[2], ".,;:，。；、")
				if strings.HasPrefix(ref, "team/results/") ||
					strings.HasPrefix(ref, "team/tasks/") ||
					strings.HasPrefix(ref, "team/inbox/") ||
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
		if payload, err := teamTaskPayload(task); err == nil {
			result = append(result, *payload)
		}
	}
	return result
}

func normalizeTeamHistoryLimit(limit, defaultLimit, maxLimit int) int {
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
	next := tasks[len(tasks)-1].ID
	return &next
}

func nextTeamEventBeforeID(events []TeamEventPayload) *int {
	if len(events) == 0 {
		return nil
	}
	next := events[len(events)-1].ID
	return &next
}

func teamTaskPayload(task models.TeamTask) (*TeamTaskPayload, error) {
	payload := &TeamTaskPayload{TeamTask: task}
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
	return payload, nil
}

func buildInitialLeaderTaskPayload(teamName string) map[string]interface{} {
	normalizedTeamName := strings.TrimSpace(teamName)
	if normalizedTeamName == "" {
		normalizedTeamName = "current"
	}
	prompt := fmt.Sprintf("请介绍`team %s`当前 Redis Team成员构成，包括各角色的职责分工、运行状态与技术能力边界。同时说明团队内部的协作与通信机制(team_send)，例如任务流转方式、消息同步方式、上下文共享方式以及可调用的方法、工具与操作能力，以便后续能够更高效地开展团队工作", normalizedTeamName)
	return map[string]interface{}{
		"intent":             initialLeaderTaskIntent,
		"title":              "介绍当前 Redis Team 成员与协作机制",
		"prompt":             prompt,
		"origin":             "system_bootstrap",
		"executionMode":      "leader_control_plane_snapshot",
		"requiresDelegation": false,
		"anchorEligible":     false,
	}
}

func teamEventPayloads(events []models.TeamEvent) []TeamEventPayload {
	result := make([]TeamEventPayload, 0, len(events))
	for _, event := range events {
		payload := TeamEventPayload{TeamEvent: event}
		if event.PayloadJSON != nil && strings.TrimSpace(*event.PayloadJSON) != "" {
			_ = json.Unmarshal([]byte(*event.PayloadJSON), &payload.Payload)
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
	if summary := eventString(payload, "last_summary", "lastSummary", "summary", "diagnostic"); summary != "" {
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
