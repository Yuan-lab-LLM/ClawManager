package repository

import (
	"clawreef/internal/models"
	"errors"
	"github.com/upper/db/v4"
	"strings"
)

type RuntimeProviderRefCatalog interface {
	RuntimeProviderRefs([]int) (map[int][]models.RuntimeProviderRef, error)
}

func (r *llmModelRepository) RuntimeProviderRefs(ids []int) (map[int][]models.RuntimeProviderRef, error) {
	result := map[int][]models.RuntimeProviderRef{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		ProviderID string `db:"provider_id"`
		ModelRef   string `db:"model_ref"`
		ModelID    int    `db:"model_id"`
	}
	if err := r.sess.Collection("llm_runtime_provider_refs").Find(db.Cond{"model_id": ids}).OrderBy("provider_id", "model_ref").All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ModelID] = append(result[row.ModelID], models.RuntimeProviderRef{ProviderID: row.ProviderID, ModelID: row.ModelRef})
	}
	return result, nil
}

func (r *userModelRepository) RuntimeProviderRefs(ids []int) (map[int][]models.RuntimeProviderRef, error) {
	reader, ok := r.LLMModelRepository.(RuntimeProviderRefCatalog)
	if !ok || len(ids) == 0 {
		return map[int][]models.RuntimeProviderRef{}, nil
	}
	items, err := r.ListActive()
	if err != nil {
		return nil, err
	}
	wanted := map[int]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	allowed := []int{}
	for _, m := range items {
		if wanted[m.ID] {
			allowed = append(allowed, m.ID)
		}
	}
	return reader.RuntimeProviderRefs(allowed)
}

func reserveRuntimeProviderRef(sess db.Session, m *models.LLMModel) error {
	name := strings.TrimSpace(m.DisplayName)
	if name == "" {
		return nil
	}
	// CatalogProviderName is transient discovery metadata, not a persisted field.
	// Repository-backed instance injection uses the display-name-derived namespace.
	persisted := *m
	persisted.CatalogProviderName = ""
	provider := strings.ToLower(models.RuntimeCatalogProviderName(persisted))
	name = strings.ToLower(name)
	refs := []models.RuntimeProviderRef{{ProviderID: provider, ModelID: name}, models.RuntimeOpenClawProviderRef(persisted)}
	seen := map[models.RuntimeProviderRef]bool{}
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if _, err := sess.SQL().Exec(`INSERT INTO llm_runtime_provider_refs (provider_id,model_ref,model_id) VALUES (?,?,?) ON DUPLICATE KEY UPDATE model_id=model_id`, ref.ProviderID, ref.ModelID, m.ID); err != nil {
			return err
		}
		row, err := sess.SQL().QueryRow(`SELECT model_id FROM llm_runtime_provider_refs WHERE provider_id=? AND model_ref=? FOR UPDATE`, ref.ProviderID, ref.ModelID)
		if err != nil {
			return err
		}
		var id int
		if err := row.Scan(&id); err != nil {
			return err
		}
		if id != m.ID {
			deleted, err := runtimeModelOwnerDeleted(sess, id)
			if err != nil {
				return err
			}
			if deleted {
				continue
			}
			return errors.New("legacy provider/model reference is reserved by another deployment")
		}
	}
	return nil
}
