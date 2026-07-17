package services

import (
	"context"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"clawreef/internal/models"
)

const (
	skillVisibilityPrivate = "private"
	skillVisibilityPublic  = "public"
)

type SkillHubTagPayload struct {
	ID          int     `json:"id"`
	TagKey      string  `json:"tag_key"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SortOrder   int     `json:"sort_order"`
	AdminOnly   bool    `json:"admin_only"`
}

type SkillHubCatalogQuery struct {
	TagKeys  []string
	Search   string
	Page     int
	PageSize int
}

type SkillHubCatalogResponse struct {
	Items      []SkillPayload `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

type PublishSkillHubRequest struct {
	TagIDs []int `json:"tag_ids" binding:"required,min=1"`
}

type UpdateSkillHubTagsRequest struct {
	TagIDs []int `json:"tag_ids" binding:"required,min=1"`
}

type InstallHubSkillRequest struct {
	InstanceID int `json:"instance_id" binding:"required,min=1"`
}

func isAdminRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "admin")
}

func (s *skillService) skillBlobForPublish(skill *models.Skill) (*models.SkillBlob, error) {
	if skill == nil || skill.CurrentVersionID == nil {
		return nil, fmt.Errorf("skill has no version")
	}
	version, err := s.repo.GetVersionByID(*skill.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	if version == nil {
		return nil, fmt.Errorf("skill has no version")
	}
	blob, err := s.repo.GetBlobByID(version.BlobID)
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return nil, fmt.Errorf("skill blob not found")
	}
	return blob, nil
}

func isHubPublishableBlob(blob *models.SkillBlob) bool {
	if blob == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		return false
	}
	risk := strings.ToLower(strings.TrimSpace(blob.RiskLevel))
	if risk != skillRiskNone && risk != skillRiskLow {
		return false
	}
	if strings.TrimSpace(blob.ObjectKey) == "" {
		return false
	}
	return true
}

func (s *skillService) isHubPublishable(skill *models.Skill, blob *models.SkillBlob) bool {
	if skill == nil || blob == nil || isDeletedSkill(skill) {
		return false
	}
	if !isUserManagedSkill(*skill) && !strings.EqualFold(strings.TrimSpace(skill.SourceType), skillSourceDiscovered) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(skill.Status), "active") {
		return false
	}
	return isHubPublishableBlob(blob)
}

func (s *skillService) CanDownloadSkill(actorUserID int, actorRole string, skill *models.Skill) bool {
	if skill == nil || !isUserManagedSkill(*skill) {
		return false
	}
	return s.CanViewSkill(actorUserID, actorRole, skill)
}

func (s *skillService) CanViewSkill(actorUserID int, actorRole string, skill *models.Skill) bool {
	if skill == nil || isDeletedSkill(skill) {
		return false
	}
	if isAdminRole(actorRole) {
		return isUserManagedSkill(*skill) || strings.EqualFold(skill.SourceType, skillSourceDiscovered)
	}
	if skill.UserID == actorUserID {
		return isUserManagedSkill(*skill) || strings.EqualFold(skill.SourceType, skillSourceDiscovered)
	}
	return isUserManagedSkill(*skill) && strings.EqualFold(strings.TrimSpace(skill.Visibility), skillVisibilityPublic)
}

func (s *skillService) CanAttachSkill(actorUserID int, actorRole string, skill *models.Skill, instance *models.Instance) bool {
	if skill == nil || instance == nil {
		return false
	}
	if !isUserManagedSkill(*skill) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(skill.Status), "active") {
		return false
	}
	if skill.RiskLevel == skillRiskMedium || skill.RiskLevel == skillRiskHigh {
		return false
	}
	if isAdminRole(actorRole) {
		return true
	}
	if instance.UserID != actorUserID {
		return false
	}
	if skill.UserID == actorUserID {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(skill.Visibility), skillVisibilityPublic)
}

func (s *skillService) hubTagsToPayload(tags []models.SkillHubTag) []SkillHubTagPayload {
	result := make([]SkillHubTagPayload, 0, len(tags))
	for _, tag := range tags {
		result = append(result, SkillHubTagPayload{
			ID:          tag.ID,
			TagKey:      tag.TagKey,
			Name:        tag.Name,
			Description: tag.Description,
			SortOrder:   tag.SortOrder,
			AdminOnly:   tag.AdminOnly,
		})
	}
	return result
}

