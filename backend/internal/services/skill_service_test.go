package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestImportArchiveAllowsDisabledScanner(t *testing.T) {
	repo := newStubSkillRepository()
	instanceRepo := newStubInstanceRepository()
	commandService := &stubInstanceCommandService{}
	storage := newStubObjectStorage()
	service := NewSkillService(repo, instanceRepo, commandService, storage, &noopSkillScannerClient{})

	fileHeader := newMultipartFileHeader(t, "skill.zip", newSkillArchive(t, "hello-skill"))

	results, err := service.ImportArchive(context.Background(), 7, fileHeader)
	if err != nil {
		t.Fatalf("ImportArchive returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one imported skill, got %d", len(results))
	}

	imported := results[0]
	if imported.SourceType != skillSourceUploaded {
		t.Fatalf("expected source_type %q, got %q", skillSourceUploaded, imported.SourceType)
	}
	if imported.ScanStatus != "pending" {
		t.Fatalf("expected scan_status pending, got %q", imported.ScanStatus)
	}
	if imported.RiskLevel != skillRiskUnknown {
		t.Fatalf("expected risk_level %q, got %q", skillRiskUnknown, imported.RiskLevel)
	}
	if imported.LastScannedAt != nil {
		t.Fatalf("expected last_scanned_at to stay nil when scanner is disabled")
	}
	if len(repo.scanResults) != 0 {
		t.Fatalf("expected no scan results to be created, got %d", len(repo.scanResults))
	}
	if len(storage.objects) != 1 {
		t.Fatalf("expected one stored archive object, got %d", len(storage.objects))
	}
	if imported.CurrentVersionID == nil {
		t.Fatalf("expected imported skill to have a current version")
	}
}

func TestAttachImportedSkillQueuesInstallCommandWhenScannerDisabled(t *testing.T) {
	repo := newStubSkillRepository()
	instanceRepo := newStubInstanceRepository(models.Instance{ID: 42, UserID: 7, Name: "demo-instance"})
	commandService := &stubInstanceCommandService{}
	storage := newStubObjectStorage()
	service := NewSkillService(repo, instanceRepo, commandService, storage, &noopSkillScannerClient{})

	fileHeader := newMultipartFileHeader(t, "skill.zip", newSkillArchive(t, "hello-skill"))
	results, err := service.ImportArchive(context.Background(), 7, fileHeader)
	if err != nil {
		t.Fatalf("ImportArchive returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one imported skill, got %d", len(results))
	}

	imported := results[0]
	attached, err := service.AttachSkillToInstance(42, imported.ID)
	if err != nil {
		t.Fatalf("AttachSkillToInstance returned error: %v", err)
	}
	if attached.SourceType != "injected_by_clawmanager" {
		t.Fatalf("expected instance skill source_type injected_by_clawmanager, got %q", attached.SourceType)
	}
	if attached.Status != "active" {
		t.Fatalf("expected instance skill status active, got %q", attached.Status)
	}
	if attached.Skill == nil {
		t.Fatalf("expected attached payload to include skill details")
	}
	if attached.Skill.InstanceCount != 1 {
		t.Fatalf("expected attached skill instance count 1, got %d", attached.Skill.InstanceCount)
	}
	if len(commandService.created) != 1 {
		t.Fatalf("expected one install command, got %d", len(commandService.created))
	}

	command := commandService.created[0]
	if command.instanceID != 42 {
		t.Fatalf("expected command for instance 42, got %d", command.instanceID)
	}
	if command.request.CommandType != InstanceCommandTypeInstallSkill {
		t.Fatalf("expected command type %q, got %q", InstanceCommandTypeInstallSkill, command.request.CommandType)
	}

	if got := command.request.Payload["skill_id"]; got != formatExternalSkillID(imported.ID) {
		t.Fatalf("expected payload skill_id %q, got %#v", formatExternalSkillID(imported.ID), got)
	}
	if imported.CurrentVersionID == nil {
		t.Fatalf("expected imported skill to have a current version")
	}
	if got := command.request.Payload["skill_version"]; got != formatExternalVersionID(*imported.CurrentVersionID) {
		t.Fatalf("expected payload skill_version %q, got %#v", formatExternalVersionID(*imported.CurrentVersionID), got)
	}
	if got := command.request.Payload["target_name"]; got != imported.SkillKey {
		t.Fatalf("expected payload target_name %q, got %#v", imported.SkillKey, got)
	}
	if imported.ContentMD5 == nil {
		t.Fatalf("expected imported skill to expose content_md5")
	}
	if got := command.request.Payload["content_md5"]; got != *imported.ContentMD5 {
		t.Fatalf("expected payload content_md5 %q, got %#v", *imported.ContentMD5, got)
	}
}

