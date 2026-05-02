package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	backupStatusCreating  = "creating"
	backupStatusCompleted = "completed"
	backupStatusFailed    = "failed"
	backupStatusDeleted   = "deleted"

	backupTypeManual    = "manual"
	backupTypeScheduled = "scheduled"

	// backupBaseDir is the host directory where backup archives are stored.
	backupBaseDir = "/tmp/clawreef/backups"

	// backupJobImage is the container image used by backup/restore jobs.
	backupJobImage = "busybox:1.37"

	// backupJobTimeout is the maximum duration a backup job may run.
	backupJobTimeout int64 = 600 // 10 minutes

	// maxBackupsPerInstance limits the number of active backups per instance.
	maxBackupsPerInstance = 20
)

// BackupService defines the interface for instance backup operations.
type BackupService interface {
	CreateBackup(userID, instanceID int, name string) (*models.Backup, error)
	// CreateScheduledBackup is called by the scheduler. It skips user-ownership
	// checks, sets backup_type = "scheduled", and computes expires_at from
	// retentionDays. retentionDays must be >= 1.
	CreateScheduledBackup(instanceID int, name string, retentionDays int) (*models.Backup, error)
	ListBackups(userID, instanceID int) ([]models.Backup, error)
	GetBackup(userID, backupID int) (*models.Backup, error)
	DeleteBackup(userID, backupID int) error
	RestoreBackup(userID, backupID int) error
}

type backupService struct {
	backupRepo   repository.BackupRepository
	instanceRepo repository.InstanceRepository
	pvcService   *k8s.PVCService
}

// NewBackupService creates a new backup service.
func NewBackupService(
	backupRepo repository.BackupRepository,
	instanceRepo repository.InstanceRepository,
) BackupService {
	return &backupService{
		backupRepo:   backupRepo,
		instanceRepo: instanceRepo,
		pvcService:   k8s.NewPVCService(),
	}
}

// ---------- helpers ----------

// resolveOwnedInstance loads an instance and verifies ownership.
func (s *backupService) resolveOwnedInstance(userID, instanceID int) (*models.Instance, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if instance.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}
	return instance, nil
}

// backupDir returns the host directory that stores backups for an instance.
func backupDir(instanceID int) string {
	return filepath.Join(backupBaseDir, fmt.Sprintf("instance-%d", instanceID))
}

// backupFilePath returns the full host path for a backup archive.
func backupFilePath(instanceID, backupID int) string {
	return filepath.Join(backupDir(instanceID), fmt.Sprintf("backup-%d.tar.gz", backupID))
}

// instanceDataDir returns the HostPath directory for an instance's PVC data.
func instanceDataDir(userID, instanceID int) string {
	return fmt.Sprintf("/tmp/clawreef/user-%d/instance-%d", userID, instanceID)
}

// getBackupImage returns the container image for backup jobs, allowing override
// via the CLAWMANAGER_BACKUP_JOB_IMAGE environment variable.
func getBackupImage() string {
	if img := os.Getenv("CLAWMANAGER_BACKUP_JOB_IMAGE"); img != "" {
		return img
	}
	return backupJobImage
}

// ---------- CreateBackup ----------

func (s *backupService) CreateBackup(userID, instanceID int, name string) (*models.Backup, error) {
	if name == "" {
		return nil, fmt.Errorf("backup name is required")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("backup name is too long")
	}

	instance, err := s.resolveOwnedInstance(userID, instanceID)
	if err != nil {
		return nil, err
	}

	// Enforce per-instance backup limit.
	count, err := s.backupRepo.CountByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if count >= maxBackupsPerInstance {
		return nil, fmt.Errorf("backup limit reached: maximum %d backups per instance", maxBackupsPerInstance)
	}

	now := time.Now()
	backup := &models.Backup{
		InstanceID: instanceID,
		BackupName: name,
		Status:     backupStatusCreating,
		BackupType: backupTypeManual,
		CreatedAt:  now,
	}

	if err := s.backupRepo.Create(backup); err != nil {
		return nil, err
	}

	// Compute paths.
	archivePath := backupFilePath(instanceID, backup.ID)
	backup.BackupPath = &archivePath

	if err := s.backupRepo.Update(backup); err != nil {
		return nil, err
	}

	// Launch the backup job asynchronously.
	go s.runBackupJob(instance, backup)

	return backup, nil
}

// ---------- runBackupJob ----------

