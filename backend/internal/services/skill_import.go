package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"time"

	"clawreef/internal/models"
)

const (
	skillImportConflictNone           = "none"
	skillImportConflictUnchanged      = "unchanged"
	skillImportConflictContentChanged = "content_changed"

	skillImportActionAuto       = "auto"
	skillImportActionNewVersion = "new_version"
	skillImportActionSaveAsNew  = "save_as_new"
	skillImportActionSkip       = "skip"

	skillImportResultCreated    = "created"
	skillImportResultVersioned  = "versioned"
	skillImportResultUnchanged  = "unchanged"
	skillImportResultSavedAsNew = "saved_as_new"
)

type SkillImportPreviewItem struct {
	DirectoryName     string  `json:"directory_name"`
	SkillKey          string  `json:"skill_key"`
	ContentHash       string  `json:"content_hash"`
	ConflictType      string  `json:"conflict_type"`
	ExistingSkillID   *int    `json:"existing_skill_id,omitempty"`
	ExistingName      *string `json:"existing_name,omitempty"`
	CurrentVersionNo  *int    `json:"current_version_no,omitempty"`
	SuggestedSkillKey *string `json:"suggested_skill_key,omitempty"`
}

type SkillImportDecision struct {
	DirectoryName string  `json:"directory_name"`
	Action        string  `json:"action"`
	SkillKey      *string `json:"skill_key,omitempty"`
}

type SkillImportResultItem struct {
	Skill             SkillPayload `json:"skill"`
	Action            string       `json:"action"`
	PreviousVersionNo *int         `json:"previous_version_no,omitempty"`
	DirectoryName     string       `json:"directory_name"`
}

type ImportDirectoryOptions struct {
	Action           string
	OverrideSkillKey string
}

func (s *skillService) PreviewHubImport(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillImportPreviewItem, error) {
	_ = ctx
	directories, _, err := readSkillArchiveDirectories(fileHeader)
	if err != nil {
		return nil, err
	}
	items := make([]SkillImportPreviewItem, 0, len(directories))
	for _, dir := range directories {
		item, err := s.previewImportDirectory(userID, dir)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *skillService) ImportHubArchiveWithDecisions(ctx context.Context, userID int, fileHeader *multipart.FileHeader, decisions []SkillImportDecision) ([]SkillImportResultItem, error) {
	directories, filename, err := readSkillArchiveDirectories(fileHeader)
	if err != nil {
		return nil, err
	}
	decisionMap := mapSkillImportDecisions(decisions)
	hasDecisions := len(decisions) > 0

	results := make([]SkillImportResultItem, 0, len(directories))
	for _, dir := range directories {
		preview, err := s.previewImportDirectory(userID, dir)
		if err != nil {
			return nil, err
		}
		opts := resolveImportDecision(preview, decisionMap[dir.Name], hasDecisions)

		if preview.ConflictType == skillImportConflictUnchanged || opts.Action == skillImportActionSkip {
			if preview.ConflictType == skillImportConflictUnchanged && preview.ExistingSkillID != nil {
				payload, err := s.loadSkillPayloadByID(*preview.ExistingSkillID)
				if err != nil {
					return nil, err
				}
				results = append(results, SkillImportResultItem{
					Skill:         *payload,
					Action:        skillImportResultUnchanged,
					DirectoryName: dir.Name,
				})
			}
			continue
		}

		result, err := s.importDirectoryWithOptions(ctx, userID, dir, filename, opts)
		if err != nil {
			return nil, err
		}
		result.DirectoryName = dir.Name
		results = append(results, *result)
	}
	return results, nil
}

func readSkillArchiveDirectories(fileHeader *multipart.FileHeader) ([]extractedSkillDirectory, string, error) {
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileHeader.Filename)), ".zip") {
		return nil, "", fmt.Errorf("only .zip skill archives are supported")
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("failed to open uploaded archive: %w", err)
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read uploaded archive: %w", err)
	}
	directories, err := extractSkillDirectories(fileHeader.Filename, raw)
	if err != nil {
		return nil, "", err
	}
	if len(directories) == 0 {
		return nil, "", fmt.Errorf("no skill directories found in archive")
	}
	return directories, fileHeader.Filename, nil
}

func mapSkillImportDecisions(decisions []SkillImportDecision) map[string]SkillImportDecision {
	result := make(map[string]SkillImportDecision, len(decisions))
	for _, item := range decisions {
		key := strings.TrimSpace(item.DirectoryName)
		if key == "" {
			continue
		}
		result[key] = item
	}
	return result
}

