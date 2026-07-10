package models

import "time"

const (
	TeamStatusCreating = "creating"
	TeamStatusRunning  = "running"
	TeamStatusFailed   = "failed"

	TeamMemberStatusCreating = "creating"
	TeamMemberStatusIdle     = "idle"
	TeamMemberStatusBusy     = "busy"
	TeamMemberStatusFailed   = "failed"
	TeamMemberStatusOffline  = "offline"
	TeamMemberStatusDeleting = "deleting"
	TeamMemberStatusDeleted  = "deleted"

	TeamMemberAvailabilityUnknown = "unknown"
	TeamMemberAvailabilityIdle    = "idle"
	TeamMemberAvailabilityBusy    = "busy"
	TeamMemberAvailabilityBlocked = "blocked"
	TeamMemberAvailabilityOffline = "offline"

	TeamStatusDeleting = "deleting"
	TeamStatusDeleted  = "deleted"

	TeamTaskStatusPending    = "pending"
	TeamTaskStatusDispatched = "dispatched"
	TeamTaskStatusRunning    = "running"
	TeamTaskStatusSucceeded  = "succeeded"
	TeamTaskStatusFailed     = "failed"
	TeamTaskStatusStale      = "stale"
)

type Team struct {
	ID                  int       `db:"id,primarykey,autoincrement" json:"id"`
	UserID              int       `db:"user_id" json:"user_id"`
	Name                string    `db:"name" json:"name"`
	Description         *string   `db:"description" json:"description,omitempty"`
	Status              string    `db:"status" json:"status"`
	CommunicationMode   string    `db:"communication_mode" json:"communication_mode"`
	RedisURLSecretName  *string   `db:"redis_url_secret_name" json:"-"`
	RedisURLSecretKey   *string   `db:"redis_url_secret_key" json:"-"`
	TeamTokenSecretName *string   `db:"team_token_secret_name" json:"-"`
	TeamTokenSecretKey  *string   `db:"team_token_secret_key" json:"-"`
	RedisEventsLastID   string    `db:"redis_events_last_id" json:"redis_events_last_id"`
	SharedPVCName       *string   `db:"shared_pvc_name" json:"shared_pvc_name,omitempty"`
	SharedPVCNamespace  *string   `db:"shared_pvc_namespace" json:"shared_pvc_namespace,omitempty"`
	SharedMountPath     string    `db:"shared_mount_path" json:"shared_mount_path"`
	StorageClass        *string   `db:"storage_class" json:"storage_class,omitempty"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
}

func (Team) TableName() string {
	return "teams"
}

type TeamMember struct {
	ID            int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID        int        `db:"team_id" json:"team_id"`
	InstanceID    *int       `db:"instance_id" json:"instance_id,omitempty"`
	UserID        int        `db:"user_id" json:"user_id"`
	MemberKey     string     `db:"member_key" json:"member_key"`
	DisplayName   string     `db:"display_name" json:"display_name"`
	Role          string     `db:"role" json:"role"`
	RuntimeType   string     `db:"runtime_type" json:"runtime_type"`
	InstanceMode  string     `db:"instance_mode" json:"instance_mode"`
	Description   *string    `db:"description" json:"description,omitempty"`
	Status        string     `db:"status" json:"status"`
	CurrentTaskID *int       `db:"current_task_id" json:"current_task_id,omitempty"`
	Progress      int        `db:"progress" json:"progress"`
	LastSeenAt    *time.Time `db:"last_seen_at" json:"last_seen_at,omitempty"`
	Availability  string     `db:"availability" json:"availability"`
	RuntimeStatus *string    `db:"runtime_status" json:"runtime_status,omitempty"`
	RuntimeTaskID *string    `db:"runtime_task_id" json:"runtime_task_id,omitempty"`
	RuntimeIntent *string    `db:"runtime_intent" json:"runtime_intent,omitempty"`
	BlockedReason *string    `db:"blocked_reason" json:"blocked_reason,omitempty"`
	LastSummary   *string    `db:"last_summary" json:"last_summary,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

func (TeamMember) TableName() string {
	return "team_members"
}

type TeamTask struct {
	ID                   int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID               int        `db:"team_id" json:"team_id"`
	TargetMemberID       int        `db:"target_member_id" json:"target_member_id"`
	CreatedBy            *int       `db:"created_by" json:"created_by,omitempty"`
	MessageID            string     `db:"message_id" json:"message_id"`
	Status               string     `db:"status" json:"status"`
	WorkflowState        string     `db:"workflow_state" json:"workflow_state"`
	PlanVersion          int64      `db:"plan_version" json:"plan_version"`
	LedgerVersion        int64      `db:"ledger_version" json:"ledger_version"`
	CurrentPhaseID       *string    `db:"current_phase_id" json:"current_phase_id,omitempty"`
	AcceptedCompletionID *string    `db:"accepted_completion_id" json:"accepted_completion_id,omitempty"`
	PayloadJSON          string     `db:"payload_json" json:"-"`
	ResultJSON           *string    `db:"result_json" json:"-"`
	ErrorMessage         *string    `db:"error_message" json:"error_message,omitempty"`
	RedisStreamID        *string    `db:"redis_stream_id" json:"redis_stream_id,omitempty"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
	DispatchedAt         *time.Time `db:"dispatched_at" json:"dispatched_at,omitempty"`
	StartedAt            *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt           *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt            time.Time  `db:"updated_at" json:"updated_at"`
}

func (TeamTask) TableName() string {
	return "team_tasks"
}

type TeamEvent struct {
	ID            int        `db:"id,primarykey,autoincrement" json:"id"`
	EventID       *string    `db:"event_id" json:"event_id,omitempty"`
	CompletionID  *string    `db:"completion_id" json:"completion_id,omitempty"`
	SequenceNo    int64      `db:"sequence_no" json:"sequence_no"`
	TeamID        int        `db:"team_id" json:"team_id"`
	MemberID      *int       `db:"member_id" json:"member_id,omitempty"`
	TaskID        *int       `db:"task_id" json:"task_id,omitempty"`
	MessageID     *string    `db:"message_id" json:"message_id,omitempty"`
	EventType     string     `db:"event_type" json:"event_type"`
	PayloadJSON   *string    `db:"payload_json" json:"-"`
	RedisStreamID *string    `db:"redis_stream_id" json:"redis_stream_id,omitempty"`
	OccurredAt    *time.Time `db:"occurred_at" json:"occurred_at,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
}

func (TeamEvent) TableName() string {
	return "team_events"
}

type TeamWorkItem struct {
	ID                int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID            int        `db:"team_id" json:"team_id"`
	RootTaskID        int        `db:"root_task_id" json:"root_task_id"`
	WorkID            string     `db:"work_id" json:"work_id"`
	AssignmentID      *string    `db:"assignment_id" json:"assignment_id,omitempty"`
	CanonicalWorkID   *string    `db:"canonical_work_id" json:"canonical_work_id,omitempty"`
	PhaseID           *string    `db:"phase_id" json:"phase_id,omitempty"`
	Revision          int        `db:"revision" json:"revision"`
	RequiredForRoot   bool       `db:"required_for_root" json:"required_for_root"`
	SupersededBy      *string    `db:"superseded_by" json:"superseded_by,omitempty"`
	ReviewRequired    bool       `db:"review_required" json:"review_required"`
	ValidatedRevision *int       `db:"validated_revision" json:"validated_revision,omitempty"`
	OwnerMemberID     *int       `db:"owner_member_id" json:"owner_member_id,omitempty"`
	Title             string     `db:"title" json:"title"`
	Status            string     `db:"status" json:"status"`
	DependsOnJSON     *string    `db:"depends_on_json" json:"-"`
	ResultJSON        *string    `db:"result_json" json:"-"`
	ArtifactRefsJSON  *string    `db:"artifact_refs_json" json:"-"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	StartedAt         *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt        *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
}

type TeamWorkflowPhase struct {
	ID               int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID           int        `db:"team_id" json:"team_id"`
	RootTaskID       int        `db:"root_task_id" json:"root_task_id"`
	PhaseID          string     `db:"phase_id" json:"phase_id"`
	PlanVersion      int64      `db:"plan_version" json:"plan_version"`
	SequenceNo       int        `db:"sequence_no" json:"sequence_no"`
	Status           string     `db:"status" json:"status"`
	RequiredForRoot  bool       `db:"required_for_root" json:"required_for_root"`
	DecisionRequired bool       `db:"decision_required" json:"decision_required"`
	DependsOnJSON    *string    `db:"depends_on_json" json:"-"`
	NextPhaseID      *string    `db:"next_phase_id" json:"next_phase_id,omitempty"`
	CompletionPolicy *string    `db:"completion_policy" json:"completion_policy,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	CompletedAt      *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

func (TeamWorkflowPhase) TableName() string {
	return "team_workflow_phases"
}

type TeamEventOutbox struct {
	ID            int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID        int        `db:"team_id" json:"team_id"`
	SourceEventID string     `db:"source_event_id" json:"source_event_id"`
	Destination   string     `db:"destination" json:"destination"`
	MessageID     string     `db:"message_id" json:"message_id"`
	PayloadJSON   string     `db:"payload_json" json:"-"`
	Status        string     `db:"status" json:"status"`
	Attempts      int        `db:"attempts" json:"attempts"`
	AvailableAt   time.Time  `db:"available_at" json:"available_at"`
	LastError     *string    `db:"last_error" json:"last_error,omitempty"`
	DeliveredAt   *time.Time `db:"delivered_at" json:"delivered_at,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

func (TeamEventOutbox) TableName() string {
	return "team_event_outbox"
}

func (TeamWorkItem) TableName() string {
	return "team_work_items"
}
