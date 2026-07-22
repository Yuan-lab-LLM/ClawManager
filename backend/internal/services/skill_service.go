package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
)

var (
	chownRuntimePathOwner = os.Chown
	currentEffectiveUID   = os.Geteuid
)

const (
	skillRiskUnknown = "unknown"
	skillRiskNone    = "none"
	skillRiskLow     = "low"
	skillRiskMedium  = "medium"
	skillRiskHigh    = "high"

	skillSourceUploaded   = "uploaded"
	skillSourceDiscovered = "discovered"

	skillStatusActive  = "active"
	skillStatusDeleted = "deleted"
)

type SkillPayload struct {
	ID               int                   `json:"id"`
	ExternalSkillID  string                `json:"external_skill_id"`
	UserID           int                   `json:"user_id"`
	SkillKey         string                `json:"skill_key"`
	Name             string                `json:"name"`
	Description      *string               `json:"description,omitempty"`
	Status           string                `json:"status"`
	SourceType       string                `json:"source_type"`
	RiskLevel        string                `json:"risk_level"`
	ScanStatus       string                `json:"scan_status"`
	LastScannedAt    *time.Time            `json:"last_scanned_at,omitempty"`
	CurrentVersionID *int                  `json:"current_version_id,omitempty"`
	CurrentVersionNo *int                  `json:"current_version_no,omitempty"`
	ContentHash      *string               `json:"content_hash,omitempty"`
	ContentMD5       *string               `json:"content_md5,omitempty"`
	ArchiveHash      *string               `json:"archive_hash,omitempty"`
	RiskReason       *string               `json:"risk_reason,omitempty"`
	TopFindings      []SkillFindingPayload `json:"top_findings,omitempty"`
	InstanceCount    int                   `json:"instance_count"`
	Visibility       string                `json:"visibility"`
	PublishedAt      *time.Time            `json:"published_at,omitempty"`
	PublishedBy      *int                  `json:"published_by,omitempty"`
	Tags             []SkillHubTagPayload  `json:"tags,omitempty"`
	Publishable              bool                  `json:"publishable"`
	PublishBlockedReason     *string               `json:"publish_blocked_reason,omitempty"`
	PackageCollectError      *string               `json:"package_collect_error,omitempty"`
	PackageMaterializeStatus *string               `json:"package_materialize_status,omitempty"`
	PackageMaterializeError  *string               `json:"package_materialize_error,omitempty"`
	OwnerUsername            *string               `json:"owner_username,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}

type SkillFindingPayload struct {
	Analyzer    string  `json:"analyzer"`
	Severity    string  `json:"severity"`
	Category    string  `json:"category"`
	RuleID      string  `json:"rule_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	FilePath    *string `json:"file_path,omitempty"`
	LineNumber  *int    `json:"line_number,omitempty"`
	Remediation string  `json:"remediation"`
	Snippet     *string `json:"snippet,omitempty"`
}

type SkillVersionPayload struct {
	ID                int       `json:"id"`
	ExternalVersionID string    `json:"external_version_id"`
	SkillID           int       `json:"skill_id"`
	BlobID            int       `json:"blob_id"`
	VersionNo         int       `json:"version_no"`
	SourceType        string    `json:"source_type"`
	ContentHash       string    `json:"content_hash"`
	ContentMD5        string    `json:"content_md5"`
	ArchiveHash       string    `json:"archive_hash"`
	ObjectKey         string    `json:"object_key"`
	FileName          string    `json:"file_name"`
	RiskLevel         string    `json:"risk_level"`
	CreatedAt         time.Time `json:"created_at"`
}

type InstanceSkillPayload struct {
	ID             int           `json:"id"`
	InstanceID     int           `json:"instance_id"`
	SkillID        int           `json:"skill_id"`
	SkillVersionID *int          `json:"skill_version_id,omitempty"`
	SourceType     string        `json:"source_type"`
	InstallPath    *string       `json:"install_path,omitempty"`
	WorkspaceDir   *string       `json:"workspace_dir,omitempty"`
	ObservedHash   *string       `json:"observed_hash,omitempty"`
	ContentMD5     *string       `json:"content_md5,omitempty"`
	Status         string        `json:"status"`
	LastSeenAt     *time.Time    `json:"last_seen_at,omitempty"`
	RemovedAt      *time.Time    `json:"removed_at,omitempty"`
	Skill          *SkillPayload `json:"skill,omitempty"`
}

type SkillScanResultPayload struct {
	ID             int                    `json:"id"`
	BlobID         int                    `json:"blob_id"`
	Engine         string                 `json:"engine"`
	RiskLevel      string                 `json:"risk_level"`
	Status         string                 `json:"status"`
	Summary        *string                `json:"summary,omitempty"`
	Findings       map[string]interface{} `json:"findings,omitempty"`
	ParsedFindings []SkillFindingPayload  `json:"parsed_findings,omitempty"`
	ScannedAt      *time.Time             `json:"scanned_at,omitempty"`
}

type UpdateSkillRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
}

type AttachSkillToInstanceRequest struct {
	SkillID int `json:"skill_id" binding:"required,min=1"`
}