func resolveImportDecision(preview SkillImportPreviewItem, decision SkillImportDecision, hasDecision bool) ImportDirectoryOptions {
	if preview.ConflictType == skillImportConflictUnchanged {
		return ImportDirectoryOptions{Action: skillImportActionSkip}
	}
	if !hasDecision {
		return ImportDirectoryOptions{Action: skillImportActionAuto}
	}
	if strings.TrimSpace(decision.DirectoryName) == "" {
		switch preview.ConflictType {
		case skillImportConflictNone:
			return ImportDirectoryOptions{Action: skillImportActionNewVersion}
		case skillImportConflictContentChanged:
			return ImportDirectoryOptions{Action: skillImportActionNewVersion}
		default:
			return ImportDirectoryOptions{Action: skillImportActionSkip}
		}
	}
	switch strings.TrimSpace(decision.Action) {
	case skillImportActionSaveAsNew:
		key := preview.SkillKey
		if preview.SuggestedSkillKey != nil && strings.TrimSpace(*preview.SuggestedSkillKey) != "" {
			key = *preview.SuggestedSkillKey
		}
		if decision.SkillKey != nil && strings.TrimSpace(*decision.SkillKey) != "" {
			key = strings.TrimSpace(*decision.SkillKey)
		}
		return ImportDirectoryOptions{Action: skillImportActionSaveAsNew, OverrideSkillKey: key}
	case skillImportActionSkip:
		return ImportDirectoryOptions{Action: skillImportActionSkip}
	default:
		return ImportDirectoryOptions{Action: skillImportActionNewVersion}
	}
}

func (s *skillService) previewImportDirectory(userID int, dir extractedSkillDirectory) (SkillImportPreviewItem, error) {
	skillKey := sanitizeSkillKey(dir.Name)
	if skillKey == "" {
		return SkillImportPreviewItem{}, fmt.Errorf("skill directory name %q is invalid", dir.Name)
	}
	contentHash := hashDirectory(dir.Files)
	item := SkillImportPreviewItem{
		DirectoryName: dir.Name,
		SkillKey:      skillKey,
		ContentHash:   contentHash,
		ConflictType:  skillImportConflictNone,
	}

	skill, err := s.repo.GetSkillByUserKey(userID, skillKey)
	if err != nil {
		return SkillImportPreviewItem{}, err
	}
	if skill == nil {
		return item, nil
	}

	existingName := skill.Name
	item.ExistingSkillID = &skill.ID
	item.ExistingName = &existingName

	existingHash, versionNo, err := s.currentSkillContentHash(skill)
	if err != nil {
		return SkillImportPreviewItem{}, err
	}
	if versionNo != nil {
		item.CurrentVersionNo = versionNo
	}
	if existingHash != "" && existingHash == contentHash {
		item.ConflictType = skillImportConflictUnchanged
		return item, nil
	}
	item.ConflictType = skillImportConflictContentChanged
	suggested := s.nextUploadSkillKey(userID, skillKey)
	item.SuggestedSkillKey = &suggested
	return item, nil
}

func (s *skillService) currentSkillContentHash(skill *models.Skill) (string, *int, error) {
	if skill == nil || skill.CurrentVersionID == nil {
		return "", nil, nil
	}
	version, err := s.repo.GetVersionByID(*skill.CurrentVersionID)
	if err != nil {
		return "", nil, err
	}
	if version == nil {
		return "", nil, nil
	}
	versionNo := version.VersionNo
	blob, err := s.repo.GetBlobByID(version.BlobID)
	if err != nil {
		return "", nil, err
	}
	if blob == nil {
		return "", &versionNo, nil
	}
	return blob.ContentHash, &versionNo, nil
}

func (s *skillService) loadSkillPayloadByID(skillID int) (*SkillPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill not found")
	}
	payload, err := s.toSkillPayload(*skill)
	if err != nil {
		return nil, err
	}
	if err := s.enrichSkillPayload(payload, *skill, nil); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *skillService) nextUploadSkillKey(userID int, baseKey string) string {
	candidate := strings.TrimSpace(baseKey)
	if candidate == "" {
		candidate = "skill"
	}
	for i := 2; i <= 99; i++ {
		next := fmt.Sprintf("%s-%d", candidate, i)
		existing, err := s.repo.GetSkillByUserKey(userID, next)
		if err == nil && existing == nil {
			return next
		}
	}
	return fmt.Sprintf("%s-%d", candidate, time.Now().UTC().Unix())
}

func (s *skillService) importDirectory(ctx context.Context, userID int, dir extractedSkillDirectory, originalName string) (*SkillPayload, error) {
	result, err := s.importDirectoryWithOptions(ctx, userID, dir, originalName, ImportDirectoryOptions{Action: skillImportActionAuto})
	if err != nil {
		return nil, err
	}
	return &result.Skill, nil
}