func (s *skillService) liteInstanceForSkill(skillID int) *models.Instance {
	if s == nil || s.repo == nil || s.instanceRepo == nil || skillID <= 0 {
		return nil
	}
	items, err := s.repo.ListActiveInstanceSkillsBySkillID(skillID)
	if err != nil || len(items) == 0 {
		return nil
	}
	for _, item := range items {
		instance, err := s.instanceRepo.GetByID(item.InstanceID)
		if err != nil || instance == nil {
			continue
		}
		if isLiteRuntimeInstance(instance) {
			return instance
		}
	}
	return nil
}

func (s *skillService) enrichSkillPayload(payload *SkillPayload, skill models.Skill, instance *models.Instance) error {
	if instance == nil {
		instance = s.liteInstanceForSkill(skill.ID)
	}
	tags, err := s.repo.ListHubTagsBySkillID(skill.ID)
	if err != nil {
		return err
	}
	payload.Visibility = skill.Visibility
	if strings.TrimSpace(payload.Visibility) == "" {
		payload.Visibility = skillVisibilityPrivate
	}
	payload.PublishedAt = skill.PublishedAt
	payload.PublishedBy = skill.PublishedBy
	payload.Tags = s.hubTagsToPayload(tags)

	blob, blobErr := s.skillBlobForPublish(&skill)
	if blobErr == nil {
		payload.Publishable = s.isHubPublishable(&skill, blob)
		payload.ScanStatus = blob.ScanStatus
	} else {
		payload.Publishable = false
	}
	skipAgentCollectFailure := instance != nil && isLiteRuntimeInstance(instance)
	payload.PublishBlockedReason = s.publishBlockedReasonForSkill(&skill, blob, blobErr, payload.Publishable, skipAgentCollectFailure)
	if s.materializeService != nil && blobErr == nil && blob != nil && strings.TrimSpace(blob.ObjectKey) == "" {
		if status, materializeErr := s.materializeService.GetObservedStatus(skill.ID, blob); status != nil {
			payload.PackageMaterializeStatus = status
			payload.PackageMaterializeError = materializeErr
		}
	}
	if collectErr := s.resolvePackageCollectError(skill.ID, blob, blobErr, skipAgentCollectFailure); collectErr != nil {
		payload.PackageCollectError = collectErr
	}

	if s.userRepo != nil {
		owner, err := s.userRepo.GetByID(skill.UserID)
		if err != nil {
			return err
		}
		if owner != nil {
			payload.OwnerUsername = &owner.Username
		}
	}
	return nil
}

func truncateCollectError(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func (s *skillService) resolvePackageCollectError(skillID int, blob *models.SkillBlob, blobErr error, skipAgentCollectFailure bool) *string {
	if blobErr != nil || blob == nil || strings.TrimSpace(blob.ObjectKey) != "" {
		return nil
	}
	if s.materializeService != nil {
		if _, materializeErr := s.materializeService.GetObservedStatus(skillID, blob); materializeErr != nil {
			return materializeErr
		}
		job, err := s.materializeService.FindLatestBySkillID(skillID)
		if err == nil && job != nil && job.LastError != nil && strings.TrimSpace(*job.LastError) != "" {
			summary := truncateCollectError(*job.LastError, 512)
			if summary != "" {
				return &summary
			}
		}
	}
	if skipAgentCollectFailure {
		return nil
	}
	cmd, err := s.latestCollectPackageFailure(skillID)
	if err != nil || cmd == nil {
		return nil
	}
	if cmd.ErrorMessage == nil {
		return nil
	}
	summary := truncateCollectError(*cmd.ErrorMessage, 512)
	if summary == "" {
		return nil
	}
	return &summary
}

func (s *skillService) latestCollectPackageFailure(skillID int) (*models.InstanceCommand, error) {
	if s.commandRepo == nil {
		return nil, nil
	}
	return s.commandRepo.FindLatestFailedCollectSkillPackage(formatExternalSkillID(skillID))
}

func (s *skillService) publishBlockedReasonForSkill(skill *models.Skill, blob *models.SkillBlob, blobErr error, publishable bool, skipAgentCollectFailure bool) *string {
	if publishable || skill == nil {
		return nil
	}
	reason := func(value string) *string {
		return &value
	}
	if isDeletedSkill(skill) {
		return reason("skill_deleted")
	}
	if !strings.EqualFold(strings.TrimSpace(skill.Status), skillStatusActive) {
		return reason("skill_inactive")
	}
	if blobErr != nil || blob == nil {
		return reason("skill_package_pending")
	}
	if strings.TrimSpace(blob.ObjectKey) == "" {
		if s.materializeService != nil {
			if job, err := s.materializeService.FindLatestBySkillID(skill.ID); err == nil && job != nil {
				if blocked := materializeBlockedReason(job); blocked != nil {
					return blocked
				}
				return reason("skill_package_pending")
			}
		}
		if skipAgentCollectFailure {
			return reason("skill_package_pending")
		}
		if cmd, err := s.latestCollectPackageFailure(skill.ID); err == nil && cmd != nil {
			return reason("skill_package_collect_failed")
		}
		return reason("skill_package_pending")
	}
	if strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "failed") {
		return reason("skill_scan_failed")
	}
	if !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		return reason("skill_not_scanned")
	}
	risk := strings.ToLower(strings.TrimSpace(blob.RiskLevel))
	if risk != skillRiskNone && risk != skillRiskLow {
		return reason("skill_risk_blocked")
	}
	return nil
}

