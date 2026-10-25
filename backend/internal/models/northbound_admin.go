package models

import "time"

type NorthboundAdminSettings struct {
	ID                        int       `db:"id,primarykey" json:"id"`
	APIEnabled                bool      `db:"api_enabled" json:"api_enabled"`
	ExternalNodePort          int       `db:"external_node_port" json:"external_node_port"`
	RequireExplicitCallers    bool      `db:"require_explicit_callers" json:"require_explicit_callers"`
	ChallengeTTLSeconds       int       `db:"challenge_ttl_seconds" json:"challenge_ttl_seconds"`
	AccessTokenTTLSeconds     int       `db:"access_token_ttl_seconds" json:"access_token_ttl_seconds"`
	RefreshTokenTTLSeconds    int       `db:"refresh_token_ttl_seconds" json:"refresh_token_ttl_seconds"`
	CoreRequestTimeoutSeconds int       `db:"core_request_timeout_seconds" json:"core_request_timeout_seconds"`
	ChallengeRatePerMinute    int       `db:"challenge_rate_per_minute" json:"challenge_rate_per_minute"`
	LoginRatePerMinute        int       `db:"login_rate_per_minute" json:"login_rate_per_minute"`
	AccountLoginRatePerMinute int       `db:"account_login_rate_per_minute" json:"account_login_rate_per_minute"`
	CreateRatePerMinute       int       `db:"create_rate_per_minute" json:"create_rate_per_minute"`
	QueryRatePerMinute        int       `db:"query_rate_per_minute" json:"query_rate_per_minute"`
	ShareRatePerMinute        int       `db:"share_rate_per_minute" json:"share_rate_per_minute"`
	MaxPendingOperations      int       `db:"max_pending_operations" json:"max_pending_operations"`
	OperationTickMilliseconds int       `db:"operation_tick_milliseconds" json:"operation_tick_milliseconds"`
	OperationLeaseSeconds     int       `db:"operation_lease_seconds" json:"operation_lease_seconds"`
	OperationMaxAttempts      int       `db:"operation_max_attempts" json:"operation_max_attempts"`
	AllowedLiteTypesJSON      string    `db:"allowed_lite_types" json:"-"`
	AllowedProTypesJSON       string    `db:"allowed_pro_types" json:"-"`
	AllowedLiteTypes          []string  `db:"-" json:"allowed_lite_types"`
	AllowedProTypes           []string  `db:"-" json:"allowed_pro_types"`
	LiteCPUCores              float64   `db:"lite_cpu_cores" json:"lite_cpu_cores"`
	LiteMemoryGB              int       `db:"lite_memory_gb" json:"lite_memory_gb"`
	LiteDiskGB                int       `db:"lite_disk_gb" json:"lite_disk_gb"`
	ProCPUCores               float64   `db:"pro_cpu_cores" json:"pro_cpu_cores"`
	ProMemoryGB               int       `db:"pro_memory_gb" json:"pro_memory_gb"`
	ProDiskGB                 int       `db:"pro_disk_gb" json:"pro_disk_gb"`
	WorkBuddyProCPUCores      float64   `db:"workbuddy_pro_cpu_cores" json:"workbuddy_pro_cpu_cores"`
	WorkBuddyProMemoryGB      int       `db:"workbuddy_pro_memory_gb" json:"workbuddy_pro_memory_gb"`
	WorkBuddyProDiskGB        int       `db:"workbuddy_pro_disk_gb" json:"workbuddy_pro_disk_gb"`
	Version                   int64     `db:"version" json:"version"`
	UpdatedBy                 *int      `db:"updated_by" json:"updated_by,omitempty"`
	CreatedAt                 time.Time `db:"created_at" json:"created_at"`
	UpdatedAt                 time.Time `db:"updated_at" json:"updated_at"`
}

func (NorthboundAdminSettings) TableName() string { return "northbound_admin_settings" }

type NorthboundCallerPolicy struct {
	APIKeysJSON    *string   `db:"api_keys_json" json:"-"`
	APIKeysVersion int64     `db:"api_keys_version" json:"-"`
	UserID         int       `db:"user_id,primarykey" json:"user_id"`
	Username       string    `db:"username" json:"username,omitempty"`
	Email          string    `db:"email" json:"email,omitempty"`
	Enabled        bool      `db:"enabled" json:"enabled"`
	ScopesJSON     string    `db:"scopes_json" json:"-"`
	Scopes         []string  `db:"-" json:"scopes"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
	UpdatedBy      *int      `db:"updated_by" json:"updated_by,omitempty"`
}

func (NorthboundCallerPolicy) TableName() string { return "northbound_caller_policies" }

type NorthboundOperationalStats struct {
	ActiveSessions       int `json:"active_sessions"`
	QueuedOperations     int `json:"queued_operations"`
	ProcessingOperations int `json:"processing_operations"`
	FailedOperations     int `json:"failed_operations"`
}

type NorthboundSettingsAudit struct {
	ID          int64     `json:"id"`
	ActorUserID *int      `json:"actor_user_id,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	Action      string    `json:"action"`
	BeforeJSON  *string   `json:"before_json,omitempty"`
	AfterJSON   *string   `json:"after_json,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