// runBackupJob creates a K8s Job that archives the instance data directory.
// It updates the backup record on completion or failure.
func (s *backupService) runBackupJob(instance *models.Instance, backup *models.Backup) {
	if s.pvcService == nil {
		s.markBackupFailed(backup, "k8s pvc service not initialized")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(backupJobTimeout)*time.Second)
	defer cancel()

	client := s.pvcService.GetClient()
	if client == nil {
		s.markBackupFailed(backup, "k8s client not initialized")
		return
	}

	namespace := client.GetSystemNamespace()
	jobName := fmt.Sprintf("backup-%d-%d", backup.InstanceID, backup.ID)
	srcDir := instanceDataDir(instance.UserID, instance.ID)
	dstDir := backupDir(instance.ID)
	archiveName := fmt.Sprintf("backup-%d.tar.gz", backup.ID)

	ttl := int32(300) // auto-cleanup finished jobs after 5 min
	backoffLimit := int32(0)
	timeout := backupJobTimeout

	hostPathDir := corev1.HostPathDirectoryOrCreate
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "clawreef",
				"managed-by": "clawreef",
				"component":  "backup",
				"backup-id":  fmt.Sprintf("%d", backup.ID),
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &timeout,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "backup",
							Image: getBackupImage(),
							Command: []string{
								"sh", "-c",
								fmt.Sprintf("mkdir -p /backup-dest && tar czf /backup-dest/%s -C /instance-data .", archiveName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "instance-data", MountPath: "/instance-data", ReadOnly: true},
								{Name: "backup-dest", MountPath: "/backup-dest"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "instance-data",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: srcDir, Type: &hostPathDir},
							},
						},
						{
							Name: "backup-dest",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: dstDir, Type: &hostPathDir},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.Clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		s.markBackupFailed(backup, fmt.Sprintf("failed to create backup job: %v", err))
		return
	}

	fmt.Printf("[BackupService] Backup job %s created for backup %d\n", jobName, backup.ID)

	// Poll until the job completes or fails.
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.markBackupFailed(backup, "backup job timed out")
			return
		case <-ticker.C:
			j, err := client.Clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
			if err != nil {
				continue
			}
			if j.Status.Succeeded > 0 {
				s.markBackupCompleted(backup)
				fmt.Printf("[BackupService] Backup %d completed successfully\n", backup.ID)
				return
			}
			if j.Status.Failed > 0 {
				s.markBackupFailed(backup, "backup job failed")
				return
			}
		}
	}
}

func (s *backupService) markBackupCompleted(backup *models.Backup) {
	// Re-read from DB to avoid overwriting a concurrent soft-delete.
	current, err := s.backupRepo.GetByID(backup.ID)
	if err != nil || current == nil || current.Status == backupStatusDeleted {
		return
	}

	now := time.Now()
	current.Status = backupStatusCompleted
	current.CompletedAt = &now
	if err := s.backupRepo.Update(current); err != nil {
		fmt.Printf("[BackupService] Error marking backup %d as completed: %v\n", backup.ID, err)
	}
}

func (s *backupService) markBackupFailed(backup *models.Backup, reason string) {
	fmt.Printf("[BackupService] Backup %d failed: %s\n", backup.ID, reason)

	// Re-read from DB to avoid overwriting a concurrent soft-delete.
	current, err := s.backupRepo.GetByID(backup.ID)
	if err != nil || current == nil || current.Status == backupStatusDeleted {
		return
	}

	now := time.Now()
	current.Status = backupStatusFailed
	current.CompletedAt = &now
	if err := s.backupRepo.Update(current); err != nil {
		fmt.Printf("[BackupService] Error marking backup %d as failed: %v\n", backup.ID, err)
	}
}

// ---------- ListBackups ----------

func (s *backupService) ListBackups(userID, instanceID int) ([]models.Backup, error) {
	if _, err := s.resolveOwnedInstance(userID, instanceID); err != nil {
		return nil, err
	}
	return s.backupRepo.ListByInstanceID(instanceID)
}

// ---------- GetBackup ----------

func (s *backupService) GetBackup(userID, backupID int) (*models.Backup, error) {
	backup, err := s.backupRepo.GetByID(backupID)
	if err != nil {
		return nil, err
	}
	if backup == nil {
		return nil, fmt.Errorf("backup not found")
	}

	// Verify ownership through the instance.
	if _, err := s.resolveOwnedInstance(userID, backup.InstanceID); err != nil {
		return nil, err
	}
	return backup, nil
}

// ---------- DeleteBackup ----------

func (s *backupService) DeleteBackup(userID, backupID int) error {
	backup, err := s.backupRepo.GetByID(backupID)
	if err != nil {
		return err
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}

	instance, err := s.resolveOwnedInstance(userID, backup.InstanceID)
	if err != nil {
		return err
	}

	// Soft-delete: mark as deleted and remove the archive file via a K8s Job.
	backup.Status = backupStatusDeleted
	if err := s.backupRepo.Update(backup); err != nil {
		return err
	}

	// Best-effort cleanup of the archive file.
	if backup.BackupPath != nil {
		go s.runDeleteJob(instance, backup)
	}
	return nil
}

// ---------- RestoreBackup ----------

func (s *backupService) RestoreBackup(userID, backupID int) error {
	backup, err := s.backupRepo.GetByID(backupID)
	if err != nil {
		return err
	}
	if backup == nil {
		return fmt.Errorf("backup not found")
	}
	if backup.Status != backupStatusCompleted {
		return fmt.Errorf("only completed backups can be restored")
	}

	instance, err := s.resolveOwnedInstance(userID, backup.InstanceID)
	if err != nil {
		return err
	}

	// Safety: instance should be stopped before restoring.
	if instance.Status == "running" || instance.Status == "creating" {
		return fmt.Errorf("instance must be stopped before restoring a backup")
	}

	go s.runRestoreJob(instance, backup)
	return nil
}