type AgentSkillRecord struct {
	SkillID      string                 `json:"skill_id"`
	SkillVersion string                 `json:"skill_version"`
	Identifier   string                 `json:"identifier" binding:"required"`
	InstallPath  string                 `json:"install_path"`
	ContentMD5   string                 `json:"content_md5" binding:"required"`
	Source       string                 `json:"source"`
	Type         string                 `json:"type"`
	SizeBytes    int64                  `json:"size_bytes"`
	FileCount    int                    `json:"file_count"`
	CollectedAt  *time.Time             `json:"collected_at,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type AgentSkillInventoryReportRequest struct {
	AgentID    string             `json:"agent_id" binding:"required"`
	ReportedAt *time.Time         `json:"reported_at,omitempty"`
	Mode       string             `json:"mode"`
	Trigger    string             `json:"trigger"`
	Skills     []AgentSkillRecord `json:"skills" binding:"required"`
}

type AgentSkillPackageUploadRequest struct {
	AgentID      string `json:"agent_id"`
	SkillID      string `json:"skill_id"`
	SkillVersion string `json:"skill_version"`
	Identifier   string `json:"identifier"`
	ContentMD5   string `json:"content_md5"`
	Source       string `json:"source"`
}

type SkillService interface {
	ImportArchive(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillPayload, error)
	ListSkills(userID int) ([]SkillPayload, error)
	ListAllSkills() ([]SkillPayload, error)
	ListAvailableSkillsForInstance(instanceID int, userID int, userRole string) ([]SkillPayload, error)
	GetSkill(actorUserID int, actorRole string, skillID int) (*SkillPayload, error)
	UpdateSkill(userID, skillID int, req UpdateSkillRequest) (*SkillPayload, error)
	DeleteSkill(actorUserID int, actorRole string, skillID int) error
	DownloadSkill(actorUserID int, actorRole string, skillID int) ([]byte, string, error)
	DownloadSkillVersionByExternalID(externalVersionID string) ([]byte, string, error)
	ListVersions(actorUserID int, actorRole string, skillID int) ([]SkillVersionPayload, error)
	ListInstanceSkills(instanceID int) ([]InstanceSkillPayload, error)
	AttachSkillToInstance(actorUserID int, actorRole string, instanceID int, skillID int) (*InstanceSkillPayload, error)
	RemoveSkillFromInstance(instanceID int, skillID int) error
	SyncAgentSkills(instanceID int, req AgentSkillInventoryReportRequest) error
	UploadAgentSkillPackage(ctx context.Context, instanceID int, req AgentSkillPackageUploadRequest, fileHeader *multipart.FileHeader) (*SkillPayload, error)
	ListScanResults(actorUserID int, actorRole string, skillID int) ([]SkillScanResultPayload, error)
	ListHubTags(actorRole string) ([]SkillHubTagPayload, error)
	ListHubCatalog(actorUserID int, actorRole string, query SkillHubCatalogQuery) (*SkillHubCatalogResponse, error)
	ListMyHubSkills(userID int) ([]SkillPayload, error)
	ListAllHubSkillsAdmin() ([]SkillPayload, error)
	GetSkillHubDetail(actorUserID int, actorRole string, skillID int) (*SkillPayload, error)
	PublishToHub(actorUserID int, actorRole string, skillID int, tagIDs []int) (*SkillPayload, error)
	UnpublishFromHub(actorUserID int, actorRole string, skillID int) (*SkillPayload, error)
	UpdateHubTags(actorUserID int, actorRole string, skillID int, tagIDs []int) (*SkillPayload, error)
	InstallHubSkill(actorUserID int, actorRole string, skillID, instanceID int) (*InstanceSkillPayload, error)
	BatchInstallHubSkill(actorUserID int, actorRole string, skillID int, instanceIDs []int) []BatchInstallHubSkillResult
	PublishFromInstance(actorUserID int, actorRole string, instanceID, skillID int, tagIDs []int) (*SkillPayload, error)
	ImportInstanceSkillToLibrary(actorUserID int, actorRole string, instanceID, skillID int) (*SkillPayload, error)
	RetrySkillPackageCollection(actorUserID int, actorRole string, instanceID, skillID int) error
	ListAttachableSkills(actorUserID int, actorRole string) ([]SkillPayload, error)
	ImportHubArchive(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillPayload, error)
	PreviewHubImport(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillImportPreviewItem, error)
	ImportHubArchiveWithDecisions(ctx context.Context, userID int, fileHeader *multipart.FileHeader, decisions []SkillImportDecision) ([]SkillImportResultItem, error)
	SyncRuntimeAgentSkillsReport(payload map[string]any) error
	RequestLiteSkillInventorySync(instanceID int) error
	CompletePendingSkillInventorySync(instanceID int)
}

type skillService struct {
	repo               repository.SkillRepository
	instanceRepo       repository.InstanceRepository
	userRepo           repository.UserRepository
	commandService     InstanceCommandService
	commandRepo        repository.InstanceCommandRepository
	storage            ObjectStorageService
	scanner            SkillScannerClient
	runtimeSkillSync   *runtimeSkillSyncDeps
	materializeService *SkillPackageMaterializeService
}

func NewSkillService(repo repository.SkillRepository, instanceRepo repository.InstanceRepository, userRepo repository.UserRepository, commandService InstanceCommandService, commandRepo repository.InstanceCommandRepository, storage ObjectStorageService, scanner SkillScannerClient) SkillService {
	return &skillService{repo: repo, instanceRepo: instanceRepo, userRepo: userRepo, commandService: commandService, commandRepo: commandRepo, storage: storage, scanner: scanner}
}

func ConfigureSkillPackageMaterialize(service SkillService, materialize *SkillPackageMaterializeService) {
	if impl, ok := service.(*skillService); ok {
		impl.materializeService = materialize
	}
}

func (s *skillService) ImportArchive(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillPayload, error) {
	directories, filename, err := readSkillArchiveDirectories(fileHeader)
	if err != nil {
		return nil, err
	}

	results := make([]SkillPayload, 0, len(directories))
	for _, dir := range directories {
		payload, err := s.importDirectory(ctx, userID, dir, filename)
		if err != nil {
			return nil, err
		}
		results = append(results, *payload)
	}
	return results, nil
}

func (s *skillService) ListSkills(userID int) ([]SkillPayload, error) {
	items, err := s.repo.ListSkillsByUser(userID)
	if err != nil {
		return nil, err
	}
	filtered := make([]models.Skill, 0, len(items))
	for _, item := range items {
		if isDeletedSkill(&item) {
			continue
		}
		if isUserManagedSkill(item) {
			filtered = append(filtered, item)
		}
	}
	return s.toSkillPayloads(filtered, userID, "")
}

func (s *skillService) ListAllSkills() ([]SkillPayload, error) {
	items, err := s.repo.ListAllSkills()
	if err != nil {
		return nil, err
	}
	return s.toSkillPayloads(items, 0, "admin")
}

func (s *skillService) ListAvailableSkillsForInstance(instanceID int, userID int, userRole string) ([]SkillPayload, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if !strings.EqualFold(userRole, "admin") && instance.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	items, err := s.repo.ListAllSkills()
	if err != nil {
		return nil, err
	}

	filtered := make([]models.Skill, 0, len(items))
	for _, item := range items {
		if !isUserManagedSkill(item) || !strings.EqualFold(item.Status, "active") {
			continue
		}
		if !canActorAttachSkillToInstance(instance, item, userID, userRole) {
			continue
		}
		filtered = append(filtered, item)
	}
	return s.toSkillPayloads(filtered, userID, userRole)
}

func (s *skillService) GetSkill(actorUserID int, actorRole string, skillID int) (*SkillPayload, error) {
	item, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if item == nil || !s.CanViewSkill(actorUserID, actorRole, item) {
		return nil, fmt.Errorf("skill not found")
	}
	if !isUserManagedSkill(*item) && !isAdminRole(actorRole) && item.UserID != actorUserID {
		return nil, fmt.Errorf("skill not found")
	}
	payload, err := s.toSkillPayload(*item)
	if err != nil {
		return nil, err
	}
	if err := s.enrichSkillPayload(payload, *item, nil); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *skillService) UpdateSkill(userID, skillID int, req UpdateSkillRequest) (*SkillPayload, error) {
	item, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID {
		return nil, fmt.Errorf("skill not found")
	}
	if !isUserManagedSkill(*item) {
		return nil, fmt.Errorf("skill not found")
	}
	item.Name = strings.TrimSpace(req.Name)
	item.Description = req.Description
	if status := strings.TrimSpace(req.Status); status != "" {
		item.Status = status
	}
	item.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateSkill(item); err != nil {
		return nil, err
	}
	return s.toSkillPayload(*item)
}

func (s *skillService) DeleteSkill(actorUserID int, actorRole string, skillID int) error {
	item, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return err
	}
	if item == nil || isDeletedSkill(item) {
		return fmt.Errorf("skill not found")
	}
	if !isAdminRole(actorRole) && item.UserID != actorUserID {
		return fmt.Errorf("skill not found")
	}
	if !isUserManagedSkill(*item) {
		return fmt.Errorf("skill not found")
	}
	now := time.Now().UTC()
	item.Status = skillStatusDeleted
	item.Visibility = skillVisibilityPrivate
	item.PublishedAt = nil
	item.PublishedBy = nil
	item.SkillKey = deletedSkillKey(item.SkillKey, item.ID)
	item.UpdatedAt = now
	if err := s.repo.ReplaceSkillTagAssignments(skillID, []int{}); err != nil {
		return err
	}
	return s.repo.UpdateSkill(item)
}

func (s *skillService) DownloadSkill(actorUserID int, actorRole string, skillID int) ([]byte, string, error) {
	item, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, "", err
	}
	if item == nil || !s.CanDownloadSkill(actorUserID, actorRole, item) {
		return nil, "", fmt.Errorf("skill not found")
	}
	if item.CurrentVersionID == nil {
		return nil, "", fmt.Errorf("skill has no version")
	}
	version, err := s.repo.GetVersionByID(*item.CurrentVersionID)
	if err != nil {
		return nil, "", err
	}
	if version == nil {
		return nil, "", fmt.Errorf("skill version not found")
	}
	blob, err := s.repo.GetBlobByID(version.BlobID)
	if err != nil {
		return nil, "", err
	}
	if blob == nil || strings.TrimSpace(blob.ObjectKey) == "" {
		return nil, "", fmt.Errorf("skill blob not found")
	}
	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	return content, blob.FileName, nil
}

func (s *skillService) DownloadSkillVersionByExternalID(externalVersionID string) ([]byte, string, error) {
	versionID, err := parseExternalVersionID(externalVersionID)
	if err != nil {
		return nil, "", err
	}
	version, err := s.repo.GetVersionByID(versionID)
	if err != nil {
		return nil, "", err
	}
	if version == nil {
		return nil, "", fmt.Errorf("skill version not found")
	}
	blob, err := s.repo.GetBlobByID(version.BlobID)
	if err != nil {
		return nil, "", err
	}
	if blob == nil {
		return nil, "", fmt.Errorf("skill blob not found")
	}
	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		return nil, "", err
	}
	return content, blob.FileName, nil
}

func (s *skillService) ListVersions(actorUserID int, actorRole string, skillID int) ([]SkillVersionPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || !s.CanViewSkill(actorUserID, actorRole, skill) || !isUserManagedSkill(*skill) {
		return nil, fmt.Errorf("skill not found")
	}
	items, err := s.repo.ListVersionsBySkillID(skillID)
	if err != nil {
		return nil, err
	}
	result := make([]SkillVersionPayload, 0, len(items))
	for _, item := range items {
		blob, err := s.repo.GetBlobByID(item.BlobID)
		if err != nil {
			return nil, err
		}
		result = append(result, SkillVersionPayload{
			ID: item.ID, ExternalVersionID: formatExternalVersionID(item.ID), SkillID: item.SkillID, BlobID: item.BlobID, VersionNo: item.VersionNo,
			SourceType: item.SourceType, ContentHash: blob.ContentHash, ContentMD5: s.resolveContentMD5(blob), ArchiveHash: blob.ArchiveHash,
			ObjectKey: blob.ObjectKey, FileName: blob.FileName, RiskLevel: blob.RiskLevel, CreatedAt: item.CreatedAt,
		})
	}
	return result, nil
}

func (s *skillService) ListInstanceSkills(instanceID int) ([]InstanceSkillPayload, error) {
	if err := s.reconcileRemovedInstanceSkillsFromCommands(instanceID); err != nil {
		return nil, err
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListInstanceSkills(instanceID)
	if err != nil {
		return nil, err
	}
	result := make([]InstanceSkillPayload, 0, len(items))
	for _, item := range items {
		if isRemovedInstanceSkill(&item) {
			continue
		}
		payload := InstanceSkillPayload{
			ID: item.ID, InstanceID: item.InstanceID, SkillID: item.SkillID, SkillVersionID: item.SkillVersionID,
			SourceType: item.SourceType, InstallPath: item.InstallPath, WorkspaceDir: item.WorkspaceDir, ObservedHash: item.ObservedHash,
			Status: item.Status, LastSeenAt: item.LastSeenAt, RemovedAt: item.RemovedAt,
		}
		skill, err := s.repo.GetSkillByID(item.SkillID)
		if err != nil {
			return nil, err
		}
		if skill != nil {
			skillPayload, err := s.toSkillPayload(*skill)
			if err != nil {
				return nil, err
			}
			if err := s.enrichSkillPayload(skillPayload, *skill, instance); err != nil {
				return nil, err
			}
			payload.Skill = skillPayload
		}
		result = append(result, payload)
	}
	return result, nil
}

func (s *skillService) reconcileRemovedInstanceSkillsFromCommands(instanceID int) error {
	if s.commandService == nil {
		return nil
	}
	commands, err := s.commandService.ListByInstanceID(instanceID, 500)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if command.CommandType != InstanceCommandTypeUninstallSkill || command.Status != instanceCommandStatusSucceeded {
			continue
		}
		if skillID, ok := intPayloadValue(command.Payload["skill_id"]); ok {
			if err := s.repo.MarkInstanceSkillRemoved(instanceID, skillID, commandFinishedAt(command)); err != nil {
				return err
			}
		}
		if skillKey, ok := stringPayloadValue(command.Payload["target_name"]); ok {
			if err := s.repo.MarkInstanceSkillRemovedBySkillKey(instanceID, skillKey, commandFinishedAt(command)); err != nil {
				return err
			}
		}
	}
	return nil
}

func commandFinishedAt(command InstanceCommandPayload) time.Time {
	if command.FinishedAt != nil && !command.FinishedAt.IsZero() {
		return *command.FinishedAt
	}
	if !command.IssuedAt.IsZero() {
		return command.IssuedAt
	}
	return time.Time{}
}

func (s *skillService) AttachSkillToInstance(actorUserID int, actorRole string, instanceID int, skillID int) (*InstanceSkillPayload, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if err := EnsureInstanceWorkspacePathForServerScan(context.Background(), s.instanceRepo, instance); err != nil {
		return nil, err
	}
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill not found")
	}
	if !s.CanAttachSkill(actorUserID, actorRole, skill, instance) {
		return nil, fmt.Errorf("skill_attach_forbidden")
	}
	if !isUserManagedSkill(*skill) {
		return nil, fmt.Errorf("skill not found")
	}
	if skill.Status != "active" {
		return nil, fmt.Errorf("skill is not active")
	}
	if isBlockedSkillRisk(skill.RiskLevel) {
		return nil, fmt.Errorf("skill is blocked by risk policy")
	}

	versionID := skill.CurrentVersionID
	var blob *models.SkillBlob
	if versionID != nil {
		version, err := s.repo.GetVersionByID(*versionID)
		if err != nil {
			return nil, err
		}
		blob, err = s.repo.GetBlobByID(version.BlobID)
		if err != nil {
			return nil, err
		}
		if err := s.materializeLiteInstanceSkill(context.Background(), instanceID, skill, blob); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	item := &models.InstanceSkill{
		InstanceID: instanceID, SkillID: skillID, SkillVersionID: versionID,
		SourceType: "injected_by_clawmanager", Status: "active", LastSeenAt: &now, UpdatedAt: now,
	}
	if err := s.repo.UpsertInstanceSkill(item); err != nil {
		return nil, err
	}
	if versionID != nil && blob != nil {
		if _, err := s.commandService.Create(instanceID, nil, CreateInstanceCommandRequest{
			CommandType: InstanceCommandTypeInstallSkill,
			Payload: map[string]interface{}{
				"skill_id":      formatExternalSkillID(skillID),
				"skill_version": formatExternalVersionID(*versionID),
				"target_name":   skill.SkillKey,
				"content_md5":   s.resolveContentMD5(blob),
			},
			IdempotencyKey: fmt.Sprintf("install-skill-%d-%d-%d", instanceID, skillID, now.UnixNano()),
			TimeoutSeconds: 300,
		}); err != nil {
			return nil, fmt.Errorf("failed to queue install skill command: %w", err)
		}
	}
	items, err := s.ListInstanceSkills(instanceID)
	if err != nil {
		return nil, err
	}
	for _, candidate := range items {
		if candidate.SkillID == skillID {
			return &candidate, nil
		}
	}
	return nil, fmt.Errorf("instance skill not found after attach")
}

func (s *skillService) materializeLiteInstanceSkill(ctx context.Context, instanceID int, skill *models.Skill, blob *models.SkillBlob) error {
	if s == nil || s.instanceRepo == nil || s.storage == nil || skill == nil || blob == nil {
		return nil
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if !isLiteRuntimeInstance(instance) || instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
		return nil
	}

	content, err := s.storage.GetObject(ctx, blob.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to load skill archive for lite materialization: %w", err)
	}
	dirs, err := extractSkillDirectories(blob.FileName, content)
	if err != nil {
		return fmt.Errorf("failed to read skill archive for lite materialization: %w", err)
	}
	if len(dirs) != 1 {
		return fmt.Errorf("lite skill materialization requires exactly one skill directory")
	}

	targetName := sanitizeSkillKey(skill.SkillKey)
	if targetName == "" {
		targetName = sanitizeSkillKey(dirs[0].Name)
	}
	if targetName == "" {
		return fmt.Errorf("lite skill materialization target is invalid")
	}

	targetRoot := runtimeSkillInstallRoot(instance)
	if targetRoot == "" {
		return nil
	}
	if err := writeSkillDirectoryAtomically(targetRoot, targetName, dirs[0].Files); err != nil {
		return err
	}
	return ensureLiteRuntimePersistentOwnership(instance)
}

func resolveInstanceSkillSourceType(existing *models.InstanceSkill, incoming string, skill *models.Skill) string {
	incoming = normalizeSkillSource(incoming)
	if existing != nil && strings.EqualFold(strings.TrimSpace(existing.SourceType), "injected_by_clawmanager") {
		return "injected_by_clawmanager"
	}
	if incoming == "injected_by_clawmanager" {
		return incoming
	}
	if existing != nil && skill != nil && strings.EqualFold(strings.TrimSpace(skill.SourceType), skillSourceUploaded) {
		if strings.EqualFold(strings.TrimSpace(existing.SourceType), "injected_by_clawmanager") {
			return existing.SourceType
		}
		if incoming == "discovered_in_instance" && strings.TrimSpace(existing.SourceType) != "" {
			return existing.SourceType
		}
	}
	return incoming
}

func liteRuntimePersistentRoot(instance *models.Instance) string {
	if instance == nil || instance.WorkspacePath == nil || strings.TrimSpace(*instance.WorkspacePath) == "" {
		return ""
	}
	workspacePath := filepath.Clean(strings.TrimSpace(*instance.WorkspacePath))
	if strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeHermes) {
		return filepath.Join(workspacePath, "home", ".hermes")
	}
	return filepath.Join(workspacePath, "home", ".openclaw")
}

func ensureLiteRuntimePersistentOwnership(instance *models.Instance) error {
	if os.PathSeparator != '/' || instance == nil || instance.ID <= 0 || instance.WorkspacePath == nil {
		return nil
	}
	workspacePath := filepath.Clean(strings.TrimSpace(*instance.WorkspacePath))
	persistentRoot := liteRuntimePersistentRoot(instance)
	if workspacePath == "" || persistentRoot == "" || !isPathWithin(workspacePath, persistentRoot) {
		return nil
	}

	uid := RuntimeLinuxID(instance.ID)
	gid := uid
	for _, dir := range liteRuntimePersistentAncestors(workspacePath, persistentRoot) {
		if err := chownRuntimePath(dir, uid, gid, 0750); err != nil {
			return err
		}
	}
	return filepath.WalkDir(persistentRoot, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !isPathWithin(workspacePath, current) {
			return fmt.Errorf("lite runtime path escapes workspace: %s", current)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		mode := os.FileMode(0640)
		if entry.IsDir() {
			mode = 0750
		}
		return chownRuntimePath(current, uid, gid, mode)
	})
}

func liteRuntimePersistentAncestors(workspacePath, persistentRoot string) []string {
	workspacePath = filepath.Clean(workspacePath)
	persistentRoot = filepath.Clean(persistentRoot)
	result := []string{workspacePath}
	rel, err := filepath.Rel(workspacePath, persistentRoot)
	if err != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return result
	}
	current := workspacePath
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		result = append(result, current)
	}
	return result
}

func chownRuntimePath(targetPath string, uid, gid int, mode os.FileMode) error {
	if err := chownRuntimePathOwner(targetPath, uid, gid); err != nil {
		if currentEffectiveUID() != 0 && errors.Is(err, os.ErrPermission) {
			if chmodErr := os.Chmod(targetPath, mode); chmodErr != nil {
				return fmt.Errorf("failed to set lite runtime permissions on %s: %w", targetPath, chmodErr)
			}
			return nil
		}
		return fmt.Errorf("failed to set lite runtime owner on %s: %w", targetPath, err)
	}
	if err := os.Chmod(targetPath, mode); err != nil {
		return fmt.Errorf("failed to set lite runtime permissions on %s: %w", targetPath, err)
	}
	return nil
}

func writeSkillDirectoryAtomically(targetRoot, relativePath string, files map[string][]byte) error {
	targetRoot = filepath.Clean(strings.TrimSpace(targetRoot))
	relativePath = sanitizeWorkspaceRelativePath(relativePath)
	if targetRoot == "." || targetRoot == "" || relativePath == "" {
		return fmt.Errorf("invalid runtime skill target")
	}
	targetPath, err := joinRuntimeSkillPath(targetRoot, relativePath)
	if err != nil {
		return fmt.Errorf("invalid runtime skill target")
	}
	if err := os.MkdirAll(targetRoot, 0750); err != nil {
		return fmt.Errorf("failed to prepare runtime skill root: %w", err)
	}
	tmpRoot := filepath.Join(targetRoot, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0750); err != nil {
		return fmt.Errorf("failed to prepare runtime skill temp root: %w", err)
	}

	tmpNameSafe := strings.ReplaceAll(relativePath, "/", "-")
	tmpDir, err := os.MkdirTemp(tmpRoot, ".tmp-skill-"+tmpNameSafe+"-")
	if err != nil {
		return fmt.Errorf("failed to create lite skill temp dir: %w", err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	for relPath, body := range files {
		clean := normalizeSkillRelPath(relPath)
		if clean == "" || hasHiddenPathSegment(clean) {
			continue
		}
		parts := strings.Split(clean, "/")
		if len(parts) == 0 {
			continue
		}
		entryPath := filepath.Join(append([]string{tmpDir}, parts...)...)
		if !isPathWithin(tmpDir, entryPath) {
			return fmt.Errorf("skill archive entry escapes target: %s", relPath)
		}
		if err := os.MkdirAll(filepath.Dir(entryPath), 0750); err != nil {
			return fmt.Errorf("failed to prepare lite skill directory: %w", err)
		}
		if err := os.WriteFile(entryPath, body, 0640); err != nil {
			return fmt.Errorf("failed to write lite skill file: %w", err)
		}
	}

	if !isPathWithin(targetRoot, targetPath) {
		return fmt.Errorf("runtime skill target escapes root")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0750); err != nil {
		return fmt.Errorf("failed to prepare lite skill target parent: %w", err)
	}
	backupPath := targetPath + ".old"
	_ = os.RemoveAll(backupPath)
	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return fmt.Errorf("failed to stage existing lite skill directory: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect existing lite skill directory: %w", err)
	}
	if err := os.Rename(tmpDir, targetPath); err != nil {
		if _, statErr := os.Stat(backupPath); statErr == nil {
			_ = os.Rename(backupPath, targetPath)
		}
		return fmt.Errorf("failed to install lite skill directory: %w", err)
	}
	cleanupTmp = false
	_ = os.RemoveAll(backupPath)
	return nil
}

func isPathWithin(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *skillService) RemoveSkillFromInstance(instanceID int, skillID int) error {
	item, err := s.repo.GetInstanceSkill(instanceID, skillID)
	if err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	if err := s.removeRuntimeInstanceSkillDirectory(instanceID, item); err != nil {
		return err
	}
	now := time.Now().UTC()
	item.Status = "removed"
	item.RemovedAt = &now
	item.UpdatedAt = now
	if err := s.repo.UpsertInstanceSkill(item); err != nil {
		return err
	}
	if _, err := s.commandService.Create(instanceID, nil, CreateInstanceCommandRequest{
		CommandType:    InstanceCommandTypeUninstallSkill,
		Payload:        map[string]interface{}{"skill_id": skillID, "target_name": skillKeyForRemoval(item)},
		IdempotencyKey: fmt.Sprintf("remove-skill-%d-%d-%d", instanceID, skillID, now.UnixNano()),
		TimeoutSeconds: 300,
	}); err != nil {
		return fmt.Errorf("failed to queue uninstall skill command: %w", err)
	}
	return nil
}

func (s *skillService) removeRuntimeInstanceSkillDirectory(instanceID int, item *models.InstanceSkill) error {
	if s == nil || s.instanceRepo == nil || item == nil {
		return nil
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if !isLiteRuntimeInstance(instance) && !SupportsServerWorkspaceSkillScan(instance) {
		return nil
	}
	relativePath := ""
	if item.WorkspaceDir != nil {
		relativePath = sanitizeWorkspaceRelativePath(strings.TrimSpace(*item.WorkspaceDir))
	}
	if relativePath == "" {
		skillKey := strings.TrimSpace(skillKeyForRemoval(item))
		if skillKey == "" || strings.HasPrefix(skillKey, "skill-") {
			skill, err := s.repo.GetSkillByID(item.SkillID)
			if err != nil {
				return err
			}
			if skill != nil {
				skillKey = strings.TrimSpace(skill.SkillKey)
			}
		}
		relativePath = sanitizeWorkspaceRelativePath(skillKey)
	}
	if relativePath == "" {
		return nil
	}
	targetRoot := runtimeSkillInstallRoot(instance)
	if targetRoot == "" {
		return nil
	}
	targetPath, err := joinRuntimeSkillPath(targetRoot, relativePath)
	if err != nil {
		return fmt.Errorf("runtime skill removal target is invalid")
	}
	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("failed to remove runtime skill directory: %w", err)
	}
	return ensureLiteRuntimePersistentOwnership(instance)
}

func (s *skillService) removeLiteInstanceSkillDirectory(instanceID int, item *models.InstanceSkill) error {
	return s.removeRuntimeInstanceSkillDirectory(instanceID, item)
}
func isBlockedSkillRisk(value string) bool {
	value = strings.TrimSpace(value)
	return strings.EqualFold(value, skillRiskMedium) || strings.EqualFold(value, skillRiskHigh)
}

func isRemovedInstanceSkill(item *models.InstanceSkill) bool {
	if item == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	return status == "removed" || status == "missing"
}

func isUserRemovedInstanceSkill(item *models.InstanceSkill) bool {
	return item != nil && strings.EqualFold(strings.TrimSpace(item.Status), "removed")
}

func (s *skillService) SyncAgentSkills(instanceID int, req AgentSkillInventoryReportRequest) error {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	ownerUserID := instance.UserID

	reportedAt := time.Now().UTC()
	if req.ReportedAt != nil && !req.ReportedAt.IsZero() {
		reportedAt = req.ReportedAt.UTC()
	}
	active := make([]int, 0, len(req.Skills))
	for _, record := range req.Skills {
		hash := workspaceContentHashForRecord(instance, record)
		if hash == "" {
			continue
		}
		normalizedSource := normalizeSkillSource(record.Source)
		var skill *models.Skill
		var version *models.SkillVersion
		var blob *models.SkillBlob

		if normalizedSource == "injected_by_clawmanager" {
			if skillID, err := parseExternalSkillID(record.SkillID); err == nil {
				item, err := s.repo.GetSkillByID(skillID)
				if err != nil {
					return err
				}
				if item != nil && item.UserID == ownerUserID {
					skill = item
				}
			}
		}

		skillKey := skillKeyFromRelativePath(record.Identifier)
		if skillKey == "" {
			skillKey = sanitizeSkillKey(record.Identifier)
		}
		if skillKey == "" {
			skillKey = hash[:skillMin(16, len(hash))]
		}
		if skill == nil {
			item, err := s.repo.GetSkillByUserKey(ownerUserID, skillKey)
			if err != nil {
				return err
			}
			if item != nil && (normalizedSource != "discovered_in_instance" ||
				strings.EqualFold(item.SourceType, skillSourceDiscovered) ||
				strings.EqualFold(item.SourceType, skillSourceUploaded)) {
				skill = item
			}
		}

		if skill != nil && liteInventoryUsesWorkspaceHash(instance) {
			reconciledBlob, reconciledVersion, reconcileErr := s.reconcileLiteDiscoveredBlob(skill, hash)
			if reconcileErr != nil {
				return reconcileErr
			}
			if reconciledBlob != nil {
				blob = reconciledBlob
				version = reconciledVersion
			}
		}

		if blob == nil {
			var err error
			blob, err = s.repo.GetBlobByContentHash(hash)
			if err != nil {
				return err
			}
			if blob == nil && skill != nil {
				version, blob, err = s.findVersionByContentMD5(skill.ID, hash)
				if err != nil {
					return err
				}
			}
			if blob == nil {
				blob = &models.SkillBlob{
					ContentHash: hash,
					ArchiveHash: hash,
					ObjectKey:   "",
					FileName:    sanitizeSkillKey(record.Identifier) + ".zip",
					MediaType:   "application/zip",
					SizeBytes:   0,
					ScanStatus:  "pending",
					RiskLevel:   skillRiskUnknown,
				}
				if err := s.repo.CreateBlob(blob); err != nil {
					return err
				}
			}
		}
		if strings.TrimSpace(blob.ObjectKey) == "" {
			if !isLiteRuntimeInstance(instance) {
				_ = s.enqueueCollectSkillPackage(instanceID, map[string]interface{}{
					"skill_id":      record.SkillID,
					"skill_version": record.SkillVersion,
					"identifier":    record.Identifier,
					"content_md5":   hash,
					"source":        normalizedSource,
				}, fmt.Sprintf("collect-skill-package-%d-%s", instanceID, hash))
			}
		}
		if skill == nil {
			if normalizedSource == "discovered_in_instance" {
				skillKey = s.nextDiscoveredSkillKey(ownerUserID, skillKey, hash)
			}
			skill = &models.Skill{
				UserID: ownerUserID, SkillKey: skillKey, Name: strings.TrimSpace(record.Identifier),
				SourceType: skillSourceDiscovered, Status: "active", Visibility: skillVisibilityPrivate, RiskLevel: blob.RiskLevel,
				LastScannedAt: blob.LastScannedAt, LastScanResultID: blob.LastScanResultID,
			}
			if skill.Name == "" {
				skill.Name = skillKey
			}
			if err := s.repo.CreateSkill(skill); err != nil {
				return err
			}
		}
		if version == nil {
			version, err = s.repo.GetVersionBySkillAndBlob(skill.ID, blob.ID)
			if err != nil {
				return err
			}
		}
		if version == nil && !(strings.EqualFold(skill.SourceType, skillSourceUploaded) && normalizedSource == "injected_by_clawmanager") {
			latest, err := s.repo.GetLatestVersionBySkillID(skill.ID)
			if err != nil {
				return err
			}
			versionNo := 1
			if latest != nil {
				versionNo = latest.VersionNo + 1
			}
			version = &models.SkillVersion{SkillID: skill.ID, BlobID: blob.ID, VersionNo: versionNo, SourceType: skillSourceDiscovered}
			if err := s.repo.CreateVersion(version); err != nil {
				return err
			}
		}
		if version != nil && !strings.EqualFold(skill.SourceType, skillSourceUploaded) {
			skill.CurrentVersionID = &version.ID
			skill.RiskLevel = blob.RiskLevel
			skill.LastScannedAt = blob.LastScannedAt
			skill.LastScanResultID = blob.LastScanResultID
			if err := s.repo.UpdateSkill(skill); err != nil {
				return err
			}
		}
		existingInstanceSkill, err := s.repo.GetInstanceSkill(instanceID, skill.ID)
		if err != nil {
			return err
		}
		if isUserRemovedInstanceSkill(existingInstanceSkill) {
			continue
		}

		active = append(active, skill.ID)
		workspaceDir := sanitizeWorkspaceRelativePath(strings.TrimSpace(record.Identifier))
		resolvedSource := resolveInstanceSkillSourceType(existingInstanceSkill, normalizedSource, skill)
		instanceSkill := &models.InstanceSkill{
			InstanceID: instanceID, SkillID: skill.ID, SkillVersionID: optionalVersionID(version), SourceType: resolvedSource,
			InstallPath: optionalString(strings.TrimSpace(record.InstallPath)), ObservedHash: optionalString(hash),
			Status: "active", LastSeenAt: &reportedAt, UpdatedAt: reportedAt, RemovedAt: nil,
		}
		if workspaceDir != "" {
			instanceSkill.WorkspaceDir = optionalString(workspaceDir)
		}
		if err := s.repo.UpsertInstanceSkill(instanceSkill); err != nil {
			return err
		}
		if liteInventoryUsesWorkspaceHash(instance) && strings.TrimSpace(blob.ObjectKey) == "" && workspaceDir != "" && s.materializeService != nil {
			if refreshedBlob, blobErr := s.repo.GetBlobByContentHash(hash); blobErr == nil && refreshedBlob != nil {
				blob = refreshedBlob
			}
			_, _ = s.materializeService.Enqueue(context.Background(), EnqueueMaterializeRequest{
				InstanceID:     instanceID,
				SkillID:        skill.ID,
				BlobID:         blob.ID,
				WorkspaceDir:   workspaceDir,
				ContentHash:    hash,
				TriggerSource:  MaterializeTriggerSync,
				IdempotencyKey: fmt.Sprintf("materialize-%d-%s", instanceID, hash),
			})
		}
	}
	if strings.EqualFold(strings.TrimSpace(req.Mode), "full") {
		if err := s.repo.MarkMissingInstanceSkills(instanceID, active, reportedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *skillService) UploadAgentSkillPackage(ctx context.Context, instanceID int, req AgentSkillPackageUploadRequest, fileHeader *multipart.FileHeader) (*SkillPayload, error) {
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileHeader.Filename)), ".zip") {
		return nil, fmt.Errorf("only .zip skill archives are supported")
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded skill package: %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read uploaded skill package: %w", err)
	}
	directories, err := extractSkillDirectories(fileHeader.Filename, raw)
	if err != nil {
		return nil, err
	}
	if len(directories) != 1 {
		return nil, fmt.Errorf("agent skill package must contain exactly one skill directory")
	}
	dir := directories[0]
	contentMD5 := hashDirectory(dir.Files)
	expectedMD5 := strings.TrimSpace(req.ContentMD5)
	if expectedMD5 != "" && !strings.EqualFold(contentMD5, expectedMD5) {
		return nil, utils.NewHubError(
			"skill_package_md5_mismatch",
			fmt.Sprintf("skill package md5 mismatch: expected %s got %s", expectedMD5, contentMD5),
			map[string]string{"expected": expectedMD5, "computed": contentMD5},
		)
	}

	blob, err := s.persistDiscoveredSkillPackage(ctx, instanceID, dir, contentMD5, nil)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		return nil, fmt.Errorf("skill package scan failed")
	}

	normalizedSource := normalizeSkillSource(req.Source)
	skillKey := sanitizeSkillKey(req.Identifier)
	if skillKey == "" {
		skillKey = sanitizeSkillKey(dir.Name)
	}
	if skillKey == "" {
		skillKey = contentMD5[:skillMin(16, len(contentMD5))]
	}

	var skill *models.Skill
	if skillID, err := parseExternalSkillID(req.SkillID); err == nil {
		item, err := s.repo.GetSkillByID(skillID)
		if err != nil {
			return nil, err
		}
		if item != nil && item.UserID == instance.UserID {
			skill = item
		}
	}
	if skill == nil {
		item, err := s.repo.GetSkillByUserKey(instance.UserID, skillKey)
		if err != nil {
			return nil, err
		}
		if item != nil {
			skill = item
		}
	}
	if skill == nil {
		skill = &models.Skill{
			UserID: instance.UserID, SkillKey: skillKey, Name: strings.TrimSpace(req.Identifier),
			SourceType: skillSourceDiscovered, Status: "active", Visibility: skillVisibilityPrivate, RiskLevel: blob.RiskLevel,
			LastScannedAt: blob.LastScannedAt, LastScanResultID: blob.LastScanResultID,
		}
		if strings.TrimSpace(skill.Name) == "" {
			skill.Name = dir.Name
		}
		if err := s.repo.CreateSkill(skill); err != nil {
			return nil, err
		}
	}

	version, err := s.repo.GetVersionBySkillAndBlob(skill.ID, blob.ID)
	if err != nil {
		return nil, err
	}
	if version == nil {
		latest, err := s.repo.GetLatestVersionBySkillID(skill.ID)
		if err != nil {
			return nil, err
		}
		versionNo := 1
		if latest != nil {
			versionNo = latest.VersionNo + 1
		}
		version = &models.SkillVersion{SkillID: skill.ID, BlobID: blob.ID, VersionNo: versionNo, SourceType: skillSourceDiscovered}
		if err := s.repo.CreateVersion(version); err != nil {
			return nil, err
		}
	}

	skill.CurrentVersionID = &version.ID
	skill.RiskLevel = blob.RiskLevel
	skill.LastScannedAt = blob.LastScannedAt
	skill.LastScanResultID = blob.LastScanResultID
	skill.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateSkill(skill); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	instanceSkill := &models.InstanceSkill{
		InstanceID:     instanceID,
		SkillID:        skill.ID,
		SkillVersionID: &version.ID,
		SourceType:     normalizedSource,
		InstallPath:    nil,
		ObservedHash:   optionalString(contentMD5),
		Status:         "active",
		LastSeenAt:     &now,
		UpdatedAt:      now,
	}
	if err := s.repo.UpsertInstanceSkill(instanceSkill); err != nil {
		return nil, err
	}
	return s.toSkillPayload(*skill)
}

func (s *skillService) ListScanResults(actorUserID int, actorRole string, skillID int) ([]SkillScanResultPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || !s.CanViewSkill(actorUserID, actorRole, skill) {
		return nil, fmt.Errorf("skill not found")
	}
	if skill.CurrentVersionID == nil {
		return nil, nil
	}
	version, err := s.repo.GetVersionByID(*skill.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListScanResultsByBlobID(version.BlobID)
	if err != nil {
		return nil, err
	}
	result := make([]SkillScanResultPayload, 0, len(items))
	for _, item := range items {
		payload := SkillScanResultPayload{
			ID: item.ID, BlobID: item.BlobID, Engine: item.Engine, RiskLevel: item.RiskLevel,
			Status: item.Status, Summary: item.Summary, ScannedAt: item.ScannedAt,
		}
		if item.FindingsJSON != nil && strings.TrimSpace(*item.FindingsJSON) != "" {
			_ = json.Unmarshal([]byte(*item.FindingsJSON), &payload.Findings)
		}
		payload.ParsedFindings = parseSkillFindings(&item)
		result = append(result, payload)
	}
	return result, nil
}

type extractedSkillDirectory struct {
	Name        string
	ArchivePath string
	Files       map[string][]byte
}

func extractSkillDirectories(filename string, raw []byte) ([]extractedSkillDirectory, error) {
	fileMap, err := extractArchiveFileMap(filename, raw)
	if err != nil {
		return nil, err
	}

	normalized := map[string][]byte{}
	for name, content := range fileMap {
		clean := normalizeArchiveEntryPath(name)
		if clean == "" || isArchiveMetadataEntry(clean) {
			continue
		}
		normalized[clean] = content
	}
	if len(normalized) == 0 {
		return nil, nil
	}

	if hasSkillManifest(normalized) {
		return []extractedSkillDirectory{{
			Name:        archiveSkillName(filename),
			ArchivePath: archiveSkillName(filename),
			Files:       normalized,
		}}, nil
	}

	grouped := map[string]map[string][]byte{}
	for clean, content := range normalized {
		parts := strings.Split(clean, "/")
		if len(parts) < 2 {
			return nil, fmt.Errorf("archive must contain SKILL.md at the root or top-level skill directories; found loose file %s", clean)
		}
		root := parts[0]
		if _, ok := grouped[root]; !ok {
			grouped[root] = map[string][]byte{}
		}
		grouped[root][strings.Join(parts[1:], "/")] = content
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]extractedSkillDirectory, 0, len(keys))
	for _, key := range keys {
		files := grouped[key]
		if hasSkillManifest(files) {
			result = append(result, extractedSkillDirectory{Name: key, ArchivePath: key, Files: files})
			continue
		}

		// Hermes stores some skills below a category directory, for example
		// software-development/systematic-debugging/SKILL.md. Treat that
		// category as archive organization rather than part of the skill itself.
		nested, ok := extractCategorySkillDirectories(files)
		if !ok {
			return nil, fmt.Errorf("skill directory %s must contain SKILL.md", key)
		}
		for index := range nested {
			nested[index].ArchivePath = path.Join(key, nested[index].ArchivePath)
		}
		result = append(result, nested...)
	}
	return ensureUniqueSkillDirectoryNames(result), nil
}

// extractCategorySkillDirectories accepts Hermes category paths above standard
// skill directories, such as mlops/evaluation/weights-and-biases/SKILL.md.
// Every file must still belong to a directory containing a SKILL.md manifest.
func extractCategorySkillDirectories(files map[string][]byte) ([]extractedSkillDirectory, bool) {
	roots := make([]string, 0)
	for name := range files {
		clean := normalizeArchiveEntryPath(name)
		if strings.EqualFold(path.Base(clean), "SKILL.md") && path.Dir(clean) != "." {
			roots = append(roots, path.Dir(clean))
		}
	}
	if len(roots) == 0 {
		return nil, false
	}
	// Match the deepest manifest root first in case a skill legitimately
	// contains another skill package as an asset.
	sort.Slice(roots, func(i, j int) bool {
		if len(roots[i]) == len(roots[j]) {
			return roots[i] < roots[j]
		}
		return len(roots[i]) > len(roots[j])
	})

	grouped := make(map[string]map[string][]byte, len(roots))
	for _, root := range roots {
		grouped[root] = map[string][]byte{}
	}
	for name, content := range files {
		clean := normalizeArchiveEntryPath(name)
		matchedRoot := ""
		for _, root := range roots {
			if clean == root || strings.HasPrefix(clean, root+"/") {
				matchedRoot = root
				break
			}
		}
		if matchedRoot == "" {
			return nil, false
		}
		relative := strings.TrimPrefix(clean, matchedRoot+"/")
		if relative == "" {
			return nil, false
		}
		grouped[matchedRoot][relative] = content
	}

	sort.Strings(roots)
	result := make([]extractedSkillDirectory, 0, len(roots))
	for _, root := range roots {
		if !hasSkillManifest(grouped[root]) {
			return nil, false
		}
		result = append(result, extractedSkillDirectory{Name: path.Base(root), ArchivePath: root, Files: grouped[root]})
	}
	return result, true
}

// ensureUniqueSkillDirectoryNames keeps the familiar leaf name when possible,
// but incorporates the Hermes category path when two skills share a leaf name.
func ensureUniqueSkillDirectoryNames(directories []extractedSkillDirectory) []extractedSkillDirectory {
	counts := make(map[string]int, len(directories))
	for _, directory := range directories {
		counts[directory.Name]++
	}
	used := make(map[string]int, len(directories))
	for index := range directories {
		directory := &directories[index]
		if counts[directory.Name] == 1 || directory.ArchivePath == directory.Name {
			used[directory.Name]++
			continue
		}
		base := strings.ReplaceAll(directory.ArchivePath, "/", "-")
		if base == "" {
			base = directory.Name
		}
		candidate := base
		for suffix := 2; used[candidate] > 0; suffix++ {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		directory.Name = candidate
		used[candidate]++
	}
	return directories
}

func normalizeArchiveEntryPath(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = path.Clean(strings.TrimPrefix(strings.TrimSpace(value), "./"))
	if value == "." || value == "" || strings.HasPrefix(value, "..") {
		return ""
	}
	return value
}

func hasSkillManifest(files map[string][]byte) bool {
	for key := range files {
		if strings.EqualFold(normalizeArchiveEntryPath(key), "SKILL.md") {
			return true
		}
	}
	return false
}

func isArchiveMetadataEntry(value string) bool {
	clean := normalizeArchiveEntryPath(value)
	if clean == "" {
		return true
	}
	parts := strings.Split(clean, "/")
	if parts[0] == "__MACOSX" {
		return true
	}
	base := parts[len(parts)-1]
	return base == ".DS_Store" || base == "Thumbs.db" || strings.HasPrefix(base, "._")
}

func archiveSkillName(filename string) string {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/"))
	if ext := path.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	if sanitizeSkillKey(name) == "" {
		return "skill"
	}
	return name
}

func extractArchiveFileMap(filename string, raw []byte) (map[string][]byte, error) {
	lower := strings.ToLower(strings.TrimSpace(filename))
	fileMap := map[string][]byte{}
	switch {
	case strings.HasSuffix(lower, ".zip"):
		reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, fmt.Errorf("failed to read zip archive: %w", err)
		}
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			rc, err := entry.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open zip entry: %w", err)
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read zip entry: %w", err)
			}
			fileMap[entry.Name] = content
		}
	default:
		return nil, fmt.Errorf("only .zip skill archives are supported")
	}
	return fileMap, nil
}

func (s *skillService) ensureBlobObject(ctx context.Context, blob *models.SkillBlob, archiveBytes []byte) error {
	if strings.TrimSpace(blob.ObjectKey) == "" {
		return fmt.Errorf("skill blob has no object key")
	}
	if _, err := s.storage.GetObject(ctx, blob.ObjectKey); err == nil {
		return nil
	}
	mediaType := strings.TrimSpace(blob.MediaType)
	if mediaType == "" {
		mediaType = "application/zip"
	}
	if err := s.storage.PutObject(ctx, blob.ObjectKey, archiveBytes, mediaType); err != nil {
		return fmt.Errorf("failed to restore skill blob object: %w", err)
	}
	blob.SizeBytes = int64(len(archiveBytes))
	blob.MediaType = mediaType
	blob.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateBlob(blob)
}

func (s *skillService) enqueueCollectSkillPackage(instanceID int, payload map[string]interface{}, idempotencyKey string) error {
	_, err := s.commandService.Create(instanceID, nil, CreateInstanceCommandRequest{
		CommandType:    InstanceCommandTypeCollectSkillPackage,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		TimeoutSeconds: 600,
	})
	return err
}

func (s *skillService) requestSkillPackageCollection(instanceID int, skill *models.Skill, instanceSkill *models.InstanceSkill, idempotencySuffix string) error {
	blob, err := s.skillBlobForPublish(skill)
	if err != nil {
		return err
	}
	if strings.TrimSpace(blob.ObjectKey) != "" {
		return nil
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance != nil && isLiteRuntimeInstance(instance) {
		if s.materializeService == nil {
			return fmt.Errorf("skill_package_pending")
		}
		workspaceDir := resolveLiteWorkspaceDir(instanceSkill, skill)
		if workspaceDir == "" {
			return fmt.Errorf("skill_package_pending")
		}
		trigger := MaterializeTriggerRetry
		if strings.HasPrefix(strings.TrimSpace(idempotencySuffix), "import-") {
			trigger = MaterializeTriggerImport
		} else if strings.HasPrefix(strings.TrimSpace(idempotencySuffix), "publish-") {
			trigger = MaterializeTriggerPublish
		}
		if strings.TrimSpace(idempotencySuffix) == "" {
			idempotencySuffix = fmt.Sprintf("%d-%d", instanceID, skill.ID)
		}
		job, err := s.materializeService.Enqueue(context.Background(), EnqueueMaterializeRequest{
			InstanceID:     instanceID,
			SkillID:        skill.ID,
			BlobID:         blob.ID,
			WorkspaceDir:   workspaceDir,
			ContentHash:    s.resolveContentMD5(blob),
			TriggerSource:  trigger,
			IdempotencyKey: fmt.Sprintf("materialize-%d-%s", instanceID, s.resolveContentMD5(blob)),
		})
		if err != nil {
			return err
		}
		if trigger != MaterializeTriggerSync && job != nil {
			_ = s.materializeService.ProcessJob(context.Background(), job.ID)
			latest, latestErr := s.materializeService.FindLatestBySkillID(skill.ID)
			if latestErr == nil && latest != nil && strings.EqualFold(strings.TrimSpace(latest.Status), MaterializeJobStatusFailed) {
				return fmt.Errorf("skill_package_materialize_failed")
			}
		}
		refreshedBlob, blobErr := s.repo.GetBlobByID(blob.ID)
		if blobErr == nil && refreshedBlob != nil && strings.TrimSpace(refreshedBlob.ObjectKey) != "" {
			return nil
		}
		return fmt.Errorf("skill_package_pending")
	}
	payload := map[string]interface{}{
		"skill_id":    formatExternalSkillID(skill.ID),
		"identifier":  skill.SkillKey,
		"content_md5": s.resolveContentMD5(blob),
		"source":      instanceSkill.SourceType,
	}
	if skill.CurrentVersionID != nil {
		payload["skill_version"] = formatExternalVersionID(*skill.CurrentVersionID)
	}
	if strings.TrimSpace(idempotencySuffix) == "" {
		idempotencySuffix = fmt.Sprintf("%d-%d", instanceID, skill.ID)
	}
	_ = s.enqueueCollectSkillPackage(instanceID, payload, fmt.Sprintf("collect-skill-package-%s", idempotencySuffix))
	return fmt.Errorf("skill_package_pending")
}

func (s *skillService) recordScanFromStoredBlob(blob *models.SkillBlob) error {
	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to read stored skill package: %w", err)
	}
	directories, err := extractSkillDirectories(blob.FileName, content)
	if err != nil {
		return err
	}
	if len(directories) == 0 {
		return fmt.Errorf("no skill directories found in stored package")
	}
	return s.recordScan(blob, &directories[0])
}

func (s *skillService) promoteSkillToUploadedLibrary(skill *models.Skill) error {
	if skill.CurrentVersionID == nil {
		return fmt.Errorf("skill has no version")
	}
	version, err := s.repo.GetVersionByID(*skill.CurrentVersionID)
	if err != nil {
		return err
	}
	if version == nil {
		return fmt.Errorf("skill has no version")
	}
	now := time.Now().UTC()
	skill.SourceType = skillSourceUploaded
	if strings.TrimSpace(skill.Visibility) == "" {
		skill.Visibility = skillVisibilityPrivate
	}
	skill.UpdatedAt = now
	version.SourceType = skillSourceUploaded
	version.UpdatedAt = now
	if err := s.repo.UpdateSkill(skill); err != nil {
		return err
	}
	return s.repo.UpdateVersion(version)
}

func (s *skillService) recordScan(blob *models.SkillBlob, dir *extractedSkillDirectory) error {
	if s.scanner == nil {
		return fmt.Errorf("skill scanner is not configured")
	}
	if dir == nil {
		return fmt.Errorf("skill scanner requires real skill package content")
	}
	archiveBytes, _, err := buildNormalizedZip(*dir)
	if err != nil {
		return fmt.Errorf("failed to prepare skill archive for scanning: %w", err)
	}
	riskLevel, findings, summary, err := s.scanner.ScanArchive(context.Background(), blob.FileName, archiveBytes, nil)
	if err != nil {
		return fmt.Errorf("skill scanner failed: %w", err)
	}
	if strings.TrimSpace(summary) == "" {
		summary = "Skill scanned by external skill-scanner service"
	}
	scannedAt := time.Now().UTC()
	findingsJSON, _ := json.Marshal(findings)
	result := &models.SkillScanResult{
		BlobID: blob.ID, Engine: "skill-scanner", RiskLevel: riskLevel, Status: "completed",
		Summary: &summary, FindingsJSON: optionalString(string(findingsJSON)), ScannedAt: &scannedAt,
	}
	if err := s.repo.CreateScanResult(result); err != nil {
		return err
	}
	blob.ScanStatus = "completed"
	blob.RiskLevel = riskLevel
	blob.LastScannedAt = &scannedAt
	blob.LastScanResultID = &result.ID
	if err := s.repo.UpdateBlob(blob); err != nil {
		return err
	}
	return nil
}

func buildNormalizedZip(dir extractedSkillDirectory) ([]byte, string, error) {
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	keys := make([]string, 0, len(dir.Files))
	for key := range dir.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		content := dir.Files[key]
		writer, err := zipWriter.Create(path.Join(dir.Name, key))
		if err != nil {
			return nil, "", fmt.Errorf("failed to create normalized zip entry: %w", err)
		}
		if _, err := writer.Write(content); err != nil {
			return nil, "", fmt.Errorf("failed to write normalized zip content: %w", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize zip archive: %w", err)
	}
	hash := sha256.Sum256(buffer.Bytes())
	return buffer.Bytes(), hex.EncodeToString(hash[:]), nil
}

func hashDirectory(files map[string][]byte) string {
	digest := md5.New()
	entryKinds := map[string]string{}
	fileMap := map[string][]byte{}
	for key, body := range files {
		clean := normalizeSkillRelPath(key)
		if clean == "" || hasHiddenPathSegment(clean) {
			continue
		}
		fileMap[clean] = body
		entryKinds[clean] = "file"
		for _, dir := range parentDirs(clean) {
			if dir == "" || hasHiddenPathSegment(dir) {
				continue
			}
			entryKinds[dir] = "dir"
		}
	}
	entryKeys := make([]string, 0, len(entryKinds))
	for key := range entryKinds {
		entryKeys = append(entryKeys, key)
	}
	sort.Strings(entryKeys)
	for _, key := range entryKeys {
		_, _ = digest.Write([]byte(key))
		_, _ = digest.Write([]byte("\n"))
		if entryKinds[key] == "dir" {
			_, _ = digest.Write([]byte("dir\n"))
			continue
		}
		_, _ = digest.Write([]byte("file\n"))
		_, _ = digest.Write(fileMap[key])
		_, _ = digest.Write([]byte("\n"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (s *skillService) resolveContentMD5(blob *models.SkillBlob) string {
	if blob == nil {
		return ""
	}
	contentHash := strings.TrimSpace(blob.ContentHash)
	if len(contentHash) == 32 {
		return contentHash
	}
	if s.storage == nil || strings.TrimSpace(blob.ObjectKey) == "" {
		return contentHash
	}
	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		return contentHash
	}
	files, err := extractArchiveFileMap(blob.FileName, content)
	if err != nil {
		sum := md5.Sum(content)
		return hex.EncodeToString(sum[:])
	}
	return hashDirectory(flattenSingleTopLevelDir(files))
}

func normalizeSkillRelPath(value string) string {
	value = path.Clean(strings.TrimPrefix(strings.TrimSpace(value), "./"))
	if value == "." || value == "" || strings.HasPrefix(value, "..") {
		return ""
	}
	return value
}

func hasHiddenPathSegment(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func parentDirs(value string) []string {
	parts := strings.Split(value, "/")
	if len(parts) <= 1 {
		return nil
	}
	dirs := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/")
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func flattenSingleTopLevelDir(files map[string][]byte) map[string][]byte {
	normalized := map[string][]byte{}
	topLevel := map[string]struct{}{}
	for key, body := range files {
		clean := normalizeSkillRelPath(key)
		if clean == "" || hasHiddenPathSegment(clean) {
			continue
		}
		normalized[clean] = body
		part := clean
		if slash := strings.IndexByte(clean, '/'); slash >= 0 {
			part = clean[:slash]
		}
		topLevel[part] = struct{}{}
	}
	if len(topLevel) != 1 {
		return normalized
	}
	var root string
	for key := range topLevel {
		root = key
	}
	prefix := root + "/"
	flattened := map[string][]byte{}
	for key, body := range normalized {
		if strings.HasPrefix(key, prefix) {
			flattened[strings.TrimPrefix(key, prefix)] = body
			continue
		}
		flattened[key] = body
	}
	return flattened
}

func sanitizeSkillKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '.':
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return result
}

func (s *skillService) toSkillPayloads(items []models.Skill, actorUserID int, actorRole string) ([]SkillPayload, error) {
	result := make([]SkillPayload, 0, len(items))
	for _, item := range items {
		payload, err := s.toSkillPayload(item)
		if err != nil {
			return nil, err
		}
		if actorUserID > 0 || isAdminRole(actorRole) {
			if err := s.enrichSkillPayload(payload, item, nil); err != nil {
				return nil, err
			}
		}
		result = append(result, *payload)
	}
	return result, nil
}

func (s *skillService) toSkillPayload(item models.Skill) (*SkillPayload, error) {
	payload := &SkillPayload{
		ID: item.ID, ExternalSkillID: formatExternalSkillID(item.ID), UserID: item.UserID, SkillKey: item.SkillKey, Name: item.Name, Description: item.Description,
		Status: item.Status, SourceType: item.SourceType, RiskLevel: item.RiskLevel, ScanStatus: "pending",
		Visibility: skillVisibilityPrivate,
		LastScannedAt: item.LastScannedAt, CurrentVersionID: item.CurrentVersionID, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if strings.TrimSpace(item.Visibility) != "" {
		payload.Visibility = item.Visibility
	}
	if item.CurrentVersionID != nil {
		version, err := s.repo.GetVersionByID(*item.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		if version != nil {
			payload.CurrentVersionNo = &version.VersionNo
			blob, err := s.repo.GetBlobByID(version.BlobID)
			if err != nil {
				return nil, err
			}
			if blob != nil {
				contentMD5 := s.resolveContentMD5(blob)
				payload.ContentHash = &blob.ContentHash
				payload.ContentMD5 = &contentMD5
				payload.ArchiveHash = &blob.ArchiveHash
				payload.ScanStatus = blob.ScanStatus
				payload.LastScannedAt = blob.LastScannedAt
			}
		}
	}
	if item.LastScanResultID != nil {
		scanResult, err := s.repo.GetScanResultByID(*item.LastScanResultID)
		if err != nil {
			return nil, err
		}
		findings := parseSkillFindings(scanResult)
		payload.TopFindings = topRiskFindings(findings, 3)
		payload.RiskReason = summarizeRiskReason(payload.TopFindings)
	}
	instanceSkills, err := s.findInstanceRefs(item.ID)
	if err != nil {
		return nil, err
	}
	payload.InstanceCount = instanceSkills
	return payload, nil
}

func parseSkillFindings(result *models.SkillScanResult) []SkillFindingPayload {
	if result == nil || result.FindingsJSON == nil || strings.TrimSpace(*result.FindingsJSON) == "" {
		return []SkillFindingPayload{}
	}
	var raw struct {
		Findings []struct {
			Analyzer    string  `json:"analyzer"`
			Severity    string  `json:"severity"`
			Category    string  `json:"category"`
			RuleID      string  `json:"rule_id"`
			Title       string  `json:"title"`
			Description string  `json:"description"`
			FilePath    *string `json:"file_path"`
			LineNumber  *int    `json:"line_number"`
			Remediation string  `json:"remediation"`
			Snippet     *string `json:"snippet"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(*result.FindingsJSON), &raw); err != nil {
		return []SkillFindingPayload{}
	}
	items := make([]SkillFindingPayload, 0, len(raw.Findings))
	for _, item := range raw.Findings {
		items = append(items, SkillFindingPayload{
			Analyzer:    item.Analyzer,
			Severity:    item.Severity,
			Category:    item.Category,
			RuleID:      item.RuleID,
			Title:       item.Title,
			Description: item.Description,
			FilePath:    item.FilePath,
			LineNumber:  item.LineNumber,
			Remediation: item.Remediation,
			Snippet:     item.Snippet,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return severityRank(items[i].Severity) > severityRank(items[j].Severity)
	})
	return items
}

