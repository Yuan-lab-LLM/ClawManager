package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// BackupRepository defines storage operations for instance backups.
type BackupRepository interface {
	Create(backup *models.Backup) error
	GetByID(id int) (*models.Backup, error)
	ListByInstanceID(instanceID int) ([]models.Backup, error)
	Update(backup *models.Backup) error
	Delete(id int) error
	CountByInstanceID(instanceID int) (int, error)
	// ListExpired returns completed backups whose expires_at is before now.
	ListExpired(now time.Time) ([]models.Backup, error)
	// GetLatestScheduledBackup returns the most recent scheduled backup for an instance, or nil.
	GetLatestScheduledBackup(instanceID int) (*models.Backup, error)
}

type backupRepository struct {
	sess db.Session
}

// NewBackupRepository creates a new backup repository.
func NewBackupRepository(sess db.Session) BackupRepository {
	return &backupRepository{sess: sess}
}

func (r *backupRepository) Create(backup *models.Backup) error {
	res, err := r.sess.Collection("backups").Insert(backup)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		backup.ID = int(id)
	}
	return nil
}

func (r *backupRepository) GetByID(id int) (*models.Backup, error) {
	var backup models.Backup
	if err := r.sess.Collection("backups").Find(db.Cond{"id": id}).One(&backup); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get backup: %w", err)
	}
	return &backup, nil
}

func (r *backupRepository) ListByInstanceID(instanceID int) ([]models.Backup, error) {
	var backups []models.Backup
	if err := r.sess.Collection("backups").Find(db.Cond{
		"instance_id": instanceID,
		"status <>":   "deleted",
	}).OrderBy("-created_at", "-id").All(&backups); err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	return backups, nil
}

func (r *backupRepository) Update(backup *models.Backup) error {
	if err := r.sess.Collection("backups").Find(db.Cond{"id": backup.ID}).Update(backup); err != nil {
		return fmt.Errorf("failed to update backup: %w", err)
	}
	return nil
}

func (r *backupRepository) Delete(id int) error {
	if err := r.sess.Collection("backups").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}
	return nil
}

func (r *backupRepository) CountByInstanceID(instanceID int) (int, error) {
	count, err := r.sess.Collection("backups").Find(db.Cond{
		"instance_id": instanceID,
		"status <>":   "deleted",
	}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count backups: %w", err)
	}
	return int(count), nil
}

func (r *backupRepository) ListExpired(now time.Time) ([]models.Backup, error) {
	var backups []models.Backup
	if err := r.sess.Collection("backups").Find(db.Cond{
		"status":       "completed",
		"expires_at <": now,
	}).All(&backups); err != nil {
		return nil, fmt.Errorf("failed to list expired backups: %w", err)
	}
	return backups, nil
}

func (r *backupRepository) GetLatestScheduledBackup(instanceID int) (*models.Backup, error) {
	var backup models.Backup
	if err := r.sess.Collection("backups").Find(db.Cond{
		"instance_id": instanceID,
		"backup_type": "scheduled",
		"status <>":   "deleted",
	}).OrderBy("-created_at").Limit(1).One(&backup); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest scheduled backup: %w", err)
	}
	return &backup, nil
}
