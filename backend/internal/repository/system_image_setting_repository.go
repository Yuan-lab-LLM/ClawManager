package repository

import (
	"fmt"
	"strings"
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

// NewSystemImageSettingRepository creates a new repository and ensures the table exists.
func NewSystemImageSettingRepository(sess db.Session) SystemImageSettingRepository {
	repo := &systemImageSettingRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *systemImageSettingRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS system_image_settings (
  id INT AUTO_INCREMENT PRIMARY KEY,
  instance_type VARCHAR(50) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  image VARCHAR(500) NOT NULL,
  is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_instance_type (instance_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure system_image_settings table: %w", err))
	}

	r.ensureIsEnabledColumn()
	r.ensureInstanceTypeIsNotUnique()
	r.ensureInstanceTypeIndex()
}

func (r *systemImageSettingRepository) ensureIsEnabledColumn() {
	var count int
	row, err := r.sess.SQL().QueryRow(`
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = 'system_image_settings'
  AND column_name = 'is_enabled'
`)
	if err != nil {
		panic(fmt.Errorf("failed to inspect system_image_settings columns: %w", err))
	}
	if err := row.Scan(&count); err != nil {
		panic(fmt.Errorf("failed to scan system_image_settings column count: %w", err))
	}

	if count == 0 {
		if _, err := r.sess.SQL().Exec("ALTER TABLE system_image_settings ADD COLUMN is_enabled BOOLEAN NOT NULL DEFAULT TRUE"); err != nil {
			panic(fmt.Errorf("failed to ensure system_image_settings.is_enabled column: %w", err))
		}
	}
}

func (r *systemImageSettingRepository) ensureInstanceTypeIsNotUnique() {
	rows, err := r.sess.SQL().Query(`
SELECT DISTINCT INDEX_NAME
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'system_image_settings'
  AND column_name = 'instance_type'
  AND NON_UNIQUE = 0
  AND INDEX_NAME <> 'PRIMARY'
`)
	if err != nil {
		panic(fmt.Errorf("failed to inspect system_image_settings indexes: %w", err))
	}
	defer rows.Close()

	var indexNames []string
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			panic(fmt.Errorf("failed to scan system_image_settings unique index: %w", err))
		}
		indexNames = append(indexNames, indexName)
	}

	for _, indexName := range indexNames {
		escapedName := strings.ReplaceAll(indexName, "`", "``")
		statement := fmt.Sprintf("ALTER TABLE system_image_settings DROP INDEX `%s`", escapedName)
		if _, err := r.sess.SQL().Exec(statement); err != nil {
			panic(fmt.Errorf("failed to drop unique instance_type index %s: %w", indexName, err))
		}
	}
}

func (r *systemImageSettingRepository) ensureInstanceTypeIndex() {
	var count int
	row, err := r.sess.SQL().QueryRow(`
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'system_image_settings'
  AND index_name = 'idx_instance_type'
`)
	if err != nil {
		panic(fmt.Errorf("failed to inspect system_image_settings idx_instance_type index: %w", err))
	}
	if err := row.Scan(&count); err != nil {
		panic(fmt.Errorf("failed to scan system_image_settings idx_instance_type index count: %w", err))
	}

	if count == 0 {
		if _, err := r.sess.SQL().Exec("CREATE INDEX idx_instance_type ON system_image_settings (instance_type)"); err != nil {
			panic(fmt.Errorf("failed to create idx_instance_type index: %w", err))
		}
	}
}

func (r *systemImageSettingRepository) List() ([]models.SystemImageSetting, error) {
	var settings []models.SystemImageSetting
	if err := r.sess.Collection("system_image_settings").Find().OrderBy("instance_type", "-updated_at", "-id").All(&settings); err != nil {
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
