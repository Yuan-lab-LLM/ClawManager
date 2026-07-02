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
	ID             int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID         int        `db:"team_id" json:"team_id"`
	TargetMemberID int        `db:"target_member_id" json:"target_member_id"`
	CreatedBy      *int       `db:"created_by" json:"created_by,omitempty"`
	MessageID      string     `db:"message_id" json:"message_id"`
	Status         string     `db:"status" json:"status"`
	PayloadJSON    string     `db:"payload_json" json:"-"`
	ResultJSON     *string    `db:"result_json" json:"-"`
	ErrorMessage   *string    `db:"error_message" json:"error_message,omitempty"`
	RedisStreamID  *string    `db:"redis_stream_id" json:"redis_stream_id,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	DispatchedAt   *time.Time `db:"dispatched_at" json:"dispatched_at,omitempty"`
	StartedAt      *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt     *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
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
	ID               int        `db:"id,primarykey,autoincrement" json:"id"`
	TeamID           int        `db:"team_id" json:"team_id"`
	RootTaskID       int        `db:"root_task_id" json:"root_task_id"`
	WorkID           string     `db:"work_id" json:"work_id"`
	OwnerMemberID    *int       `db:"owner_member_id" json:"owner_member_id,omitempty"`
	Title            string     `db:"title" json:"title"`
	Status           string     `db:"status" json:"status"`
	DependsOnJSON    *string    `db:"depends_on_json" json:"-"`
	ResultJSON       *string    `db:"result_json" json:"-"`
	ArtifactRefsJSON *string    `db:"artifact_refs_json" json:"-"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	StartedAt        *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt       *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

func (TeamWorkItem) TableName() string {
	return "team_work_items"
}
