package models

import "time"

type Skill struct {
	ID               int        `db:"id,primarykey,autoincrement" json:"id"`
	UserID           int        `db:"user_id" json:"user_id"`
	SkillKey         string     `db:"skill_key" json:"skill_key"`
	Name             string     `db:"name" json:"name"`
	Description      *string    `db:"description" json:"description,omitempty"`
	CurrentVersionID *int       `db:"current_version_id" json:"current_version_id,omitempty"`
	SourceType       string     `db:"source_type" json:"source_type"`
	Status           string     `db:"status" json:"status"`
	Visibility       string     `db:"visibility" json:"visibility"`
	PublishedAt      *time.Time `db:"published_at" json:"published_at,omitempty"`
	PublishedBy      *int       `db:"published_by" json:"published_by,omitempty"`
	RiskLevel        string     `db:"risk_level" json:"risk_level"`
	LastScannedAt    *time.Time `db:"last_scanned_at" json:"last_scanned_at,omitempty"`
	LastScanResultID *int       `db:"last_scan_result_id" json:"last_scan_result_id,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

func (s Skill) TableName() string { return "skills" }

type SkillBlob struct {
	ID               int        `db:"id,primarykey,autoincrement" json:"id"`
	ContentHash      string     `db:"content_hash" json:"content_hash"`
	ArchiveHash      string     `db:"archive_hash" json:"archive_hash"`
	ObjectKey        string     `db:"object_key" json:"object_key"`
	FileName         string     `db:"file_name" json:"file_name"`
	MediaType        string     `db:"media_type" json:"media_type"`
	SizeBytes        int64      `db:"size_bytes" json:"size_bytes"`
	ScanStatus       string     `db:"scan_status" json:"scan_status"`
	RiskLevel        string     `db:"risk_level" json:"risk_level"`
	LastScannedAt    *time.Time `db:"last_scanned_at" json:"last_scanned_at,omitempty"`
	LastScanResultID *int       `db:"last_scan_result_id" json:"last_scan_result_id,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

func (s SkillBlob) TableName() string { return "skill_blobs" }

type SkillVersion struct {
	ID           int       `db:"id,primarykey,autoincrement" json:"id"`
	SkillID      int       `db:"skill_id" json:"skill_id"`
	BlobID       int       `db:"blob_id" json:"blob_id"`
	VersionNo    int       `db:"version_no" json:"version_no"`
	ManifestJSON *string   `db:"manifest_json" json:"-"`
	SourceType   string    `db:"source_type" json:"source_type"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

func (s SkillVersion) TableName() string { return "skill_versions" }

type InstanceSkill struct {
	ID             int        `db:"id,primarykey,autoincrement" json:"id"`
	InstanceID     int        `db:"instance_id" json:"instance_id"`
	SkillID        int        `db:"skill_id" json:"skill_id"`
	SkillVersionID *int       `db:"skill_version_id" json:"skill_version_id,omitempty"`
	SourceType     string     `db:"source_type" json:"source_type"`
	InstallPath    *string    `db:"install_path" json:"install_path,omitempty"`
	WorkspaceDir   *string    `db:"workspace_dir" json:"workspace_dir,omitempty"`
	ObservedHash   *string    `db:"observed_hash" json:"observed_hash,omitempty"`
	Status         string     `db:"status" json:"status"`
	LastSeenAt     *time.Time `db:"last_seen_at" json:"last_seen_at,omitempty"`
	RemovedAt      *time.Time `db:"removed_at" json:"removed_at,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}

func (s InstanceSkill) TableName() string { return "instance_skills" }

type SkillScanResult struct {
	ID           int        `db:"id,primarykey,autoincrement" json:"id"`
	BlobID       int        `db:"blob_id" json:"blob_id"`
	Engine       string     `db:"engine" json:"engine"`
	RiskLevel    string     `db:"risk_level" json:"risk_level"`
	Status       string     `db:"status" json:"status"`
	Summary      *string    `db:"summary" json:"summary,omitempty"`
	FindingsJSON *string    `db:"findings_json" json:"-"`
	ScannedAt    *time.Time `db:"scanned_at" json:"scanned_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

func (s SkillScanResult) TableName() string { return "skill_scan_results" }