func topRiskFindings(items []SkillFindingPayload, limit int) []SkillFindingPayload {
	if limit <= 0 || len(items) == 0 {
		return []SkillFindingPayload{}
	}
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func summarizeRiskReason(items []SkillFindingPayload) *string {
	if len(items) == 0 {
		return nil
	}
	first := items[0]
	summary := strings.TrimSpace(first.Title)
	if summary == "" {
		summary = strings.TrimSpace(first.Description)
	}
	if summary == "" {
		return nil
	}
	if first.FilePath != nil && strings.TrimSpace(*first.FilePath) != "" {
		summary = fmt.Sprintf("%s (%s)", summary, strings.TrimSpace(*first.FilePath))
	}
	return &summary
}

func severityRank(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CRITICAL":
		return 5
	case "HIGH":
		return 4
	case "MEDIUM", "MODERATE":
		return 3
	case "LOW", "WARNING":
		return 2
	case "INFO", "SAFE", "NONE":
		return 1
	default:
		return 0
	}
}

func (s *skillService) findInstanceRefs(skillID int) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	items, err := s.repo.ListActiveInstanceSkillsBySkillID(skillID)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func normalizeSkillSource(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "discovered_in_instance"
	}
	return value
}

func canActorAttachSkillToInstance(instance *models.Instance, skill models.Skill, userID int, userRole string) bool {
	if instance == nil {
		return false
	}
	if strings.EqualFold(userRole, "admin") {
		return true
	}
	return instance.UserID == userID
}

