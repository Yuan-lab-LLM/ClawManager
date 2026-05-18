package repository

import (
	"fmt"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// BackupScheduleRepository defines storage operations for backup schedules.
type BackupScheduleRepository interface {
	Create(schedule *models.BackupSchedule) error
	GetByID(id int) (*models.BackupSchedule, error)
	ListByInstanceID(instanceID int) ([]models.BackupSchedule, error)
	Update(schedule *models.BackupSchedule) error
	Delete(id int) error
	ListAllActive() ([]models.BackupSchedule, error)
}

type backupScheduleRepository struct {
	sess db.Session
}

// NewBackupScheduleRepository creates a new backup schedule repository.
func NewBackupScheduleRepository(sess db.Session) BackupScheduleRepository {
	return &backupScheduleRepository{sess: sess}
}

func (r *backupScheduleRepository) Create(schedule *models.BackupSchedule) error {
	res, err := r.sess.Collection("backup_schedules").Insert(schedule)
	if err != nil {
		return fmt.Errorf("failed to create backup schedule: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		schedule.ID = int(id)
	}
	return nil
}

func (r *backupScheduleRepository) GetByID(id int) (*models.BackupSchedule, error) {
	var schedule models.BackupSchedule
	if err := r.sess.Collection("backup_schedules").Find(db.Cond{"id": id}).One(&schedule); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get backup schedule: %w", err)
	}
	return &schedule, nil
}

func (r *backupScheduleRepository) ListByInstanceID(instanceID int) ([]models.BackupSchedule, error) {
	var schedules []models.BackupSchedule
	if err := r.sess.Collection("backup_schedules").Find(db.Cond{
		"instance_id": instanceID,
	}).OrderBy("-created_at", "-id").All(&schedules); err != nil {
		return nil, fmt.Errorf("failed to list backup schedules: %w", err)
	}
	return schedules, nil
}

func (r *backupScheduleRepository) Update(schedule *models.BackupSchedule) error {
	if err := r.sess.Collection("backup_schedules").Find(db.Cond{"id": schedule.ID}).Update(schedule); err != nil {
		return fmt.Errorf("failed to update backup schedule: %w", err)
	}
	return nil
}

func (r *backupScheduleRepository) Delete(id int) error {
	if err := r.sess.Collection("backup_schedules").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete backup schedule: %w", err)
	}
	return nil
}

func (r *backupScheduleRepository) ListAllActive() ([]models.BackupSchedule, error) {
	var schedules []models.BackupSchedule
	if err := r.sess.Collection("backup_schedules").Find(db.Cond{
		"is_active": true,
	}).All(&schedules); err != nil {
		return nil, fmt.Errorf("failed to list active backup schedules: %w", err)
	}
	return schedules, nil
}