func (s *skillService) importDirectoryWithOptions(ctx context.Context, userID int, dir extractedSkillDirectory, originalName string, opts ImportDirectoryOptions) (*SkillImportResultItem, error) {
	baseSkillKey := sanitizeSkillKey(dir.Name)
	if baseSkillKey == "" {
		return nil, fmt.Errorf("skill directory name %q is invalid", dir.Name)
	}

	targetSkillKey := baseSkillKey
	isSaveAsNew := opts.Action == skillImportActionSaveAsNew
	if isSaveAsNew {
		targetSkillKey = sanitizeSkillKey(opts.OverrideSkillKey)
		if targetSkillKey == "" {
			return nil, fmt.Errorf("invalid skill key for save_as_new")
		}
	}

	contentHash := hashDirectory(dir.Files)
	archiveBytes, archiveHash, err := buildNormalizedZip(dir)
	if err != nil {
		return nil, err
	}

	blob, err := s.repo.GetBlobByContentHash(contentHash)
	if err != nil {
		return nil, err
	}
	if blob == nil {
		blob = &models.SkillBlob{
			ContentHash: contentHash, ArchiveHash: archiveHash,
			ObjectKey: fmt.Sprintf("%d/%s/%s.zip", userID, targetSkillKey, contentHash),
			FileName:  fmt.Sprintf("%s.zip", targetSkillKey),
			MediaType: "application/zip", SizeBytes: int64(len(archiveBytes)),
			ScanStatus: "pending", RiskLevel: skillRiskUnknown,
		}
		if err := s.storage.PutObject(ctx, blob.ObjectKey, archiveBytes, blob.MediaType); err != nil {
			return nil, err
		}
		if err := s.repo.CreateBlob(blob); err != nil {
			return nil, err
		}
		if err := s.recordScan(blob, &dir); err != nil {
			return nil, err
		}
	} else {
		if err := s.ensureBlobObject(ctx, blob, archiveBytes); err != nil {
			return nil, err
		}
		if blob.LastScanResultID == nil || blob.ScanStatus != "completed" {
			if err := s.recordScan(blob, &dir); err != nil {
				return nil, err
			}
		}
	}

	existingBefore, err := s.repo.GetSkillByUserKey(userID, targetSkillKey)
	if err != nil {
		return nil, err
	}
	var previousVersionNo *int
	if existingBefore != nil {
		_, versionNo, err := s.currentSkillContentHash(existingBefore)
		if err != nil {
			return nil, err
		}
		previousVersionNo = versionNo
	}

	if isSaveAsNew && existingBefore != nil {
		return nil, fmt.Errorf("skill key %q already exists", targetSkillKey)
	}

	skill := existingBefore
	created := false
	if skill == nil {
		description := fmt.Sprintf("Imported from %s", originalName)
		skill = &models.Skill{
			UserID: userID, SkillKey: targetSkillKey, Name: dir.Name, Description: &description,
			SourceType: skillSourceUploaded, Status: "active", Visibility: skillVisibilityPrivate, RiskLevel: blob.RiskLevel,
			LastScannedAt: blob.LastScannedAt, LastScanResultID: blob.LastScanResultID,
		}
		if err := s.repo.CreateSkill(skill); err != nil {
			return nil, err
		}
		created = true
	}

	version, err := s.repo.GetVersionBySkillAndBlob(skill.ID, blob.ID)
	if err != nil {
		return nil, err
	}
	versionCreated := false
	if version == nil {
		latest, err := s.repo.GetLatestVersionBySkillID(skill.ID)
		if err != nil {
			return nil, err
		}
		versionNo := 1
		if latest != nil {
			versionNo = latest.VersionNo + 1
		}
		manifest, _ := json.Marshal(map[string]interface{}{"root_dir": dir.Name, "files": len(dir.Files)})
		manifestJSON := string(manifest)
		version = &models.SkillVersion{
			SkillID: skill.ID, BlobID: blob.ID, VersionNo: versionNo, ManifestJSON: &manifestJSON, SourceType: skillSourceUploaded,
		}
		if err := s.repo.CreateVersion(version); err != nil {
			return nil, err
		}
		versionCreated = true
	}

	skill.CurrentVersionID = &version.ID
	skill.RiskLevel = blob.RiskLevel
	skill.LastScannedAt = blob.LastScannedAt
	skill.LastScanResultID = blob.LastScanResultID
	skill.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateSkill(skill); err != nil {
		return nil, err
	}

	payload, err := s.toSkillPayload(*skill)
	if err != nil {
		return nil, err
	}
	if err := s.enrichSkillPayload(payload, *skill, nil); err != nil {
		return nil, err
	}

	action := skillImportResultVersioned
	if created || isSaveAsNew {
		action = skillImportResultCreated
		if isSaveAsNew {
			action = skillImportResultSavedAsNew
		}
	} else if !versionCreated {
		action = skillImportResultUnchanged
	}

	result := &SkillImportResultItem{
		Skill:         *payload,
		Action:        action,
		DirectoryName: dir.Name,
	}
	if action == skillImportResultVersioned && previousVersionNo != nil {
		result.PreviousVersionNo = previousVersionNo
	}
	return result, nil
}
