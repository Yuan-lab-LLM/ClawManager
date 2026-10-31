package models

import "time"

type SecurityScanConfig struct {
	ID                  int       `db:"id,primarykey,autoincrement" json:"id"`
	DefaultMode         string    `db:"default_mode" json:"default_mode"`
	QuickAnalyzersJSON  string    `db:"quick_analyzers_json" json:"-"`
	DeepAnalyzersJSON   string    `db:"deep_analyzers_json" json:"-"`
	QuickTimeoutSeconds int       `db:"quick_timeout_seconds" json:"quick_timeout_seconds"`
	DeepTimeoutSeconds  int       `db:"deep_timeout_seconds" json:"deep_timeout_seconds"`
	AllowFallback       bool      `db:"allow_fallback" json:"allow_fallback"`
	UpdatedBy           *int      `db:"updated_by" json:"updated_by,omitempty"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
}

func (s SecurityScanConfig) TableName() string { return "security_scan_configs" }

type SecurityScanJob struct {
	ID              int        `db:"id,primarykey,autoincrement" json:"id"`
	AssetType       string     `db:"asset_type" json:"asset_type"`
	ScanMode        string     `db:"scan_mode" json:"scan_mode"`
	Status          string     `db:"status" json:"status"`
	RequestedBy     *int       `db:"requested_by" json:"requested_by,omitempty"`
	ScopeJSON       *string    `db:"scope_json" json:"-"`
	TotalItems      int        `db:"total_items" json:"total_items"`
	CompletedItems  int        `db:"completed_items" json:"completed_items"`
	FailedItems     int        `db:"failed_items" json:"failed_items"`
	CurrentItemName *string    `db:"current_item_name" json:"current_item_name,omitempty"`
	StartedAt       *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt      *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}

func (s SecurityScanJob) TableName() string { return "security_scan_jobs" }

type SecurityScanJobItem struct {
	ID           int        `db:"id,primarykey,autoincrement" json:"id"`
	JobID        int        `db:"job_id" json:"job_id"`
	AssetType    string     `db:"asset_type" json:"asset_type"`
	AssetID      int        `db:"asset_id" json:"asset_id"`
	AssetName    string     `db:"asset_name" json:"asset_name"`
	Status       string     `db:"status" json:"status"`
	ProgressPct  int        `db:"progress_pct" json:"progress_pct"`
	RiskLevel    *string    `db:"risk_level" json:"risk_level,omitempty"`
	Summary      *string    `db:"summary" json:"summary,omitempty"`
	ScanResultID *int       `db:"scan_result_id" json:"scan_result_id,omitempty"`
	CachedResult bool       `db:"cached_result" json:"cached_result"`
	ErrorMessage *string    `db:"error_message" json:"error_message,omitempty"`
	StartedAt    *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

func (s SecurityScanJobItem) TableName() string { return "security_scan_job_items" }

type SecurityScanReport struct {
	ID          int       `db:"id,primarykey,autoincrement" json:"id"`
	JobID       int       `db:"job_id" json:"job_id"`
	SummaryJSON string    `db:"summary_json" json:"-"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

func (s SecurityScanReport) TableName() string { return "security_scan_reports" }
