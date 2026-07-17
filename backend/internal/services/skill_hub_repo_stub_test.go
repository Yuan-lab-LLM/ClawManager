package services

import (
	"time"

	"clawreef/internal/models"
)

type skillRepoStub struct {
	skills                  map[int]*models.Skill
	blobs                   map[int]*models.SkillBlob
	versions                map[int]*models.SkillVersion
	tags                    map[int]*models.SkillHubTag
	tagAssignments          map[int][]int
	instanceSkillsBySkillID map[int][]models.InstanceSkill
	instanceSkills          []models.InstanceSkill
	hardDeleteCalled        bool
}

func (s *skillRepoStub) ListSkillsByUser(userID int) ([]models.Skill, error) {
	items := make([]models.Skill, 0)
	for _, skill := range s.skills {
		if skill.UserID == userID {
			items = append(items, *skill)
		}
	}
	return items, nil
}

func (s *skillRepoStub) ListAllSkills() ([]models.Skill, error) {
	items := make([]models.Skill, 0, len(s.skills))
	for _, skill := range s.skills {
		items = append(items, *skill)
	}
	return items, nil
}

func (s *skillRepoStub) GetSkillByID(id int) (*models.Skill, error) {
	if skill, ok := s.skills[id]; ok {
		copy := *skill
		return &copy, nil
	}
	return nil, nil
}

