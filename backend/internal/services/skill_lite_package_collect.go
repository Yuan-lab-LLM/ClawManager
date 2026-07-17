package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
)

func loadLiteSkillDirectoryFromWorkspace(instance *models.Instance, workspaceDir string) (extractedSkillDirectory, string, error) {
	workspaceDir = sanitizeWorkspaceRelativePath(strings.TrimSpace(workspaceDir))
	if workspaceDir == "" {
		return extractedSkillDirectory{}, "", fmt.Errorf("workspace skill directory is required")
	}
	root := runtimeSkillInstallRoot(instance)
	if root == "" {
		return extractedSkillDirectory{}, "", fmt.Errorf("runtime skill workspace root is not configured")
	}
	skillRoot, err := joinRuntimeSkillPath(root, workspaceDir)
	if err != nil {
		return extractedSkillDirectory{}, "", fmt.Errorf("workspace skill directory is invalid: %s", workspaceDir)
	}
	files, err := collectLiteSkillDirectoryFiles(skillRoot)
	if err != nil {
		return extractedSkillDirectory{}, "", err
	}
	if len(files) == 0 {
		return extractedSkillDirectory{}, "", fmt.Errorf("lite skill directory not found: %s", workspaceDir)
	}
	dir := extractedSkillDirectory{Name: workspaceDir, Files: files}
	return dir, hashDirectory(files), nil
}

func resolveLiteWorkspaceDir(instanceSkill *models.InstanceSkill, skill *models.Skill) string {
	if instanceSkill != nil && instanceSkill.WorkspaceDir != nil && strings.TrimSpace(*instanceSkill.WorkspaceDir) != "" {
		return sanitizeWorkspaceRelativePath(strings.TrimSpace(*instanceSkill.WorkspaceDir))
	}
	if instanceSkill != nil {
		if key := skillKeyForRemoval(instanceSkill); key != "" && !strings.HasPrefix(key, "skill-") {
			return sanitizeWorkspaceRelativePath(key)
		}
	}
	if skill != nil && strings.TrimSpace(skill.Name) != "" {
		return sanitizeWorkspaceRelativePath(strings.TrimSpace(skill.Name))
	}
	if skill != nil {
		return sanitizeWorkspaceRelativePath(strings.TrimSpace(skill.SkillKey))
	}
	return ""
}

func (s *skillService) persistDiscoveredSkillPackage(ctx context.Context, instanceID int, dir extractedSkillDirectory, contentMD5 string, existingBlob *models.SkillBlob) (*models.SkillBlob, error) {
	if s == nil || s.storage == nil {
		return nil, fmt.Errorf("object storage is not configured")
	}
	contentMD5 = strings.TrimSpace(contentMD5)
	if contentMD5 == "" {
		return nil, fmt.Errorf("content hash is required")
	}

	archiveBytes, archiveHash, err := buildNormalizedZip(dir)
	if err != nil {
		return nil, err
	}

	blob := existingBlob
	if blob == nil {
		blob, err = s.repo.GetBlobByContentHash(contentMD5)
		if err != nil {
			return nil, err
		}
	}
	if blob == nil {
		blob = &models.SkillBlob{
			ContentHash: contentMD5,
			ArchiveHash: archiveHash,
			ObjectKey:   fmt.Sprintf("discovered/%d/%s/%s.zip", instanceID, sanitizeSkillKey(dir.Name), contentMD5),
			FileName:    fmt.Sprintf("%s.zip", sanitizeSkillKey(dir.Name)),
			MediaType:   "application/zip",
			SizeBytes:   int64(len(archiveBytes)),
			ScanStatus:  "pending",
			RiskLevel:   skillRiskUnknown,
		}
		if err := s.storage.PutObject(ctx, blob.ObjectKey, archiveBytes, blob.MediaType); err != nil {
			return nil, err
		}
		if err := s.repo.CreateBlob(blob); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(blob.ObjectKey) == "" {
		blob.ObjectKey = fmt.Sprintf("discovered/%d/%s/%s.zip", instanceID, sanitizeSkillKey(dir.Name), contentMD5)
		blob.FileName = fmt.Sprintf("%s.zip", sanitizeSkillKey(dir.Name))
		blob.MediaType = "application/zip"
		blob.SizeBytes = int64(len(archiveBytes))
		blob.ArchiveHash = archiveHash
		if err := s.storage.PutObject(ctx, blob.ObjectKey, archiveBytes, blob.MediaType); err != nil {
			return nil, err
		}
		if err := s.repo.UpdateBlob(blob); err != nil {
			return nil, err
		}
	}

	if blob.LastScanResultID == nil || !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		if err := s.recordScan(blob, &dir); err != nil {
			blob.ScanStatus = "failed"
			blob.UpdatedAt = timeNowUTC()
			_ = s.repo.UpdateBlob(blob)
		}
	}
	updated, err := s.repo.GetBlobByID(blob.ID)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		blob = updated
	}
	return blob, nil
}