func (s *skillService) validateHubTagSelection(actorRole string, tagIDs []int) error {
	if len(tagIDs) == 0 {
		return fmt.Errorf("skill_tags_required")
	}
	hasPublicTag := false
	for _, tagID := range tagIDs {
		tag, err := s.repo.GetHubTagByID(tagID)
		if err != nil {
			return err
		}
		if tag == nil {
			return fmt.Errorf("skill hub tag not found")
		}
		if tag.AdminOnly && !isAdminRole(actorRole) {
			return fmt.Errorf("access denied")
		}
		if !tag.AdminOnly {
			hasPublicTag = true
		}
	}
	if !hasPublicTag {
		return fmt.Errorf("skill_tags_required")
	}
	return nil
}

func (s *skillService) ListHubTags(actorRole string) ([]SkillHubTagPayload, error) {
	tags, err := s.repo.ListHubTags(isAdminRole(actorRole))
	if err != nil {
		return nil, err
	}
	return s.hubTagsToPayload(tags), nil
}

func (s *skillService) ListHubCatalog(_ int, _ string, query SkillHubCatalogQuery) (*SkillHubCatalogResponse, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 1000 {
		query.PageSize = 1000
	}

	items, err := s.repo.ListPublicHubSkills()
	if err != nil {
		return nil, err
	}

	tagKeySet := map[string]struct{}{}
	for _, key := range query.TagKeys {
		key = strings.TrimSpace(key)
		if key != "" {
			tagKeySet[key] = struct{}{}
		}
	}
	search := strings.ToLower(strings.TrimSpace(query.Search))

	filtered := make([]SkillPayload, 0, len(items))
	for _, item := range items {
		blob, blobErr := s.skillBlobForPublish(&item)
		if blobErr != nil || !s.isHubPublishable(&item, blob) {
			continue
		}
		if len(tagKeySet) > 0 {
			tags, err := s.repo.ListHubTagsBySkillID(item.ID)
			if err != nil {
				return nil, err
			}
			matched := false
			for _, tag := range tags {
				if _, ok := tagKeySet[tag.TagKey]; ok {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if search != "" {
			haystack := strings.ToLower(strings.Join([]string{item.Name, item.SkillKey, derefString(item.Description)}, " "))
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		payload, err := s.toSkillPayload(item)
		if err != nil {
			return nil, err
		}
		if err := s.enrichSkillPayload(payload, item, nil); err != nil {
			return nil, err
		}
		filtered = append(filtered, *payload)
	}

	total := len(filtered)
	start := (query.Page - 1) * query.PageSize
	if start > total {
		start = total
	}
	end := start + query.PageSize
	if end > total {
		end = total
	}
	pageItems := filtered[start:end]
	totalPages := total / query.PageSize
	if total%query.PageSize != 0 {
		totalPages++
	}
	if totalPages == 0 {
		totalPages = 1
	}

	return &SkillHubCatalogResponse{
		Items:      pageItems,
		Total:      total,
		Page:       query.Page,
		PageSize:   query.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *skillService) ListMyHubSkills(userID int) ([]SkillPayload, error) {
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
	result := make([]SkillPayload, 0, len(filtered))
	for _, item := range filtered {
		payload, err := s.toSkillPayload(item)
		if err != nil {
			return nil, err
		}
		if err := s.enrichSkillPayload(payload, item, nil); err != nil {
			return nil, err
		}
		result = append(result, *payload)
	}
	return result, nil
}

func (s *skillService) ListAllHubSkillsAdmin() ([]SkillPayload, error) {
	items, err := s.repo.ListSkillsForHubAdmin()
	if err != nil {
		return nil, err
	}
	result := make([]SkillPayload, 0, len(items))
	for _, item := range items {
		if isDeletedSkill(&item) {
			continue
		}
		payload, err := s.toSkillPayload(item)
		if err != nil {
			return nil, err
		}
		if err := s.enrichSkillPayload(payload, item, nil); err != nil {
			return nil, err
		}
		result = append(result, *payload)
	}
	return result, nil
}

func (s *skillService) GetSkillHubDetail(actorUserID int, actorRole string, skillID int) (*SkillPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || !s.CanViewSkill(actorUserID, actorRole, skill) {
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

func (s *skillService) PublishToHub(actorUserID int, actorRole string, skillID int, tagIDs []int) (*SkillPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || isDeletedSkill(skill) {
		return nil, fmt.Errorf("skill not found")
	}
	if skill.UserID != actorUserID && !isAdminRole(actorRole) {
		return nil, fmt.Errorf("skill not found")
	}
	if err := s.validateHubTagSelection(actorRole, tagIDs); err != nil {
		return nil, err
	}
	blob, err := s.skillBlobForPublish(skill)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(blob.ObjectKey) == "" {
		return nil, fmt.Errorf("skill_package_pending")
	}
	if !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		return nil, fmt.Errorf("skill_not_scanned")
	}
	if !isHubPublishableBlob(blob) {
		return nil, fmt.Errorf("skill_risk_blocked")
	}
	if strings.EqualFold(skill.SourceType, skillSourceDiscovered) {
		skill.SourceType = skillSourceUploaded
	}
	if err := s.repo.ReplaceSkillTagAssignments(skillID, tagIDs); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	skill.Visibility = skillVisibilityPublic
	skill.PublishedAt = &now
	skill.PublishedBy = &actorUserID
	skill.UpdatedAt = now
	if err := s.repo.UpdateSkill(skill); err != nil {
		return nil, err
	}
	return s.GetSkillHubDetail(actorUserID, actorRole, skillID)
}

func (s *skillService) UnpublishFromHub(actorUserID int, actorRole string, skillID int) (*SkillPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill not found")
	}
	if skill.UserID != actorUserID && !isAdminRole(actorRole) {
		return nil, fmt.Errorf("skill not found")
	}
	skill.Visibility = skillVisibilityPrivate
	skill.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateSkill(skill); err != nil {
		return nil, err
	}
	return s.GetSkillHubDetail(actorUserID, actorRole, skillID)
}

func (s *skillService) UpdateHubTags(actorUserID int, actorRole string, skillID int, tagIDs []int) (*SkillPayload, error) {
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill not found")
	}
	if skill.UserID != actorUserID && !isAdminRole(actorRole) {
		return nil, fmt.Errorf("skill not found")
	}
	if !strings.EqualFold(strings.TrimSpace(skill.Visibility), skillVisibilityPublic) {
		return nil, fmt.Errorf("skill is not published to hub")
	}
	if err := s.validateHubTagSelection(actorRole, tagIDs); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceSkillTagAssignments(skillID, tagIDs); err != nil {
		return nil, err
	}
	skill.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateSkill(skill); err != nil {
		return nil, err
	}
	return s.GetSkillHubDetail(actorUserID, actorRole, skillID)
}

func (s *skillService) InstallHubSkill(actorUserID int, actorRole string, skillID, instanceID int) (*InstanceSkillPayload, error) {
	return s.AttachSkillToInstance(actorUserID, actorRole, instanceID, skillID)
}

func (s *skillService) ImportInstanceSkillToLibrary(actorUserID int, actorRole string, instanceID, skillID int) (*SkillPayload, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if !isAdminRole(actorRole) && instance.UserID != actorUserID {
		return nil, fmt.Errorf("access denied")
	}
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || skill.UserID != instance.UserID || isDeletedSkill(skill) {
		return nil, fmt.Errorf("skill not found")
	}
	instanceSkill, err := s.repo.GetInstanceSkill(instanceID, skillID)
	if err != nil {
		return nil, err
	}
	if instanceSkill == nil || instanceSkill.Status == "removed" {
		return nil, fmt.Errorf("skill not found on instance")
	}
	if isUserManagedSkill(*skill) {
		blob, blobErr := s.skillBlobForPublish(skill)
		if blobErr == nil && strings.TrimSpace(blob.ObjectKey) != "" && strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
			return s.GetSkillHubDetail(actorUserID, actorRole, skillID)
		}
	}
	if err := s.requestSkillPackageCollection(instanceID, skill, instanceSkill, fmt.Sprintf("import-%d-%d", instanceID, skillID)); err != nil {
		return nil, err
	}
	blob, err := s.skillBlobForPublish(skill)
	if err != nil {
		return nil, err
	}
	content, err := s.storage.GetObject(context.Background(), blob.ObjectKey)
	if err != nil {
		return nil, err
	}
	if err := s.ensureBlobObject(context.Background(), blob, content); err != nil {
		return nil, err
	}
	if blob.LastScanResultID == nil || !strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		if err := s.recordScanFromStoredBlob(blob); err != nil {
			return nil, err
		}
	}
	skill, err = s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, fmt.Errorf("skill not found")
	}
	blob, err = s.repo.GetBlobByID(blob.ID)
	if err != nil {
		return nil, err
	}
	if blob != nil {
		skill.RiskLevel = blob.RiskLevel
		skill.LastScannedAt = blob.LastScannedAt
		skill.LastScanResultID = blob.LastScanResultID
	}
	if err := s.promoteSkillToUploadedLibrary(skill); err != nil {
		return nil, err
	}
	return s.GetSkillHubDetail(actorUserID, actorRole, skillID)
}

func (s *skillService) RetrySkillPackageCollection(actorUserID int, actorRole string, instanceID, skillID int) error {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if !isAdminRole(actorRole) && instance.UserID != actorUserID {
		return fmt.Errorf("access denied")
	}
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return err
	}
	if skill == nil || skill.UserID != instance.UserID || isDeletedSkill(skill) {
		return fmt.Errorf("skill not found")
	}
	instanceSkill, err := s.repo.GetInstanceSkill(instanceID, skillID)
	if err != nil {
		return err
	}
	if instanceSkill == nil || instanceSkill.Status == "removed" {
		return fmt.Errorf("skill not found on instance")
	}
	blob, blobErr := s.skillBlobForPublish(skill)
	if blobErr == nil && strings.TrimSpace(blob.ObjectKey) != "" && strings.EqualFold(strings.TrimSpace(blob.ScanStatus), "completed") {
		return nil
	}
	if isLiteRuntimeInstance(instance) && s.materializeService != nil {
		if job, findErr := s.materializeService.FindLatestBySkillID(skillID); findErr == nil && job != nil && strings.EqualFold(strings.TrimSpace(job.Status), MaterializeJobStatusFailed) {
			_ = s.materializeService.RetryJob(skillID)
		}
	}
	return s.requestSkillPackageCollection(instanceID, skill, instanceSkill, fmt.Sprintf("retry-%d-%d-%d", instanceID, skillID, time.Now().Unix()))
}