func TestImportArchiveDoesNotFailWhenInstanceCountAggregationFails(t *testing.T) {
	repo := newStubSkillRepository()
	instanceRepo := newStubInstanceRepository()
	instanceRepo.getAllErr = errors.New("instance aggregation unavailable")
	commandService := &stubInstanceCommandService{}
	storage := newStubObjectStorage()
	service := NewSkillService(repo, instanceRepo, commandService, storage, &noopSkillScannerClient{})

	fileHeader := newMultipartFileHeader(t, "skill.zip", newSkillArchive(t, "hello-skill"))

	results, err := service.ImportArchive(context.Background(), 7, fileHeader)
	if err != nil {
		t.Fatalf("ImportArchive returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one imported skill, got %d", len(results))
	}
	if results[0].InstanceCount != 0 {
		t.Fatalf("expected instance count fallback to 0, got %d", results[0].InstanceCount)
	}
}

type stubSkillRepository struct {
	nextSkillID      int
	nextBlobID       int
	nextVersionID    int
	nextInstanceID   int
	nextScanResultID int
	skills           map[int]*models.Skill
	blobs            map[int]*models.SkillBlob
	versions         map[int]*models.SkillVersion
	instanceSkills   map[int]map[int]*models.InstanceSkill
	scanResults      map[int]*models.SkillScanResult
}

func newStubSkillRepository() *stubSkillRepository {
	return &stubSkillRepository{
		nextSkillID:      1,
		nextBlobID:       1,
		nextVersionID:    1,
		nextInstanceID:   1,
		nextScanResultID: 1,
		skills:           map[int]*models.Skill{},
		blobs:            map[int]*models.SkillBlob{},
		versions:         map[int]*models.SkillVersion{},
		instanceSkills:   map[int]map[int]*models.InstanceSkill{},
		scanResults:      map[int]*models.SkillScanResult{},
	}
}

