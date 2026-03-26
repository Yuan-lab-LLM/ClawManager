package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// LLMModelRepository defines repository operations for admin-managed models.
type LLMModelRepository interface {
	List() ([]models.LLMModel, error)
	ListActive() ([]models.LLMModel, error)
	GetByID(id int) (*models.LLMModel, error)
	GetByDisplayName(displayName string) (*models.LLMModel, error)
	Save(model *models.LLMModel) error
	Delete(id int) error
}

type llmModelRepository struct {
	sess db.Session
}

// NewLLMModelRepository creates a new model repository and ensures its table exists.
func NewLLMModelRepository(sess db.Session) LLMModelRepository {
	repo := &llmModelRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *llmModelRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS llm_models (
  id INT AUTO_INCREMENT PRIMARY KEY,
  display_name VARCHAR(255) NOT NULL UNIQUE,
  description TEXT NULL,
  provider_type VARCHAR(100) NOT NULL,
  base_url VARCHAR(500) NOT NULL,
  provider_model_name VARCHAR(255) NOT NULL,
  api_key TEXT NULL,
  api_key_secret_ref VARCHAR(255) NULL,
  is_secure BOOLEAN NOT NULL DEFAULT FALSE,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  input_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  output_price DECIMAL(18,8) NOT NULL DEFAULT 0,
  currency VARCHAR(16) NOT NULL DEFAULT 'USD',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_llm_models_provider_type (provider_type),
  INDEX idx_llm_models_is_active (is_active),
  INDEX idx_llm_models_is_secure (is_secure)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure llm_models table: %w", err))
	}
}

func (r *llmModelRepository) List() ([]models.LLMModel, error) {
	var items []models.LLMModel
	if err := r.sess.Collection("llm_models").Find().OrderBy("-is_secure", "display_name").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list llm models: %w", err)
	}
	return items, nil
}

func (r *llmModelRepository) ListActive() ([]models.LLMModel, error) {
	var items []models.LLMModel
	if err := r.sess.Collection("llm_models").Find(db.Cond{"is_active": true}).OrderBy("-is_secure", "display_name").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list active llm models: %w", err)
	}
	return items, nil
}

func (r *llmModelRepository) GetByID(id int) (*models.LLMModel, error) {
	var item models.LLMModel
	err := r.sess.Collection("llm_models").Find(db.Cond{"id": id}).One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get llm model by id: %w", err)
	}
	return &item, nil
}

func (r *llmModelRepository) GetByDisplayName(displayName string) (*models.LLMModel, error) {
	var item models.LLMModel
	err := r.sess.Collection("llm_models").Find(db.Cond{"display_name": displayName}).One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get llm model by display name: %w", err)
	}
	return &item, nil
}

func (r *llmModelRepository) Save(model *models.LLMModel) error {
	now := time.Now()
	if model.ID == 0 {
		model.CreatedAt = now
		model.UpdatedAt = now
		res, err := r.sess.Collection("llm_models").Insert(model)
		if err != nil {
			return fmt.Errorf("failed to create llm model: %w", err)
		}
		model.ID = int(res.ID().(int64))
		return nil
	}

	model.UpdatedAt = now
	if err := r.sess.Collection("llm_models").Find(db.Cond{"id": model.ID}).Update(model); err != nil {
		return fmt.Errorf("failed to update llm model: %w", err)
	}
	return nil
}

func (r *llmModelRepository) Delete(id int) error {
	if err := r.sess.Collection("llm_models").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete llm model: %w", err)
	}
	return nil
}
