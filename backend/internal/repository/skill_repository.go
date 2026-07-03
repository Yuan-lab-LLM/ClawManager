package repository

import (
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type SkillRepository interface {
	ListSkillsByUser(userID int) ([]models.Skill, error)
	ListAllSkills() ([]models.Skill, error)
	GetSkillByID(id int) (*models.Skill, error)
	GetSkillByUserKey(userID int, skillKey string) (*models.Skill, error)
	CreateSkill(skill *models.Skill) error
	UpdateSkill(skill *models.Skill) error
	DeleteSkill(id int) error
	GetBlobByContentHash(hash string) (*models.SkillBlob, error)
	GetBlobByID(id int) (*models.SkillBlob, error)
	CreateBlob(blob *models.SkillBlob) error
	UpdateBlob(blob *models.SkillBlob) error
	ListVersionsBySkillID(skillID int) ([]models.SkillVersion, error)
	GetVersionByID(id int) (*models.SkillVersion, error)
	GetVersionBySkillAndBlob(skillID, blobID int) (*models.SkillVersion, error)
	GetLatestVersionBySkillID(skillID int) (*models.SkillVersion, error)
	CreateVersion(version *models.SkillVersion) error
	ListInstanceSkills(instanceID int) ([]models.InstanceSkill, error)
	GetInstanceSkill(instanceID, skillID int) (*models.InstanceSkill, error)
	UpsertInstanceSkill(item *models.InstanceSkill) error
	MarkInstanceSkillRemoved(instanceID int, skillID int, observedAt time.Time) error
	MarkInstanceSkillRemovedBySkillKey(instanceID int, skillKey string, observedAt time.Time) error
	MarkInstanceSkillsRemovedByWorkspacePath(instanceID int, workspacePath string, observedAt time.Time) error
	MarkMissingInstanceSkills(instanceID int, activeSkillIDs []int, observedAt time.Time) error
	CreateScanResult(result *models.SkillScanResult) error
	GetScanResultByID(id int) (*models.SkillScanResult, error)
	ListScanResultsByBlobID(blobID int) ([]models.SkillScanResult, error)
	GetLatestScanResultByBlobID(blobID int) (*models.SkillScanResult, error)
	GetLatestScanResultBySkillID(skillID int) (*models.SkillScanResult, error)
}

type skillRepository struct{ sess db.Session }

func NewSkillRepository(sess db.Session) SkillRepository { return &skillRepository{sess: sess} }

func (r *skillRepository) ListSkillsByUser(userID int) ([]models.Skill, error) {
	var items []models.Skill
	if err := r.sess.Collection("skills").Find(db.Cond{"user_id": userID}).OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list skills by user: %w", err)
	}
	return items, nil
}

func (r *skillRepository) ListAllSkills() ([]models.Skill, error) {
	var items []models.Skill
	if err := r.sess.Collection("skills").Find().OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list all skills: %w", err)
	}
	return items, nil
}

