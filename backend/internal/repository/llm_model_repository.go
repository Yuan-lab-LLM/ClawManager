package repository

import (
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

// NewLLMModelRepository creates a new model repository (schema is owned by embedded migrations).
func NewLLMModelRepository(sess db.Session) LLMModelRepository {
	repo := &llmModelRepository{sess: sess}
	return repo
}

func (r *llmModelRepository) readModels(cond db.Cond, order bool) ([]models.LLMModel, error) {
	var items []models.LLMModel
	// Read scope and grants in one snapshot. Never interpret a failed grant query as public.
	err := r.sess.Tx(func(sess db.Session) error {
		query := sess.Collection("llm_models").Find(cond)
		if order {
			query = query.OrderBy("-is_secure", "model_group", "deployment_priority", "display_name")
		}
		if err := query.All(&items); err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		ids := make([]int, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		var grants []struct {
			ModelID int `db:"model_id"`
			UserID  int `db:"user_id"`
		}
		if err := sess.Collection("llm_model_user_grants").Find(db.Cond{"model_id": ids}).All(&grants); err != nil {
			return err
		}
		byModel := make(map[int][]int)
		for _, grant := range grants {
			byModel[grant.ModelID] = append(byModel[grant.ModelID], grant.UserID)
		}
		for i := range items {
			items[i].AllowedUserIDs = byModel[items[i].ID]
			if items[i].AllowedUserIDs == nil {
				items[i].AllowedUserIDs = []int{}
			}
		}
		return nil
	})
	if err == nil {
		populateProviderModels(items)
	}
	return items, err
}

func (r *llmModelRepository) List() ([]models.LLMModel, error) {
	return r.readModels(db.Cond{}, true)
}
func (r *llmModelRepository) ListActive() ([]models.LLMModel, error) {
	return r.readModels(db.Cond{"is_active": true}, true)
}
func (r *llmModelRepository) GetByID(id int) (*models.LLMModel, error) {
	items, err := r.readModels(db.Cond{"id": id}, false)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}
func (r *llmModelRepository) GetByDisplayName(name string) (*models.LLMModel, error) {
	items, err := r.readModels(db.Cond{"display_name": name}, false)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func (r *llmModelRepository) Save(model *models.LLMModel) error {
	err := r.sess.Tx(func(sess db.Session) error {
		if model.ID != 0 {
			row, err := sess.SQL().QueryRow("SELECT visibility_scope, access_policy_version FROM llm_models WHERE id = ? FOR UPDATE", model.ID)
			if err != nil {
				return err
			}
			var scope string
			var version int
			if err := row.Scan(&scope, &version); err != nil {
				return err
			}
			if model.UpdateAccessPolicy {
				if model.AccessPolicyVersion != version {
					return models.ErrModelAccessConflict
				}
				model.AccessPolicyVersion = version + 1
			} else {
				// A legacy client editing price etc. cannot reset an ACL read earlier.
				model.VisibilityScope, model.AccessPolicyVersion = scope, version
				var grants []struct {
					UserID int `db:"user_id"`
				}
				if err := sess.Collection("llm_model_user_grants").Find(db.Cond{"model_id": model.ID}).All(&grants); err != nil {
					return err
				}
				model.AllowedUserIDs = []int{}
				for _, grant := range grants {
					model.AllowedUserIDs = append(model.AllowedUserIDs, grant.UserID)
				}
			}
		} else {
			model.AccessPolicyVersion = 1
			if model.VisibilityScope == "" {
				model.VisibilityScope = models.ModelVisibilityAll
			}
		}
		if model.UpdateAccessPolicy && model.VisibilityScope == models.ModelVisibilitySelected {
			if len(model.AllowedUserIDs) == 0 {
				return models.ErrInvalidModelAccess
			}
			count, err := sess.Collection("users").Find(db.Cond{"id": model.AllowedUserIDs, "is_active": true}).Count()
			if err != nil {
				return err
			}
			if int(count) != len(model.AllowedUserIDs) {
				return models.ErrInvalidModelAccess
			}
		}
		model.UpdatedAt = time.Now()
		if model.ID == 0 {
			model.CreatedAt = model.UpdatedAt
			res, err := sess.Collection("llm_models").Insert(model)
			if err != nil {
				return err
			}
			model.ID = int(res.ID().(int64))
		} else if err := sess.Collection("llm_models").Find(db.Cond{"id": model.ID}).Update(model); err != nil {
			return err
		}
		if model.UpdateAccessPolicy {
			if err := sess.Collection("llm_model_user_grants").Find(db.Cond{"model_id": model.ID}).Delete(); err != nil {
				return err
			}
			if model.VisibilityScope == models.ModelVisibilitySelected {
				for _, userID := range model.AllowedUserIDs {
					if _, err := sess.Collection("llm_model_user_grants").Insert(map[string]interface{}{"model_id": model.ID, "user_id": userID}); err != nil {
						return err
					}
				}
			}
		}
		return revokeUnauthorizedTasks(sess, model.ID)
	})
	if err == nil {
		publishModelAccessChange()
	}
	return err
}
func (r *llmModelRepository) Delete(id int) error {
	err := r.sess.Tx(func(sess db.Session) error {
		if err := sess.Collection("llm_models").Find(db.Cond{"id": id}).Delete(); err != nil {
			return err
		}
		return revokeUnauthorizedTasks(sess, id)
	})
	if err == nil {
		publishModelAccessChange()
	}
	return err
}

func populateProviderModels(items []models.LLMModel) {
	for index := range items {
		models.PopulateLLMProviderModels(&items[index])
	}
}