func (s *skillRepoStub) GetSkillByUserKey(userID int, skillKey string) (*models.Skill, error) {
	for _, skill := range s.skills {
		if skill.UserID == userID && skill.SkillKey == skillKey && skill.Status == skillStatusActive {
			copy := *skill
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *skillRepoStub) CreateSkill(*models.Skill) error { return nil }
func (s *skillRepoStub) UpdateSkill(skill *models.Skill) error {
	if s.skills == nil {
		s.skills = map[int]*models.Skill{}
	}
	copy := *skill
	s.skills[skill.ID] = &copy
	return nil
}

func (s *skillRepoStub) DeleteSkill(int) error {
	s.hardDeleteCalled = true
	return nil
}

func (s *skillRepoStub) GetBlobByContentHash(string) (*models.SkillBlob, error) { return nil, nil }

func (s *skillRepoStub) GetBlobByID(id int) (*models.SkillBlob, error) {
	if blob, ok := s.blobs[id]; ok {
		copy := *blob
		return &copy, nil
	}
	return nil, nil
}

func (s *skillRepoStub) CreateBlob(blob *models.SkillBlob) error {
	if s.blobs == nil {
		s.blobs = map[int]*models.SkillBlob{}
	}
	if blob.ID == 0 {
		blob.ID = len(s.blobs) + 1
	}
	copy := *blob
	s.blobs[blob.ID] = &copy
	return nil
}
func (s *skillRepoStub) UpdateBlob(blob *models.SkillBlob) error {
	if s.blobs == nil {
		s.blobs = map[int]*models.SkillBlob{}
	}
	copy := *blob
	s.blobs[blob.ID] = &copy
	return nil
}
func (s *skillRepoStub) ListVersionsBySkillID(int) ([]models.SkillVersion, error) {
	return nil, nil
}

func (s *skillRepoStub) GetVersionByID(id int) (*models.SkillVersion, error) {
	if version, ok := s.versions[id]; ok {
		copy := *version
		return &copy, nil
	}
	return nil, nil
}

func (s *skillRepoStub) GetVersionBySkillAndBlob(int, int) (*models.SkillVersion, error) {
	return nil, nil
}

func (s *skillRepoStub) GetLatestVersionBySkillID(int) (*models.SkillVersion, error) {
	return nil, nil
}

func (s *skillRepoStub) CreateVersion(*models.SkillVersion) error { return nil }

func (s *skillRepoStub) UpdateVersion(version *models.SkillVersion) error {
	if s.versions == nil {
		s.versions = map[int]*models.SkillVersion{}
	}
	copy := *version
	s.versions[version.ID] = &copy
	return nil
}

func (s *skillRepoStub) ListInstanceSkills(int) ([]models.InstanceSkill, error) {
	return nil, nil
}

func (s *skillRepoStub) ListActiveInstanceSkillsBySkillID(skillID int) ([]models.InstanceSkill, error) {
	if s.instanceSkillsBySkillID != nil {
		if items, ok := s.instanceSkillsBySkillID[skillID]; ok {
			return filterActiveInstanceSkills(items), nil
		}
	}
	items := make([]models.InstanceSkill, 0)
	for _, item := range s.instanceSkills {
		if item.SkillID == skillID && item.Status != "removed" && item.Status != "missing" {
			items = append(items, item)
		}
	}
	return items, nil
}

func filterActiveInstanceSkills(items []models.InstanceSkill) []models.InstanceSkill {
	active := make([]models.InstanceSkill, 0, len(items))
	for _, item := range items {
		if item.Status != "removed" && item.Status != "missing" {
			active = append(active, item)
		}
	}
	return active
}

func (s *skillRepoStub) GetInstanceSkill(instanceID, skillID int) (*models.InstanceSkill, error) {
	for _, item := range s.instanceSkills {
		if item.InstanceID == instanceID && item.SkillID == skillID {
			copy := item
			return &copy, nil
		}
	}
	return nil, nil
}
func (s *skillRepoStub) UpsertInstanceSkill(*models.InstanceSkill) error            { return nil }
func (s *skillRepoStub) MarkInstanceSkillRemoved(int, int, time.Time) error         { return nil }
func (s *skillRepoStub) MarkInstanceSkillRemovedBySkillKey(int, string, time.Time) error {
	return nil
}
func (s *skillRepoStub) MarkInstanceSkillsRemovedByWorkspacePath(int, string, time.Time) error {
	return nil
}
func (s *skillRepoStub) MarkMissingInstanceSkills(int, []int, time.Time) error      { return nil }
func (s *skillRepoStub) CreateScanResult(result *models.SkillScanResult) error {
	if result.ID == 0 {
		result.ID = 99
	}
	return nil
}
func (s *skillRepoStub) GetScanResultByID(int) (*models.SkillScanResult, error)     { return nil, nil }
func (s *skillRepoStub) ListScanResultsByBlobID(int) ([]models.SkillScanResult, error) {
	return nil, nil
}

func (s *skillRepoStub) GetLatestScanResultByBlobID(int) (*models.SkillScanResult, error) {
	return nil, nil
}

func (s *skillRepoStub) GetLatestScanResultBySkillID(int) (*models.SkillScanResult, error) {
	return nil, nil
}

func (s *skillRepoStub) ListHubTags(bool) ([]models.SkillHubTag, error) { return nil, nil }

func (s *skillRepoStub) GetHubTagByID(id int) (*models.SkillHubTag, error) {
	if tag, ok := s.tags[id]; ok {
		copy := *tag
		return &copy, nil
	}
	return nil, nil
}

func (s *skillRepoStub) ListHubTagsBySkillID(skillID int) ([]models.SkillHubTag, error) {
	tagIDs := s.tagAssignments[skillID]
	result := make([]models.SkillHubTag, 0, len(tagIDs))
	for _, tagID := range tagIDs {
		if tag, ok := s.tags[tagID]; ok {
			result = append(result, *tag)
		}
	}
	return result, nil
}

func (s *skillRepoStub) ReplaceSkillTagAssignments(skillID int, tagIDs []int) error {
	if s.tagAssignments == nil {
		s.tagAssignments = map[int][]int{}
	}
	s.tagAssignments[skillID] = append([]int(nil), tagIDs...)
	return nil
}

func (s *skillRepoStub) ListPublicHubSkills() ([]models.Skill, error) {
	items := make([]models.Skill, 0)
	for _, skill := range s.skills {
		if skill.Visibility == skillVisibilityPublic && skill.Status == skillStatusActive {
			items = append(items, *skill)
		}
	}
	return items, nil
}

func (s *skillRepoStub) ListSkillsForHubAdmin() ([]models.Skill, error) {
	return s.ListAllSkills()
}

type hubTagRepoStub struct {
	tags map[int]*models.SkillHubTag
}

func (s *hubTagRepoStub) ListSkillsByUser(int) ([]models.Skill, error)             { return nil, nil }
func (s *hubTagRepoStub) ListAllSkills() ([]models.Skill, error)                   { return nil, nil }
func (s *hubTagRepoStub) GetSkillByID(int) (*models.Skill, error)                  { return nil, nil }
func (s *hubTagRepoStub) GetSkillByUserKey(int, string) (*models.Skill, error)     { return nil, nil }
func (s *hubTagRepoStub) CreateSkill(*models.Skill) error                          { return nil }
func (s *hubTagRepoStub) UpdateSkill(*models.Skill) error                          { return nil }
func (s *hubTagRepoStub) DeleteSkill(int) error                                    { return nil }
func (s *hubTagRepoStub) GetBlobByContentHash(string) (*models.SkillBlob, error)   { return nil, nil }
func (s *hubTagRepoStub) GetBlobByID(int) (*models.SkillBlob, error)               { return nil, nil }
func (s *hubTagRepoStub) CreateBlob(*models.SkillBlob) error                       { return nil }
func (s *hubTagRepoStub) UpdateBlob(*models.SkillBlob) error                       { return nil }
func (s *hubTagRepoStub) ListVersionsBySkillID(int) ([]models.SkillVersion, error) { return nil, nil }
func (s *hubTagRepoStub) GetVersionByID(int) (*models.SkillVersion, error)         { return nil, nil }
func (s *hubTagRepoStub) GetVersionBySkillAndBlob(int, int) (*models.SkillVersion, error) {
	return nil, nil
}
func (s *hubTagRepoStub) GetLatestVersionBySkillID(int) (*models.SkillVersion, error) {
	return nil, nil
}
func (s *hubTagRepoStub) CreateVersion(*models.SkillVersion) error { return nil }
func (s *hubTagRepoStub) UpdateVersion(*models.SkillVersion) error { return nil }
func (s *hubTagRepoStub) ListInstanceSkills(int) ([]models.InstanceSkill, error) {
	return nil, nil
}
func (s *hubTagRepoStub) ListActiveInstanceSkillsBySkillID(int) ([]models.InstanceSkill, error) {
	return nil, nil
}
func (s *hubTagRepoStub) GetInstanceSkill(int, int) (*models.InstanceSkill, error) { return nil, nil }
func (s *hubTagRepoStub) UpsertInstanceSkill(*models.InstanceSkill) error            { return nil }
func (s *hubTagRepoStub) MarkInstanceSkillRemoved(int, int, time.Time) error         { return nil }
func (s *hubTagRepoStub) MarkInstanceSkillRemovedBySkillKey(int, string, time.Time) error {
	return nil
}
func (s *hubTagRepoStub) MarkInstanceSkillsRemovedByWorkspacePath(int, string, time.Time) error {
	return nil
}
func (s *hubTagRepoStub) MarkMissingInstanceSkills(int, []int, time.Time) error      { return nil }
func (s *hubTagRepoStub) CreateScanResult(*models.SkillScanResult) error               { return nil }
func (s *hubTagRepoStub) GetScanResultByID(int) (*models.SkillScanResult, error)     { return nil, nil }
func (s *hubTagRepoStub) ListScanResultsByBlobID(int) ([]models.SkillScanResult, error) {
	return nil, nil
}
func (s *hubTagRepoStub) GetLatestScanResultByBlobID(int) (*models.SkillScanResult, error) {
	return nil, nil
}
func (s *hubTagRepoStub) GetLatestScanResultBySkillID(int) (*models.SkillScanResult, error) {
	return nil, nil
}
func (s *hubTagRepoStub) ListHubTags(bool) ([]models.SkillHubTag, error) { return nil, nil }
func (s *hubTagRepoStub) GetHubTagByID(id int) (*models.SkillHubTag, error) {
	if tag, ok := s.tags[id]; ok {
		return tag, nil
	}
	return nil, nil
}
func (s *hubTagRepoStub) ListHubTagsBySkillID(int) ([]models.SkillHubTag, error) { return nil, nil }
func (s *hubTagRepoStub) ReplaceSkillTagAssignments(int, []int) error              { return nil }
func (s *hubTagRepoStub) ListPublicHubSkills() ([]models.Skill, error)             { return nil, nil }
func (s *hubTagRepoStub) ListSkillsForHubAdmin() ([]models.Skill, error)           { return nil, nil }