func (r *skillRepository) GetSkillByID(id int) (*models.Skill, error) {
	var item models.Skill
	if err := r.sess.Collection("skills").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) GetSkillByUserKey(userID int, skillKey string) (*models.Skill, error) {
	var item models.Skill
	if err := r.sess.Collection("skills").Find(db.Cond{"user_id": userID, "skill_key": skillKey}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill by key: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) CreateSkill(skill *models.Skill) error {
	ensureTimestamps(&skill.CreatedAt, &skill.UpdatedAt)
	res, err := r.sess.Collection("skills").Insert(skill)
	if err != nil {
		if isDuplicateEntryError(err) {
			existing, findErr := r.GetSkillByUserKey(skill.UserID, skill.SkillKey)
			if findErr != nil {
				return findErr
			}
			if existing != nil {
				*skill = *existing
				return nil
			}
		}
		return fmt.Errorf("failed to create skill: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		skill.ID = int(id)
	}
	return nil
}

func (r *skillRepository) UpdateSkill(skill *models.Skill) error {
	if skill.UpdatedAt.IsZero() {
		skill.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("skills").Find(db.Cond{"id": skill.ID}).Update(skill); err != nil {
		return fmt.Errorf("failed to update skill: %w", err)
	}
	return nil
}

func (r *skillRepository) DeleteSkill(id int) error {
	if err := r.sess.Collection("skills").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}
	return nil
}

func (r *skillRepository) GetBlobByContentHash(hash string) (*models.SkillBlob, error) {
	var item models.SkillBlob
	if err := r.sess.Collection("skill_blobs").Find(db.Cond{"content_hash": hash}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill blob by content hash: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) GetBlobByID(id int) (*models.SkillBlob, error) {
	var item models.SkillBlob
	if err := r.sess.Collection("skill_blobs").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill blob: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) CreateBlob(blob *models.SkillBlob) error {
	ensureTimestamps(&blob.CreatedAt, &blob.UpdatedAt)
	res, err := r.sess.Collection("skill_blobs").Insert(blob)
	if err != nil {
		return fmt.Errorf("failed to create skill blob: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		blob.ID = int(id)
	}
	return nil
}

func (r *skillRepository) UpdateBlob(blob *models.SkillBlob) error {
	if blob.UpdatedAt.IsZero() {
		blob.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("skill_blobs").Find(db.Cond{"id": blob.ID}).Update(blob); err != nil {
		return fmt.Errorf("failed to update skill blob: %w", err)
	}
	return nil
}

func (r *skillRepository) ListVersionsBySkillID(skillID int) ([]models.SkillVersion, error) {
	var items []models.SkillVersion
	if err := r.sess.Collection("skill_versions").Find(db.Cond{"skill_id": skillID}).OrderBy("-version_no", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list skill versions: %w", err)
	}
	return items, nil
}

func (r *skillRepository) GetVersionByID(id int) (*models.SkillVersion, error) {
	var item models.SkillVersion
	if err := r.sess.Collection("skill_versions").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill version: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) GetVersionBySkillAndBlob(skillID, blobID int) (*models.SkillVersion, error) {
	var item models.SkillVersion
	if err := r.sess.Collection("skill_versions").Find(db.Cond{"skill_id": skillID, "blob_id": blobID}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill version by blob: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) GetLatestVersionBySkillID(skillID int) (*models.SkillVersion, error) {
	var item models.SkillVersion
	if err := r.sess.Collection("skill_versions").Find(db.Cond{"skill_id": skillID}).OrderBy("-version_no", "-id").One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest skill version: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) CreateVersion(version *models.SkillVersion) error {
	ensureTimestamps(&version.CreatedAt, &version.UpdatedAt)
	res, err := r.sess.Collection("skill_versions").Insert(version)
	if err != nil {
		return fmt.Errorf("failed to create skill version: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		version.ID = int(id)
	}
	return nil
}

func (r *skillRepository) ListInstanceSkills(instanceID int) ([]models.InstanceSkill, error) {
	var items []models.InstanceSkill
	if err := r.sess.Collection("instance_skills").Find(db.Cond{"instance_id": instanceID}).OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list instance skills: %w", err)
	}
	return items, nil
}

func (r *skillRepository) GetInstanceSkill(instanceID, skillID int) (*models.InstanceSkill, error) {
	var item models.InstanceSkill
	if err := r.sess.Collection("instance_skills").Find(db.Cond{"instance_id": instanceID, "skill_id": skillID}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance skill: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) UpsertInstanceSkill(item *models.InstanceSkill) error {
	existing, err := r.GetInstanceSkill(item.InstanceID, item.SkillID)
	if err != nil {
		return err
	}
	if existing == nil {
		ensureTimestamps(&item.CreatedAt, &item.UpdatedAt)
		res, err := r.sess.Collection("instance_skills").Insert(item)
		if err != nil {
			if !isDuplicateEntryError(err) {
				return fmt.Errorf("failed to create instance skill: %w", err)
			}
			existing, err = r.GetInstanceSkill(item.InstanceID, item.SkillID)
			if err != nil {
				return err
			}
			if existing == nil {
				return fmt.Errorf("failed to create instance skill: %w", err)
			}
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = time.Now().UTC()
			}
			if err := r.sess.Collection("instance_skills").Find(db.Cond{"id": existing.ID}).Update(item); err != nil {
				return fmt.Errorf("failed to update instance skill after duplicate insert: %w", err)
			}
			return nil
		}
		if id, ok := res.ID().(int64); ok {
			item.ID = int(id)
		}
		return nil
	}
	item.ID = existing.ID
	item.CreatedAt = existing.CreatedAt
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("instance_skills").Find(db.Cond{"id": existing.ID}).Update(item); err != nil {
		return fmt.Errorf("failed to update instance skill: %w", err)
	}
	return nil
}

func isDuplicateEntryError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}

func (r *skillRepository) MarkInstanceSkillRemoved(instanceID int, skillID int, observedAt time.Time) error {
	item, err := r.GetInstanceSkill(instanceID, skillID)
	if err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	if item.UpdatedAt.After(observedAt) {
		return nil
	}
	item.Status = "removed"
	item.RemovedAt = &observedAt
	item.UpdatedAt = observedAt
	if err := r.sess.Collection("instance_skills").Find(db.Cond{"id": item.ID}).Update(item); err != nil {
		return fmt.Errorf("failed to mark instance skill removed: %w", err)
	}
	return nil
}

func (r *skillRepository) MarkInstanceSkillRemovedBySkillKey(instanceID int, skillKey string, observedAt time.Time) error {
	skillKey = strings.TrimSpace(skillKey)
	if skillKey == "" {
		return nil
	}
	var skills []models.Skill
	if err := r.sess.Collection("skills").Find(db.Cond{"skill_key": skillKey}).All(&skills); err != nil && err != db.ErrNoMoreRows {
		return fmt.Errorf("failed to find skills by key for removal: %w", err)
	}
	for _, skill := range skills {
		if err := r.MarkInstanceSkillRemoved(instanceID, skill.ID, observedAt); err != nil {
			return err
		}
	}
	return nil
}

func (r *skillRepository) MarkInstanceSkillsRemovedByWorkspacePath(instanceID int, workspacePath string, observedAt time.Time) error {
	deletedPath := normalizeInstanceSkillWorkspacePath(workspacePath)
	if deletedPath == "" {
		return nil
	}
	var items []models.InstanceSkill
	if err := r.sess.Collection("instance_skills").Find(db.Cond{"instance_id": instanceID}).All(&items); err != nil && err != db.ErrNoMoreRows {
		return fmt.Errorf("failed to list instance skills for workspace deletion: %w", err)
	}
	for _, item := range items {
		if isRemovedInstanceSkillRecord(item) {
			continue
		}
		matches := instanceSkillInstallPathMatchesDelete(item, deletedPath)
		if !matches {
			skill, err := r.GetSkillByID(item.SkillID)
			if err != nil {
				return err
			}
			matches = skill != nil && workspaceDeleteTargetsSkillKey(deletedPath, skill.SkillKey)
		}
		if !matches {
			continue
		}
		item.Status = "removed"
		item.RemovedAt = &observedAt
		item.UpdatedAt = observedAt
		if err := r.sess.Collection("instance_skills").Find(db.Cond{"id": item.ID}).Update(&item); err != nil {
			return fmt.Errorf("failed to mark instance skill removed after workspace deletion: %w", err)
		}
	}
	return nil
}

func normalizeInstanceSkillWorkspacePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	parts := strings.Split(value, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) > 0 && strings.EqualFold(clean[0], "config") {
		clean = clean[1:]
	}
	return strings.Join(clean, "/")
}

func instanceSkillInstallPathMatchesDelete(item models.InstanceSkill, deletedPath string) bool {
	if item.InstallPath == nil {
		return false
	}
	installPath := normalizeInstanceSkillWorkspacePath(*item.InstallPath)
	if installPath == "" || deletedPath == "" {
		return false
	}
	return installPath == deletedPath ||
		strings.HasPrefix(installPath, deletedPath+"/") ||
		strings.HasPrefix(deletedPath, installPath+"/") ||
		strings.HasSuffix(installPath, "/"+deletedPath) ||
		strings.HasSuffix(deletedPath, "/"+installPath)
}

func workspaceDeleteTargetsSkillKey(deletedPath string, skillKey string) bool {
	key := strings.ToLower(strings.TrimSpace(skillKey))
	if key == "" || deletedPath == "" {
		return false
	}
	segments := strings.Split(strings.ToLower(deletedPath), "/")
	last := segments[len(segments)-1]
	if last != key {
		return false
	}
	if len(segments) <= 2 {
		return true
	}
	for i, segment := range segments {
		if segment == "skills" && i+1 < len(segments) && segments[i+1] == key {
			return true
		}
		if segment == ".openclaw" || segment == "openclaw" {
			return true
		}
	}
	return false
}

func isRemovedInstanceSkillRecord(item models.InstanceSkill) bool {
	return strings.EqualFold(strings.TrimSpace(item.Status), "removed") || item.RemovedAt != nil
}

func (r *skillRepository) MarkMissingInstanceSkills(instanceID int, activeSkillIDs []int, observedAt time.Time) error {
	find := r.sess.Collection("instance_skills").Find(db.Cond{"instance_id": instanceID})
	if len(activeSkillIDs) > 0 {
		find = find.And(db.Cond{"skill_id NOT IN": activeSkillIDs})
	}
	var items []models.InstanceSkill
	if err := find.All(&items); err != nil && err != db.ErrNoMoreRows {
		return fmt.Errorf("failed to list stale instance skills: %w", err)
	}
	for _, item := range items {
		item.Status = "removed"
		item.RemovedAt = &observedAt
		item.UpdatedAt = observedAt
		if err := r.sess.Collection("instance_skills").Find(db.Cond{"id": item.ID}).Update(item); err != nil {
			return fmt.Errorf("failed to mark instance skill removed: %w", err)
		}
	}
	return nil
}

func (r *skillRepository) CreateScanResult(result *models.SkillScanResult) error {
	ensureTimestamps(&result.CreatedAt, &result.UpdatedAt)
	res, err := r.sess.Collection("skill_scan_results").Insert(result)
	if err != nil {
		return fmt.Errorf("failed to create skill scan result: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		result.ID = int(id)
	}
	return nil
}

func (r *skillRepository) GetScanResultByID(id int) (*models.SkillScanResult, error) {
	var item models.SkillScanResult
	if err := r.sess.Collection("skill_scan_results").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill scan result: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) ListScanResultsByBlobID(blobID int) ([]models.SkillScanResult, error) {
	var items []models.SkillScanResult
	if err := r.sess.Collection("skill_scan_results").Find(db.Cond{"blob_id": blobID}).OrderBy("-scanned_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list skill scan results: %w", err)
	}
	return items, nil
}

func (r *skillRepository) GetLatestScanResultByBlobID(blobID int) (*models.SkillScanResult, error) {
	var item models.SkillScanResult
	if err := r.sess.Collection("skill_scan_results").Find(db.Cond{"blob_id": blobID}).OrderBy("-scanned_at", "-id").One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest skill scan result: %w", err)
	}
	return &item, nil
}

func (r *skillRepository) GetLatestScanResultBySkillID(skillID int) (*models.SkillScanResult, error) {
	skill, err := r.GetSkillByID(skillID)
	if err != nil || skill == nil || skill.LastScanResultID == nil {
		return nil, err
	}
	return r.GetScanResultByID(*skill.LastScanResultID)
}