func isUserManagedSkill(skill models.Skill) bool {
	return strings.EqualFold(strings.TrimSpace(skill.SourceType), skillSourceUploaded)
}

func isDeletedSkill(skill *models.Skill) bool {
	if skill == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(skill.Status), skillStatusDeleted)
}

func deletedSkillKey(skillKey string, skillID int) string {
	const maxSkillKeyLength = 120
	suffix := fmt.Sprintf("__deleted_%d", skillID)
	trimmed := strings.TrimSpace(skillKey)
	if trimmed == "" {
		trimmed = "skill"
	}
	if strings.HasSuffix(trimmed, suffix) {
		return trimmed
	}
	if len(trimmed)+len(suffix) <= maxSkillKeyLength {
		return trimmed + suffix
	}
	runes := []rune(trimmed)
	maxPrefixLength := maxSkillKeyLength - len(suffix)
	if len(runes) > maxPrefixLength {
		runes = runes[:maxPrefixLength]
	}
	return string(runes) + suffix
}

func optionalVersionID(version *models.SkillVersion) *int {
	if version == nil {
		return nil
	}
	return &version.ID
}

func (s *skillService) findVersionByContentMD5(skillID int, contentMD5 string) (*models.SkillVersion, *models.SkillBlob, error) {
	versions, err := s.repo.ListVersionsBySkillID(skillID)
	if err != nil {
		return nil, nil, err
	}
	for _, candidate := range versions {
		blob, err := s.repo.GetBlobByID(candidate.BlobID)
		if err != nil {
			return nil, nil, err
		}
		if blob != nil && s.resolveContentMD5(blob) == contentMD5 {
			return &candidate, blob, nil
		}
	}
	return nil, nil, nil
}

