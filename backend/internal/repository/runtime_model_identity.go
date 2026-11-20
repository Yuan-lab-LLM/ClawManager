package repository

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// RuntimeModelResolver is additive: old repository implementations and runtime
// protocols remain usable. Alias resolution is never an authorization bypass.
type RuntimeModelResolver interface {
	ResolveRuntimeModel(string) (*models.LLMModel, error)
}

// RuntimeModelAliasCatalog is optional for legacy/in-memory repositories. The
// caller supplies authorized deployment IDs, never a provider's discovery list.
type RuntimeModelAliasCatalog interface {
	RuntimeModelAliases([]int) (map[int][]string, error)
}

func (r *llmModelRepository) RuntimeModelAliases(ids []int) (map[int][]string, error) {
	result := make(map[int][]string)
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		Alias   string `db:"alias"`
		ModelID int    `db:"model_id"`
	}
	if err := r.sess.Collection("llm_runtime_model_aliases").Find(db.Cond{"model_id": ids}).OrderBy("alias").All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ModelID] = append(result[row.ModelID], row.Alias)
	}
	return result, nil
}

func (r *userModelRepository) RuntimeModelAliases(ids []int) (map[int][]string, error) {
	reader, ok := r.LLMModelRepository.(RuntimeModelAliasCatalog)
	if !ok || len(ids) == 0 {
		return map[int][]string{}, nil
	}
	items, err := r.ListActive()
	if err != nil {
		return nil, err
	}
	requested := make(map[int]bool, len(ids))
	for _, id := range ids {
		requested[id] = true
	}
	allowed := make([]int, 0, len(items))
	for _, m := range items {
		if requested[m.ID] {
			allowed = append(allowed, m.ID)
		}
	}
	return reader.RuntimeModelAliases(allowed)
}

func newRuntimeModelID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return "cm-deploy-v1-" + hex.EncodeToString(entropy[:]), nil
}

func runtimeModelAliases(m *models.LLMModel) []string {
	return []string{strings.ToLower(strings.TrimSpace(m.DisplayName)), m.RuntimeModelID, models.RuntimeModelAlias(m.DisplayName)}
}

func reserveRuntimeModelAliases(sess db.Session, m *models.LLMModel) error {
	for _, alias := range runtimeModelAliases(m) {
		if alias == "" {
			continue
		}
		// Lock the unique alias even when two different model rows are being saved.
		if _, err := sess.SQL().Exec(`INSERT INTO llm_runtime_model_aliases (alias, model_id) VALUES (?, ?) ON DUPLICATE KEY UPDATE alias = alias`, alias, m.ID); err != nil {
			return err
		}
		row, err := sess.SQL().QueryRow(`SELECT model_id FROM llm_runtime_model_aliases WHERE alias = ? FOR UPDATE`, alias)
		if err != nil {
			return err
		}
		var owner int
		if err := row.Scan(&owner); err != nil {
			return err
		}
		if owner != m.ID {
			// A deleted deployment keeps ownership of its historical references,
			// but must not reserve a UI display name forever. The new deployment
			// is reachable by its own stable identity, never by the old alias.
			if alias != m.RuntimeModelID {
				deleted, err := runtimeModelOwnerDeleted(sess, owner)
				if err != nil {
					return err
				}
				if deleted {
					continue
				}
			}
			return errors.New("model name is reserved by another deployment's historical sessions; choose a different display name")
		}
	}
	return nil
}

func runtimeModelOwnerDeleted(sess db.Session, id int) (bool, error) {
	count, err := sess.Collection("llm_models").Find(db.Cond{"id": id}).Count()
	return count == 0, err
}

func (r *llmModelRepository) ResolveRuntimeModel(ref string) (*models.LLMModel, error) {
	id, found, err := r.resolveRuntimeModelID(ref)
	if err != nil || !found {
		return nil, err
	}
	m, err := r.GetByID(id)
	if err != nil {
		return nil, err
	}
	if m == nil || !m.IsActive {
		return nil, models.ErrModelUnavailable
	}
	return m, nil
}

// A client may send either a bare model ID or its persisted provider/model ref.
// Resolve the COMPLETE reference; stripping a provider could select a different
// deployment. A slash is also legal in a bare model name, so inspect both
// namespaces and reject an ambiguous match rather than choosing by precedence.
func (r *llmModelRepository) resolveRuntimeModelID(ref string) (int, bool, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	query := "SELECT model_id FROM llm_runtime_model_aliases WHERE alias = ?"
	args := []any{ref}
	if provider, model, qualified := strings.Cut(ref, "/"); qualified && provider != "" && model != "" {
		// One statement observes both namespaces in the same database snapshot.
		query += " UNION SELECT model_id FROM llm_runtime_provider_refs WHERE provider_id = ? AND model_ref = ?"
		args = append(args, provider, model)
	}
	rows, err := r.sess.SQL().Query(query, args...)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	id, found := 0, false
	for rows.Next() {
		var candidate int
		if err := rows.Scan(&candidate); err != nil {
			return 0, false, err
		}
		if found && candidate != id {
			return 0, false, models.ErrModelUnavailable
		}
		id, found = candidate, true
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	return id, found, nil
}

func (r *userModelRepository) ResolveRuntimeModel(ref string) (*models.LLMModel, error) {
	resolver, ok := r.LLMModelRepository.(RuntimeModelResolver)
	if !ok {
		return nil, nil
	}
	m, err := resolver.ResolveRuntimeModel(ref)
	if err != nil {
		return nil, err
	}
	if m != nil && (!m.IsActive || !m.VisibleTo(r.userID)) {
		return nil, models.ErrModelUnavailable
	}
	return m, nil
}