func (r *stubSkillRepository) ListSkillsByUser(userID int) ([]models.Skill, error) {
	items := make([]models.Skill, 0)
	for _, skill := range r.skills {
		if skill.UserID == userID {
			items = append(items, *cloneSkill(skill))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *stubSkillRepository) ListAllSkills() ([]models.Skill, error) {
	items := make([]models.Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		items = append(items, *cloneSkill(skill))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *stubSkillRepository) GetSkillByID(id int) (*models.Skill, error) {
	skill := r.skills[id]
	if skill == nil {
		return nil, nil
	}
	return cloneSkill(skill), nil
}

func (r *stubSkillRepository) GetSkillByUserKey(userID int, skillKey string) (*models.Skill, error) {
	for _, skill := range r.skills {
		if skill.UserID == userID && skill.SkillKey == skillKey {
			return cloneSkill(skill), nil
		}
	}
	return nil, nil
}

func (r *stubSkillRepository) CreateSkill(skill *models.Skill) error {
	copyItem := *skill
	if copyItem.ID == 0 {
		copyItem.ID = r.nextSkillID
		r.nextSkillID++
	}
	now := time.Now().UTC()
	if copyItem.CreatedAt.IsZero() {
		copyItem.CreatedAt = now
	}
	if copyItem.UpdatedAt.IsZero() {
		copyItem.UpdatedAt = now
	}
	r.skills[copyItem.ID] = cloneSkill(&copyItem)
	skill.ID = copyItem.ID
	skill.CreatedAt = copyItem.CreatedAt
	skill.UpdatedAt = copyItem.UpdatedAt
	return nil
}

func (r *stubSkillRepository) UpdateSkill(skill *models.Skill) error {
	r.skills[skill.ID] = cloneSkill(skill)
	return nil
}

func (r *stubSkillRepository) DeleteSkill(id int) error {
	delete(r.skills, id)
	return nil
}

func (r *stubSkillRepository) GetBlobByContentHash(hash string) (*models.SkillBlob, error) {
	for _, blob := range r.blobs {
		if blob.ContentHash == hash {
			return cloneBlob(blob), nil
		}
	}
	return nil, nil
}

func (r *stubSkillRepository) GetBlobByID(id int) (*models.SkillBlob, error) {
	blob := r.blobs[id]
	if blob == nil {
		return nil, nil
	}
	return cloneBlob(blob), nil
}

func (r *stubSkillRepository) CreateBlob(blob *models.SkillBlob) error {
	copyItem := *blob
	if copyItem.ID == 0 {
		copyItem.ID = r.nextBlobID
		r.nextBlobID++
	}
	now := time.Now().UTC()
	if copyItem.CreatedAt.IsZero() {
		copyItem.CreatedAt = now
	}
	if copyItem.UpdatedAt.IsZero() {
		copyItem.UpdatedAt = now
	}
	r.blobs[copyItem.ID] = cloneBlob(&copyItem)
	blob.ID = copyItem.ID
	blob.CreatedAt = copyItem.CreatedAt
	blob.UpdatedAt = copyItem.UpdatedAt
	return nil
}

func (r *stubSkillRepository) UpdateBlob(blob *models.SkillBlob) error {
	r.blobs[blob.ID] = cloneBlob(blob)
	return nil
}

func (r *stubSkillRepository) ListVersionsBySkillID(skillID int) ([]models.SkillVersion, error) {
	items := make([]models.SkillVersion, 0)
	for _, version := range r.versions {
		if version.SkillID == skillID {
			items = append(items, *cloneVersion(version))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].VersionNo < items[j].VersionNo })
	return items, nil
}

func (r *stubSkillRepository) GetVersionByID(id int) (*models.SkillVersion, error) {
	version := r.versions[id]
	if version == nil {
		return nil, nil
	}
	return cloneVersion(version), nil
}

func (r *stubSkillRepository) GetVersionBySkillAndBlob(skillID, blobID int) (*models.SkillVersion, error) {
	for _, version := range r.versions {
		if version.SkillID == skillID && version.BlobID == blobID {
			return cloneVersion(version), nil
		}
	}
	return nil, nil
}

func (r *stubSkillRepository) GetLatestVersionBySkillID(skillID int) (*models.SkillVersion, error) {
	var latest *models.SkillVersion
	for _, version := range r.versions {
		if version.SkillID != skillID {
			continue
		}
		if latest == nil || version.VersionNo > latest.VersionNo {
			latest = version
		}
	}
	if latest == nil {
		return nil, nil
	}
	return cloneVersion(latest), nil
}

func (r *stubSkillRepository) CreateVersion(version *models.SkillVersion) error {
	copyItem := *version
	if copyItem.ID == 0 {
		copyItem.ID = r.nextVersionID
		r.nextVersionID++
	}
	now := time.Now().UTC()
	if copyItem.CreatedAt.IsZero() {
		copyItem.CreatedAt = now
	}
	if copyItem.UpdatedAt.IsZero() {
		copyItem.UpdatedAt = now
	}
	r.versions[copyItem.ID] = cloneVersion(&copyItem)
	version.ID = copyItem.ID
	version.CreatedAt = copyItem.CreatedAt
	version.UpdatedAt = copyItem.UpdatedAt
	return nil
}

func (r *stubSkillRepository) ListInstanceSkills(instanceID int) ([]models.InstanceSkill, error) {
	items := make([]models.InstanceSkill, 0)
	for _, item := range r.instanceSkills[instanceID] {
		items = append(items, *cloneInstanceSkill(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SkillID < items[j].SkillID })
	return items, nil
}

func (r *stubSkillRepository) GetInstanceSkill(instanceID, skillID int) (*models.InstanceSkill, error) {
	items := r.instanceSkills[instanceID]
	if items == nil || items[skillID] == nil {
		return nil, nil
	}
	return cloneInstanceSkill(items[skillID]), nil
}

func (r *stubSkillRepository) UpsertInstanceSkill(item *models.InstanceSkill) error {
	if r.instanceSkills[item.InstanceID] == nil {
		r.instanceSkills[item.InstanceID] = map[int]*models.InstanceSkill{}
	}
	copyItem := *item
	if existing := r.instanceSkills[item.InstanceID][item.SkillID]; existing != nil {
		copyItem.ID = existing.ID
	} else if copyItem.ID == 0 {
		copyItem.ID = r.nextInstanceID
		r.nextInstanceID++
	}
	now := time.Now().UTC()
	if copyItem.CreatedAt.IsZero() {
		copyItem.CreatedAt = now
	}
	if copyItem.UpdatedAt.IsZero() {
		copyItem.UpdatedAt = now
	}
	r.instanceSkills[item.InstanceID][item.SkillID] = cloneInstanceSkill(&copyItem)
	item.ID = copyItem.ID
	item.CreatedAt = copyItem.CreatedAt
	item.UpdatedAt = copyItem.UpdatedAt
	return nil
}

func (r *stubSkillRepository) MarkMissingInstanceSkills(instanceID int, activeSkillIDs []int, observedAt time.Time) error {
	return nil
}

func (r *stubSkillRepository) CreateScanResult(result *models.SkillScanResult) error {
	copyItem := *result
	if copyItem.ID == 0 {
		copyItem.ID = r.nextScanResultID
		r.nextScanResultID++
	}
	now := time.Now().UTC()
	if copyItem.CreatedAt.IsZero() {
		copyItem.CreatedAt = now
	}
	if copyItem.UpdatedAt.IsZero() {
		copyItem.UpdatedAt = now
	}
	r.scanResults[copyItem.ID] = cloneScanResult(&copyItem)
	result.ID = copyItem.ID
	result.CreatedAt = copyItem.CreatedAt
	result.UpdatedAt = copyItem.UpdatedAt
	return nil
}

func (r *stubSkillRepository) GetScanResultByID(id int) (*models.SkillScanResult, error) {
	result := r.scanResults[id]
	if result == nil {
		return nil, nil
	}
	return cloneScanResult(result), nil
}

func (r *stubSkillRepository) ListScanResultsByBlobID(blobID int) ([]models.SkillScanResult, error) {
	items := make([]models.SkillScanResult, 0)
	for _, result := range r.scanResults {
		if result.BlobID == blobID {
			items = append(items, *cloneScanResult(result))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *stubSkillRepository) GetLatestScanResultByBlobID(blobID int) (*models.SkillScanResult, error) {
	var latest *models.SkillScanResult
	for _, result := range r.scanResults {
		if result.BlobID != blobID {
			continue
		}
		if latest == nil || result.ID > latest.ID {
			latest = result
		}
	}
	if latest == nil {
		return nil, nil
	}
	return cloneScanResult(latest), nil
}

func (r *stubSkillRepository) GetLatestScanResultBySkillID(skillID int) (*models.SkillScanResult, error) {
	skill := r.skills[skillID]
	if skill == nil || skill.LastScanResultID == nil {
		return nil, nil
	}
	return r.GetScanResultByID(*skill.LastScanResultID)
}

type stubInstanceRepository struct {
	instances map[int]*models.Instance
	getAllErr error
}

func newStubInstanceRepository(instances ...models.Instance) *stubInstanceRepository {
	items := map[int]*models.Instance{}
	for _, instance := range instances {
		copyItem := instance
		items[instance.ID] = &copyItem
	}
	return &stubInstanceRepository{instances: items}
}

func (r *stubInstanceRepository) Create(instance *models.Instance) error {
	r.instances[instance.ID] = cloneInstance(instance)
	return nil
}

func (r *stubInstanceRepository) GetByID(id int) (*models.Instance, error) {
	instance := r.instances[id]
	if instance == nil {
		return nil, nil
	}
	return cloneInstance(instance), nil
}

func (r *stubInstanceRepository) GetByAccessToken(accessToken string) (*models.Instance, error) {
	return nil, nil
}

func (r *stubInstanceRepository) GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error) {
	return nil, nil
}

func (r *stubInstanceRepository) GetAll(offset, limit int) ([]models.Instance, error) {
	if r.getAllErr != nil {
		return nil, r.getAllErr
	}
	items := make([]models.Instance, 0, len(r.instances))
	for _, instance := range r.instances {
		items = append(items, *cloneInstance(instance))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *stubInstanceRepository) CountAll() (int, error) {
	return len(r.instances), nil
}

func (r *stubInstanceRepository) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	items := make([]models.Instance, 0)
	for _, instance := range r.instances {
		if instance.UserID == userID {
			items = append(items, *cloneInstance(instance))
		}
	}
	return items, nil
}

func (r *stubInstanceRepository) CountByUserID(userID int) (int, error) {
	count := 0
	for _, instance := range r.instances {
		if instance.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (r *stubInstanceRepository) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	for _, instance := range r.instances {
		if instance.UserID == userID && instance.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *stubInstanceRepository) GetAllRunning() ([]models.Instance, error) {
	return r.GetAll(0, 100000)
}

func (r *stubInstanceRepository) Update(instance *models.Instance) error {
	r.instances[instance.ID] = cloneInstance(instance)
	return nil
}

func (r *stubInstanceRepository) Delete(id int) error {
	delete(r.instances, id)
	return nil
}

type stubInstanceCommandService struct {
	created []stubCreatedCommand
}

type stubCreatedCommand struct {
	instanceID int
	request    CreateInstanceCommandRequest
}

func (s *stubInstanceCommandService) Create(instanceID int, issuedBy *int, req CreateInstanceCommandRequest) (*InstanceCommandPayload, error) {
	s.created = append(s.created, stubCreatedCommand{instanceID: instanceID, request: req})
	now := time.Now().UTC()
	return &InstanceCommandPayload{
		ID:             len(s.created),
		CommandType:    req.CommandType,
		Payload:        req.Payload,
		Status:         instanceCommandStatusPending,
		IdempotencyKey: req.IdempotencyKey,
		IssuedBy:       issuedBy,
		IssuedAt:       now,
		TimeoutSeconds: req.TimeoutSeconds,
	}, nil
}

func (s *stubInstanceCommandService) GetNextForAgent(session *AgentSession) (*AgentCommandEnvelope, error) {
	return nil, nil
}

func (s *stubInstanceCommandService) MarkStarted(session *AgentSession, commandID int, startedAt *time.Time) error {
	return nil
}

func (s *stubInstanceCommandService) MarkFinished(session *AgentSession, commandID int, req AgentCommandFinishRequest) error {
	return nil
}

func (s *stubInstanceCommandService) ListByInstanceID(instanceID int, limit int) ([]InstanceCommandPayload, error) {
	return nil, nil
}

type stubObjectStorage struct {
	objects map[string][]byte
}

func newStubObjectStorage() *stubObjectStorage {
	return &stubObjectStorage{objects: map[string][]byte{}}
}

func (s *stubObjectStorage) PutObject(ctx context.Context, objectKey string, body []byte, contentType string) error {
	copyBody := make([]byte, len(body))
	copy(copyBody, body)
	s.objects[objectKey] = copyBody
	return nil
}

func (s *stubObjectStorage) GetObject(ctx context.Context, objectKey string) ([]byte, error) {
	body, ok := s.objects[objectKey]
	if !ok {
		return nil, fmt.Errorf("object %q not found", objectKey)
	}
	copyBody := make([]byte, len(body))
	copy(copyBody, body)
	return copyBody, nil
}

func newSkillArchive(t *testing.T, skillDir string) []byte {
	t.Helper()

	var body bytes.Buffer
	writer := zip.NewWriter(&body)

	readme, err := writer.Create(skillDir + "/README.md")
	if err != nil {
		t.Fatalf("Create README.md: %v", err)
	}
	if _, err := readme.Write([]byte("# hello skill\n")); err != nil {
		t.Fatalf("Write README.md: %v", err)
	}

	manifest, err := writer.Create(skillDir + "/manifest.json")
	if err != nil {
		t.Fatalf("Create manifest.json: %v", err)
	}
	if _, err := manifest.Write([]byte(`{"name":"hello-skill"}`)); err != nil {
		t.Fatalf("Write manifest.json: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Close zip writer: %v", err)
	}
	return body.Bytes()
}

func newMultipartFileHeader(t *testing.T, fileName string, content []byte) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(int64(len(content) + 1024)); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}

	files := req.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("expected one uploaded file, got %d", len(files))
	}
	return files[0]
}

func cloneSkill(item *models.Skill) *models.Skill {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func cloneBlob(item *models.SkillBlob) *models.SkillBlob {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func cloneVersion(item *models.SkillVersion) *models.SkillVersion {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func cloneInstanceSkill(item *models.InstanceSkill) *models.InstanceSkill {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func cloneScanResult(item *models.SkillScanResult) *models.SkillScanResult {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func cloneInstance(item *models.Instance) *models.Instance {
	if item == nil {
		return nil
	}
	copyItem := *item
	return &copyItem
}

func TestHashDirectoryPreservesSingleTopLevelSubdirectory(t *testing.T) {
	files := map[string][]byte{
		"src/main.py": []byte("print('hello')\n"),
	}

	got := hashDirectory(files)
	want := referenceSkillContentMD5(map[string][]byte{
		"src/main.py": []byte("print('hello')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory() = %s, want %s", got, want)
	}

	flattened := referenceSkillContentMD5(map[string][]byte{
		"main.py": []byte("print('hello')\n"),
	})
	if got == flattened {
		t.Fatalf("hashDirectory stripped the skill's internal src/ directory")
	}
}

func TestExtractSkillDirectoriesStripsArchiveRootOnlyOnce(t *testing.T) {
	archive := buildTestZip(t, map[string][]byte{
		"weather/src/main.py": []byte("print('weather')\n"),
	})

	dirs, err := extractSkillDirectories("weather.zip", archive)
	if err != nil {
		t.Fatalf("extractSkillDirectories() error = %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("extractSkillDirectories() returned %d dirs, want 1", len(dirs))
	}
	if _, ok := dirs[0].Files["src/main.py"]; !ok {
		t.Fatalf("expected skill files to preserve src/main.py after stripping archive root once: %#v", dirs[0].Files)
	}

	got := hashDirectory(dirs[0].Files)
	want := referenceSkillContentMD5(map[string][]byte{
		"src/main.py": []byte("print('weather')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory(extracted files) = %s, want %s", got, want)
	}
}

func TestFlattenSingleTopLevelDirForArchiveRoot(t *testing.T) {
	files := map[string][]byte{
		"weather/src/main.py": []byte("print('weather')\n"),
	}

	got := hashDirectory(flattenSingleTopLevelDir(files))
	want := referenceSkillContentMD5(map[string][]byte{
		"src/main.py": []byte("print('weather')\n"),
	})
	if got != want {
		t.Fatalf("hashDirectory(flattenSingleTopLevelDir(files)) = %s, want %s", got, want)
	}
}

func buildTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry, err := writer.Create(key)
		if err != nil {
			t.Fatalf("Create(%q): %v", key, err)
		}
		if _, err := entry.Write(files[key]); err != nil {
			t.Fatalf("Write(%q): %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	return buffer.Bytes()
}

func referenceSkillContentMD5(files map[string][]byte) string {
	entryKinds := map[string]string{}
	fileMap := map[string][]byte{}
	for key, body := range files {
		clean := path.Clean(key)
		if clean == "." || clean == "" {
			continue
		}
		fileMap[clean] = body
		entryKinds[clean] = "file"
		parts := splitTestPath(clean)
		for i := 1; i < len(parts); i++ {
			entryKinds[path.Join(parts[:i]...)] = "dir"
		}
	}

	keys := make([]string, 0, len(entryKinds))
	for key := range entryKinds {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := md5.New()
	for _, key := range keys {
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

func splitTestPath(value string) []string {
	result := []string{}
	for _, part := range bytes.Split([]byte(value), []byte("/")) {
		if len(part) > 0 {
			result = append(result, string(part))
		}
	}
	return result
}