func (s *skillService) PublishFromInstance(actorUserID int, actorRole string, instanceID, skillID int, tagIDs []int) (*SkillPayload, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if !isAdminRole(actorRole) && instance.UserID != actorUserID {
		return nil, fmt.Errorf("access denied")
	}
	skill, err := s.repo.GetSkillByID(skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil || skill.UserID != instance.UserID {
		return nil, fmt.Errorf("skill not found")
	}
	if !isUserManagedSkill(*skill) {
		return nil, fmt.Errorf("skill_not_in_library")
	}
	instanceSkill, err := s.repo.GetInstanceSkill(instanceID, skillID)
	if err != nil {
		return nil, err
	}
	if instanceSkill == nil || instanceSkill.Status == "removed" {
		return nil, fmt.Errorf("skill not found on instance")
	}
	if err := s.requestSkillPackageCollection(instanceID, skill, instanceSkill, fmt.Sprintf("publish-%d-%d", instanceID, skillID)); err != nil {
		return nil, err
	}
	return s.PublishToHub(actorUserID, actorRole, skillID, tagIDs)
}

func (s *skillService) ListAttachableSkills(actorUserID int, actorRole string) ([]SkillPayload, error) {
	result := make([]SkillPayload, 0)
	seen := map[int]struct{}{}

	mine, err := s.ListMyHubSkills(actorUserID)
	if err != nil {
		return nil, err
	}
	for _, item := range mine {
		if strings.EqualFold(item.SourceType, skillSourceDiscovered) {
			continue
		}
		if item.Status != "active" || item.RiskLevel == skillRiskMedium || item.RiskLevel == skillRiskHigh {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}

	catalog, err := s.ListHubCatalog(actorUserID, actorRole, SkillHubCatalogQuery{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}
	for _, item := range catalog.Items {
		if item.UserID == actorUserID {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func (s *skillService) ImportHubArchive(ctx context.Context, userID int, fileHeader *multipart.FileHeader) ([]SkillPayload, error) {
	items, err := s.ImportHubArchiveWithDecisions(ctx, userID, fileHeader, nil)
	if err != nil {
		return nil, err
	}
	results := make([]SkillPayload, 0, len(items))
	for _, item := range items {
		results = append(results, item.Skill)
	}
	return results, nil
}