// ---------- runRestoreJob ----------

func (s *backupService) runRestoreJob(instance *models.Instance, backup *models.Backup) {
	if s.pvcService == nil {
		fmt.Printf("[BackupService] Restore for backup %d skipped: pvc service not initialized\n", backup.ID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(backupJobTimeout)*time.Second)
	defer cancel()

	client := s.pvcService.GetClient()
	if client == nil {
		fmt.Printf("[BackupService] Restore for backup %d skipped: k8s client not initialized\n", backup.ID)
		return
	}

	namespace := client.GetSystemNamespace()
	jobName := fmt.Sprintf("restore-%d-%d", backup.InstanceID, backup.ID)
	dstDir := instanceDataDir(instance.UserID, instance.ID)
	srcDir := backupDir(instance.ID)
	archiveName := fmt.Sprintf("backup-%d.tar.gz", backup.ID)

	ttl := int32(300)
	backoffLimit := int32(0)
	timeout := backupJobTimeout
	hostPathDir := corev1.HostPathDirectoryOrCreate

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "clawreef",
				"managed-by": "clawreef",
				"component":  "restore",
				"backup-id":  fmt.Sprintf("%d", backup.ID),
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &timeout,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "restore",
							Image: getBackupImage(),
							Command: []string{
								"sh", "-c",
								fmt.Sprintf("rm -rf /instance-data/* && tar xzf /backup-src/%s -C /instance-data", archiveName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "instance-data", MountPath: "/instance-data"},
								{Name: "backup-src", MountPath: "/backup-src", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "instance-data",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: dstDir, Type: &hostPathDir},
							},
						},
						{
							Name: "backup-src",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: srcDir, Type: &hostPathDir},
							},
						},
					},
				},
			},
		},
	}

	if _, err := client.Clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		fmt.Printf("[BackupService] Failed to create restore job for backup %d: %v\n", backup.ID, err)
		return
	}
	fmt.Printf("[BackupService] Restore job %s created for backup %d\n", jobName, backup.ID)
}

// ---------- runDeleteJob ----------

// runDeleteJob creates a K8s Job that removes the backup archive file.
func (s *backupService) runDeleteJob(instance *models.Instance, backup *models.Backup) {
	if s.pvcService == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := s.pvcService.GetClient()
	if client == nil {
		return
	}

	namespace := client.GetSystemNamespace()
	jobName := fmt.Sprintf("backup-del-%d-%d", backup.InstanceID, backup.ID)
	dir := backupDir(instance.ID)
	archiveName := fmt.Sprintf("backup-%d.tar.gz", backup.ID)

	ttl := int32(120)
	backoffLimit := int32(0)
	timeout := int64(60)
	hostPathDir := corev1.HostPathDirectoryOrCreate

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":        "clawreef",
				"managed-by": "clawreef",
				"component":  "backup-cleanup",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &timeout,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "cleanup",
							Image: getBackupImage(),
							Command: []string{
								"sh", "-c",
								fmt.Sprintf("rm -f /backup-dir/%s", archiveName),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "backup-dir", MountPath: "/backup-dir"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "backup-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: dir, Type: &hostPathDir},
							},
						},
					},
				},
			},
		},
	}

	if _, err := client.Clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		fmt.Printf("[BackupService] Failed to create delete job for backup %d: %v\n", backup.ID, err)
	}
}

// ---------- CreateScheduledBackup ----------

// CreateScheduledBackup creates a backup on behalf of the scheduler.
// It does NOT check user ownership (the scheduler is a system actor).
// retentionDays must be >= 1; expires_at is set to now + retentionDays.
func (s *backupService) CreateScheduledBackup(instanceID int, name string, retentionDays int) (*models.Backup, error) {
	if name == "" {
		return nil, fmt.Errorf("backup name is required")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("backup name is too long")
	}
	if retentionDays < 1 {
		return nil, fmt.Errorf("retention days must be at least 1")
	}

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}

	// Enforce per-instance backup limit.
	count, err := s.backupRepo.CountByInstanceID(instanceID)
	if err != nil {
		return nil, err
	}
	if count >= maxBackupsPerInstance {
		return nil, fmt.Errorf("backup limit reached: maximum %d backups per instance", maxBackupsPerInstance)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(retentionDays) * 24 * time.Hour)
	backup := &models.Backup{
		InstanceID: instanceID,
		BackupName: name,
		Status:     backupStatusCreating,
		BackupType: backupTypeScheduled,
		CreatedAt:  now,
		ExpiresAt:  &expiresAt,
	}

	if err := s.backupRepo.Create(backup); err != nil {
		return nil, err
	}

	archivePath := backupFilePath(instanceID, backup.ID)
	backup.BackupPath = &archivePath

	if err := s.backupRepo.Update(backup); err != nil {
		return nil, err
	}

	go s.runBackupJob(instance, backup)

	return backup, nil
}