func (s *skillService) materializeSkillPackageFromWorkspace(ctx context.Context, instanceID int, workspaceDir, expectedMD5 string, targetBlobID int) (*models.SkillBlob, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if !isLiteRuntimeInstance(instance) && !SupportsServerWorkspaceSkillScan(instance) {
		return nil, fmt.Errorf("instance does not support workspace skill materialization")
	}

	dir, contentMD5, err := loadLiteSkillDirectoryFromWorkspace(instance, workspaceDir)
	if err != nil {
		return nil, err
	}

	var existingBlob *models.SkillBlob
	if targetBlobID > 0 {
		existingBlob, err = s.repo.GetBlobByID(targetBlobID)
		if err != nil {
			return nil, err
		}
	}
	if existingBlob == nil {
		existingBlob, err = s.repo.GetBlobByContentHash(contentMD5)
		if err != nil {
			return nil, err
		}
	}
	if existingBlob != nil && !strings.EqualFold(strings.TrimSpace(existingBlob.ContentHash), contentMD5) {
		existingBlob.ContentHash = contentMD5
		existingBlob.ArchiveHash = contentMD5
	}
	if existingBlob != nil && strings.TrimSpace(existingBlob.ObjectKey) != "" && strings.EqualFold(strings.TrimSpace(existingBlob.ScanStatus), "completed") {
		return existingBlob, nil
	}
	return s.persistDiscoveredSkillPackage(ctx, instanceID, dir, contentMD5, existingBlob)
}

func (s *skillService) reconcileLiteDiscoveredBlob(skill *models.Skill, contentHash string) (*models.SkillBlob, *models.SkillVersion, error) {
	if s == nil || s.repo == nil || skill == nil || skill.CurrentVersionID == nil {
		return nil, nil, nil
	}
	contentHash = strings.TrimSpace(contentHash)
	if contentHash == "" {
		return nil, nil, nil
	}
	version, err := s.repo.GetVersionByID(*skill.CurrentVersionID)
	if err != nil {
		return nil, nil, err
	}
	if version == nil {
		return nil, nil, nil
	}
	blob, err := s.repo.GetBlobByID(version.BlobID)
	if err != nil {
		return nil, version, err
	}
	if blob == nil {
		return nil, version, nil
	}
	if !strings.EqualFold(strings.TrimSpace(blob.ContentHash), contentHash) {
		blob.ContentHash = contentHash
		blob.ArchiveHash = contentHash
		if err := s.repo.UpdateBlob(blob); err != nil {
			return nil, version, err
		}
	}
	return blob, version, nil
}

func liteInventoryUsesWorkspaceHash(instance *models.Instance) bool {
	return isLiteRuntimeInstance(instance) || SupportsServerWorkspaceSkillScan(instance)
}

func workspaceContentHashForRecord(instance *models.Instance, record AgentSkillRecord) string {
	if !runtimeInventoryUsesWorkspaceHash(instance) {
		return strings.TrimSpace(record.ContentMD5)
	}
	workspaceDir := sanitizeWorkspaceRelativePath(strings.TrimSpace(record.Identifier))
	if workspaceDir == "" {
		return strings.TrimSpace(record.ContentMD5)
	}
	_, computed, err := loadLiteSkillDirectoryFromWorkspace(instance, workspaceDir)
	if err != nil || strings.TrimSpace(computed) == "" {
		return strings.TrimSpace(record.ContentMD5)
	}
	return computed
}

func workspaceContentHashForLiteRecord(instance *models.Instance, record AgentSkillRecord) string {
	return workspaceContentHashForRecord(instance, record)
}

func runtimeInventoryUsesWorkspaceHash(instance *models.Instance) bool {
	return liteInventoryUsesWorkspaceHash(instance)
}

func (s *skillService) syncSkillRecordFromBlob(skillID int, blob *models.SkillBlob) error {
	if blob == nil {
		return nil
	}
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return err
	}
	if skill == nil {
		return nil
	}
	skill.RiskLevel = blob.RiskLevel
	skill.LastScannedAt = blob.LastScannedAt
	skill.LastScanResultID = blob.LastScanResultID
	skill.UpdatedAt = timeNowUTC()
	return s.repo.UpdateSkill(skill)
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}
