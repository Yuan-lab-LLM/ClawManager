package services

import (
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
)

// ---------- stub repositories ----------

type stubBackupRepository struct {
	backups  map[int]*models.Backup
	nextID   int
	createFn func(*models.Backup) error
}

func newStubBackupRepo() *stubBackupRepository {
	return &stubBackupRepository{backups: make(map[int]*models.Backup), nextID: 1}
}

func (r *stubBackupRepository) Create(b *models.Backup) error {
	if r.createFn != nil {
		return r.createFn(b)
	}
	b.ID = r.nextID
	r.nextID++
	clone := *b
	r.backups[b.ID] = &clone
	return nil
}

func (r *stubBackupRepository) GetByID(id int) (*models.Backup, error) {
	b, ok := r.backups[id]
	if !ok {
		return nil, nil
	}
	clone := *b
	return &clone, nil
}

func (r *stubBackupRepository) ListByInstanceID(instanceID int) ([]models.Backup, error) {
	var result []models.Backup
	for _, b := range r.backups {
		if b.InstanceID == instanceID && b.Status != "deleted" {
			result = append(result, *b)
		}
	}
	return result, nil
}

func (r *stubBackupRepository) Update(b *models.Backup) error {
	clone := *b
	r.backups[b.ID] = &clone
	return nil
}

func (r *stubBackupRepository) Delete(id int) error {
	delete(r.backups, id)
	return nil
}

func (r *stubBackupRepository) CountByInstanceID(instanceID int) (int, error) {
	count := 0
	for _, b := range r.backups {
		if b.InstanceID == instanceID && b.Status != "deleted" {
			count++
		}
	}
	return count, nil
}

func (r *stubBackupRepository) ListExpired(now time.Time) ([]models.Backup, error) {
	var result []models.Backup
	for _, b := range r.backups {
		if b.Status == "completed" && b.ExpiresAt != nil && b.ExpiresAt.Before(now) {
			result = append(result, *b)
		}
	}
	return result, nil
}

func (r *stubBackupRepository) GetLatestScheduledBackup(instanceID int) (*models.Backup, error) {
	var latest *models.Backup
	for _, b := range r.backups {
		if b.InstanceID == instanceID && b.BackupType == "scheduled" && b.Status != "deleted" {
			if latest == nil || b.CreatedAt.After(latest.CreatedAt) {
				clone := *b
				latest = &clone
			}
		}
	}
	return latest, nil
}

type stubInstanceRepository struct {
	instances map[int]*models.Instance
}

func newStubInstanceRepo(instances ...*models.Instance) *stubInstanceRepository {
	m := make(map[int]*models.Instance)
	for _, inst := range instances {
		m[inst.ID] = inst
	}
	return &stubInstanceRepository{instances: m}
}

func (r *stubInstanceRepository) Create(i *models.Instance) error   { return nil }
func (r *stubInstanceRepository) GetAll(o, l int) ([]models.Instance, error) {
	return nil, nil
}
func (r *stubInstanceRepository) CountAll() (int, error) { return 0, nil }
func (r *stubInstanceRepository) GetByUserID(uid, o, l int) ([]models.Instance, error) {
	return nil, nil
}
func (r *stubInstanceRepository) CountByUserID(uid int) (int, error) { return 0, nil }
func (r *stubInstanceRepository) ExistsByUserIDAndName(uid int, name string) (bool, error) {
	return false, nil
}
func (r *stubInstanceRepository) GetAllRunning() ([]models.Instance, error) { return nil, nil }
func (r *stubInstanceRepository) Update(i *models.Instance) error           { return nil }
func (r *stubInstanceRepository) Delete(id int) error                       { return nil }
func (r *stubInstanceRepository) GetByAccessToken(t string) (*models.Instance, error) {
	return nil, nil
}
func (r *stubInstanceRepository) GetByAgentBootstrapToken(t string) (*models.Instance, error) {
	return nil, nil
}

func (r *stubInstanceRepository) GetByID(id int) (*models.Instance, error) {
	inst, ok := r.instances[id]
	if !ok {
		return nil, nil
	}
	clone := *inst
	return &clone, nil
}



// ---------- helper ----------

func newTestBackupService(instRepo *stubInstanceRepository, backupRepo *stubBackupRepository) *backupService {
	return &backupService{
		backupRepo:   backupRepo,
		instanceRepo: instRepo,
		pvcService:   nil, // K8s not needed for unit tests
	}
}

func testInstance(id, userID int, status string) *models.Instance {
	return &models.Instance{
		ID:     id,
		UserID: userID,
		Status: status,
		Name:   fmt.Sprintf("test-instance-%d", id),
	}
}

// ---------- tests ----------

func TestCreateBackupValidatesName(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		newStubBackupRepo(),
	)

	_, err := svc.CreateBackup(10, 1, "")
	if err == nil || err.Error() != "backup name is required" {
		t.Fatalf("expected 'backup name is required', got %v", err)
	}
}

func TestCreateBackupChecksOwnership(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		newStubBackupRepo(),
	)

	// User 99 does not own instance 1.
	_, err := svc.CreateBackup(99, 1, "my-backup")
	if err == nil || err.Error() != "access denied" {
		t.Fatalf("expected 'access denied', got %v", err)
	}
}

func TestCreateBackupInstanceNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(),
		newStubBackupRepo(),
	)

	_, err := svc.CreateBackup(10, 999, "my-backup")
	if err == nil || err.Error() != "instance not found" {
		t.Fatalf("expected 'instance not found', got %v", err)
	}
}

func TestCreateBackupEnforcesLimit(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		backupRepo,
	)

	// Fill up to the limit.
	for i := 0; i < maxBackupsPerInstance; i++ {
		backupRepo.backups[backupRepo.nextID] = &models.Backup{
			ID:         backupRepo.nextID,
			InstanceID: 1,
			Status:     backupStatusCompleted,
		}
		backupRepo.nextID++
	}

	_, err := svc.CreateBackup(10, 1, "one-too-many")
	if err == nil {
		t.Fatal("expected backup limit error, got nil")
	}
}

func TestCreateBackupSuccess(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		backupRepo,
	)

	backup, err := svc.CreateBackup(10, 1, "nightly-snapshot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup.ID == 0 {
		t.Fatal("expected non-zero backup ID")
	}
	if backup.BackupName != "nightly-snapshot" {
		t.Fatalf("expected name 'nightly-snapshot', got %q", backup.BackupName)
	}
	if backup.Status != backupStatusCreating {
		t.Fatalf("expected status 'creating', got %q", backup.Status)
	}
	if backup.BackupPath == nil {
		t.Fatal("expected backup path to be set")
	}
}

func TestListBackupsOwnership(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		newStubBackupRepo(),
	)

	_, err := svc.ListBackups(99, 1)
	if err == nil || err.Error() != "access denied" {
		t.Fatalf("expected 'access denied', got %v", err)
	}
}

func TestGetBackupNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		newStubBackupRepo(),
	)

	_, err := svc.GetBackup(10, 999)
	if err == nil || err.Error() != "backup not found" {
		t.Fatalf("expected 'backup not found', got %v", err)
	}
}

func TestGetBackupOwnership(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	backupRepo.backups[1] = &models.Backup{ID: 1, InstanceID: 1, Status: backupStatusCompleted}
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		backupRepo,
	)

	// User 99 should not be able to access backup belonging to user 10's instance.
	_, err := svc.GetBackup(99, 1)
	if err == nil || err.Error() != "access denied" {
		t.Fatalf("expected 'access denied', got %v", err)
	}

	// Owner should succeed.
	b, err := svc.GetBackup(10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.ID != 1 {
		t.Fatalf("expected backup ID 1, got %d", b.ID)
	}
}

func TestDeleteBackupSoftDeletes(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	path := "/tmp/clawreef/backups/instance-1/backup-1.tar.gz"
	backupRepo.backups[1] = &models.Backup{
		ID: 1, InstanceID: 1, Status: backupStatusCompleted, BackupPath: &path,
	}
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "stopped")),
		backupRepo,
	)

	if err := svc.DeleteBackup(10, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the backup is marked as deleted.
	b := backupRepo.backups[1]
	if b.Status != backupStatusDeleted {
		t.Fatalf("expected status 'deleted', got %q", b.Status)
	}
}

func TestRestoreRequiresCompletedBackup(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	backupRepo.backups[1] = &models.Backup{ID: 1, InstanceID: 1, Status: backupStatusCreating}
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "stopped")),
		backupRepo,
	)

	err := svc.RestoreBackup(10, 1)
	if err == nil || err.Error() != "only completed backups can be restored" {
		t.Fatalf("expected 'only completed backups can be restored', got %v", err)
	}
}

func TestRestoreRequiresStoppedInstance(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	backupRepo.backups[1] = &models.Backup{ID: 1, InstanceID: 1, Status: backupStatusCompleted}
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		backupRepo,
	)

	err := svc.RestoreBackup(10, 1)
	if err == nil || err.Error() != "instance must be stopped before restoring a backup" {
		t.Fatalf("expected instance-must-be-stopped error, got %v", err)
	}
}

func TestCreateBackupNameTooLong(t *testing.T) {
	t.Parallel()
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		newStubBackupRepo(),
	)

	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'a'
	}
	_, err := svc.CreateBackup(10, 1, string(longName))
	if err == nil || err.Error() != "backup name is too long" {
		t.Fatalf("expected 'backup name is too long', got %v", err)
	}
}

func TestMarkCompletedSkipsDeletedBackup(t *testing.T) {
	t.Parallel()
	backupRepo := newStubBackupRepo()
	backupRepo.backups[1] = &models.Backup{
		ID: 1, InstanceID: 1, Status: backupStatusDeleted,
	}
	svc := newTestBackupService(
		newStubInstanceRepo(testInstance(1, 10, "running")),
		backupRepo,
	)

	// Simulate the goroutine trying to mark a deleted backup as completed.
	svc.markBackupCompleted(&models.Backup{ID: 1})

	// Status should remain "deleted".
	b := backupRepo.backups[1]
	if b.Status != backupStatusDeleted {
		t.Fatalf("expected status to remain 'deleted', got %q", b.Status)
	}
}
