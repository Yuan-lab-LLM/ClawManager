package models

import "time"

type NorthboundAuthChallenge struct {
	ID          int64      `db:"id,primarykey,autoincrement" json:"-"`
	ChallengeID string     `db:"challenge_id" json:"challenge_id"`
	NonceHash   string     `db:"nonce_hash" json:"-"`
	KeyID       string     `db:"key_id" json:"key_id"`
	Status      string     `db:"status" json:"status"`
	SourceIP    *string    `db:"source_ip" json:"-"`
	ExpiresAt   time.Time  `db:"expires_at" json:"expires_at"`
	UsedAt      *time.Time `db:"used_at" json:"-"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"-"`
}

func (NorthboundAuthChallenge) TableName() string { return "northbound_auth_challenges" }

type NorthboundSession struct {
	ID                       int64      `db:"id,primarykey,autoincrement" json:"-"`
	SessionID                string     `db:"session_id" json:"session_id"`
	UserID                   int        `db:"user_id" json:"user_id"`
	RefreshTokenHash         string     `db:"refresh_token_hash" json:"-"`
	PreviousRefreshTokenHash *string    `db:"previous_refresh_token_hash" json:"-"`
	RefreshTokenHistory      string     `db:"refresh_token_history" json:"-"`
	ScopesJSON               string     `db:"scopes_json" json:"-"`
	Status                   string     `db:"status" json:"status"`
	AccessExpiresAt          time.Time  `db:"access_expires_at" json:"access_expires_at"`
	RefreshExpiresAt         time.Time  `db:"refresh_expires_at" json:"refresh_expires_at"`
	LastUsedAt               *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	LastIP                   *string    `db:"last_ip" json:"-"`
	UserAgent                *string    `db:"user_agent" json:"-"`
	CreatedAt                time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at" json:"-"`
	RevokedAt                *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
}

func (NorthboundSession) TableName() string { return "northbound_sessions" }

type NorthboundOperation struct {
	OwnerRequestKey    *string    `db:"owner_request_key" json:"-"`
	Owner              *string    `db:"owner" json:"-"`
	TeamID             *int       `db:"team_id" json:"team_id,omitempty"`
	TemplateID         *int       `db:"template_id" json:"template_id,omitempty"`
	APIKeyID           *string    `db:"api_key_id" json:"-"`
	AuthScope          *string    `db:"auth_scope" json:"-"`
	ID                 int64      `db:"id,primarykey,autoincrement" json:"-"`
	OperationID        string     `db:"operation_id" json:"operation_id"`
	UserID             int        `db:"user_id" json:"-"`
	SessionID          string     `db:"session_id" json:"-"`
	OperationType      string     `db:"operation_type" json:"resource_type"`
	IdempotencyKeyHash string     `db:"idempotency_key_hash" json:"-"`
	RequestHash        string     `db:"request_hash" json:"-"`
	RequestPayload     string     `db:"request_payload" json:"-"`
	Status             string     `db:"status" json:"status"`
	AvailableAt        time.Time  `db:"available_at" json:"-"`
	InstanceID         *int       `db:"instance_id" json:"instance_id,omitempty"`
	AttemptCount       int        `db:"attempt_count" json:"attempt_count,omitempty"`
	LeaseOwner         *string    `db:"lease_owner" json:"-"`
	LeaseExpiresAt     *time.Time `db:"lease_expires_at" json:"-"`
	ErrorCode          *string    `db:"error_code" json:"error_code,omitempty"`
	ErrorMessage       *string    `db:"error_message" json:"error_message,omitempty"`
	CreatedAt          time.Time  `db:"created_at" json:"created_at"`
	StartedAt          *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt         *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	UpdatedAt          time.Time  `db:"updated_at" json:"updated_at"`
}

func (NorthboundOperation) TableName() string { return "northbound_operations" }