func (s *skillService) nextDiscoveredSkillKey(userID int, baseKey, hash string) string {
	candidate := baseKey
	if candidate == "" {
		candidate = "discovered-skill"
	}
	existing, err := s.repo.GetSkillByUserKey(userID, candidate)
	if err == nil && existing == nil {
		return candidate
	}
	suffix := hash
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	candidate = fmt.Sprintf("%s-%s", candidate, suffix)
	existing, err = s.repo.GetSkillByUserKey(userID, candidate)
	if err == nil && existing == nil {
		return candidate
	}
	return fmt.Sprintf("%s-%d", candidate, time.Now().UTC().Unix())
}

func formatExternalSkillID(id int) string {
	return fmt.Sprintf("skill_%d", id)
}

func formatExternalVersionID(id int) string {
	return fmt.Sprintf("ver_%d", id)
}

func parseExternalVersionID(value string) (int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "ver_"))
	if value == "" {
		return 0, fmt.Errorf("invalid skill version")
	}
	var id int
	if _, err := fmt.Sscanf(value, "%d", &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid skill version")
	}
	return id, nil
}

func parseExternalSkillID(value string) (int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "skill_"))
	if value == "" {
		return 0, fmt.Errorf("invalid skill id")
	}
	var id int
	if _, err := fmt.Sscanf(value, "%d", &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid skill id")
	}
	return id, nil
}

func skillKeyForRemoval(item *models.InstanceSkill) string {
	if item == nil {
		return ""
	}
	if item.InstallPath != nil && strings.TrimSpace(*item.InstallPath) != "" {
		parts := strings.Split(strings.TrimSpace(*item.InstallPath), "/")
		return parts[len(parts)-1]
	}
	return fmt.Sprintf("skill-%d", item.SkillID)
}

func skillMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
