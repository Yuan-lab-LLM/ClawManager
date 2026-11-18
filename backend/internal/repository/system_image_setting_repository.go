package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// SystemImageSettingRepository defines repository operations for runtime image settings.
type SystemImageSettingRepository interface {
	List() ([]models.SystemImageSetting, error)
	GetByID(id int) (*models.SystemImageSetting, error)
	ListByInstanceType(instanceType string) ([]models.SystemImageSetting, error)
	Save(setting *models.SystemImageSetting) error
	DeleteByID(id int) error
	DeleteByInstanceType(instanceType string) error
}

type systemImageSettingRepository struct {
	sess db.Session
}

// NewSystemImageSettingRepository creates a new repository (schema is owned by embedded migrations).
func NewSystemImageSettingRepository(sess db.Session) SystemImageSettingRepository {
	repo := &systemImageSettingRepository{sess: sess}
	return repo
}

func (r *systemImageSettingRepository) List() ([]models.SystemImageSetting, error) {
	var settings []models.SystemImageSetting
	if err := r.sess.Collection("system_image_settings").Find().OrderBy("instance_type", "runtime_type", "-updated_at", "-id").All(&settings); err != nil {
		return nil, fmt.Errorf("failed to list system image settings: %w", err)
	}
	return settings, nil
}

func (r *systemImageSettingRepository) GetByID(id int) (*models.SystemImageSetting, error) {
	var setting models.SystemImageSetting
	err := r.sess.Collection("system_image_settings").Find(db.Cond{"id": id}).One(&setting)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get system image setting by id: %w", err)
	}
	return &setting, nil
}

func (r *systemImageSettingRepository) ListByInstanceType(instanceType string) ([]models.SystemImageSetting, error) {
	var settings []models.SystemImageSetting
	if err := r.sess.Collection("system_image_settings").Find(db.Cond{"instance_type": instanceType}).OrderBy("-updated_at", "-id").All(&settings); err != nil {
		return nil, fmt.Errorf("failed to list system image settings by instance type: %w", err)
	}
	return settings, nil
}

func (r *systemImageSettingRepository) Save(setting *models.SystemImageSetting) error {
	now := time.Now()
	if setting.ID > 0 {
		existing, err := r.GetByID(setting.ID)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("system image setting not found")
		}

		existing.InstanceType = setting.InstanceType
		existing.RuntimeType = setting.RuntimeType
		existing.RuntimeVariant = setting.RuntimeVariant
		existing.DisplayName = setting.DisplayName
		existing.Image = setting.Image
		existing.IsEnabled = setting.IsEnabled
		existing.UpdatedAt = now
		if err := r.sess.Collection("system_image_settings").Find(db.Cond{"id": existing.ID}).Update(existing); err != nil {
			return fmt.Errorf("failed to update system image setting: %w", err)
		}

		*setting = *existing
		return nil
	}

	setting.CreatedAt = now
	setting.UpdatedAt = now
	res, err := r.sess.Collection("system_image_settings").Insert(setting)
	if err != nil {
		return fmt.Errorf("failed to create system image setting: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		setting.ID = int(id)
	}
	return nil
}

func (r *systemImageSettingRepository) DeleteByID(id int) error {
	if err := r.sess.Collection("system_image_settings").Find(db.Cond{"id": id}).Delete(); err != nil {
		if err == db.ErrNoMoreRows {
			return nil
		}
		return fmt.Errorf("failed to delete system image setting by id: %w", err)
	}
	return nil
}

func (r *systemImageSettingRepository) DeleteByInstanceType(instanceType string) error {
	if err := r.sess.Collection("system_image_settings").Find(db.Cond{"instance_type": instanceType}).Delete(); err != nil {
		if err == db.ErrNoMoreRows {
			return nil
		}
		return fmt.Errorf("failed to delete system image settings by instance type: %w", err)
	}
	return nil
}
